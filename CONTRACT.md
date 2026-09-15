# Authorized bounded content reader

`content.thrift` adds `abstraction.storage/content-reader@1`. The existing
`storage.thrift` direct Store, Local and Writable interfaces retain their mode.
The read service adapts one explicitly configured native Store+Local privately.
It implements no placement, commit or writable projection.

`service.Listen(endpoint, store, policy)` requires an explicit content policy.
The receiver obtains native Program evidence and requires the service account.
The account and observed executable path form its resource scope. This identity
check supplies no blanket permission to stored bytes. Policy receives the native
peer and requested digest before Find or file opening, and again before each
read. Policy must be concurrent-safe, bounded and context-aware. Release of an
owned resource remains available after policy revocation. Missing Local support
returns unsupported; the service never discovers owner stores by itself.

A failed policy lookup reports `unavailable`; an evaluated refusal reports
`forbidden`. Go policy callbacks wrap `service.ErrPolicyUnavailable` for lookup
failures. Other callback errors retain refusal semantics. Neither outcome exposes
content or substitutes a cached permit. A later explicit read can retry a
recovered policy service against the same live resource.

Open requires canonical lowercase SHA-256 naming and returns an opaque random
handle, requested digest, observed size and `verification=unverified`. Existing
Find discovers a name; it does not hash bytes. The caller must hash completed
bytes before treating them as the requested content. Tests deliberately expose a
mislabeled fixture with the unverified claim intact.

Resources belong to one provider lifetime and authenticated account/program.
Limits are 32 resources globally, eight per scope, 32 active framed connections,
1 MiB control frames and 1..65536 bytes per read. Resources expire after 30 idle
seconds and a one-second sweep closes expired files. Lookup in a new instance,
after expiry or after release returns gap. Another program cannot read or release
a known resource. Lost Open replies may retain a slot until expiry; clients do
not retry automatically.

Read compares exact offsets/total and reports EOF only on successful data at the
issued size. The service checks regular file type before and after open, retains
the opened file identity, and compares size/modification metadata around reads.
Observed mutation returns changed and invalidates the handle. Discard previous
chunks when changed occurs. This is advisory mutation detection, not an immutable
snapshot: same-size writes with restored metadata may evade it. Final hash
verification is always the caller's obligation.

Service Close stops admission, cancels contexts and closes resource files. Serve
joins its handlers before returning. Policy runs outside the global resource
mutex. Native Store/Local lack context parameters and remain trusted bounded
provider callbacks; this wrapper cannot forcibly cancel a blocking custom native
provider or filesystem operation. Errors returned over the protocol expose no
private backing paths.

Go `client.New(endpoint)` supplies Open(ctx,digest), Read(ctx,Resource,offset,limit)
and Close(ctx,Resource). C++ storage::Client exposes the corresponding resource-
explicit calls with fresh default waits, explicit deadlines and cancellation.
Resources are caller-retained values; the client maintains no private path cache.

C++ service packaging is opt-in: configure `ABSTRACTION_STORAGE_BUILD_SERVICE=ON`
and `ABSTRACTION_STORAGE_BUILD_LEGACY=OFF` for a service-only prefix. Its
`abstraction_storage_content` package exports `abstraction::storage_content`
(protocol) and `abstraction::storage_client` (shared IPC). The existing native
package default retains its independent build.

# Authorized bounded content writer

`content.thrift` also defines `abstraction.storage/content-writer@1`. Go
`Host.EnableWriter(policy, limit)` adds it to the reader's endpoint before Serve.
The configured provider must implement Store, Local and Writable. The write
policy is a separate callback from the read policy; a read permit grants no
write. Runtime `Options.StorageWritePolicy` and `StorageWriteLimit` compose it,
and incomplete writer configuration refuses before any listener opens.
`ContentPolicyFromRights(client, "abstraction.storage/content.write")` enforces
it through the general rights decision service.

Begin takes a caller-retained request identity, canonical digest and declared
size. The service evaluates write policy before Find, Place or any file effect.
A declared size above the configured limit returns `too_large` with the limit.
Place selects the provider's staging location; the service creates only that
staging file. Find never reports staged bytes, so readers observe `not_found`.

Append accepts 1..65536 bytes at exactly the received offset. Policy is rechecked
before bytes are staged. Another offset returns `out_of_order` with received.
Bytes beyond the declared size return `too_large` and stage nothing. A failed
write discards the upload. Commit rechecks policy, requires every declared byte,
compares the running SHA-256 of accepted bytes with the digest and then calls the
provider's Commit once. The native content store renames staging into its final
name; readers see either no object or the whole object. A mismatch discards the
upload. A committed result reports `evidence=hashed`.

Request identities are 16..128 characters from `A-Z a-z 0-9 _ -` and are scoped
to the account and observed program. Repeating Begin with the same digest and
size resumes the live upload with its received count or returns the committed
result. A different digest or size returns `conflict` with zero provider
effects. Another live upload of the same digest returns `busy`. An existing
provider naming match returns `present` with `evidence=named`; that result is an
unverified naming claim. Content addressing keeps one object per digest.

