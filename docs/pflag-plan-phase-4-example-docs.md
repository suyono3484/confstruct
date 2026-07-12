# Phase 4 — Example and docs

Part of [the `pflag` implementation plan](pflag-implementation-plan.md).
Previous: [Phase 3 — `pflagBackend` core](pflag-plan-phase-3-backend.md).
Next: none — this is the last phase.

## Tracker

| Step | Status | Notes |
| --- | --- | --- |
| [4.1 Example app](#41-example-app) | Not started | |
| [4.2 Optional cobra example](#42-optional-cobra-example) | Not started | |
| [4.3 Update `pflag-integration.md` status](#43-update-pflag-integrationmd-status) | Not started | |
| [4.4 Update `populate-error-handling.md` status](#44-update-populate-error-handlingmd-status) | Not started | |

Status values: `Not started`, `In progress`, `Done`.

## 4.1 Example app

`example/pflag/main.go`, gated behind the `example` build tag exactly like
the existing examples (check `example/map` for the tag comment and
package layout). Should reproduce the `main.go` sketch from [Recommended
direction](pflag-integration.md#recommended-direction) closely enough
that a reader can copy it, plus a comment showing an unset flag falling
through to a lower layer.

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

Update `populate-error-handling.md`'s Status line the same way once
[Phase 0](pflag-plan-phase-0-error-handling.md) merges.

This is the last phase. See [the plan
index](pflag-implementation-plan.md#open-follow-ups) for follow-up work
explicitly deferred beyond all four phases.
