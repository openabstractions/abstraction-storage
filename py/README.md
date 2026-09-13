# Content service client

Install the current abstraction-storage-content package with the facade storage
extra and shared IPC package. `Machine().resolve_storage(scope="local")` selects
one authorized content reader; absence and authorization refusals are explicit.
`Open`, `Read`, `Close` use generated request/reply codecs. `Copy(resource, writer)`
streams at most 64 KiB per request under one total wait budget and reports partial
confirmed writes through CopyError. A cancelled wait leaves the resource open;
Close remains explicit and is permitted after authorization revocation.

Resources contain opaque handles, naming digests and observed sizes. Content is
unverified; verify the assembled digest. A gap requires explicit reopen. The
client reads no provider files and performs no automatic fallback. The legacy
storage provider module remains an explicit independent provider API.

Default individual calls have fresh transport timeouts. An explicit absolute
deadline is retained. `with_waiting` selects a fresh waiting policy and replaces
cancellation, while keeping the selected endpoint. Python currently uses the
shared native library; no Python platform pipe/socket implementation is added.
