# History

Design records for [`CONTRACT.md`](CONTRACT.md), kept out of the rule pages
per `research/vocabulary/DECISION.md` S7.

## 2026-09-23: contract shape

Before this date `CONTRACT.md` stated its rules as continuous prose with no
rule ids, and carried no `Binds:` line naming `content.thrift`. It was
absent from `idl/gen/tags.go`'s `ruleDocuments`, so a test or refusal citing
its promises had nothing to resolve against — the same gap
`research/reviews/providers-2026-09-23.md` F14 found in the sibling
`abstraction-storage-over-local-stores` contract. This page now declares
`STO-R`, `STO-W`, `STO-O` and `STO-I` for its four served profiles, in the
order reader, writer, changes, inventory (S2), with the RFC 8174 boilerplate
stated once.

## 2026-09-23: retention

`research/storage-classes/DECISION.md`, reviewed at
`research/storage-classes/REVIEW.md` ("accept with changes"), added the
`STO-T` rules: four storage classes (`data`, `cache`, `tmp`, `logs`), who
declares a class, where the retention policy lives, and the one collector
that enforces it. `STO-W1` (a committed object is `cache` until a hold names
it) is additive and waits on `holds@1`'s own server; until that server
exists, no committed object is a removal candidate by class, so the
collector's `cache` sweep (`STO-T4`) has nothing to offer. The decision's
own §11 holds the evidence for each of its nine choices, and §12 its review
checklist; both are cited from there rather than duplicated here.

## 2026-10-01: request input

