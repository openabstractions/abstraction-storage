namespace * abstraction.storage.content
// Additive service profile; storage.thrift retains direct provider interfaces.
encoding json { escape="minimal" indent="2" map_keys="utf8-bytes" numbers="integer-decimal" opaque="verbatim" terminator="newline" duplicate_keys="refuse" depth_limit="64" }
refusal {
 1: malformed(stage="grammar")
 2: bad_string(stage="grammar")
 3: number_spelling(stage="grammar")
 4: wrong_type(stage="grammar")
 5: depth_exceeded(stage="grammar")
 6: duplicate_key(stage="grammar")
 7: duplicate_field(stage="structure")
 8: unknown_field(stage="structure")
 9: missing_field(stage="structure")
 10: bad_binary(stage="structure")
 11: bad_enum(stage="structure")
 12: trailing_bytes(stage="document")
}

enum Verification { 1: unverified }(unknown="refuse")
enum OpenOutcome { 1: opened 2: not_found 3: forbidden 4: invalid 5: unsupported 6: unavailable 7: exhausted }(unknown="refuse")
enum ReadOutcome { 1: data 2: gap 3: forbidden 4: invalid 5: unavailable 6: changed }(unknown="refuse")
enum CloseOutcome { 1: closed 2: gap 3: forbidden }(unknown="refuse")
struct Resource {
 1: required string handle
 2: required string digest
 3: required i64 size
 4: required Verification verification
}(unknown_fields="refuse",doc="Opaque resource bound to receiving account and observed program and provider lifetime. Digest is the requested canonical sha256 naming key, not a verified hash. Size is observed and nonnegative. Verification is always unverified; consumer verifies assembled bytes. No private path or immutable-snapshot claim.")
struct OpenResult {
 1: required OpenOutcome outcome
 2: optional Resource resource(omit="absent")
}(document="true",unknown_fields="refuse",doc="Resource present exactly for opened. Authorization precedes lookup. not_found means no known match, not global absence.")
struct Chunk {
 1: required i64 offset
 2: required i64 total
 3: required binary data
 4: required bool eof
}(unknown_fields="refuse",doc="Offset equals requested offset; total equals issued resource size. Length at most max_bytes and offset+length at most total. eof iff offset+length equals total. Non-EOF data is nonempty. Error/absence never means EOF.")
struct ReadResult {
 1: required ReadOutcome outcome
 2: optional Chunk chunk(omit="absent")
}(unknown_fields="refuse",doc="Chunk present exactly for data. changed reports observed mutation and invalidates resource; discard assembly and explicitly reopen. Mutation detection is advisory; verify completed bytes.")
struct CloseResult { 1: required CloseOutcome outcome }(unknown_fields="refuse")
service ContentReader {
 OpenResult Open(1:string digest)(doc="Canonical sha256: plus 64 lowercase hexadecimal digits. Explicit content policy required; same-account identity alone grants no content permission. At most 32 resources globally and 8 per scope; 30 seconds idle expires.")
 ReadResult Read(1:string handle,2:i64 offset,3:i64 max_bytes)(doc="offset>=0; max_bytes is 1..65536. Authority rechecked before bytes. Unknown/expired/closed/restarted resources give gap; another scope's known resource gives forbidden.")
 CloseResult Close(1:string handle)(doc="Release own scoped resource even after content authorization revocation. Provider shutdown closes all owned resources.")
}(wire_name="abstraction.storage/content-reader@1",doc="Read-only bounded access through configured native Store+Local adapters. Naming lookup is unverified; unsupported providers refuse. Writes use the separate content-writer profile. Callbacks are trusted provider code required to honor bounded execution/context.")

