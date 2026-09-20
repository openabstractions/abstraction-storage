# abstraction-storage

Read or write shared content by its verified identity while the storage service
keeps backend paths and file permissions private. A client opens content by its
canonical identifier, transfers bounded chunks and closes its own resource. The
same application API can sit over a local store or another authorized provider.

Choose the profile that matches the application:

| Need | Contract |
| --- | --- |
| Read bytes by canonical digest | `abstraction.storage/content-reader@1` |
| Store bytes atomically under their SHA-256 digest | `abstraction.storage/content-writer@1` |
| Observe the latest content inventory and changes | `abstraction.storage/content-changes@1` |

Each profile is resolved independently through the facade. Rights are checked
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
macOS local Program proof remains unavailable.

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

**Codecs generated, no application-facing server yet.** Every one of the five
services has a generated request/reply codec and wire-level client in Go, C++,
Python, Rust and JavaScript — `ManifestsClient`, `HoldsClient`,
`InventoryClient`, `ContentRemoverClient`, `InventorySourceClient` — starting
in [`go/abstraction/storage/content/rec.go`](go/abstraction/storage/content/rec.go)
and mirrored in `cpp/abstraction/storage/content/rec.h`,
`py/abstraction/storage/content/_codec.py`, `rs/abstraction/storage/content/rec.rs`
and `javascript/js/abstraction/storage/content/index.d.mts`. `go/service`, this
repository's only server, registers exactly `content-reader@1`,
`content-writer@1` and `content-changes@1`; nothing elsewhere in the tree
implements `Manifests`, `Holds`, `Inventory` or `ContentRemover`, and
[`CONTRACT.md`](CONTRACT.md) does not cover them. The facade has no
`ResolveStorageManifests`, `ResolveStorageHolds` or `ResolveStorageInventory`,
and the rights catalogue carries no `holds.manage`, `inventory.read` or
`inventory.provide` action. These four are declared wire contracts today.

`InventorySource` is served today, by the sibling layer
`abstraction-storage-over-local-stores`'s `inventoryd` (not yet published as
its own repository), which answers `Describe`, `Snapshot`, `Observe` and `Verify` for
what it reads on one machine. `content.thrift`'s service doc states the
runtime accepts a source only under an `abstraction.storage/inventory.provide`
rule, naming sources that never talk to applications. That acceptance path is
a design statement in the contract text. Nothing under `serve/` in the parent
project registers a source, reads an `--inventory-source` flag or evaluates
an `inventory.provide` rule. `Manifests`, `Holds`, `Inventory` and
`ContentRemover` are how the runtime would eventually expose what
`InventorySource` providers report.

## Explicit native providers

The existing `Store`, `Local` and `Writable` interfaces
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
generated protocol client in JavaScript. The content-references profiles are
generated in every language, with `InventorySource` served by a sibling
layer's provider and the other four undeployed, per the section above. No
tagged release exists for this repository. Service support and language
clients are separate from published package availability and native platform
qualification. macOS local Program proof remains unavailable.

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
