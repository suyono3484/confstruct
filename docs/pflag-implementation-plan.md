# Implementation plan: `pflag` backend

## Status

Actionable plan. Turns the decisions recorded in
[`pflag-integration.md`](pflag-integration.md) and
[`populate-error-handling.md`](populate-error-handling.md) into an ordered
set of code changes. Where those two documents leave a mechanism unspecified
— notably, how `Populate` gets whole-struct visibility for duplicate flag
name detection — this plan proposes one (see
[Phase 2](pflag-plan-phase-2-duplicate-detection.md)) and flags it as new
design surface, not something already decided.

Nothing in this plan should be implemented out of order: Phase 0 changes a
shared code path (`entry[T].setSlot`, `walkAndInject`) that every existing
backend already exercises, so it needs its own PR, its own test run, and a
green `go test github.com/suyono3484/confstruct` before any `pflag.go` line
is written.

Each phase below is its own document, sized for a separate PR with its own
tests. Phase 0 is a pure bugfix to existing behavior and should land and be
tagged independently of whether the `pflag` backend ships immediately after.

## Package layout

Decided: the `pflag` backend ships in its own package,
`github.com/suyono3484/confstruct/pflag`, rather than as another file in the
root `confstruct` package alongside `env.go`/`file.go`/`map.go`/`override.go`.
Application code ends up importing both packages, e.g.:

```go
import (
    "github.com/suyono3484/confstruct"
    cspflag "github.com/suyono3484/confstruct/pflag"
    "github.com/spf13/pflag"
)
```

The alias above is required in practice, not just style: the new package and
`github.com/spf13/pflag` both are named `pflag`, so any file importing both
needs to alias one of them.

This decision has a direct consequence for Phases 2 and 3 as currently
drafted: both assume `pflagBackend` lives inside package `confstruct` and
satisfies the unexported `fieldAwareBackend` and (proposed)
`nameCollisionBackend` interfaces. Go requires an unexported interface
method to be declared in the *same* package as the implementing type for
the method sets to match, so a `pflagBackend` type living in the new
`pflag` package cannot satisfy either interface as drafted today.

**Decided:** resolve this with an embeddable "seal" adapter exported from
`confstruct` per hook (`FieldLookupSeal`, `NameCollisionSeal`), rather than
exporting `fieldAwareBackend`/`nameCollisionBackend` themselves. A plain
exported interface was considered and rejected — it would make "any
backend can veto a `Populate` call" a public, third-party-implementable
capability, which is more generic than this library otherwise exposes. See
[pflag-integration.md#cross-package-hook-mechanism-decided](pflag-integration.md#cross-package-hook-mechanism-decided)
for the full mechanism and rationale. Each affected phase document below
has been updated to sketch against this mechanism instead of the plain
unexported interfaces.

## Phases

| Phase | Change | Touches existing backends? | Depends on |
| --- | --- | --- | --- |
| [0 — `Populate` error handling](populate-error-handling.md) | `Populate` reports initial coercion failures | Yes — `Map`, `File`, `Env`, `Override` all flow through `setSlot` | none |
| [1 — name conversion](pflag-plan-phase-1-name-conversion.md) | Identifier → flag-name conversion helper + `cs.pflag` tag grammar | No (new file, no exported surface yet) | none |
| [2 — duplicate-name validation](pflag-plan-phase-2-duplicate-detection.md) | Struct-wide duplicate-name validation mechanism in `Populate` | Adds one new optional interface; existing backends unaffected because none will implement it | Phase 1 |
| [3 — `pflagBackend` core](pflag-plan-phase-3-backend.md) | `pflagBackend` itself (`Lookup`, `lookupField`, `Name`, `Describe`, constructor) | No | Phases 1–2 |
| [4 — example and docs](pflag-plan-phase-4-example-docs.md) | Example app + doc updates | No | Phase 3 |

Phase 0 has shipped; its implementation notes are folded into
[populate-error-handling.md](populate-error-handling.md), now a settled
design-rationale document rather than a plan. Start at [Phase
1](pflag-plan-phase-1-name-conversion.md) and follow the "Next" link at the
bottom of each phase document in order.

## Tracker

Update this table as work lands — one row per phase, checked off only once
its own exit criteria (tests green, doc status line updated where
applicable) are met, not when a PR merely opens.

| Phase | Status | PR | Notes |
| --- | --- | --- | --- |
| [0 — `Populate` error handling](populate-error-handling.md) | Done | `add_pflag_phase0` branch, 3 review passes | Implementation plan doc retired; behavior and rationale now live in `populate-error-handling.md` and the README's [Error handling](../README.md#error-handling) section |
| [1 — name conversion](pflag-plan-phase-1-name-conversion.md) | Done | `add_pflag_phase1` branch | Code review found one issue (non-ASCII/underscore identifiers silently bypassing the ASCII-only contract); fixed before landing — see [pflag-plan-phase-1-code-review.md](pflag-plan-phase-1-code-review.md). |
| [2 — duplicate-name validation](pflag-plan-phase-2-duplicate-detection.md) | Done | `add_pflag_phase1` branch | The `checkFieldNames` algorithm shipped as a standalone function ahead of `pflagBackend` (which doesn't exist until Phase 3) — see [pflag-plan-phase-2-duplicate-detection.md](pflag-plan-phase-2-duplicate-detection.md) for the sequencing decision. |
| [3 — `pflagBackend` core](pflag-plan-phase-3-backend.md) | Done | `add_pflag_phase1` branch | `github.com/spf13/pflag` added as a direct dependency; `pflag/pflag.go` and `pflag/pflag_test.go` implement and cover the full backend. |
| [4 — example and docs](pflag-plan-phase-4-example-docs.md) | Done | `add_pflag_phase1` branch | `example/pflag`, README and `pflag-integration.md` status updates. The optional cobra example (4.2) was skipped — no coverage gap it would have filled. This is the last phase; the `pflag` backend is fully implemented. |

Status values: `Not started`, `In progress`, `Done`.

## Open follow-ups

Explicitly not part of this plan, called out so they aren't mistaken for
gaps in it:

- Unifying `walkAndInject`, `collectUnset`, and the new `collectFieldPaths`
  ([Phase 2](pflag-plan-phase-2-duplicate-detection.md#22-collecting-fieldpath-before-the-value-walk))
  into one traversal-with-callback helper. All three now walk the same
  tree shape (skip `Meta`, recurse structs, reject unexported entry
  fields). Worth doing once there's a third real consumer justifying the
  abstraction — this plan is that third consumer, but the refactor itself
  is separable from shipping `pflag` and would make this already-large diff
  harder to review. Now tracked as its own actionable item, with a
  concrete suggested design, in
  [docs/deferred/unify-struct-tree-traversal.md](deferred/unify-struct-tree-traversal.md) —
  this bullet only records *that* it's out of scope for this plan, not the
  full follow-up.
- Everything under [Explicit
  non-goals](pflag-integration.md#explicit-non-goals) in the source
  doc — no key registry, no reads by flag/path string, no write-back to the
  `FlagSet`, no auto-generated flag definitions, no `WatchableBackend`, no
  new pflag value types beyond the existing entry type set. None of that
  changes as part of this plan.
