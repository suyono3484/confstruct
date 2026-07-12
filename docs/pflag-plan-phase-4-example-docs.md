# Phase 4 — Example and docs

Part of [the `pflag` implementation plan](pflag-implementation-plan.md).
Previous: [Phase 3 — `pflagBackend` core](pflag-plan-phase-3-backend.md).
Next: none — this is the last phase.

**Package layout (decided):** the backend is `github.com/suyono3484/confstruct/pflag`,
a separate package from `confstruct` (see
[pflag-integration.md#package-layout](pflag-integration.md#package-layout)).
The example app in [4.1](#41-example-app) must import both packages, and
since both this project's new package and `github.com/spf13/pflag` are
named `pflag`, the example needs an import alias for one of them (see the
aliased example in
[pflag-integration.md](pflag-integration.md#recommended-direction)) —
worth calling out in the example's own comments, since a reader copying it
verbatim will hit the collision immediately.

## Tracker

| Step | Status | Notes |
| --- | --- | --- |
| [4.1 Example app](#41-example-app) | Not started | |
| [4.2 Optional cobra example](#42-optional-cobra-example) | Not started | |
| [4.3 Update `pflag-integration.md` status](#43-update-pflag-integrationmd-status) | Not started | |
| [4.4 Update `populate-error-handling.md` status](#44-update-populate-error-handlingmd-status) | Done | Done early: `populate-error-handling.md` was rewritten into a settled design-rationale document (and its implementation-plan sibling, `pflag-plan-phase-0-error-handling.md`, retired) once Phase 0 shipped, rather than waiting for Phase 4 |

Status values: `Not started`, `In progress`, `Done`.

## 4.1 Example app

`example/pflag/main.go`, gated behind the `example` build tag exactly like
the existing examples (check `example/map` for the tag comment and
package layout). Should reproduce the `main.go` sketch from [Recommended
direction](pflag-integration.md#recommended-direction) closely enough
that a reader can copy it, plus a comment showing an unset flag falling
through to a lower layer. Import both `github.com/suyono3484/confstruct`
and the new `github.com/suyono3484/confstruct/pflag` package with an
explicit alias (e.g. `cspflag`) alongside `github.com/spf13/pflag`, and add
a short comment noting *why* the alias is there — the two packages sharing
the name `pflag` is exactly the kind of thing a reader copying the example
will trip over silently otherwise.

## 4.2 Optional cobra example

A second example demonstrating the per-subcommand `cobra` pattern from
[Worked
example](pflag-integration.md#worked-example-per-subcommand-meta-plus-a-shared-globalconfig)
— lower priority than the single-`Populate` example; only add it if the
duplicate-detection scoping
([Phase 2](pflag-plan-phase-2-duplicate-detection.md)) needs a runnable
demonstration beyond its unit tests.

## 4.3 Update `pflag-integration.md` status

Update `pflag-integration.md`'s [Status](pflag-integration.md#status) line
once [Phase 3](pflag-plan-phase-3-backend.md) merges — it currently reads
"Exploration only... does not commit the public API or add pflag as a
dependency," which stops being true the moment `go.mod` gains the
dependency.

## 4.4 Update `populate-error-handling.md` status

Done early, ahead of this phase: once Phase 0 merged,
[`populate-error-handling.md`](populate-error-handling.md) was rewritten
from a "working draft" proposal into a settled design-rationale document
(status: Implemented), with the corresponding user-facing contract added to
the README's [Error handling](../README.md#error-handling) section. The
implementation-plan sibling document, `pflag-plan-phase-0-error-handling.md`
(a step-by-step checklist with no content not already better expressed in
code, tests, and the rationale doc), was retired at the same time.

This is the last phase. See [the plan
index](pflag-implementation-plan.md#open-follow-ups) for follow-up work
explicitly deferred beyond all four phases.
