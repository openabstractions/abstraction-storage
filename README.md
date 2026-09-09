# abstraction-storage

Bytes at rest are addressed by the sha256 digest of the bytes themselves, so
whether a machine already holds some content is one question with one answer
across every tool that stores content by name.

## The problem

Several AI tools keep model caches on the same disk, and two common ones —
Ollama and the HuggingFace hub cache — already name files by sha256. Nothing
reads across them, so a machine can hold the same weights twice under different
names, and an application about to download something cannot ask whether those
exact bytes are already here. The write side has the matching gap: an
application that picks its own destination spells `output + ".partial"` at every
call site, then needs more code to scan for, delete and skip those files. This
layer names both operations — `Find` and `Place` — and returns a `Ref` that
carries no path a caller can concatenate.

## Words

| word | meaning |
|---|---|
| **digest** | `sha256:<hex>` of the bytes; the only name a store answers to |
| **store** | something that can answer `Find(digest)` and `Place(digest, size)` |
| **ref** | what a store hands back: `Store`, `Digest`, `Size`, and nothing a caller can turn into a path |
| **place** | a reservation for bytes not yet here; it becomes findable only after `Commit` |
| **foreign store** | another tool's content-addressed directory, read but never written |

No rule on this page carries a tag, and no conformance scenario cites this
layer.

## Obtain

- **Go.** `go get github.com/openabstractions/abstraction-storage/go`. The
  module path ends in `/go`; the package is `storage`, so import it with an
  explicit alias. The newest tag is `go/v0.1.0`; `@main` is the tree as it
  stands.
- **Python.** None.
- **C++.** With CMake `FetchContent`, pinning a commit — the on-disk layout is
  a contract shared with the Go side, and a branch can move under an adopter:
```cmake
include(FetchContent)
FetchContent_Declare(abstraction_storage
    GIT_REPOSITORY https://github.com/openabstractions/abstraction-storage.git
    GIT_TAG        <a commit sha>
    GIT_SHALLOW    TRUE
)
FetchContent_MakeAvailable(abstraction_storage)
target_link_libraries(your_target PRIVATE abstraction::storage)
```

## Example

```go
package main

import (
	"fmt"
	"os"
	"path/filepath"

	storage "github.com/openabstractions/abstraction-storage/go"
)

func main() {
	root := filepath.Join(os.TempDir(), "storage-example")
	defer os.RemoveAll(root)

	// NewContent creates the store's directories if they are missing.
	content, err := storage.NewContent("local", root)
	if err != nil {
		panic(err)
	}

	// The sha256 of the three bytes "foo".
	const digest = "sha256:2c26b46b68ffc68ff99b453c1d30413413422d706483bfa0f98a5e886266e7ae"

	// Place reserves a location and writes nothing. Whoever moves the bytes --
	// possibly another process, later -- asks the store where they go.
	ref, err := content.Place(digest, 3)
	if err != nil {
		panic(err)
	}
	if err := os.WriteFile(content.Path(ref), []byte("foo"), 0o644); err != nil {
		panic(err)
	}

	_, found := content.Find(digest)
	fmt.Println("findable before commit:", found)

	if err := content.Commit(ref); err != nil {
		panic(err)
	}
	got, found := content.Find(digest)
	fmt.Println("findable after commit:", found)
	fmt.Println("store:", got.Store, "size:", got.Size)
}
```

Output:

```
findable before commit: false
findable after commit: true
store: local size: 3
```

## API overview

**Go.** `Store` is `Name() string`, `Find(digest string) (Ref, bool)`,
`Place(digest string, size int64) (Ref, error)`. `Find` hashes nothing; it
answers from the store's own naming convention, so a hit is a claim the caller
should still verify. `Place` performs no I/O on the bytes, and a reservation
becomes findable only after `Commit`. Two optional capabilities, reached by type
assertion: `Local` adds `Path(Ref) string`, `Writable` adds `Commit(Ref) error`.
`Ref` exposes `Store`, `Digest` and `Size` and nothing else; `Locator(Ref)` and
`NewRef(store, digest, locator, size)` are for code implementing a `Store`.

`NewContent(name, root)` is writable, laid out as `<root>/blobs/sha256-<hex>`
and `<root>/incoming/sha256-<hex>`, and has `Root()`. `NewForeign(name, dir,
prefix)` reads another tool's content-addressed directory and returns
`ErrReadOnly` from `Place`. `Discover()` returns the foreign stores found under
the user's home directory: Ollama's blobs, and each HuggingFace and Lemonade hub
repository. `New(stores...)` returns `*Stores`, which searches several in the
caller's order, satisfies all three interfaces, and adds `Add`, `FindAll(digest)
[]Ref`, `Names()` and `Len()`. `ErrReadOnly` and `ErrNotFound` are wrapped, so
test with `errors.Is`.

**C++.** Header `abstraction/storage/storage.h`, namespace
`abstraction::storage`, CMake target `abstraction::storage`. It covers the write
half only: `Ref`, `Store`, `Local`, `Writable`, `Content`, `make_ref`,
`locator_of`, `normal_digest`, and the exceptions `StorageError`, `ReadOnly` and
`NotFound`. `Local` here has one method the Go side does not need,
`incoming_path(const Ref&)`, the only supported way to name a location for bytes
still arriving.

## Today

Experimental, version 0.1.0, not yet used outside this organisation.

- **Go**: the whole interface; consumed by `abstraction-model`.
- **C++**: the write half. `Foreign` and `Discover` exist only in Go, so C++ can
  write a content-addressed store but cannot read anyone else's. No adopter.
- `Discover` looks only at fixed paths under the user's home directory. Caches
  moved elsewhere are not found, and a store that publishes no digest cannot
  participate at all.
- Nothing garbage-collects `incoming/`; an abandoned reservation stays there.

## Conformance

None across languages: the two implementations agree about the on-disk layout
by inspection, not by anything that runs. 7 Go tests and one C++ test
executable, run by the commands under Requirements.

## Where it sits

Below: nothing of ours. Above:
[abstraction-download](https://github.com/openabstractions/abstraction-download)
asks it whether the bytes are already here,
[abstraction-model](https://github.com/openabstractions/abstraction-model)
adds local copies as sources, and
[abstraction-facade](https://github.com/openabstractions/abstraction-facade)
hands a program the machine's stores.

One layer of [openabstractions](https://github.com/openabstractions/abstractions).
Every layer names one thing local tools rebuild on their own; the name means the
same in each language that implements it, and the conformance scenarios are what
hold an implementation to it.

## Requirements

Go 1.26 or newer. C++17 and CMake 3.16 or newer. No third-party dependencies in
either binding. Tested on Windows and Linux.

```bash
(cd go && go test ./...)
cmake -S . -B build && cmake --build build && ctest --test-dir build
```

## Licence

Apache-2.0. See [LICENSE](LICENSE).