Limits are 16 uploads globally, four per scope and 256 request records. Uploads
expire after 30 idle seconds; expiry, Abort, mismatch and provider shutdown
remove staged bytes. Abort of an owned upload remains available after write
revocation.

Request identities are durable service-owned state. `EnableWriter` takes a CAS
`Store` and an absolute record path; runtime `StorageWriteRecordPath` is
required with the writer and defaults to a bounded CAS file store. Begin records
the identity before any provider lookup or staging effect, and refuses with
`unavailable` when that record cannot be written. A committed or `present`
result is recorded when it occurs. Abort and mismatch forget the identity. An
expired upload, a failed append and provider shutdown leave the identity
unfinished: the same digest and size start a new upload, and a different digest
or size returns `conflict`. Each identity is retained for ten minutes after its
last Begin or result, across provider restarts; after that the identity names
nothing. The host must be the record file's only writer. A record file changed by
another writer fences this lifetime: later identity changes return `unavailable`.
A malformed or oversized record file refuses writer startup.

Startup restores unexpired identities and removes the provider reservation of
every unfinished identity whose digest is not findable. Records precede staging,
so crash leftovers belong to recorded digests. This requires Place to choose a
location without effects, as the native content store does.

Commit records the committed result before the provider publishes. When that
record cannot be saved, Commit returns `unavailable` and publishes nothing. When
publishing fails, the record is demoted to unfinished. A crash between the save
and the publish leaves a recorded result without content; startup demotes it,
and the next Begin with that identity starts a new upload. A retry after a
completed publish reads `committed` with `hashed` evidence whether or not any
later save succeeds.

Go `client.NewWriter`, `NewRequestID` and `Writer.Begin/Append/Commit/Abort`
validate result invariants. `Writer.Write` resumes by identity, follows
`out_of_order` and returns `*OutcomeError` for other service outcomes; it never
aborts. C++ `storage::Writer` provides the same calls and throws `WriteOutcome`
from Write. Facade resolution uses `Machine.ResolveStorageWriter` and C++
`facade::ResolveStorageWriter`. Python, Rust and JavaScript have generated writer
codecs only; their writer clients are planned and unproven.

# Change observation

`content.thrift` also defines `abstraction.storage/content-changes@1`. Go
`Host.EnableChanges(observe, interval, capacity)` adds it to the storage endpoint
before Serve. Runtime `StorageChangesPolicy`, `StorageChangesInterval` (default
one second) and `StorageChangesCapacity` (default 4096) compose it. With rights,
`ContentPolicyFromRights(client, "abstraction.storage/content.observe")` supplies
the observe policy on resource `abstraction.storage/changes`.

The host keeps one journal per provider lifetime. Its epoch is a random token and
its sequence increases from zero. The journal retains at most `capacity` recent
changes and keeps no per-subscriber queue. Writer commits append `added` when the
provider publishes a new object. A provider implementing the optional native
`storage.Lister` capability is listed when changes are enabled; those present
objects are recorded without being journaled. It is then polled every interval:
objects that appeared since the previous listing append `added`, and objects that
disappeared append `removed`. A change that is made and undone between two polls
is not reported. A listing that fails or exceeds 65536 objects makes Observe and
List `unavailable` until a later listing succeeds; no partial listing is treated
as deletions. `ContentStore` lists its committed `blobs/sha256-<hex>` entries;
reservations are never listed.

`Observe(cursor, max_changes, wait_ms)` checks the observe policy on every call.
An empty cursor starts at the current journal end. A cursor names the epoch and
the last examined sequence; a cursor from another epoch or older than the
retained journal returns `gap`. A gap is recovered by `List`, never by silently
skipping changes. Every entry is filtered through the read policy for its digest.
Entries the caller may not read are omitted without a count, and the returned
cursor still advances past them. A read decision outage returns `unavailable`
and does not advance. At the current end a call waits at most `wait_ms` for an
append, rereads once, and rechecks the observe policy before returning changes.
At most 16 calls wait at once; more return `unavailable`. Caller disconnection or
cancellation ends only the wait.

`List(continuation, limit)` builds initial state without racing observation. An
empty continuation freezes a snapshot of the known objects together with the
change cursor at that moment. Every page of that snapshot carries the same
cursor, and observing from it reports every later change. Pages are in digest
order and filtered by the read policy. At most eight snapshots are retained, each
for 30 idle seconds, bound to the caller scope; an unknown, expired or restarted
snapshot returns `gap`, and another scope's continuation returns `forbidden`.

A provider restart starts a new epoch: every earlier cursor returns `gap`, and
the new snapshot lists present objects without journaling them. `removed` is
currently reported only for deletions made outside the service, because the
service offers no delete operation. A change notice carries an unverified digest
naming key and grants no access; callers still `Open` and verify content.

Go `client.Changes` (`Observe`, `List`, `Snapshot`) and C++ `storage::Changes`
validate page shapes. Facade resolution uses `Machine.ResolveStorageChanges` and
C++ `facade::ResolveStorageChanges`.

Cross-account content grants, public release pins,
remote transports and macOS native Program proof remain separate obligations.
