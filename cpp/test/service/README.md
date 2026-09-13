# Installed C++ content reader fixture

Build/install abstraction-storage/cpp into a temporary prefix with
`ABSTRACTION_STORAGE_BUILD_SERVICE=ON` and
`ABSTRACTION_STORAGE_BUILD_LEGACY=OFF`. Configure this external consumer with that
prefix using CMAKE_PREFIX_PATH, then build Release. Its sole linked capability
is abstraction::storage_client and shared IPC. `--help` prints consumer usage.

Set OA_CPP_STORAGE_PROBE to the absolute built executable and run:

```sh
go test -race -count=1 ./openabstractions-flat/abstraction-storage/go/...
```

TestFramedStorageAndInstalledCPP starts an isolated Go service backed by a
configured temporary ForeignStore and explicit digest policy. The C++ process
reads bounded chunks, checks exact bytes and the unverified claim, closes and
observes gap, and exercises cancellation/deadlines. A second executable path is
refused use/release of the first executable's resource. The original executable
can release after content policy revocation. Reopening the service makes old
handles gap. Neither process obtains a provider path through the API.

Without OA_CPP_STORAGE_PROBE, the same Go test covers native framed reads and
policy denial but records that the C++ branch was not supplied. Unit tests cover
limits, expiry, mutation, restart, shutdown, policy ordering and client response
validation; Linux additionally exercises FIFO refusal. No installed owner store
or service is used.
