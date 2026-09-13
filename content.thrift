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
}(wire_name="abstraction.storage/content-reader@1",doc="Read-only bounded access through configured native Store+Local adapters. Naming lookup is unverified; unsupported providers refuse. No writable Place/Commit projection. Callbacks are trusted provider code required to honor bounded execution/context.")

