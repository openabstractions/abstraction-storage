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
