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
| [4.3 Update `pflag-integration.md` status](#43-update-pflag-integrationmd-status) | Done | Fixed a real bug found in the same pass: the "Recommended direction" sketch registered `db-host`/`db-port` flags for untagged fields that actually derive to `database-host`/`database-port` — added the missing `cs.pflag` tags. |
| [4.4 Update `populate-error-handling.md` status](#44-update-populate-error-handlingmd-status) | Done | Done early: `populate-error-handling.md` was rewritten into a settled design-rationale document (and its implementation-plan sibling, `pflag-plan-phase-0-error-handling.md`, retired) once Phase 0 shipped, rather than waiting for Phase 4 |
| [4.5 Update `README.md`](#45-update-readmemd) | Done | Added a `### PFlag` section under Built-in backends; removed the now-false "Command-line flags... confstruct does not provide these" row from Other backend shapes. |
| [4.6 Release notes](#46-release-notes) | Done | Decision recorded below: deferred to whoever cuts the next tagged release, not part of this phase's own deliverable. |

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

**Fixed a bug in the reference sketch before implementing this:** the
sketch registered `db-host`/`db-port` flags for `Database.Host`/
`Database.Port` fields with no `cs.pflag` tag. Untagged, those fields
actually derive to `database-host`/`database-port` (confirmed against the
real, tested derivation code and the doc's own [Mapping flag names to
fields](pflag-integration.md#mapping-flag-names-to-fields) table) — so as
originally written, the sketch's CLI override would have silently never
matched, since only `db-host`/`db-port` were ever registered. The sketch
now tags those fields `cs.pflag:"db-host"`/`cs.pflag:"db-port"`, which also
means the real example gets to demonstrate the tag-override feature for
free. Carry the tags through when writing `example/pflag/main.go`.

## 4.2 Optional cobra example

A second example demonstrating the per-subcommand `cobra` pattern from
[Worked
example](pflag-integration.md#worked-example-per-subcommand-meta-plus-a-shared-globalconfig)
— lower priority than the single-`Populate` example; only add it if the
duplicate-detection scoping
([Phase 2](pflag-plan-phase-2-duplicate-detection.md)) needs a runnable
demonstration beyond its unit tests.

## 4.3 Update `pflag-integration.md` status

Done. The status line originally said to wait "once Phase 3 merges," but
by the time this was revisited, Phases 1-3 were already fully implemented
on a single long-running branch with no intermediate per-phase merges to
`main` — so waiting for a literal merge event would have left the doc
stale relative to the code that already existed. Updated the [Status
line](pflag-integration.md#status) to "Implemented" now, matching reality,
rather than holding it for a git event this plan's actual workflow doesn't
produce. The former "Exploration only... does not commit the public API or
add pflag as a dependency" text is gone; the doc now points to
`pflag-implementation-plan.md` for the phase history and describes itself
as the design-rationale reference for the shipped backend.

## 4.4 Update `populate-error-handling.md` status

Done early, ahead of this phase: once Phase 0 merged,
[`populate-error-handling.md`](populate-error-handling.md) was rewritten
from a "working draft" proposal into a settled design-rationale document
(status: Implemented), with the corresponding user-facing contract added to
the README's [Error handling](../README.md#error-handling) section. The
implementation-plan sibling document, `pflag-plan-phase-0-error-handling.md`
(a step-by-step checklist with no content not already better expressed in
code, tests, and the rationale doc), was retired at the same time.

## 4.5 Update `README.md`

Done. Original phase scope (4.1-4.4) never mentioned the README at all,
even though `PFlag` is a full new public backend and every other built-in
backend (`Map`, `Override`, `MapFromTags`, `Env`, `File`) has its own
documented section there. Two changes:

- Added a `### PFlag` section under [Built-in
  backends](../README.md#built-in-backends), matching the depth of the
  `Env`/`File` sections: the import-alias requirement, the
  presence-not-defaults semantics, name derivation plus the `cs.pflag`
  tag, and the static/non-watchable note.
- Removed the now-false row from [Other backend
  shapes](../README.md#other-backend-shapes) — that table's whole premise
  is "confstruct does not provide these," and it listed "Command-line
  flags | Static | `--port 8080`" as an example, which stopped being true
  the moment `PFlag` shipped.

## 4.6 Release notes

**Decided:** cutting a `docs/releases/release-vX.Y.Z.md` entry (matching
the existing `release-v0.1.0.md`/`release-v0.2.0.md` convention) for the
version that ships `pflag` is out of scope for this phase's own
deliverable. This plan's phases describe implementing the backend, not
the separate act of tagging and releasing a version — Phase 0's
`populate-error-handling.md` work landed the same way, as a merged
change on the working branch, without this plan prescribing when or how
a release gets cut around it. Whoever cuts the next tagged release should
write that entry then, using `release-v0.2.0.md` as the template and
covering: the new `github.com/suyono3484/confstruct/pflag` package, the
`cs.pflag` tag, and the direct `spf13/pflag` dependency addition.

This is the last phase. See [the plan
index](pflag-implementation-plan.md#open-follow-ups) for follow-up work
explicitly deferred beyond all four phases.
