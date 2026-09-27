# abstraction-storage

Read or write shared content by its verified identity while the storage service
keeps backend paths and file permissions private. A client opens content by its
canonical identifier, transfers bounded chunks and closes its own handle. The
same application API can sit over a local store or another authorized provider.

Choose the service that matches the application:

| Need | Contract |
| --- | --- |
| Read bytes by canonical digest | `abstraction.storage/content-reader@1` |
| Store bytes atomically under their SHA-256 digest | `abstraction.storage/content-writer@1` |
| Observe the latest content inventory and changes | `abstraction.storage/content-changes@1` |

Each service is resolved independently through the facade. Rights are checked
at the service that owns the content. A digest is an identifier; callers that
require byte integrity verify the assembled digest or require hashed evidence.

## Application clients

Select `abstraction.storage/content-reader@1` through the facade and use its typed storage
client. Go, C++, Python and Rust clients validate results over the shared IPC boundary;
JavaScript binds the generated `ContentReaderClient` directly.
[C++ service package proof](cpp/test/service/README.md), [Python protocol setup](py/README.md)
and the [facade packages](https://github.com/openabstractions/abstraction-facade)
describe independent adoption and waiting/trust configuration.

The receiving service authorizes content access. Revocation, unavailable authority,
expired resources and changed content have explicit refusal/gap outcomes. A content
name alone does not grant access or prove the returned bytes: verify the assembled
digest where that guarantee is required. Copy helpers bound memory and retain one
waiting scope; cancellation leaves service-owned content intact.

A file a download job delivers is not thereby readable here by its digest.
[`abstraction-download`](https://github.com/openabstractions/abstraction-download)
writes to the sink its request names, never into the content root, and nothing
copies one into the other on completion. A job-service download has no
destination path for an application to read directly; an application pages
the bytes out of the job's `ReadResult` and hands them to `content-writer@1`
itself, to make them readable through `content-reader@1`. [The worked
bridge](https://github.com/openabstractions/abstraction-download#reading-a-finished-download-through-storage)
is on the download layer's own page.

### Reading content

`abstraction.storage/content-reader@1` reads bytes by their canonical SHA-256
digest. Resolve it with Go `Machine.ResolveStorage` or Python
`Machine().resolve_storage(scope="local")`. `Open` returns a `Resource` handle
bound to that digest; `Read` transfers bounded chunks against the handle,
never the digest, so the object cannot change under a resumed read; `Close`
releases it. A `gap` outcome on `Read` means the content changed since `Open`:
reopen by digest rather than continue the chunk sequence. `Open` itself
answers `not_found`, `forbidden`, `unsupported` or `unavailable` when there is
no handle to return.

```go
reader, err := facade.Discover().ResolveStorage(ctx, facade.Requirements{})
if err != nil {
	return err
}
opened, err := reader.Open(ctx, digest)
if err != nil {
	return err
}
if opened.Outcome != content.OpenOutcomeOpened {
	return fmt.Errorf("storage refused open: %s", opened.Outcome)
}
resource := *opened.Resource
var buf bytes.Buffer
for offset := int64(0); ; {
	chunk, err := reader.Read(ctx, resource, offset, 65536)
	if err != nil {
		return err
	}
	if chunk.Outcome == content.ReadOutcomeGap {
		return fmt.Errorf("content changed under us; reopen by digest")
	}
	if chunk.Outcome != content.ReadOutcomeData {
		return fmt.Errorf("storage refused read: %s", chunk.Outcome)
	}
	buf.Write(chunk.Chunk.Data)
	offset += int64(len(chunk.Chunk.Data))
	if chunk.Chunk.EOF {
		break
	}
}
if _, err := reader.Close(ctx, resource); err != nil {
	return err
}
fmt.Println(buf.Len(), "bytes read")
```

```python
from abstraction.facade.client import Machine

reader = Machine().resolve_storage(scope="local")
opened = reader.open(digest)
if opened.outcome != "opened":
    raise RuntimeError(f"storage refused open: {opened.outcome}")
resource = opened.resource
data, offset = bytearray(), 0
while True:
    chunk = reader.read(resource, offset, 65536)
    if chunk.outcome == "gap":
        raise RuntimeError("content changed under us; reopen by digest")
    if chunk.outcome != "data":
        raise RuntimeError(f"storage refused read: {chunk.outcome}")
    data += chunk.chunk.data
    offset += len(chunk.chunk.data)
    if chunk.chunk.eof:
        break
reader.close(resource)
print(len(data), "bytes read")
```

Python's `Client.copy(resource, destination)` does the same read loop into a
writer under one wait budget and raises `OutcomeError` on a non-`data`
outcome, including `gap`.

### Writing content

`abstraction.storage/content-writer@1` stores bytes under their SHA-256 digest.
Resolve it with Go `Machine.ResolveStorageWriter` or C++
`facade::resolve_storage_writer`. Create a request identity with
`client.NewRequestID` and retain it before writing. `Writer.Write` stages the
bytes in bounded appends and commits once every byte matches the digest. After
an uncertain failure, call `Write` again with the same identity to resume the
upload or read its committed result. Other service outcomes, such as
`too_large`, `busy`, `conflict`, `forbidden` or `unavailable`, arrive as
`*client.OutcomeError`. Readers see either no object or the whole object.

**Limits.** Sixteen uploads run service-wide at once, four per subject; each
`Append` carries at most 64 KiB (65536 bytes); the Go `Writer` client holds
each call to a 5-second wait budget by default. A multi-gigabyte file crosses
the 64 KiB bound tens of thousands of times — a 40 GB upload is roughly
655,000 `Append` calls, each its own bounded round trip. It holds one of the
sixteen (four per subject) upload slots for as long as those calls take.

```go
writer, err := facade.Discover().ResolveStorageWriter(ctx, facade.Requirements{})
if err != nil {
	return err
}
request, err := client.NewRequestID() // retain before writing
if err != nil {
	return err
}
sum := sha256.Sum256(data)
digest := "sha256:" + hex.EncodeToString(sum[:])
stored, err := writer.Write(ctx, request, digest, bytes.NewReader(data), int64(len(data)))
var outcome *client.OutcomeError
if errors.As(err, &outcome) {
	return fmt.Errorf("storage refused %s: %s", outcome.Operation, outcome.Outcome)
}
fmt.Println(stored.Digest, stored.Size, stored.Evidence)
```

`evidence=hashed` means the service hashed the bytes. `present` with
`evidence=named` reports an existing object the provider names by that digest.

Python and Rust resolve the same contract with a validating wrapper of their own;
JavaScript drives the generated writer client's `begin`/`append`/`commit` itself.
Python `Machine().resolve_storage_writer(scope="local")` returns a `Writer` whose
lowercase `write` method raises `OutcomeError` for other outcomes. Rust
`StorageMachine::resolve_storage_writer(vec![], "local")` returns a `Writer` whose
`write` returns `Error::Outcome`. Both resume by request identity and never abort.

```python
import hashlib
from abstraction.facade.client import Machine
from abstraction.storage.content.client import OutcomeError, new_request_id

writer = Machine().resolve_storage_writer(scope="local")
request = new_request_id()  # retain before writing
digest = "sha256:" + hashlib.sha256(data).hexdigest()
try:
    stored = writer.write(request, digest, data)
    print(stored.digest, stored.size, stored.evidence)
except OutcomeError as error:
    print("storage refused:", error.outcome)
```

```rust
use abstraction_facade_native::Machine;
use abstraction_facade_storage::{new_request_id, Error, StorageMachine};

fn store(endpoint: &str, digest: &str, data: &[u8]) -> Result<(), String> {
    let writer = Machine::new(endpoint)
        .resolve_storage_writer(vec![], "local")
        .map_err(|e| format!("{e:?}"))?;
    let request = new_request_id().map_err(|e| e.to_string())?; // retain before writing
    match writer.write(&request, digest, data) {
        Ok(stored) => println!("{} {} {}", stored.digest, stored.size, stored.evidence),
        Err(Error::Outcome(outcome)) => return Err(format!("storage refused: {outcome}")),
        Err(other) => return Err(format!("{other:?}")),
    }
    Ok(())
}
```

JavaScript binds the generated `ContentWriterClient` from
`@openabstractions/storage-content` and makes the calls itself. `begin` again
with the same identity returns `started` with the bytes already received.
Appends carry at most 65536 bytes.

```js
import {createHash, randomBytes} from 'node:crypto';
import {Machine} from '@openabstractions/facade';
import {NativeConnector} from '@openabstractions/ipc';
import {ContentWriterClient} from '@openabstractions/storage-content';

const connector = new NativeConnector();
const machine = new Machine(connector.runtimeEndpoint(), {connector});
const writer = (await machine.resolveService('abstraction.storage/content-writer@1')).client(ContentWriterClient);
const request = randomBytes(16).toString('hex'); // retain before writing
const digest = 'sha256:' + createHash('sha256').update(data).digest('hex');
const begun = await writer.begin(request, digest, BigInt(data.length));
if (begun.outcome === 'started') {
  for (let offset = Number(begun.upload.received); offset < data.length;) {
    const part = data.subarray(offset, offset + 65536);
    const appended = await writer.append(begun.upload.handle, BigInt(offset), part);
    if (appended.outcome !== 'accepted' && appended.outcome !== 'out_of_order') throw new Error('append ' + appended.outcome);
    offset = Number(appended.received);
  }
  const committed = await writer.commit(begun.upload.handle);
  if (committed.outcome !== 'committed') throw new Error('commit ' + committed.outcome);
} else if (begun.outcome !== 'committed' && begun.outcome !== 'present') {
  throw new Error('begin ' + begun.outcome);
}
```

### Observing changes

`abstraction.storage/content-changes@1` reports objects `added` and `removed`.
Resolve it with Go `Machine.ResolveStorageChanges`. `Snapshot` lists every object
and returns the cursor at which the list was taken. `Observe(ctx, cursor, max,
waitMS)` returns later changes and the next cursor, waiting up to `waitMS`
milliseconds when none are ready. A `gap` outcome means changes were missed:
rebuild from a fresh `Snapshot`. The service polls its provider, and a change
made and undone between two polls is not reported.

```go
changes, err := facade.Discover().ResolveStorageChanges(ctx, facade.Requirements{})
if err != nil {
	return err
}
objects, cursor, err := changes.Snapshot(ctx, 256)
if err != nil {
	return err
}
fmt.Println(len(objects), "objects")
for {
	page, err := changes.Observe(ctx, cursor, 64, 30000)
	if err != nil {
		return err
	}
	if page.Outcome != content.ChangePageOutcomePage {
		return fmt.Errorf("observe: %s", page.Outcome) // gap: take a new Snapshot
	}
	for _, change := range page.Changes {
		fmt.Println(change.Kind, change.Digest, change.Size)
	}
	cursor = page.Next
}
```

The rights service authorizes writes with `abstraction.storage/content.write` on
the digest and observation with `abstraction.storage/content.observe` on
`abstraction.storage/changes`. A read permit grants neither.

Read [CONTRACT.md](CONTRACT.md) and [content.thrift](content.thrift) for exact
outcomes, bounds and authority semantics. Service support and language clients
are separate from published package availability and native platform qualification.
Current macOS source supplies Program proof over XPC; its Unix-socket path
remains below that policy, and the published 0.2.0 package predates XPC.

## Content references: manifests, holds, inventory

[`content.thrift`](content.thrift) also defines five services for what a
machine holds and who depends on it: `Manifests`
(`abstraction.storage/manifests@1`, `Describe`/`Publish`), `Holds`
(`abstraction.storage/holds@1`, `Hold`/`Release`/`Renew`), `Inventory`
(`abstraction.storage/inventory@1`, `List`/`Holders`/`Unheld`/`Find`),
`ContentRemover` (`abstraction.storage/content-remover@1`, `Remove`) and
`InventorySource` (`abstraction.storage/inventory-source@1`,
`Describe`/`Snapshot`/`Observe`/`Verify`/`Remove`). A `Manifest` names a set of `Entry` objects by role,
digest and locator, content-addressed or name-addressed until a digest
arrives. A `Hold` records one program's declared or observed dependence on a
manifest or digest, carried by a `Holder`. `Dangling` records an index entry
naming content no store holds.

**Served today.** `Inventory` composes designated sources into one read: `go/service`
registers it with `Host.EnableInventory`, the runtime wires it in
`serve/runtime_inference.go`, and the facade resolves it with
`Machine.ResolveStorageInventory`. `InventorySource` is served by the sibling
layer `abstraction-storage-over-local-stores`'s `inventoryd` (not yet published
as its own repository), answering `Describe`, `Snapshot`, `Observe` and
`Verify` for what it reads on one machine; `content.thrift`'s service doc
states the runtime accepts a source only under an
`abstraction.storage/inventory.provide` rule, naming sources that never talk
to applications directly.

**Not built.** Every one of the five services has a generated request/reply
codec and wire-level client in Go, C++, Python, Rust and JavaScript —
`ManifestsClient`, `HoldsClient`, `ContentRemoverClient` among them — starting
in [`go/abstraction/storage/content/rec.go`](go/abstraction/storage/content/rec.go)
and mirrored in `cpp/abstraction/storage/content/rec.h`,
`py/abstraction/storage/content/_codec.py`, `rs/abstraction/storage/content/rec.rs`
and `javascript/js/abstraction/storage/content/index.d.mts`. Past the codec,
none of the three below has a server, a facade resolve method or a rights
catalogue action, and [`CONTRACT.md`](CONTRACT.md) does not cover them:

- **`Manifests`** — no server registers it; the facade has no
  `ResolveStorageManifests`.
- **`Holds`** — no server registers it; the facade has no `ResolveStorageHolds`;
  the rights catalogue carries no `holds.manage` action.
- **`ContentRemover`** — no server registers it; the facade has no resolve
  method for it.

`Manifests`, `Holds` and `ContentRemover` are how the runtime would eventually
expose what `InventorySource` providers report; `Inventory` above is the first
of the five to make that crossing.

## Explicit native providers

The existing provider `Store`, `Local` and `Writable` interfaces
([`go/storage.go`](go/storage.go)) are provider building blocks, with
`NewContentStore` ([`go/content.go`](go/content.go)) and `NewForeignStore`
([`go/foreign.go`](go/foreign.go)) as the shipped constructors composed by
`New(stores...)` ([`go/stores.go`](go/stores.go)). A program deliberately
selecting such a provider owns its filesystem access and lifecycle. The
service composes these providers; normal clients use the content-reader
contract. Existing provider data need not be copied into an application-owned
store to use the service.

Go provider sources require Go 1.26 and the identity module. Install coordinated
source/package revisions as documented by the selected client. Development
package metadata does not establish a registry release. See the source tests and
[coverage](https://github.com/openabstractions/abstractions)
for scoped evidence rather than a blanket cross-language provider verdict.

## Obtain

Install the runtime first: https://openabstractions.org/adopt.html

- **Go.** `go get github.com/openabstractions/abstraction-storage/go`. No
  tagged release; `go get` resolves a pseudo-version of `main`.
- **C++, Python, Rust, JavaScript.** Generated protocol, codec and client
  packages live in this repository under `cpp/`, `py/`, `rs/` (records) and
  `javascript/` (published as `@openabstractions/storage-content`), and native
  providers separately under `rust/`. See the facade packages for the resolved
  clients shown above.

## Today

Content-reader, content-writer and content-changes are implemented, tested
and reachable through the facade in Go, C++, Python and Rust, and through the
generated protocol client in JavaScript. The content-references services are
generated in every language, with `InventorySource` served by a sibling
layer's provider and the other four undeployed, per the section above. The
Go module has a `go/v0.3.0` tag. The tagged source defines its released
service and client surface; this section describes current source. Service
support and language clients are separate from published package availability
and native platform qualification. Current macOS source supplies Program proof over XPC; the
published 0.2.0 package predates that support.

## Requirements

**Go** 1.26 or later ([`go/go.mod`](go/go.mod)), plus
[`abstraction-cas`](https://github.com/openabstractions/abstraction-cas) and
[`abstraction-identity`](https://github.com/openabstractions/abstraction-identity).
**C++, Python, Rust and JavaScript** requirements are those of the resolving
facade package; see [the facade packages](https://github.com/openabstractions/abstraction-facade)
and [`cpp/test/service/README.md`](cpp/test/service/README.md) /
[`py/README.md`](py/README.md) for build and setup.

## Licence

[Apache-2.0](LICENSE)
