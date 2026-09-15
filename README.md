# abstraction-storage

Applications obtain content through an authorized service reader. The provider
owns backend paths and file access. A client opens a canonical content identifier,
reads bounded chunks and closes its caller-scoped resource; no provider path is
returned as a file handle for the application to open.

## Application clients

Select `abstraction.storage/content-reader@1` through the facade and use its typed storage
client. Current Go, C++, Python and Rust clients use the shared IPC boundary.
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
`facade::ResolveStorageWriter`. Create a request identity with
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

Python, Rust and JavaScript resolve the same contract. Python
`Machine().resolve_storage_writer(scope="local")` returns a writer whose `Write`
raises `OutcomeError` for other outcomes. Rust
`StorageMachine::resolve_storage_writer(vec![], "local")` returns a writer whose
`write` returns `Error::Outcome`. Both resume by request identity and never
abort.

```python
import hashlib
from abstraction.facade.client import Machine
from abstraction.storage.content.client import OutcomeError, new_request_id

writer = Machine().resolve_storage_writer(scope="local")
request = new_request_id()  # retain before writing
digest = "sha256:" + hashlib.sha256(data).hexdigest()
try:
    stored = writer.Write(request, digest, data)
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
`@openabstractions/storage-content` and makes the calls itself. `Begin` again
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
const begun = await writer.Begin(request, digest, BigInt(data.length));
if (begun.outcome === 'started') {
  for (let offset = Number(begun.upload.received); offset < data.length;) {
    const part = data.subarray(offset, offset + 65536);
    const appended = await writer.Append(begun.upload.handle, BigInt(offset), part);
    if (appended.outcome !== 'accepted' && appended.outcome !== 'out_of_order') throw new Error('append ' + appended.outcome);
    offset = Number(appended.received);
  }
  const committed = await writer.Commit(begun.upload.handle);
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

## Explicit native providers

The existing `Store`, `Local`, `Writable`, `NewContent` and foreign-store adapters
are provider building blocks. A program deliberately selecting such a provider
owns its filesystem access and lifecycle. The service composes these providers;
normal clients use the content-reader contract. Existing provider data need not
be copied into an application-owned store to use the service.

Go provider sources require Go 1.26 and the identity module. Install coordinated
source/package revisions as documented by the selected client. Development
package metadata does not establish a registry release. See the source tests and
[coverage](https://github.com/openabstractions/abstractions)
for scoped evidence rather than a blanket cross-language provider verdict.

[Apache-2.0](LICENSE)
