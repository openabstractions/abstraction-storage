# abstraction.storage contract

Binds: `content.thrift`

`content.thrift` defines `abstraction.storage/content-reader@1`,
`content-writer@1`, `content-changes@1` and `inventory@1`: read, write,
observe and compose shared content by its canonical digest, while the
service keeps backend paths and file permissions private. It also defines
`inventory-source@1`, `manifests@1`, `holds@1` and `content-remover@1`,
covered in [Not built](#not-built). `storage.thrift`'s `Store`, `Local` and
`Writable` interfaces are the native provider building blocks these services
compose; a program selecting one of those directly owns its own filesystem
access, outside this contract's promises. [README.md](README.md) holds the
walkthrough: setup, a worked read, write and observe.

## Reading this page

The rule ids below, such as `STO-R1`, are what tests and refusals cite.
Rules are grouped by the service they belong to: reader, writer, changes,
inventory, then retention.

| letter | topic |
|---|---|
| R | content-reader@1 |
| W | content-writer@1 |
| O | content-changes@1 (observation) |
| I | inventory@1 |
| T | retention (`research/storage-classes/DECISION.md` §8) |

`A` (admission) and `E` (error and outcome) are reserved with one meaning in
every contract (S12) and are not separately declared here: every rule below
states its own admission and outcome inline. `X` (extension) is unused.

The key words MUST, MUST NOT, REQUIRED, SHALL, SHALL NOT, SHOULD, SHOULD NOT,
RECOMMENDED, NOT RECOMMENDED, MAY and OPTIONAL in this page are to be
interpreted as described in BCP 14 (RFC 2119, RFC 8174) when, and only when,
they appear in all capitals, as shown here.

This page renames two words in prose only, with no wire change: `resource`
(the reader's open handle) is `handle`; `scope` (the account-plus-program a
policy decision is about) is `subject`. Neither word waits on a later wire
version, so this page carries no prose-to-wire table.

## Reader

- **[STO-R1] Removed object.** The service MUST answer `not_found` on `Open`
  of a removed digest and MUST NOT change bytes under an already-open
  handle.
- **[STO-R2] Explicit policy required.** `Listen(endpoint, store, policy)`
  MUST require an explicit content policy; the service MUST NOT discover
  owner stores by itself.
- **[STO-R3] Subject from native evidence.** The receiving service MUST
  obtain native Program evidence and MUST require the service account. The
  account and observed executable path form the subject the policy decides
  on. This identity check MUST NOT by itself grant any permission to stored
  bytes.
- **[STO-R4] Policy checked before and on every read.** Policy MUST be
  evaluated with the native peer and requested digest before `Find` or file
  opening, and again before each `Read`. Policy MUST be concurrent-safe,
  bounded and context-aware. Release of an owned handle MUST remain
  available after policy revocation.
- **[STO-R5] Missing Local support.** A configured provider with no `Local`
  support MUST answer `unsupported`.
- **[STO-R6] Policy outage against refusal.** A failed policy lookup MUST
  report `unavailable`; an evaluated refusal MUST report `forbidden`.
  Neither outcome MUST expose content or substitute a cached permit. A later
  explicit read MAY retry a recovered policy service against the same live
  handle.
- **[STO-R7] Open returns an unverified handle.** `Open` MUST require
  canonical lowercase SHA-256 naming and MUST return an opaque random
  handle, the requested digest, the observed size and
  `verification=unverified`. `Find` discovers a name; it MUST NOT hash
  bytes. The caller MUST hash the completed bytes before treating them as
  the requested content.
- **[STO-R8] Handle lifetime and limits.** A handle MUST belong to one
  provider lifetime and one authenticated account/program. The service MUST
  hold at most 32 handles globally and eight per subject, at most 32 active
  framed connections, at most 1 MiB per control frame and 1..65536 bytes per
  read. A handle MUST expire after 30 idle seconds, swept within one second
  of expiry. A lookup in a new instance, after expiry or after release, MUST
  return `gap`. Another program MUST NOT read or release a known handle. A
  lost `Open` reply MAY retain its slot until expiry; a client MUST NOT
  retry it automatically.
- **[STO-R9] Read against exact offsets.** `Read` MUST compare the exact
  offset and total against the issued size and MUST report EOF only on
  successful data at that size.
- **[STO-R10] Mutation detection invalidates the handle.** The service MUST
  check regular file type before and after open, MUST retain the opened
  file identity, and MUST compare size and modification metadata around
  each read. Observed mutation MUST return `changed` and MUST invalidate
  the handle; the caller MUST discard previously read chunks when `changed`
  occurs. This is advisory mutation detection, not an immutable snapshot
  (see [Divergences](#divergences)); final hash verification remains the
  caller's own obligation.
- **[STO-R11] Close and shutdown.** Service `Close` MUST stop admission,
  cancel contexts and close the files behind open handles, and `Serve` MUST
  join its handlers before returning. Policy MUST run outside the global
  handle mutex.
- **[STO-R12] No private paths in errors.** An error returned over the
  protocol MUST NOT expose a private backing path.
- **[STO-R13] Caller-retained handles.** A handle is a caller-retained
  value; the client MUST maintain no private path cache.

Native `Store`/`Local` lack context parameters and remain trusted bounded
provider callbacks: this wrapper cannot forcibly cancel a blocking custom
native provider or filesystem operation. C++ service packaging is opt-in
(`ABSTRACTION_STORAGE_BUILD_SERVICE=ON`,
`ABSTRACTION_STORAGE_BUILD_LEGACY=OFF` for a service-only prefix); its
`abstraction_storage_content` package exports `abstraction::storage_content`
(protocol) and `abstraction::storage_client` (shared IPC), beside the native
package's own independent build.

## Writer

- **[STO-W1] Class at commit.** A committed object is `cache` until a hold
  names it; `Begin` MUST NOT carry a class. Lands with `holds@1` (see
  [Not built](#not-built)).
- **[STO-W2] Separate write policy.** `EnableWriter(policy, limit)` MUST
  register a write policy separate from the read policy; a read permit MUST
  grant no write. The configured provider MUST implement `Store`, `Local`
  and `Writable`. Incomplete writer configuration MUST refuse before any
  listener opens.
- **[STO-W3] Policy checked before every effect.** `Begin` MUST evaluate
  write policy before `Find`, `Place` or any file effect. A declared size
  above the configured limit MUST return `too_large` with the limit. `Place`
  MUST create only the provider's own staging file; `Find` MUST NOT report
  staged bytes, so a reader observes `not_found`.
- **[STO-W4] Append is bounded and ordered.** `Append` MUST accept
  1..65536 bytes at exactly the received offset, with policy rechecked
  before bytes are staged. Another offset MUST return `out_of_order` with
  the count received. Bytes beyond the declared size MUST return
  `too_large` and MUST stage nothing. A failed write MUST discard the
  upload.
- **[STO-W5] Commit verifies and publishes once.** `Commit` MUST recheck
  policy, MUST require every declared byte, MUST compare the running
  SHA-256 of accepted bytes with the digest, and MUST then call the
  provider's `Commit` exactly once. A reader MUST see either no object or
  the whole object; a mismatch MUST discard the upload. A committed result
  MUST report `evidence=hashed`.
- **[STO-W6] Identity resumes a live or committed upload.** A request
  identity is 16..128 characters from `A-Z a-z 0-9 _ -`, bound to the
  subject: the account and observed program. Repeating `Begin` with the
  same digest and size MUST resume the live upload at its received count,
  or MUST return the committed result. A different digest or size MUST
  return `conflict` with zero provider effects. Another live upload of the
  same digest MUST return `busy`. An existing provider naming match MUST
  return `present` with `evidence=named`, an unverified naming claim.
  Content addressing keeps one object per digest.
- **[STO-W7] Writer limits and expiry.** The service MUST hold at most 16
  uploads globally, four per subject and 256 request records. An upload
  MUST expire after 30 idle seconds; expiry, `Abort`, a mismatch and
  provider shutdown MUST remove staged bytes. `Abort` of an owned upload
  MUST remain available after write revocation.
- **[STO-W8] Request identities are durable.** `EnableWriter` MUST take a
  CAS `Store` and an absolute record path; `Begin` MUST record the identity
  before any provider lookup or staging effect, and MUST refuse with
  `unavailable` when that record cannot be written. A committed or
  `present` result MUST be recorded when it occurs. `Abort` and a mismatch
  MUST forget the identity. An expired upload, a failed append and provider
  shutdown MUST leave the identity unfinished: the same digest and size
  start a new upload, and a different digest or size returns `conflict`.
  Each identity MUST be retained for ten minutes after its last `Begin` or
  result, across provider restarts. The host MUST be the record file's only
  writer; a record file changed by another writer MUST fence this lifetime,
  so a later identity change returns `unavailable`. A malformed or oversized
  record file MUST refuse writer startup.
- **[STO-W9] Startup reconciles unfinished uploads.** Startup MUST restore
  unexpired identities and MUST remove the provider reservation of every
  unfinished identity whose digest is not findable. `Place` MUST choose a
  location without effects, so a crash leftover always belongs to a
  recorded digest.
- **[STO-W10] Commit's own crash window.** `Commit` MUST record the
  committed result before the provider publishes. When that record cannot
  be saved, `Commit` MUST return `unavailable` and MUST publish nothing.
  When publishing fails, the record MUST be demoted to unfinished. A crash
  between the save and the publish MUST leave a recorded result without
  content; startup MUST demote it, and the next `Begin` with that identity
  starts a new upload. A retry after a completed publish MUST read
  `committed` with `hashed` evidence whether or not any later save
  succeeds.

Go `client.NewWriter`, `NewRequestID` and `Writer.Begin/Append/Commit/Abort`
validate result invariants; `Writer.Write` resumes by identity, follows
`out_of_order` and returns `*OutcomeError` for other outcomes, never
aborting. C++ `storage::Writer` provides the same calls in snake_case and
throws `WriteOutcome` from `write`. Facade resolution uses
`Machine.ResolveStorageWriter` and C++ `facade::resolve_storage_writer`.
Python, Rust and JavaScript have generated writer codecs only; their writer
clients are planned and unproven.

## Changes

- **[STO-O1] Removal journaled.** The service MUST append `removed` once
  per removal, updating the known-object set in the same step. This covers
  both a deletion made outside the service and one the retention collector
  makes under `STO-T3`/`STO-T4`.
- **[STO-O2] One journal per provider lifetime.** `EnableChanges(observe,
  interval, capacity)` MUST register one journal per provider lifetime,
  keyed by a random epoch with a sequence increasing from zero. The journal
  MUST retain at most `capacity` recent changes (default 4096) and MUST
  keep no per-subscriber queue.
- **[STO-O3] Commits and listing produce entries.** A writer commit MUST
  append `added` when the provider publishes a new object. A provider
  implementing the optional native `storage.Lister` capability MUST be
  polled every `interval` (default one second) once changes are enabled:
  an object that appeared since the previous listing appends `added`, one
  that disappeared appends `removed`. A change made and undone between two
  polls MUST NOT be reported. A listing that fails or exceeds 65536 objects
  MUST make `Observe` and `List` `unavailable` until a later listing
  succeeds; no partial listing is treated as deletions.
- **[STO-O4] Observe advances by cursor.** `Observe(cursor, max_changes,
  wait_ms)` MUST check the observe policy on every call. An empty cursor
  MUST start at the current journal end. A cursor names the epoch and the
  last examined sequence; a cursor from another epoch or older than the
  retained journal MUST return `gap`, recovered only by `List`, never by
  silently skipping changes. Every entry MUST be filtered through the read
  policy for its digest: an entry the caller may not read is omitted
  without a count, and the returned cursor still advances past it. A read
  decision outage MUST return `unavailable` and MUST NOT advance. At the
  current end, a call MUST wait at most `wait_ms` for an append, reread
  once, and recheck the observe policy before returning changes. At most 16
  calls MAY wait at once; a further call MUST return `unavailable`. Caller
  disconnection or cancellation MUST end only the wait.
- **[STO-O5] List freezes a snapshot.** `List(continuation, limit)` MUST
  build its initial state without racing observation. An empty continuation
  MUST freeze a snapshot of the known objects together with the change
  cursor at that moment; every page of that snapshot carries the same
  cursor, and observing from it reports every later change. Pages MUST be
  in digest order and filtered by the read policy. The service MUST retain
  at most eight snapshots, each for 30 idle seconds, bound to the caller
  subject; an unknown, expired or restarted snapshot MUST return `gap`, and
  another subject's continuation MUST return `forbidden`.
- **[STO-O6] Restart starts a new epoch.** A provider restart MUST start a
  new epoch: every earlier cursor MUST return `gap`, and the new snapshot
  lists present objects without journaling them. A change notice carries an
  unverified digest naming key and grants no access; the caller MUST still
  `Open` and verify content.

Go `client.Changes` (`Observe`, `List`, `Snapshot`) and C++
`storage::Changes` validate page shapes. Facade resolution uses
`Machine.ResolveStorageChanges` and C++ `facade::resolve_storage_changes`.

## Inventory

- **[STO-I1] Sources are designated, not discovered.** `EnableInventory(gate,
  sources)` composes only the `sources` designated at call time; the
  composition MUST discover none by itself. Each source carries its bound
  program and the store names the runtime accepted under
  `abstraction.storage/inventory.provide`. A record of any other store MUST
  be dropped: acceptance is per store, and a source reporting one it was not
  given reports nothing here.
- **[STO-I2] The composition owns freezing and rewriting.** Everything on a
  page besides freezing, paging, store acceptance and the holder rewrite is
  what a source supplied, unchanged. An empty continuation MUST drain every
  source's `Snapshot` once and freeze the result with the change cursor of
  that moment; pages are the frozen records in source order, then store and
  id order. The service MUST retain at most eight compositions, each for 30
  idle seconds and bound to the caller subject; an unknown or expired one
  MUST return `gap`, another subject's MUST return `forbidden`.
- **[STO-I3] Page bound and outage.** `limit` counts manifests, stray
  objects and dangling references together, so a page is bounded whatever
  mix it carries. One composition MUST hold at most 65536 records; a larger
  one is `unavailable`. The gate MUST be checked before any source is read
  and again before a page returns.
- **[STO-I4] Records filtered by digest.** Each record MUST be filtered
  through the read policy for the digests it carries: a record whose every
  digest is permitted is included, one with a refused digest is omitted
  without a count, and a read decision outage returns `unavailable`. A
  record carrying no digest MUST be included: local stores publish a digest
  only where the program's own index names one, and filtering those out
  would empty the inventory rather than protect it.
- **[STO-I5] Holds are observations, rewritten.** Every hold a source
  reports is an observation: one whose attestation is not `observed`, or
  which carries no basis, MUST NOT be carried. `established_by` MUST be
  rewritten from the bound source program on every hold and on every
  dangling reference's holder; no other value is honored.
- **[STO-I6] A source failure refuses the whole composition.** A source
  that does not answer, or answers anything but a page, MUST refuse the
  whole composition with `unavailable`: an inventory that silently dropped
  a designated source would report an absence it cannot see. A store the
  source could not read entirely is different: its errors travel on its own
  `Store` record and the page still carries everything else, as the source
  profile states.
- **[STO-I7] Holders and Find observe live.** `cursor` is the content-changes
  cursor the composition was frozen at, empty when the host serves no
  change journal. `Holders` MUST re-read one target through each source's
  `Verify` and MUST return those observations as `verified`; nothing is
  served from a frozen composition. `Find` MUST compose once at call time
  and MUST return one page.
- **[STO-I8] Unheld needs two agreeing observations.** This composition
  keeps no declared-hold table and no observation history, so `Unheld` MUST
  return `unavailable` rather than report content as unheld on one reading.
  `grace_ms` and `audit_retention_ms` MUST be zero on every page.

Cross-account content grants, public release pins, remote transports and
macOS native Program proof remain separate obligations. Generated inference
output uses bounded, original-subject publication grants on `output:speech`
and `output:live` under `abstraction.storage/content.write`: the runtime
preflights the grant before upstream work and returns a single-use internal
commit capability, and `Commit` rechecks the same resource for revocation,
verifies digest and size, then commits through the shared content store.
These grants authorize that generated output; digest-scoped uploads and
content reads retain their own policy checks.

## Retention

`research/storage-classes/DECISION.md` §8. Subject letter `T` is added to
S12 in the same commit that adds these ids.

- **[STO-T1] Four classes.** Four storage classes: `data` (until its owner
  removes it), `cache` (while free space allows, recreatable from a source
  the runtime can name), `tmp` (while its kind's own lifetime rule stands,
  then `tmp_max_retention`) and `logs` (under `logs_max_use`, oldest first).
- **[STO-T2] Class by kind.** Each module's contract MUST state the class of
  every kind it keeps; no object carries a class.
- **[STO-T3] Order.** The collector MUST remove in the order `tmp`, `cache`,
  `logs` and MUST NOT touch `data`.
- **[STO-T4] Cache candidates.** The collector MUST offer a `cache` object
  only when no unexpired declared hold names it or a manifest containing
  it, every observed hold re-reads absent, and no open handle or live
  upload names it, ordered by the runtime's last-`Open` stamp, and MUST NOT
  remove one without a person's confirmation.
- **[STO-T5] Cadence.** The collector MUST run daily and under low free
  space (15% of the file system, capped at 4G).
- **[STO-T6] Record.** Each removal MUST be one log record naming class,
  key and level.

`abstraction-logging/CONTRACT.md` `LOG-S14`, `abstraction-config/CONTRACT.md`
`CFG-R4` and `CFG-S2`, and `abstraction-download/CONTRACT.md` `DL-R37`
complete this design outside this contract's own module.

## Outcomes

| service | call | outcomes |
|---|---|---|
| content-reader@1 | `Open` | `opened`, `not_found`, `forbidden`, `invalid`, `unsupported`, `unavailable`, `exhausted` |
| content-reader@1 | `Read` | `data`, `gap`, `forbidden`, `invalid`, `unavailable`, `changed` |
| content-reader@1 | `Close` | `closed`, `gap`, `forbidden` |
| content-writer@1 | `Begin` | `started`, `committed`, `present`, `forbidden`, `invalid`, `conflict`, `too_large`, `busy`, `unsupported`, `unavailable`, `exhausted` |
| content-writer@1 | `Append` | `accepted`, `gap`, `forbidden`, `invalid`, `out_of_order`, `too_large`, `unavailable` |
| content-writer@1 | `Commit` | `committed`, `gap`, `forbidden`, `incomplete`, `mismatch`, `unavailable` |
| content-writer@1 | `Abort` | `aborted`, `gap`, `forbidden` |
| content-changes@1 | `Observe`/`Snapshot` | `page`, `gap`, `forbidden`, `invalid`, `unavailable` |
| inventory@1 | `List`/`Holders`/`Unheld`/`Find` | `page`, `gap`, `forbidden`, `invalid`, `unavailable` |

`evidence`: `hashed`, `named`, `foreign`, `none`. `verification`:
`unverified`. `ChangeKind`/`SourceChangeKind`: `added`, `removed` (and, on a
source, `held`, `released`, `store_changed`).

## Lending profile

[`lend.thrift`](lend.thrift) owns `abstraction.storage/lend@1` and its generated
types. A provider lends an inventory object to a registered model server and
keeps a reversible placement ledger. Applications reach it through the runtime,
which applies caller rights. Providers admit designated runtime programs.
The initial implementation and placement guarantees are documented in
[modelbridge's contract](https://github.com/openabstractions/abstraction-provider-modelbridge/blob/main/CONTRACT.md).
An independent provider implements the storage API without importing modelbridge.

## Bounds

Reader: 32 handles globally, eight per subject, 32 active framed
connections, 1 MiB control frames, 1..65536 bytes per read, 30 idle seconds
to expiry, a one-second sweep. Writer: 16 uploads globally, four per
subject, 256 request records, 1..65536 bytes per `Append`, 30 idle seconds
to expiry, identities retained ten minutes past their last `Begin` or
result. Changes: capacity default 4096 entries, interval default one
second, at most 16 concurrent `Observe` waiters, `wait_ms` 0..30000. List:
at most eight snapshots, each 30 idle seconds. Inventory: at most 65536
records per composition, at most eight compositions, each 30 idle seconds.
Retention: low free space is 15% of the file system capped at 4G; the
collector runs daily and under that pressure; `tmp_max_retention` defaults
`10d`; `logs_max_use` defaults 10% of the file system capped at 4G.

## Divergences

- **STO-R10.** Mutation detection is advisory, not an immutable snapshot:
  same-size writes with restored metadata may evade it. A content-addressed
  store that guarantees immutability by construction is the usual pattern;
  this service departs from it and leaves final hash verification to the
  caller.
- **STO-R7.** A digest is an identifier this service returns, not proof of
  the bytes behind it: a caller that requires byte integrity verifies the
  assembled digest itself, rather than trusting the name the way a fully
  content-addressed store's own guarantee would let it.
- **STO-T4.** Android's own `cacheDir` deletes by age with no in-use
  exemption and tells the app to check existence before every read
  (`research/storage-classes/PRIOR-ART.md` §11). This collector never
  removes a `cache` object an open handle or live upload names; Android's
  guidance does not carry over.

## Not built

`Manifests` (`abstraction.storage/manifests@1`, `Describe`/`Publish`),
`Holds` (`abstraction.storage/holds@1`, `Hold`/`Release`/`Renew`) and
`ContentRemover` (`abstraction.storage/content-remover@1`, `Remove`) have a
generated codec and wire-level client in every language and no server, no
facade resolve method and no rights catalogue action. `Holds` is `STO-W1`'s
own dependency: until it exists, no committed object is a removal candidate
by class, because a class declared at `Begin` would be an unverifiable
claim. `InventorySource` is served by the sibling layer
`abstraction-storage-over-local-stores`; see its own
[CONTRACT.md](https://github.com/openabstractions/abstraction-storage-over-local-stores/blob/main/CONTRACT.md).