VISION 2026-10-01 ("Content an application uploads as request input is
written and read under a bounded input profile…") added `STO-W11`,
`STO-W12` and `STO-T7`. Before it, an application uploading an edit image,
a mask, a video's first frame or a dictation needed an exact-digest
`content.write` rule and a separate `content.read` rule, which no Panel
button grants, and the object stayed in the store indefinitely (the
2026-10-01 image models and listen reviews). `STO-W12` mirrors the output
profiles on `input:image` and `input:audio`. The wire is unchanged:
`Begin` still names no profile, and the runtime classifies the verified
bytes at publication, which `STO-W11` makes a decision of its own. An
application therefore keeps its existing writer client and changes only
the rules it asks for. `STO-T7` is the first `tmp` kind the runtime itself
removes; the general collector of `STO-T3` to `STO-T5` remains unbuilt.

## 2026-10-01: proof of possession for request input

The review of the request-input change found two faults. A writer holding
only an input profile learned from `present` whether any digest was stored,
and gained no read of an object another application or a generated output
had stored first. The input record was also written before the gateway's
presence check, so the sweep could delete an object an exact-digest write
had stored. Decided by the coordinator: proof of possession. `STO-W6` now
lets a write policy require the bytes (`ErrRequireBytes`); such a writer is
never answered `present`, uploads and verifies every byte, and an existing
object keeps its stored bytes. `STO-W11` tells publication whether the
object already existed. `STO-W12` lets an input record own only bytes its
own write published, and reads input back only for a request of the
recorded profile. `STO-T7` removes owned bytes only, marks every
exact-digest write, keeps everything when a holder or a job record cannot
be read, and states that the runtime's store serves no change journal.

## 2026-10-02: output:video

A video job's clip was written under `output:image`
(`research/live-runs/2026-10-02.md`, phase 2 finding 4). It now has its
own bounded, original-subject output profile, `output:video`, with the same
preflight, commit recheck, provenance and read-back as the others
(inference INF-S7).

## 2026-10-20: locations

The owner asked how OA data is stored per device and per user (2026-10-19).
The storage decision sto-r2, amended by the storage review and recorded as
theme T9 of the 0.8.0 plan (`research/release-0.8.0/PLAN.md` on branch
`v080-architecture`), found OA's folders computed in about eleven places
under five spellings, with provider state, engines and logs in the cache
class on macOS and Linux. It added the `STO-L` rules and the `layout`
package as their one owner. Config's machine trust check moved into
`layout` in the same change. On Windows `STO-L9` sets the owner-only DACL
at creation where T9 says a root inherits the profile's: on the owner's
machine `%TEMP%` passes Modify to a sandbox users group and `%LOCALAPPDATA%`
Full Control to an app capability, so an inherited folder is open to them.

The ST-1 review found that `layout`'s Windows DACL also admitted
Administrators, so a folder it made failed identity `ID-D1`'s check, which
the engine socket root applies. `STO-L9` now takes `ID-D1`'s private folder
as its owner-only folder: the DACL is the account and SYSTEM, and a folder
Administrators own, or one that inherits its parent's entries, is
tightened to that descriptor. The engine socket root keeps
`INF-EL2`'s check of its per-user base (`EnsureSockets`), and a folder
outside the root it is made under is refused with `outside_root`.

A blind implementer, a second model given only the Locations section and
`testdata/locations`, passed all 50 scenarios, but only after six failures
showed it four values the text contradicted or left out: the short root on
macOS and Linux (the table said `D`; there is none), the `e` kind there (the
table said `e/`; it is `D/engines`), the machine service state (called
empty; it is `M`, or `/var/lib/openabstractions` on Linux) and the Windows
machine `upgrade-v1` (named in `M`; it stays beside the 0.4.0 settings
file). It listed seventeen further questions the text did not decide. The
third review round answered every one in the section: the order of the
`STO-L3`, `STO-L4` and `STO-L9` checks, an untrusted machine entry
outranking a missing one, `$HOME` required on Linux whatever the XDG
variables hold, relative `$TMPDIR` and `$XDG_RUNTIME_DIR` counting as unset,
Windows absolute paths, the person's folders and their defaults, which
calls a sandbox or view refuses, the path-bound columns, limits and
elements (`outside_root`), lexical cleaning, classify's names, and the
record kinds' folders. Fifty scenarios were added, one for each reading a
case could decide; six of them fail on the resolver before this round.
`privatedir.Tighten` now writes the folder's own descriptor through a
handle, after the review found that it re-propagated inheritance through a
0.4.0 data root and rewrote a model file's DACL through its job-store name
(identity `ID-D1`, `ID-D3`). The account and SYSTEM stay the one owner-only
descriptor; an upgraded data root loses its Administrators entry. The path
identity check (`VISION.md` L623) also found `layout` folding case and
deciding containment with `filepath.Rel` and `..`; classifying a path and
the walk from a folder up to its root now compare cleaned spellings
exactly, and an existing file's containment is identity's `Within`.

## 2026-10-21: contract truth pass (0.8.0 W0-03)

The pass checked this page and the README against current source
(`research/release-0.8.0/audit/storage.md` §3.4, P0-1). Each change, with
its class:

- The README said a `gap` on `Read` reports changed content. Outdated rule:
  STO-R10 and `content.thrift` answer `changed`, and `gap` is an unknown or
  expired handle (STO-R8). Both read examples now stop on `changed` with a
  distinct error.
- The writer clients for Python and Rust were "planned and unproven", and
  the changes clients were listed for Go and C++ only. Outdated rule: Python
  `Writer` and `Changes` (`py/abstraction/storage/content/client.py`) and
  Rust `resolve_storage_writer` and `resolve_storage_changes`
  (`abstraction-facade/rust-storage/src/lib.rs`) exist.
- `content-changes@1` was described as reachable through the facade on the
  runtime. Accepted divergence: `EnableChanges` serves it for a host that
  calls it (`abstraction-facade/go/runtime/host.go`), and the runtime does
  not. The pages now say so, and "Not built" names the missing artifact:
  the runtime sets no `StorageChangesPolicy`.
- The README's tag statement ("no tagged release" against "`go/v0.3.0`"),
  the `rust/` description and the 0.2.0 XPC sentence. Outdated rule: every
  package carries one version from 0.8.0 (`VISION.md:1230-1247`), `rust/`
  builds generated records only, and macOS Program proof over XPC is in
  source with no recorded transcript.
- The README said `CONTRACT.md` does not cover `Manifests`, `Holds` and
  `ContentRemover`. Outdated rule: they are under "Not built".
- The remote form is stated as "Not built" with the receiver rule of
  `research/release-0.8.0/critique/security.md` item 15.
- `lend@1` refuses with `not_permitted` where `content.thrift` uses
  `forbidden`. Accepted divergence until vocabulary N4 lands on the 0.8.0
  storage window (`research/vocabulary/DECISION.md`).
