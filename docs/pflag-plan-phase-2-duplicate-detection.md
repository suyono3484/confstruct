# Phase 2 — Struct-wide duplicate-name validation

Part of [the `pflag` implementation plan](pflag-implementation-plan.md).
Previous: [Phase 1 — identifier-to-flag-name
conversion](pflag-plan-phase-1-name-conversion.md). Next:
[Phase 3 — `pflagBackend` core](pflag-plan-phase-3-backend.md).

Source: [Duplicate flag name
detection](pflag-integration.md#duplicate-flag-name-detection). **This is
the one piece of mechanism the source docs describe by requirement
("`Populate` must reject it... structural... before any `flags.Lookup`
call") without specifying how `Populate` gets whole-struct visibility.**
Today, `walkAndInject` discovers one field at a time and immediately looks
it up against every backend in the same recursive step — there is no
existing pass that first collects every entry field's path and tag, across
the whole tree, before backends are consulted.

**Package layout (decided):** the `pflag` backend now lives in its own
package, `github.com/suyono3484/confstruct/pflag`, not in package
`confstruct` (see
[pflag-integration.md#package-layout](pflag-integration.md#package-layout)).
`nameCollisionBackend` as a plain *unexported* interface in `confstruct.go`
cannot be satisfied by `pflagBackend` from another package — Go requires an
unexported interface method to be declared in the same package as the
interface. **Decided:** this is resolved with an exported
`NameCollisionSeal` type that `pflagBackend` embeds — see
[pflag-integration.md#cross-package-hook-mechanism-decided](pflag-integration.md#cross-package-hook-mechanism-decided)
for the full mechanism and why a plain exported interface was rejected in
favor of it. [2.1](#21-new-optional-backend-interface) and
[2.4](#24-field-name-collision-detection-checkfieldnames) below are updated
to sketch against that mechanism; the traversal shape, scoping rules, and
test matrix elsewhere in this phase are unaffected.

**Decided — sequencing: 2.4 ships as a free function, not a `pflagBackend`
method.** `pflagBackend` the type doesn't exist until [Phase
3](pflag-plan-phase-3-backend.md), which the [top-level
plan](pflag-implementation-plan.md) lists as *depending on* this phase —
so a `CheckFieldNames` sketched as `func (b *pflagBackend) ...` can't
actually compile or be tested until Phase 3 also lands, which would leave
this phase permanently unable to reach "Done" on its own. Instead, [2.4](#24-field-name-collision-detection-checkfieldnames)
implements the whole algorithm as a standalone function, `checkFieldNames(entries
[]confstruct.FieldPath) error`, in a new file `pflag/pflag_collision.go`
with its own `pflag/pflag_collision_test.go` — fully testable with no
`pflagBackend`, `PFlag`, or `*pflag.FlagSet` involved at all. Phase 3 then
adds `pflagBackend` and wires this in with a one-line delegate:
`func (b *pflagBackend) CheckFieldNames(entries []confstruct.FieldPath)
error { return checkFieldNames(entries) }`. This also means the
`pflagBackendErr` helper (see [2.4](#24-field-name-collision-detection-checkfieldnames))
takes no backend receiver — it doesn't need one, since `pflagBackend.Name()`
always returns the fixed constant `PFlagBackendName`.

## Tracker

| Step | Status | Notes |
| --- | --- | --- |
| [2.1 New optional `Backend` interface](#21-new-optional-backend-interface) | Done | `FieldPath`, `nameCollisionBackend`, `NameCollisionChecker`, `NameCollisionSeal` added to `confstruct.go`, including the nil-`impl` guard. |
| [2.2 Collecting `[]FieldPath`](#22-collecting-fieldpath-before-the-value-walk) | Done | `collectFieldPaths` added to `confstruct.go`. |
| [2.3 Wiring in `Populate`](#23-wiring-in-populate-confstructgo425-465) | Done | Inserted after the lowest-layer-watchable check, before `watchCtx` creation, as sketched. |
| [2.4 Field-name-collision detection (`checkFieldNames`)](#24-field-name-collision-detection-checkfieldnames) | Done | `pflag/pflag_collision.go`: `checkFieldNames`, `quotedJoin`, `pflagBackendErr`, `PFlagBackendName`. |
| [2.5 Tests](#25-tests--in-confstruct_testgo-and-pflagpflag_collision_testgo) | Done | `confstruct_test.go` (generic wiring, 5 tests) and `pflag/pflag_collision_test.go` (collision logic + `quotedJoin`, 9 tests). All green, including `-race`. |

Status values: `Not started`, `In progress`, `Done`.

## 2.1 New optional `Backend` interface

Add to `confstruct.go`, next to `fieldAwareBackend`, following the seal
mechanism decided in
[pflag-integration.md#cross-package-hook-mechanism-decided](pflag-integration.md#cross-package-hook-mechanism-decided):

```go
// FieldPath is one entry field reachable from a single Populate call: its
// dot-separated struct path and the reflect.StructField chain leading to it
// (same chain fieldAwareBackend.lookupField already receives per-field).
// Exported so an out-of-package backend's NameCollisionChecker can read it.
type FieldPath struct {
	Path  string
	Chain []reflect.StructField
}

// nameCollisionBackend is implemented by a backend that must validate,
// once per Populate call and before any Lookup runs, that no two entry
// fields reachable from the target struct resolve to the same
// backend-specific name. Returning a non-nil error fails the whole
// Populate call before any value is injected into any field.
type nameCollisionBackend interface {
	checkNames(entries []FieldPath) error
}

// NameCollisionChecker is implemented by a backend, defined outside this
// package, that wants nameCollisionBackend's validation hook. Embed
// NameCollisionSeal in the backend type and construct it with
// NewNameCollisionSeal(that type) to opt in.
type NameCollisionChecker interface {
	CheckFieldNames(entries []FieldPath) error
}

// NameCollisionSeal adapts an externally implemented NameCollisionChecker
// into the package-private nameCollisionBackend hook, the same way
// FieldLookupSeal adapts FieldLookuper into fieldAwareBackend.
type NameCollisionSeal struct {
	impl NameCollisionChecker
}

func NewNameCollisionSeal(impl NameCollisionChecker) NameCollisionSeal {
	return NameCollisionSeal{impl: impl}
}

func (s NameCollisionSeal) checkNames(entries []FieldPath) error {
	if s.impl == nil {
		panic("confstruct: NameCollisionSeal used without NewNameCollisionSeal")
	}
	return s.impl.CheckFieldNames(entries)
}
```

`NameCollisionSeal`'s zero value has a nil `impl`; the guard turns a
misuse case (a backend embedding the seal but never calling
`NewNameCollisionSeal`, e.g. an accidental bare struct literal) into a
clear, actionable panic instead of a bare nil-interface dereference trace.
`FieldLookupSeal.lookupField` in
[pflag-integration.md#cross-package-hook-mechanism-decided](pflag-integration.md#cross-package-hook-mechanism-decided)
carries the identical guard for the identical reason.

Only `pflagBackend` implements this initially. `Map`, `File`, `Env`,
`Override` are unaffected — the type assertion in `Populate` simply won't
match them.

## 2.2 Collecting `[]FieldPath` before the value walk

Add a pre-pass that mirrors `walkAndInject`'s traversal (skip `Meta`,
recurse into plain nested structs, error on unexported entry fields) but
only collects paths/chains — it must not touch backends, since the whole
point is to run before any `Lookup`:

```go
func collectFieldPaths(sv reflect.Value, prefix string, chain []reflect.StructField, out *[]FieldPath) error {
	// same field/prefix/chain bookkeeping as walkAndInject (confstruct.go:538-597),
	// minus backend interaction; append FieldPath{key, fieldChain} for each
	// entry field instead of calling lookupBackendValue/setSlot.
}
```

This duplicates `walkAndInject`'s tree-walking shape (also shared today with
`collectUnset`, `confstruct.go:502-536`). Three near-identical recursive
walks is a real duplication cost worth naming, but restructuring all three
into one shared traversal-with-callback is out of scope for this plan —
flagged under [Open follow-ups](pflag-implementation-plan.md#open-follow-ups)
rather than folded into the `pflag` diff, so this PR's reviewable surface
stays about the `pflag` backend and not a `walkAndInject` refactor.

## 2.3 Wiring in `Populate` (`confstruct.go:425-465`)

Insert between the backend-registration checks and `walkAndInject`:

```go
var fieldPaths []FieldPath
if err := collectFieldPaths(sv, "", nil, &fieldPaths); err != nil {
	meta.state.Store(stateIdle)
	return err
}

var nameErrs []error
for _, b := range meta.backends {
	if ncb, ok := b.(nameCollisionBackend); ok {
		if err := ncb.checkNames(fieldPaths); err != nil {
			nameErrs = append(nameErrs, err)
		}
	}
}
if len(nameErrs) > 0 {
	meta.state.Store(stateIdle)
	return errors.Join(nameErrs...)
}
```

This runs after the "no backends" / "lowest layer watchable" checks but
before `watchCtx`/`cancelWatches` are created, so a rejected call leaves no
watch to cancel.

## 2.4 Field-name-collision detection (`checkFieldNames`)

**Decided:** this ships as a standalone function in a new file,
`pflag/pflag_collision.go` — not a `pflagBackend` method — per the
sequencing decision at the top of this document. `pflagBackend` doesn't
exist until Phase 3, so nothing here can take a `*pflagBackend` receiver
or reference `b.Name()`.

This code has no access to `confstruct`'s unexported `backendErr` — a seal
or embedding trick only solves *interface*-method satisfaction, not access
to an unexported *function*, and there's no `pflagBackend` to embed a seal
into yet regardless. That turns out not to matter for `lookupField` (whose
errors are wrapped once, uniformly, by `walkAndInject` itself — see
[pflag-integration.md#cross-package-hook-mechanism-decided](pflag-integration.md#cross-package-hook-mechanism-decided)),
but `checkNames` has no equivalent wrap point in the [2.3
wiring](#23-wiring-in-populate-confstructgo425-465) above, so this file
gets its own tiny local helper mirroring `backendErr`'s exact format. It
takes no backend receiver — `pflagBackend.Name()` will always return the
fixed constant `PFlagBackendName`, so there's nothing to look up on an
instance:

```go
// pflagBackendErr mirrors confstruct's unexported backendErr, which this
// package cannot call directly across the package boundary (embedding a
// seal only solves interface-method satisfaction, not access to an
// unexported function). Keeps every hand-built pflag error in the same
// "confstruct: backend %q <action> %q: <cause>" shape.
func pflagBackendErr(action, key string, err error) error {
	return fmt.Errorf("confstruct: backend %q %s %q: %w", PFlagBackendName, action, key, err)
}
```

**Decided: the action word for an invalid-tag error caught here is
`"name-check"`, not `"field"` or `"lookup"`.** `"field"` already means a
`setSlot`/coercion failure elsewhere in `confstruct.go`
(`backendErr("field", ...)`, `confstruct.go:628`) — reusing it here for an
unrelated failure (an invalid tag, caught structurally, before any value
coercion) would make the same word mean two different things depending on
context. `"lookup"` is what `walkAndInject` uses when this exact
`pflagName` error is instead caught later, during a real `lookupField`
call (`confstruct.go:625`) — a case this pre-pass makes unreachable for
`pflag` in practice, since `checkFieldNames` computes `pflagName` for
every field before any lookup runs, but the two call sites still deserve
their own distinct wording rather than colliding on one.

Because this pre-pass already has to compute `pflagName` for every field to
group collisions, have it also surface invalid-tag errors here rather than
deferring every one of them to the per-field `lookupField` call later: this
matches the aggregation principle in
[populate-error-handling.md](populate-error-handling.md#aggregate-every-failure-dont-stop-at-the-first)
— report everything the structural pass can already see in one `Populate`
failure, not one fix-rerun-fix cycle per concern.

```go
func checkFieldNames(entries []confstruct.FieldPath) error {
	byName := make(map[string][]string, len(entries)) // resolved name -> field paths
	var errs []error
	for _, e := range entries {
		name, err := pflagName(e.Path, e.Chain)
		if err != nil {
			errs = append(errs, pflagBackendErr("name-check", e.Path, err))
			continue
		}
		byName[name] = append(byName[name], e.Path)
	}
	names := make([]string, 0, len(byName))
	for name := range byName {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		paths := byName[name]
		if len(paths) < 2 {
			continue
		}
		sort.Strings(paths)
		errs = append(errs, fmt.Errorf("confstruct: backend %q: duplicate flag name %q: fields %s resolve to it",
			PFlagBackendName, name, quotedJoin(paths)))
	}
	if len(errs) == 0 {
		return nil
	}
	return errors.Join(errs...)
}
```

**Decided: `quotedJoin` renders a natural, Oxford-comma-and-joined,
quoted list — no "both"/"all" qualifier in the surrounding message.**
`["A", "B"]` → `"A" and "B"`; `["A", "B", "C"]` → `"A", "B", and "C"`. Also
lives in `pflag/pflag_collision.go` alongside `checkFieldNames` — it's
pflag-specific formatting, not a shared hook:

```go
// quotedJoin renders paths (already sorted by the caller) as a natural,
// comma-and-joined, quoted list: ["A","B"] -> `"A" and "B"`,
// ["A","B","C"] -> `"A", "B", and "C"`.
func quotedJoin(paths []string) string {
	switch len(paths) {
	case 1:
		return fmt.Sprintf("%q", paths[0])
	case 2:
		return fmt.Sprintf("%q and %q", paths[0], paths[1])
	default:
		quoted := make([]string, len(paths))
		for i, p := range paths {
			quoted[i] = fmt.Sprintf("%q", p)
		}
		last := len(quoted) - 1
		return strings.Join(quoted[:last], ", ") + ", and " + quoted[last]
	}
}
```

This produces, for the two-field case:

```text
confstruct: backend "pflag": duplicate flag name "with-key": fields
"SvcA.WithKey" and "SvcA.AltKey" resolve to it
```

matching the (now-updated) example in
[pflag-integration.md#duplicate-flag-name-detection](pflag-integration.md#duplicate-flag-name-detection).
The doc's earlier text used "both resolve to it" for exactly this
two-field case; that wording is dropped in favor of always ending "resolve
to it" so the message doesn't need to branch on count.

`pflag/pflag_collision.go` needs `"fmt"`, `"sort"`, `"strings"`, and
`"github.com/suyono3484/confstruct"` — a standalone import block, separate
from whatever `pflag/pflag.go` ends up needing in Phase 3.

Map iteration order is nondeterministic; sort `paths` within each
collision group (done above, right before formatting — struct-declaration
order from the traversal makes this likely a no-op, but don't rely on
that) and sort the outer `byName` keys before appending to `errs` so
repeated runs produce byte-identical error text — this project has already
hit nondeterministic-output bugs once (see recent commit `91026bb`), so
treat map-iteration order as a footgun by default here, not an oversight
to catch later.

**Phase 3 wiring (forward reference):** once `pflagBackend` exists, it
satisfies `NameCollisionChecker` with a one-line delegate:

```go
func (b *pflagBackend) CheckFieldNames(entries []confstruct.FieldPath) error {
	return checkFieldNames(entries)
}
```

No receiver logic needed — the whole algorithm already lives here,
independent of the backend instance.

## 2.5 Tests — in `confstruct_test.go` and `pflag/pflag_collision_test.go`

Split three ways, following the sequencing decision above:

**`confstruct_test.go`** (`package confstruct` — cannot import `pflag`,
that would be an import cycle) tests the generic pre-pass wiring (2.2/2.3)
against a small test-only stub type defined in that file, directly
implementing the private `nameCollisionBackend` interface (its own
`checkNames` method) — no seal needed, since the stub already lives inside
package `confstruct`.

**`pflag/pflag_collision_test.go`** tests `checkFieldNames` (2.4) directly,
with hand-built `[]confstruct.FieldPath` — no `pflagBackend`, `PFlag`, or
`*pflag.FlagSet` involved, since none of those exist until Phase 3. Most of
[the examples table](pflag-integration.md#examples) and [the rules
section](pflag-integration.md#rules) reduce to this shape:

- Two fields with different derived names: no error.
- One untagged field and one `cs.pflag`-tagged field resolving to the same
  name: error naming both paths.
- The same tag-derived name across two *separate* `checkFieldNames` calls,
  each with its own `[]FieldPath`: no error in either call — this is the
  unit-level proof of the subcommand scenario; [Phase
  3.4](pflag-plan-phase-3-backend.md#34-tests--pflagpflag_testgo) also
  covers it end-to-end once `Populate` and `PFlag` are wired together.
- Two colliding fields plus a third, unrelated collision elsewhere in the
  same input: both collisions reported in one error.
- An invalid `cs.pflag` tag on one field plus a genuine duplicate
  elsewhere: both surface in the same `errors.Join`.
- **Decided (recommendation 6):** pin the exact duplicate-name error
  string with a dedicated test, `TestCheckFieldNames_duplicateErrorText`
  or similar, the same way Phase 1 has
  `TestPFlagName_invalidTagErrorText`. This is the mechanism that would
  catch a future drift between this error's actual text and the example in
  [pflag-integration.md#duplicate-flag-name-detection](pflag-integration.md#duplicate-flag-name-detection)
  before it becomes a doc/code inconsistency again — cover both the
  two-path case (`"A" and "B"`) and a three-or-more-path case
  (`"A", "B", and "C"`) so `quotedJoin`'s branch on count is exercised
  too.

**Deferred to [Phase 3.4](pflag-plan-phase-3-backend.md#34-tests--pflagpflag_testgo):**
the one case that genuinely needs a real `pflagBackend` and
`*pflag.FlagSet` — the check firing even when the `FlagSet` passed to
`PFlag` doesn't define either colliding flag at all, and regardless of
`Changed` — since Phase 2 has nothing to construct that check against yet.

Continue to [Phase 3 — `pflagBackend` core](pflag-plan-phase-3-backend.md).
