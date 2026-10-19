# abstraction.storage contract

Remote profile: the current service methods retain `local` (explicit or
default); the general paired listener does not offer this capability. Retained
capability-specific delegation keeps its existing contract and is separate
from that listener. Held by: `idl/gen/service_test.go`
(`TestDispatcherTableCarriesRemoteProfile`) and `serve/runtime_remote_test.go`
(`TestRemoteRuntimeDescribeUsesLiveGrantAndExplicitServices`).

Binds: `content.thrift`

`content.thrift` defines [`abstraction.storage/content-reader@1`](https://openabstractions.github.io/reference.html#concept-storage.content-reader-service),
[`content-writer@1`](https://openabstractions.github.io/reference.html#concept-storage.content-writer-service), `content-changes@1` and `inventory@1`: read, write,
observe and compose shared content by its canonical digest, while the
service keeps backend paths and file permissions private. It also defines
`inventory-source@1`, `manifests@1`, `holds@1` and `content-remover@1`,
covered in [Not built](#not-built). `storage.thrift`'s `Store`, `Local` and
`Writable` interfaces are the native provider building blocks these services
compose; a program selecting one of those directly owns its own filesystem
access, outside this contract's promises. [README.md](README.md) holds the
walkthrough: setup, a worked read, write and observe.

## Reading this page

The conventions every OA contract carries are stated once in [CONVENTIONS.md](https://github.com/openabstractions/abstractions/blob/main/idl/CONVENTIONS.md); a rule here that applies one cites its `CONV-` id.

The rule ids below, such as `STO-R1`, are what tests and refusals cite.
Rules are grouped by the service they belong to: reader, writer, changes,
inventory, then retention and locations.

| letter | topic |
|---|---|
| R | content-reader@1 |
| W | content-writer@1 |
| O | content-changes@1 (observation) |
| I | inventory@1 |
| T | retention (`research/storage-classes/DECISION.md` §8) |
| L | locations: where OA keeps files on a device (the `layout` package) |
| D | content-transfer@1: fill, proof, seal and the mapping kind (not built) |

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
  obtain native [Program](https://openabstractions.github.io/reference.html#concept-identity.listener-program-need) evidence and MUST require the service account. The
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

Native Go `Store`/`Local` lack context parameters and remain trusted bounded
provider callbacks: this wrapper cannot forcibly cancel a blocking custom
native provider or filesystem operation. C++ packaging defaults to the service
client (`ABSTRACTION_STORAGE_BUILD_SERVICE=ON`,
`ABSTRACTION_STORAGE_BUILD_LEGACY=OFF`); its
`abstraction_storage_content` package exports `abstraction::storage_content`
(protocol) and `abstraction::storage_client` (shared IPC). C++ package 0.2.0
retires the separate direct provider, header, target and native package at an
explicit source-breaking minor boundary from 0.1.0. The CMake input
`ABSTRACTION_STORAGE_BUILD_LEGACY=ON` is refused with the service-client migration
direction; explicit OFF input is accepted. The versioned content package uses
SameMinorVersion compatibility.
Old immutable sources/releases retain their API. Service failure remains a
service/transport refusal, with content state held by its Go owner.

<a id="writer"></a>

## [ContentWriter](https://openabstractions.github.io/reference.html#concept-storage.content-writer-service)

- **[STO-W1] Class at commit.** A committed object is `cache` until a hold
  names it; `Begin` MUST NOT carry a class. Lands with `holds@1` (see
  [Not built](#not-built)). Request input written under an input profile is
  `tmp` under `STO-T7`.
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
  subject: the account and canonical native Program established by receiving
  `SubjectOf`. The observed executable remains an observation. Repeating `Begin` with the
  same digest and size MUST resume the live upload at its received count,
  or MUST return the committed result. A different digest or size MUST
  return `conflict` with zero provider effects. Another live upload of the
  same digest MUST return `busy`. An existing provider naming match MUST
  return `present` with `evidence=named`, an unverified naming claim.
  Content addressing keeps one object per digest. A write policy MAY permit
  `Begin` only on proof of the bytes (`ErrRequireBytes`); such a writer
  MUST NOT be answered `present`, nor a recorded `present` replayed for it.
  `Begin` answers `started` whether or not the object exists, and the
  upload stages and verifies every byte. For an existing object it stages
  beside the writer's records, and `Commit` publishes nothing new: it
  reports `committed` with `hashed` evidence, keeps the stored bytes and
  journals no `added`. Startup removes such staging left by a crash.
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
  New native records use the private `abstraction.storage/writer-records@2`
  profile and capture account kind, account, canonical Program and observed
  executable. Under the existing writer transaction, admission MUST check
  both the canonical request namespace and the exact current executable's
  earlier namespace before staging or publication. A matching earlier request
  retains its original stored value, upload and record identity. A changed intent
  or ambiguous distinct retained records MUST refuse before provider effects.
  Earlier `@1` records MUST remain readable under their original closed shape;
  they gain no inferred canonical provenance and MUST NOT transfer across
  package versions. New metadata counts toward the existing record-file bound.
  A native receiving site without canonical provenance MUST refuse instead of
  falling back to earlier admission. Trusted subject-less and remote
  composition retain their existing scope. Read and upload handles retain the
  selected original owner; incompatible old listing/inventory continuations
  MUST refuse explicitly rather than be reinterpreted. Epoch change cursors
  retain their existing per-call authorization and filtering.
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
- **[STO-W11] Each write decision names its call.** The writer MUST pass
  the write policy the call it decides: `Begin`, `Append` or `Commit`
  before verification, each with the declared size. After `Commit` has
  matched every byte to the digest, and before it records or publishes
  anything, the writer MUST decide the call once more as publication,
  carrying the first 512 verified bytes and whether the store already held
  the object. A refusal at publication MUST
  publish nothing and MUST leave the upload live until its idle expiry, as
  a refusal before verification does. A policy that ignores the call
  decides by digest alone. Go: `service.WriteCallFrom`, `WithWriteCall`.
- **[STO-W12] Request input under an input profile.** Content an
  application uploads as the input of a request is written under an input
  profile: `input:image` for an edit input, an edit mask, a vision image or
  a video's first frame (PNG, JPEG, GIF or WebP), and `input:audio` for
  audio to transcribe (WAV, MP3, MP4, WebM, Ogg or FLAC). An application
  holds a profile with both `abstraction.storage/content.write` and
  `abstraction.storage/content.read` on its resource. An exact-digest
  `content.write` rule MUST decide first; only its absence lets a profile
  decide, and an explicit denial or a policy outage MUST NOT fall back to a
  profile. Before verification, holding either profile admits the upload
  only on proof of the bytes (`STO-W6`): a profile writer learns nothing
  from presence and never gains a read it did not earn by supplying the
  bytes. At publication the verified bytes name the one profile that MUST
  be held; bytes that fit neither are refused. The runtime MUST durably
  record the original account, proven program, digest and profile before
  publication; a failed record refuses with `unavailable`. The record owns
  the object only when its own write published the bytes; a write that
  proved bytes the store already held owns nothing. The record authorizes
  only a read the runtime makes for a request the same subject starts that
  consumes the recorded profile (image input for an image edit, vision or
  video request, audio for transcription), while that subject holds
  `content.read` on that profile, rechecked on each access. A direct
  `content-reader@1` `Open` and every other subject gain nothing from it.
  The gateway window's own storage of uploaded bytes follows the same rule,
  deciding ownership under the store lock it publishes under. The grant is
  evaluated where the store lives, for the subject that machine binds: a
  delegated request carries only the digest to a remote runtime, which
  reads its own store under its own records (`abstraction-inference`
  INF-V3), and no binding or record crosses machines.

Go `client.NewWriter`, `NewRequestID` and `Writer.Begin/Append/Commit/Abort`
validate result invariants; `Writer.Write` resumes by identity, follows
`out_of_order` and returns `*OutcomeError` for other outcomes, never
aborting. C++ `storage::Writer` provides the same calls in snake_case and
throws `WriteOutcome` from `write`. Facade resolution uses
`Machine.ResolveStorageWriter` and C++ `facade::resolve_storage_writer`.
Python `Writer` resolves through `Machine().resolve_storage_writer` and Rust
through `resolve_storage_writer`; JavaScript has the generated writer codec
and protocol client only.

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

Go `client.Changes` (`Observe`, `List`, `Snapshot`), C++
`storage::Changes` and Python `Changes` validate page shapes. Facade
resolution uses `Machine.ResolveStorageChanges`, C++
`facade::resolve_storage_changes`, Python
`Machine().resolve_storage_changes` and Rust `resolve_storage_changes`. A host
serves `content-changes@1` when it calls `EnableChanges`; the
`openabstractions` runtime does not, and resolving it against the runtime
finds no server.

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
  what a source supplied, unchanged. An empty continuation for `List` MUST
  drain every source's `Snapshot` once; `ListStore` MUST drain only sources
  accepted for its exact store. Each freezes the result with the change
  cursor of that moment; pages are the frozen records in source order, then
  store and id order. The service MUST retain at most eight compositions, each for 30
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
  profile states. The runtime's own composition for its model catalogue and
  model host (`Compose`, never served to an application) reads every source
  at once and treats a source that does not answer the same way: each store
  the source was accepted for carries the error "the inventory source did
  not answer in time", the composition names the source incomplete, and the
  other sources' records stand. It refuses only when no source answers. A
  source that answers from an earlier observation while a newer one runs
  puts the error "answered from an earlier observation, taken at <RFC 3339
  time>" on each store it reports (`storage.StaleObservationError`). The
  runtime keeps what it last knew of a store unread either way for at most
  ten refreshes in a row, five minutes, then counts the store as read and
  empty until its source reads it again.
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
- **[STO-I9] Store-scoped List avoids unrelated sources.** `ListStore(store,
  continuation, limit)` MUST authorize before reading a source, then invoke
  only designated sources whose accepted store set includes the exact
  requested store. A store not designated MUST return `invalid` without
  invoking any source. Records and the first-page store record MUST be
  filtered to that exact store; dangling references are omitted because they
  do not identify a store. The frozen continuation MUST bind both caller and
  store. The global `List` behavior is unchanged.

Cross-account content grants, public release pins and remote transports
remain separate obligations. Current macOS source supplies Program proof
over XPC; no macOS transcript records it yet. Generated inference
output uses bounded, original-subject grants on `output:speech`,
`output:live`, `output:image` and `output:video` under both
`abstraction.storage/content.write` and `abstraction.storage/content.read`.
The runtime preflights both grants before upstream work and returns a
single-use internal commit capability. Commit rechecks both resources for
revocation, verifies digest and size, and commits through the shared content
store. It durably records the original account, proven program, digest and
output profile before publication; a failed provenance write withholds the
result. The record is a publication fact, not a copied right. On Open and
each Read, an exact-digest permit authorizes access, while an absent exact
rule permits a matching generated output only under its current profile read
grant. Explicit digest denial or policy outage never falls back to profile
permission. Digest-scoped uploads gain no read authority from a write.
Request input mirrors this on the input profiles of `STO-W12`.

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
- **[STO-T7] Request input is `tmp`.** An object a request input record
  owns (`STO-W12`) is `tmp`. Its lifetime rule holds it while any recorded
  writer's identity retention lasts (`STO-W8`, ten minutes after that
  writer's last write or a `Begin` naming the digest) and while a request
  or job that has not reached its terminal state names it
  (`abstraction-inference` INF-V4). When the rule ends the runtime MUST
  remove the record, and the object too unless generated-output provenance
  names it or an exact-digest write named it. An exact-digest write marks
  the digest whether or not request input has named it yet. Each sweep
  reads every holder once; a holder that cannot answer, a job whose input
  cannot be decoded included, keeps every object for that sweep. Each
  removal leaves one `STO-T6` record. A store that serves
  `content-changes@1` reports the removal at its next listing (`STO-O3`);
  the runtime's own store serves none. A request that starts after the
  decision may read the object as `not_found`; the application uploads it
  again. Until the collector exists, the runtime looks once a minute.
- **[STO-T8] Inventory class follows holds.** For retention presentation,
  an inventory item's hold with purpose `cache` maps to `cache`; another
  hold with lifetime `lease` maps to `tmp`; every other hold maps to `data`.
  The strongest hold wins, in order `data`, `tmp`, `cache`. Purpose remains
  an open catalogue: an unnamed purpose maps by its lifetime. An item with
  no holds is `cache` only under STO-T4's content-reference conditions;
  `Inventory.Unheld` supplies the grace-checked cleanup candidates.
  Classification supplies no removal authority and changes no hold lifetime.
  Logging owns the `logs` class and its retention. A Panel using this
  inventory reports the content store and logs; working folders and staging
  outside the store remain under their owning module's collection rules.

`abstraction-logging/CONTRACT.md` `LOG-S14`, `abstraction-config/CONTRACT.md`
`CFG-R4` and `CFG-S2`, and `abstraction-download/CONTRACT.md` `DL-R37`
complete this design outside this contract's own module.

## Locations

The owner decided where OA keeps files in sto-r2 (0.8.0 plan theme T9,
2026-10-19).
Subject letter `L` is added to S12 in the same commit that adds these ids.
The Go package `layout` in this module implements the section;
[`testdata/locations`](testdata/locations/README.md) holds a scenario per
rule for any implementation.

A root is named by its **location** and its class. The locations are
`machine` (one per device), `user` (one per account) and `isolated` (one
runtime started with an explicit root: `--state-dir`, `--isolated`, a test,
a container). The classes are STO-T1's four, `data`, `cache`, `tmp` and
`logs`, and runtime state is `data`. Four record kinds have folders of their
own: `config` (the `config` row), `secret` (the credentials in
`runtime-v1`), `endpoint` (the engine sockets row; a Windows named pipe has
no folder) and `component` (`engines`). The `config` module's `process` location is the environment
layer of settings precedence and has no folder. An application is a key inside OA's
stores and owns no folder in OA's roots; its own files stay in folders its
installer owns. An account has one data root under any number of
installations: the runtime reads the declarations of the installation that
started it.

Every OA folder is named `openabstractions`. Kinds whose contract names a
path-length limit live under the short root `oa` on Windows and in an
isolated root; a macOS or Linux user location has no short root. Installer-owned names
(Program Files, the registry, the Apps list) keep `OpenAbstractions`.

| root | Windows | macOS | Linux |
|---|---|---|---|
| `D` data | `%LOCALAPPDATA%\openabstractions` | `~/Library/Application Support/openabstractions` | `$XDG_DATA_HOME/openabstractions` |
| runtime state | `D\runtime-v1` | `D/runtime-v1` | `D/runtime-v1` |
| `config` | `D\config` | `D/config` | `$XDG_CONFIG_HOME/openabstractions` |
| settings file | `<config>\config.json` | `<config>/config.json` | `<config>/config.json` |
| cache | `D\cache` | `~/Library/Caches/openabstractions` | `$XDG_CACHE_HOME/openabstractions` |
| logs | `D\logs` | `~/Library/Logs/openabstractions` | `$XDG_STATE_HOME/log/openabstractions` |
| tmp | `%TEMP%\openabstractions` | `$TMPDIR/openabstractions` | `$TMPDIR/openabstractions-<uid>`, else `/var/tmp/openabstractions-<uid>` |
| short | `%LOCALAPPDATA%\oa` | none | none |
| engine sockets | `%LOCALAPPDATA%\oa\s` | `$TMPDIR/oa`, else `/tmp/oa-<uid>` when `$TMPDIR` is unset or not owner-only | `$XDG_RUNTIME_DIR/oa`, else `/tmp/oa-<uid>` when `$XDG_RUNTIME_DIR` is unset or not owner-only |

Windows reads `FOLDERID_LocalAppData` and `FOLDERID_ProgramData` through the
process token after the profile-view probe, and tmp from `GetTempPath2`,
which reads `TMP`, `TEMP` and `USERPROFILE`. `layout` reads none of the
`LOCALAPPDATA`, `APPDATA` and `ProgramData` variables itself. The
profile-view probe it runs (facade's `bootstrap.CurrentProfileView`) still
creates its probe file in `%LOCALAPPDATA%` as the variable names it, so a
redirected variable sends the probe to another folder; 0.8.0 plan ST-2
part 1 moves the probe to the known folder. A known folder or a temporary
folder that cannot be read, or is not absolute, refuses its location with
`no_profile_folder`; no root of that location is returned. On Windows an absolute path begins with a drive
letter, a colon and a separator, or with `\\` (a UNC path); `\x` and `C:x`
are relative.

macOS and Linux read `$HOME` and the XDG Base Directory variables. The user
location there needs an absolute `$HOME`, whatever the XDG variables hold;
the machine location reads no `$HOME`. An XDG variable, `$TMPDIR` or
`$XDG_RUNTIME_DIR` that is empty or relative counts as unset: an XDG
variable then takes the specification's default (`~/.local/share`,
`~/.config`, `~/.cache`, `~/.local/state`), and tmp and the engine socket
root take their fallbacks. On macOS without an absolute `$TMPDIR`, tmp is
`/var/tmp/openabstractions-<uid>` and the engine socket root
`/tmp/oa-<uid>`. The `user` location reads these variables only; the
owner-only test of a `$TMPDIR` or `$XDG_RUNTIME_DIR` folder (`STO-L9`) is
made when the folder is created.

Every root and every path `layout` returns is cleaned lexically: a doubled
or trailing separator, `.` and `..` are removed. Classifying a path names
the longest root its cleaned spelling begins with (`data`, `runtime`,
`config`, `cache`, `logs`, `tmp`, `short` or `sockets`; a child of `D` is
`data`), or none for a relative path. It compares spellings exactly, with no
case folded: whether an existing file lies in a root is asked of the file
system (identity's `Within`, `VISION.md` L623).

Children of `D`: `runtime-v1` (rights, asks, jobs, the content store,
credentials and the other runtime records), `provider-data/<provider>`,
`engines/<provider>/<version>`, `models/<set>`, `setup` (Setup journals and
receipts), `mcp-gateway` (gateway job handles), `install` (macOS and Linux
installer bookkeeping) and `upgrade-v1` (Windows, a frozen protocol path).
`runtime-v1` keeps its 0.4.0 location on every platform, and the CRED-B1
marker and every CAS `.lock` stay with their files.

Path-bound kinds:

| kind | name | Windows, and an isolated root | macOS and Linux user location | limit |
|---|---|---|---|---|
| engine sockets | `s` | `<short>/s` | the engine sockets root | the socket path `<folder>/<8 hex digits>/e.sock` at most 107 bytes (`sun_path`), every platform |
| engines a GPU library opens without long paths | `e` | `<short>/e/<provider>/<version>` | `D/engines/<provider>/<version>` | the folder at most 87 UTF-16 code units, on Windows |
| those providers' state | `p` | `<short>/p/<provider>` | `D/provider-data/<provider>` | the same |
| Setup's model stages for those engines | `m` | `<short>/m/...` | `D/models/...` | the same |

A path-bound path is its kind's folder joined with the caller's elements
(`<provider>`, `<version>`, ...), each one folder name. An element that is
empty or made only of dots, or holds a `/` (on Windows also `\` or `:`), is
refused with `outside_root`. The limit measures that path, for engine sockets with
the 16 bytes of `/<8 hex digits>/e.sock` added. A path at the limit fits; a
longer one is refused with `path_too_long` (`STO-L6`). Only a path-bound
path is measured: resolving a location's roots refuses no long root.

An isolated root `S` is the runtime state folder and the data root, and
every other root is a reserved child: `S/config` (settings file
`S/config/config.json`), `S/cache`, `S/logs`, `S/tmp`, `S/provider-data`,
`S/engines`, `S/models`, `S/setup`, `S/mcp-gateway` and the short root
`S/oa`. An isolated location has no `install` and no `upgrade-v1`.

| machine | Windows | macOS | Linux |
|---|---|---|---|
| `M` | `%ProgramData%\openabstractions` | `/Library/Application Support/openabstractions` | `/etc/openabstractions` |
| machine settings file | `M\config\config.json` | `M/config/config.json` | `/etc/openabstractions/config.json` |
| 0.4.0 machine settings file, read until moved | `%ProgramData%\abstraction\config.json` | `/etc/abstraction/config.json` | `/etc/abstraction/config.json` |
| machine service state, reserved | `M` | `M` | `/var/lib/openabstractions` |
| frozen `upgrade-v1` | `%ProgramData%\abstraction\upgrade-v1` | none | none |

The machine root holds administrator records only: the machine settings
file and installer records. The frozen `upgrade-v1` keeps its 0.4.0 folder
beside the 0.4.0 machine settings file. No secret is written at the machine
location. A machine service's state folder is named and reserved: no 0.8
process writes it. A machine service's shared content, cache, logs and
endpoints have no folder yet. On Windows the
elevated installer creates `M`, takes ownership for Administrators, replaces
its DACL with inheritance broken and removes children it did not create, or
fails the install naming the folder's owner. On macOS and Linux the machine
level exists when a root-owned machine root passes `STO-L4`, whoever created
it; administrators write it.

A person's folder (Pictures, Documents, Downloads, Music, Videos, Desktop)
is found for a caller that offers it as a destination. Windows reads the
known folders `FOLDERID_Pictures`, `FOLDERID_Documents`,
`FOLDERID_Downloads`, `FOLDERID_Music`, `FOLDERID_Videos` and
`FOLDERID_Desktop` through the process token. macOS takes `$HOME/Pictures`,
`Documents`, `Downloads`, `Music`, `Movies` (for videos) and `Desktop`.
Linux reads `user-dirs.dirs` in `$XDG_CONFIG_HOME` (default `~/.config`),
keys `XDG_PICTURES_DIR`, `XDG_DOCUMENTS_DIR`, `XDG_DOWNLOAD_DIR`,
`XDG_MUSIC_DIR`, `XDG_VIDEOS_DIR` and `XDG_DESKTOP_DIR`, each `"$HOME/..."`
or an absolute path; a missing file or key gives `$HOME/Pictures`,
`Documents`, `Downloads`, `Music`, `Videos` or `Desktop`. A folder that
cannot be read, is relative, or is `$HOME` itself (the specification's way
to switch one off) is refused with `no_profile_folder`. A person's folder
belongs to the person, and neither a sandbox nor a profile view refuses
it.

- **[STO-L1] Roots by location and class.** Every file at a location OA
  chooses MUST lie under one root named by its location and its class or
  record kind, at the place the tables above give for the platform. A
  location a caller or an operator names (a download destination, DL-R20
  and DL-R30; the NAS store, whose root reaches the runtime as a launch flag;
  a folder adopted in place; ComfyUI's output folder) lies outside this
  rule and follows its owning contract.
- **[STO-L2] One resolver.** An OA process MUST obtain a root from `layout`
  or from its supervisor's launch flags, and MUST NOT read profile folders,
  XDG variables or the temporary directory by other means. The runtime
  passes each provider it launches its roots as launch flags; roots never
  travel in environment variables. A provider computes roots through
  `layout` only when started by hand.
- **[STO-L3] Refusal, never substitution.** When a root cannot be resolved
  (no profile folder, a relative `$HOME`, a virtualized profile view, a
  profile view that cannot be read, an App Sandbox container, a Flatpak or a
  Snap), `layout` MUST refuse with a typed reason (`no_profile_folder`,
  `virtualized_profile`, `unknown_profile_view`, `sandboxed`) and MUST NOT
  substitute the temporary directory, the home folder or another location.
  The checks run in this order: a sandbox (`sandboxed`), a profile view
  that cannot be read (`unknown_profile_view`), a virtualized view
  (`virtualized_profile`), then a folder or variable that is missing or
  relative (`no_profile_folder`). Every call that resolves the user or the
  machine location refuses so: the user roots, the machine roots,
  classifying a path, and a path-bound path of the user location. An
  isolated root is named, so neither a sandbox nor a view refuses it.
- **[STO-L4] Machine trust.** A `machine` file MUST be read only when it and
  every folder from it up to the machine root are owned by, and writable
  only by, an administrator: SYSTEM, Administrators or TrustedInstaller on
  Windows, root on Unix. On Windows a write is a right to change or remove
  what is there (write or append data, delete, a folder's delete-child,
  change permissions or owner); a right to add a new entry to a folder is
  not one, since each file read is checked itself. Each entry is the object
  a reader opens, links followed; who may read it is not judged. This
  account is never an administrator, even in an elevated process. Every
  entry that exists is judged first, and one owned or writable by anyone
  other than an administrator gives `machine_untrusted`. Only then does a
  missing file or folder, or no file named, give `machine_absent`. Either
  reason leaves the machine level absent. Nothing falls back. The `config` module's
  provenance (`CFG-P1`) carries the reason, so an administrator sees why policy is
  absent.
- **[STO-L5] Places OA never writes.** No file or folder OA names is
  created as a direct child of the home folder, in the roaming profile
  (`%APPDATA%`), or in a person's folder (Pictures, Documents, Downloads,
  Music, Videos, Desktop) except at a destination a caller named. An agent
  picture tool's default destination, `OpenAbstractions` in the Pictures
  folder, is such a destination; the tool's own process writes it, and the
  runtime writes nothing in a person's folder (`backlog/PICTURES-STORAGE.md`
  ruling 2). Platform
  convention folders such as `~/.local/share` and `~/Library` hold OA's
  folders.
- **[STO-L6] Isolated root.** A runtime given an isolated root `S` MUST place
  every `user` kind under it and its short-root kinds under `S/oa`, MUST pass
  it to every provider it launches, and MUST NOT read or write the account's
  `user` roots or the machine level. A computed path over its kind's limit
  is refused with `path_too_long`; tests that start engines use a short `S`.
  Two runtimes never share one `S`.
- **[STO-L7] Backup marks.** The cache root, the `engines` folder of `D`
  and the short root's `e` folder (each the folder that holds every
  engine), and each model set
  its source can fetch again by digest MUST carry the platform's no-backup
  mark from its creation: `CACHEDIR.TAG` (https://bford.info/cachedir/) on
  every platform and the sticky Time Machine exclusion on macOS. A gated
  set, a withdrawn upload and a person's import carry none.
- **[STO-L8] Removal levels (pending the owner's D26).** Uninstall removes the
  installation and its registrations; an account's roots are removed only
  by that account's removal action, and the machine root only by an
  administrator. Under every D26 option, an account's removal leaves the
  installer's `install` and `upgrade-v1` children while an installation
  exists.
- **[STO-L9] Owner-only roots.** Every `user` and `isolated` root, and every
  child `layout` creates, MUST be created owner-only. An owner-only folder
  is a folder private to the account as identity `ID-D1` defines it, made
  and checked by its `privatedir` package: mode 0700 on POSIX; on Windows
  owned by the account with a protected DACL of the account and SYSTEM, set
  at creation. `layout` judges a root or child in this order: a relative
  path is refused (`not_absolute`); a missing folder is created; an
  existing one MUST be refused when it is a link or junction (`link`), not
  a folder (`not_directory`), owned by another account (`not_owner`), or
  writable by another account (`open_to_others`); one already owner-only
  that no other account can read is kept; any other MUST be tightened to
  that descriptor (`privatedir.Tighten`). Tightening writes the folder's
  own descriptor through a handle opened as the folder, and what the folder
  already holds keeps its descriptor, an `ID-D3` model folder included. On
  Windows a folder Administrators or SYSTEM own counts as the account's
  and is tightened, since only an administrator, such as the account's own
  elevated process, could have made it; tightening makes the account its
  owner. On macOS and Linux a root-owned folder belongs to another account,
  except for a process that runs as root. An administrator among a folder's
  writers refuses nothing, since an administrator can write every folder. A
  0.4.0 data root carries Administrators in its DACL and loses that entry
  when it is tightened. A folder whose cleaned spelling does not lie under
  the root it is made under is refused with `outside_root`. `--state-dir S` follows the rule. On macOS
  and Linux the engine socket root is a child of `$TMPDIR` or
  `$XDG_RUNTIME_DIR` only while that folder is the account's and open to no
  group or other, the check inference `INF-EL2` applies; otherwise it is
  `/tmp/oa-<uid>`. When another account holds
  `/var/tmp/openabstractions-<uid>`, tmp is
  `$XDG_RUNTIME_DIR/openabstractions/tmp` under the same check, otherwise a
  fresh 0700 folder `/var/tmp/openabstractions-<uid>-<random>`.

Locations of data whose owner lies elsewhere:

| data | location |
|---|---|
| NAS download store (`nas_store`) | named by the operator, outside STO-L1; the `config` module names its key |
| ComfyUI output folder | the ComfyUI installer's folder, or the person's choice recorded by Setup |
| ComfyUI `user` (workflows) on macOS and Linux | the application's own data folder; `input`, `temp` and `launch` are cache |
| TheNoise engine | the short root's `e/thenoise/<version>` from its next engine version |
| inventoryd digests (Linux) | the cache root; the old file is recomputable and is deleted |
| resource audit | stays in the runtime state and counts toward the logs budget |
| MCP gateway handles under an isolated root | `S/mcp-gateway` |
| extension installs | the host application's installer owns the folder |

A remote runtime's roots are its serving account's roots on the remote
host, and a client learns endpoints, handles and typed outcomes, never
paths. A headless Linux runtime answers `no_secure_store` for secrets until
its operator opts in to `file-0600`; people who share one remote runtime
are separated by the services' keys. A job's lease tokens sit with the
job record whose lease they hold, under the job store
(`abstraction-job/CONTRACT.md`).

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
`10d`; `logs_max_use` defaults 10% of the file system capped at 4G. Request
input: 512 head bytes at publication, records of at most 64 KiB per digest,
a sweep once a minute.

## Divergences

- **`Local.Path`.** This optional direct-provider interface is an explicit
  local-binding extension. It projects that provider's own reference to a
  path on the same machine; `Store` does not imply it. The path is a
  machine-local reference, and authority stays with the caller's existing rights.
- **Lend's `link`.** `Lend.link` is an explicit local-binding extension
  describing the placement in the serving engine's directory. It is an
  absolute path on that machine, valid only there; access to it stays with
  the lending profile. The source remains named by its store and object; see
  [Lending profile](#lending-profile).
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

## Content transfer (not built)

`abstraction.storage/content-transfer@1` delivers an object's bytes as a
typed reference (VISION 2026-09-15; designed in
`research/feedback-archive-2026-09-24/result-transfer-by-reference.md`;
architect ruling 2026-11-12). No service answers it yet. The rules below
state what a fill, its proof, its seal and the `mapping` kind mean wherever
OA builds them; the provider kit's `modelfiles` is today's one
implementation, for engines OA launches (provider kit CONTRACT.md INF-MS1..
INF-MS9, KIT-MB1..KIT-MB6).

- **[STO-D1] Sources.** A fill reads from a storage `Ref`
  (`storage.thrift`), bytes located in the store that issued it, or from a
  promise, bytes requested: a download request (`request.thrift`) or a want
  the inventory records (`content.thrift` `Dangling`), naming the digest
  and size. A reference the store does not find is a promise. Which
  reference or promise serves a digest is a later design; a fill reads the
  one its caller names. A fill from a promise is not built.
- **[STO-D2] Fill.** The bytes are placed in an unnamed memory object the
  filling process owns: a pagefile-backed section on Windows, a `memfd` on
  Linux. No file is written. The object's whole size is counted on
  `abstraction.resource` before its pages are touched. A download archive
  entry (abstraction-download DL-R44) is a located source like any other:
  its bytes are copied into the object and proved, and no engine maps or
  reads the archive file in place.
- **[STO-D3] Prove.** Every byte placed is hashed. The object is lent only
  when the SHA-256 of all its bytes equals the digest, or, for an object
  OA hashed whole before, when every span's SHA-256 equals OA's own record
  of the object. A mismatch frees the object. A stream that exposes bytes
  before the fill ends exposes only spans proved against a chunk manifest
  OA trusts; it is not built.
- **[STO-D4] Seal.** After the proof no writable view of the object exists,
  and no process can make one: on Windows the section carries the protected
  descriptor `D:P(A;;0x5;;;OW)` and every handle to it maps for reading
  only; on Linux the `memfd` carries `F_SEAL_WRITE`, `F_SEAL_GROW`,
  `F_SEAL_SHRINK` and `F_SEAL_SEAL`. The proof holds for the object's
  lifetime.
- **[STO-D5] The mapping kind.** A delivery's kind is one of `bytes`,
  `mapping`, `device` and `promise`; the caller lists the kinds it accepts
  in order and receives one of them or a typed refusal, never another. A
  `mapping` is a read-only descriptor of a sealed object with its length
  and digest. An engine OA launches receives it as a model bytes URI
  (provider kit KIT-MB1); another verified process receives it duplicated
  into that process, which is not built.
- **[STO-D6] Release.** A mapping is held until its receiver releases it or
  exits (provider kit KIT-MB4); the object is then freed, or kept while
  memory is free under the residency rules, and its count on
  `abstraction.resource` ends with it.

## Not built

The remote form: an application reaching this service on another machine's
runtime has no binding; no remote `content-reader@1` or `content-writer@1`
exists, and a delegated request carries only digests (`abstraction-inference`
INF-V3). When it is built, a remote receiver MUST refuse every request that
needs a person with the outcome `forbidden` ([Outcomes](#outcomes)) and the
reason "no person reachable on this runtime", until remote questions exist. A forwarded caller is the forwarding runtime's
certificate subject; the agent behind it is an unverified label.

The runtime's enablement of `content-changes@1`: `openabstractions` sets no
`StorageChangesPolicy` on its host, so no runtime calls `EnableChanges`
([Changes](#changes)).

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

Locations: `layout` resolves, creates and checks every root, and the
`config` module's Go machine reader applies its `STO-L4` check. The modules that still compute
their own folders are listed with their owners in
`scripts/layout_sites.allowlist` and move to `layout` with the one-way
migration of their records. Not built yet: the reason in the `config`
module's provenance (`STO-L4`), the Windows installer taking ownership of
the machine root,
providers receiving their roots as launch flags (`STO-L2`), the runtime's
refusal when a second installation's runtime finds the account served, and
the removal levels (`STO-L8`, pending D26). The Python reader
(`abstraction-config/python/abstraction_config.py` and ComfyUI's vendored
copy) still checks the owner of the file and its folder only, and on
Windows no writers: a file Administrators own that Users may change is read
there and ignored by the Go reader until the 0.8.0 plan's W1-09, which owns
that reader, applies `STO-L4`.
