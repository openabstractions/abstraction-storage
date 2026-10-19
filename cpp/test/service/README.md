# Installed C++ content reader fixture

*For maintainers running the C++ storage tests.*

Build/install abstraction-storage/cpp into a temporary prefix with its default
service-client configuration. Configure this external consumer with that
prefix using CMAKE_PREFIX_PATH, then build Release. The fixture requires the
native package at the release's major.minor and refuses retired provider
artifacts in the prefix. Its sole linked capability
is abstraction::storage_client and shared IPC. `--help` prints consumer usage.

The default prefix contains the service protocol/client and shared IPC, and
omits the retired direct `ContentStore` provider. `STORAGE_CONTENT_REQUIRED_VERSION=0.1`
must refuse this prefix; `ABSTRACTION_STORAGE_BUILD_LEGACY=ON` must refuse
producer configuration with the service-client migration diagnostic. Explicit
legacy OFF input remains accepted. Immutable 0.1 sources remain in history. A missing
IPC package or sibling source refuses configuration with an explicit diagnostic;
nothing is fetched. An unavailable service fails through native transport and
does not open a local content store.

Set OA_CPP_STORAGE_PROBE to the absolute built executable and run:

```sh
go test -race -count=1 ./openabstractions-flat/abstraction-storage/go/...
```

TestFramedStorageAndInstalledCPP starts an isolated Go service backed by a
configured temporary ForeignStore and explicit digest policy. The C++ process
reads bounded chunks, checks exact bytes and the unverified claim, closes and
observes gap, and exercises cancellation/deadlines. Malformed payload presence
is checked through the generated RPC client, while the existing native wrapper
retains chunk correlation checks. A second executable path is
refused use/release of the first executable's resource. The original executable
can release after content policy revocation. Reopening the service makes old
handles gap. Neither process obtains a provider path through the API.

Without OA_CPP_STORAGE_PROBE, the same Go test covers native framed reads and
policy denial but records that the C++ branch was not supplied. Unit tests cover
limits, expiry, mutation, restart, shutdown, policy ordering and client response
validation; Linux additionally exercises FIFO refusal. No installed owner store
or service is used.
