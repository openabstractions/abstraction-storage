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
