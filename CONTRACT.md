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

Writable service semantics, cross-account content grants, public release pins,
remote transports and macOS native Program proof remain separate obligations.
