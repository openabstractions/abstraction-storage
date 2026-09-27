namespace * abstraction.storage.lend
// Storage lending profile implemented by providers such as modelbridge.
// The object uses the storage inventory's store and id; the engine is a
// registered model server's name. The runtime mediates application calls.
encoding json { escape="minimal" indent="2" map_keys="utf8-bytes" numbers="integer-decimal" opaque="verbatim" terminator="newline" duplicate_keys="refuse" depth_limit="64" }
refusal {
 1: malformed(stage="grammar")
 2: bad_string(stage="grammar")
 3: number_spelling(stage="grammar")
 4: wrong_type(stage="grammar")
 5: bad_timestamp(stage="grammar")
 6: depth_exceeded(stage="grammar")
 7: duplicate_key(stage="grammar")
 8: duplicate_field(stage="structure")
 9: unknown_field(stage="structure")
 10: missing_field(stage="structure")
 11: bad_enum(stage="structure")
 12: trailing_bytes(stage="document")
}
typedef string timestamp(write="rfc3339-micros",read="rfc3339-wide")

const list<string> lending_error_codes = ["caller_unavailable", "identity_required"]

// Link says how the borrowed file reaches the engine's directory. symbolic is
// the default. hard is the fallback when the operating system refuses this
// process a symbolic link or the engine refuses to follow one, and the source
// and the engine directory share a volume. copy is made only when the caller
// asks for it, and is counted and listed as a copy, never as a link.
enum Link { 1: symbolic 2: hard 3: copy }(unknown="refuse",reader="act")

struct Lend {
 1: required string id
 2: required string engine
 3: required string store
 4: required string object
 5: required string name
 6: required string link
 7: required Link kind
 8: required i64 size
 9: required timestamp created
}(unknown_fields="refuse",doc="One lend the ledger holds. id names this entry and is what Unlend takes. engine is the declared host name the file was lent to. store and object are the inventory's own naming of the source: object is the id Lend was given, resolved to the manifest id when the source publishes one. name is the engine's directory name for the borrowed file, which is what that engine's own list is expected to derive its model name from. link is the absolute path of the link this lend created, in the engine's directory. size is the source file's size in bytes at the time of the lend. The ledger, not the engine's list, says whether a lend is in force.")

enum LendOutcome { 1: lent 2: not_permitted 3: unsupported_engine 4: link_refused 5: already_lent 6: unknown_object 7: invalid 8: unavailable }(unknown="refuse",reader="act")
struct LendResult {
 1: required LendOutcome outcome
 2: optional Lend lend(omit="absent")
 3: required string detail
}(document="true",unknown_fields="refuse",doc="lend is present exactly for lent and already_lent; already_lent carries the entry that already holds this object at this engine. detail is display-grade text naming the refused case and carries no path for a caller that may not read one; it is empty for lent.")

enum UnlendOutcome { 1: unlent 2: not_permitted 3: unknown 4: invalid 5: unavailable }(unknown="refuse",reader="act")
struct UnlendResult {
 1: required UnlendOutcome outcome
 2: required string detail
}(unknown_fields="refuse",doc="unlent means the link and every directory this lend created are gone from the engine's directory, the source is untouched, and the entry has left the ledger. unknown is an entry no ledger row names, including one already unlent. detail is display-grade and empty for unlent.")

enum LendsOutcome { 1: page 2: not_permitted 3: invalid 4: unavailable }(unknown="refuse",reader="act")
struct LendPage {
 1: required LendsOutcome outcome
 2: required list<Lend> lends
 3: required string continuation
 4: required bool complete
}(unknown_fields="refuse",doc="One page of the ledger in created order, oldest first. complete means the ledger is exhausted; otherwise continuation is nonempty. Refusals carry no entries, an empty continuation and complete false.")

service Lending {
 LendResult Lend(1:string store,2:string object,3:string engine,4:bool copy)(doc="Place the named object in the engine's own directory by link, so that engine serves it. store and object are the inventory's naming: object is a manifest id, the name the store gives that manifest, or an object locator, and never a path. engine is a declared host name this provider supports. copy true asks for a copy where a link cannot be made and is counted as a copy; false refuses with link_refused instead. Gated by abstraction.storage/lend on resource engine:<name>, admitted through the runtime's first-use question. An object this provider's stores do not hold is unknown_object; an engine it does not support, or one whose own configuration cannot be pointed at the lent directory without a person editing its configuration file, is unsupported_engine; a link neither the operating system nor the volume allows is link_refused; an object already lent to that engine is already_lent with the entry in force. The original is never moved, renamed or deleted.")
 UnlendResult Unlend(1:string entry)(doc="Remove exactly the link this entry created and the directories this provider made for it, and drop the entry. The source file is untouched. Gated by abstraction.storage/lend on the entry's own engine:<name>. The engine's own list may keep naming the model until that engine's idle-unload timer fires; the ledger is the answer to whether the lend is in force.")
 LendPage Lends(1:string continuation,2:i64 limit)(doc="The ledger. limit is 1..256. Continuations are at most 256 bytes and name a retained page position; an unknown one is invalid.")
}(wire_name="abstraction.storage/lend@1",error_codes="lending_error_codes",doc="Storage lending implemented by a provider and mediated by the runtime. The provider listens on the shared identity-bound transport and admits designated runtime executables of its own account. A call whose peer identity could not be obtained fails with caller_unavailable; insufficient program proof fails with identity_required; another account or program reads not_permitted. The runtime decides the application caller's right before forwarding. Providers keep a ledger of their placements and preserve source files. Symbolic and hard links require no model-byte reads; an explicitly requested copy reads the source bytes.")
