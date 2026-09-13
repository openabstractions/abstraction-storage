namespace * abstraction.storage.api

// Direct storage provider interfaces; generated with --no-ipc.
encoding json {
 escape="minimal"
 indent="2"
 map_keys="utf8-bytes"
 numbers="integer-decimal"
 opaque="verbatim"
 terminator="newline"
 duplicate_keys="refuse"
 depth_limit="64"
}
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
 10: trailing_bytes(stage="document")
}
struct Ref {
 1: required string store
 2: required string digest
 3: required i64 size
 4: required string locator
}(document="true",unknown_fields="refuse",doc="Reference issued by a Store. Size zero means unknown. Locator is opaque provider binding data; applications must not treat it as path or authority. Only an explicit Local provider projects it to a path.")
struct FindResult {
 1: optional Ref reference(omit="absent")
}(unknown_fields="refuse",doc="Absent reference means no known match; discovery does not hash bytes. Naming conventions supply evidence and consumers still verify content.")
service Store {
 string Name()
 FindResult Find(1:string digest)
 Ref Place(1:string digest, 2:i64 size)(doc="Reserve a destination without writing bytes. A read-only provider refuses. The provider chooses its location.")
}(wire_name="abstraction.storage/store@1",doc="Direct provider interface. No IPC endpoint is supplied; native provider binding handles errors and reference provenance.")
service Local {
 string Path(1:Ref reference)(doc="Optional explicit local binding projects its own reference onto this machine. Availability of this interface is not implied by Store.")
}(wire_name="abstraction.storage/local@1",doc="Optional direct local-provider extension; never a remote authority grant.")
service Writable {
 void Commit(1:Ref reference)(doc="Commit already-written bytes under their digest, making a reservation findable. Preserve provider validation and errors.")
}(wire_name="abstraction.storage/writable@1",doc="Optional direct writable-provider extension.")
const list<string> failure_names = ["read_only", "not_found"]


