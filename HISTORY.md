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