enum BeginOutcome { 1: started 2: committed 3: present 4: forbidden 5: invalid 6: conflict 7: too_large 8: busy 9: unsupported 10: unavailable 11: exhausted }(unknown="refuse")
enum AppendOutcome { 1: accepted 2: gap 3: forbidden 4: invalid 5: out_of_order 6: too_large 7: unavailable }(unknown="refuse")
enum CommitOutcome { 1: committed 2: gap 3: forbidden 4: incomplete 5: mismatch 6: unavailable }(unknown="refuse")
enum AbortOutcome { 1: aborted 2: gap 3: forbidden }(unknown="refuse")
enum Evidence { 1: hashed 2: named }(unknown="refuse")
struct Upload {
 1: required string handle
 2: required string digest
 3: required i64 size
 4: required i64 received
}(unknown_fields="refuse",doc="Opaque staged upload bound to the receiving account/program scope and provider lifetime. Size is the declared total; received counts bytes accepted in order. Staged bytes are never findable or readable.")
struct Stored {
 1: required string digest
 2: required i64 size
 3: required Evidence evidence
}(unknown_fields="refuse",doc="hashed: the service hashed every byte it accepted into staging, the hash equals digest, and the provider committed that staged object. named: an existing provider naming match was found without hashing; size zero means unknown.")
struct BeginResult {
 1: required BeginOutcome outcome
 2: optional Upload upload(omit="absent")
 3: optional Stored stored(omit="absent")
 4: required i64 limit
}(unknown_fields="refuse",doc="upload present exactly for started. stored present exactly for committed and present. limit is the provider's maximum declared size for evaluated outcomes, and zero for forbidden, invalid and unavailable.")
struct AppendResult {
 1: required AppendOutcome outcome
 2: required i64 received
}(unknown_fields="refuse",doc="For accepted, out_of_order and too_large, received is the next offset the service accepts. Other outcomes carry zero.")
struct CommitResult {
 1: required CommitOutcome outcome
 2: optional Stored stored(omit="absent")
 3: required i64 received
}(unknown_fields="refuse",doc="stored present exactly for committed with hashed evidence. For incomplete, received is the next accepted offset. Other outcomes carry zero.")
struct AbortResult { 1: required AbortOutcome outcome }(unknown_fields="refuse")
service ContentWriter {
 BeginResult Begin(1:string request,2:string digest,3:i64 size)(doc="request is a caller-retained identity of 16..128 characters from A-Z a-z 0-9 _ -, scoped to the receiving account/program. digest is canonical sha256; size is the declared total, 0..limit. Write authorization precedes every lookup and provider effect. The same request with the same digest and size returns its live upload or committed result; different digest or size is conflict. present reports an existing naming match. busy reports another live upload of the same digest. At most 16 uploads globally, 4 per scope and 256 request records.")
 AppendResult Append(1:string handle,2:i64 offset,3:binary data)(doc="data is 1..65536 bytes at offset equal to received. Authority rechecked before bytes are staged. A different offset stages nothing and returns out_of_order with received. Bytes beyond the declared size stage nothing and return too_large. Unknown/expired/aborted/restarted uploads give gap; another scope's upload gives forbidden.")
 CommitResult Commit(1:string handle)(doc="Authority rechecked before visibility. Requires received equal to size and the staged bytes to hash to digest; mismatch discards the upload. Visibility changes in one provider commit step; readers observe no partial content.")
 AbortResult Abort(1:string handle)(doc="Discard own staged upload and its request record, including after write authorization revocation.")
}(wire_name="abstraction.storage/content-writer@1",doc="Bounded authorized writes through configured native Store+Local+Writable adapters. Uploads idle for 30 seconds expire and are discarded. Committed request records are retained for 10 minutes within one provider lifetime. Provider shutdown discards staged uploads.")

enum ChangeKind { 1: added 2: removed }(unknown="refuse")
enum ChangePageOutcome { 1: page 2: gap 3: forbidden 4: invalid 5: unavailable }(unknown="refuse")
enum ListingOutcome { 1: page 2: gap 3: forbidden 4: invalid 5: unavailable }(unknown="refuse")
struct Change {
 1: required i64 sequence
 2: required ChangeKind kind
 3: required string digest
 4: required i64 size
}(unknown_fields="refuse",doc="One observed change in provider journal order. Sequence increases within one provider epoch. Digest is a canonical sha256 naming key, not verified content. Size is observed; zero means unknown. A notice grants no access.")
struct ChangePage {
 1: required ChangePageOutcome outcome
 2: required list<Change> changes
 3: required string next
 4: required bool at_end
}(unknown_fields="refuse",doc="page carries at most max_changes entries the caller may read and a next cursor. next advances past every entry examined, including entries the caller may not read, which are omitted without a count. at_end means the journal end was reached during this call. Refusals carry no changes, an unchanged cursor and at_end false. gap requires restarting from List.")
struct ListedObject {
 1: required string digest
 2: required i64 size
}(unknown_fields="refuse")
struct ListingPage {
 1: required ListingOutcome outcome
 2: required list<ListedObject> objects
 3: required string continuation
 4: required bool complete
 5: required string cursor
}(unknown_fields="refuse",doc="page carries at most limit objects the caller may read, in digest order, from one frozen snapshot. cursor is the change cursor at which that snapshot was taken and is identical on every page of it; Observe from it reports every later change. complete means the snapshot is exhausted; otherwise continuation is nonempty. Objects the caller may not read are omitted without a count. Refusals carry no objects, empty continuation and cursor, and complete false.")
service ContentChanges {
 ChangePage Observe(1:string cursor,2:i64 max_changes,3:i64 wait_ms)(doc="Empty cursor starts at the current journal end. Cursors are at most 256 bytes and bind provider epoch and sequence. A cursor from another epoch or older than the retained journal returns gap. max_changes is 1..256; wait_ms is 0..30000 and waits at the current end for an append, then rereads once. Observe authorization is checked on every call and rechecked after a wait; each entry is filtered through the read policy for its digest. A read decision outage returns unavailable without advancing. Bounded waiter exhaustion returns unavailable.")
 ListingPage List(1:string continuation,2:i64 limit)(doc="Empty continuation freezes a new snapshot of the provider's known objects and its change cursor. limit is 1..256. Continuations are at most 256 bytes and name a retained snapshot; an unknown, expired or restarted snapshot returns gap. At most 8 snapshots are retained, each for 30 idle seconds; exhaustion returns unavailable. Observe authorization and per-object read filtering apply as for Observe.")
}(wire_name="abstraction.storage/content-changes@1",doc="Bounded authorized observation of objects a content store gains or loses. The provider journal retains a bounded number of recent changes per lifetime with no per-subscriber queue; a subscriber that falls behind receives gap and rebuilds from List. Service-mediated commits are journaled when they publish. External additions and deletions are journaled when the provider's optional listing capability is polled; changes that cancel out between polls are not reported. removed currently reports only external deletions, because the service has no delete operation. A provider restart starts a new epoch.")

