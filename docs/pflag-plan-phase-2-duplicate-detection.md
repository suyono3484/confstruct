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
[2.4](#24-pflagbackendcheckfieldnames) below are updated to sketch against
that mechanism; the traversal shape, scoping rules, and test matrix
elsewhere in this phase are unaffected.

## Tracker

| Step | Status | Notes |
| --- | --- | --- |
| [2.1 New optional `Backend` interface](#21-new-optional-backend-interface) | Not started | |
| [2.2 Collecting `[]FieldPath`](#22-collecting-fieldpath-before-the-value-walk) | Not started | |
| [2.3 Wiring in `Populate`](#23-wiring-in-populate-confstructgo425-465) | Not started | |
| [2.4 `pflagBackend.CheckFieldNames`](#24-pflagbackendcheckfieldnames) | Not started | |
| [2.5 Tests](#25-tests--in-confstruct_testgo-or-a-new-pflag_testgo) | Not started | |

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
	return s.impl.CheckFieldNames(entries)
}
```

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

## 2.4 `pflagBackend.CheckFieldNames`

`pflagBackend` embeds `confstruct.NameCollisionSeal` (see
[2.1](#21-new-optional-backend-interface) and
[pflag-integration.md#cross-package-hook-mechanism-decided](pflag-integration.md#cross-package-hook-mechanism-decided))
and implements `CheckFieldNames`, the method the seal forwards to. Note
this runs in package `pflag`, so it has no access to `confstruct`'s
unexported `backendErr` — a seal or embedding trick only solves
*interface*-method satisfaction, not access to an unexported *function*.
That turns out not to matter for `lookupField` (whose errors are wrapped
once, uniformly, by `walkAndInject` itself — see
[pflag-integration.md#cross-package-hook-mechanism-decided](pflag-integration.md#cross-package-hook-mechanism-decided)),
but `checkNames` has no equivalent wrap point in the [2.3
wiring](#23-wiring-in-populate-confstructgo425-465) above, so `pflagBackend`
gets its own tiny local helper mirroring `backendErr`'s exact format:

```go
// pflagBackendErr mirrors confstruct's unexported backendErr, which
// pflagBackend cannot call directly across the package boundary (embedding
// a seal only solves interface-method satisfaction, not access to an
// unexported function). Keeps every hand-built pflagBackend error in the
// same "confstruct: backend %q <action> %q: <cause>" shape.
func pflagBackendErr(action string, b *pflagBackend, key string, err error) error {
	return fmt.Errorf("confstruct: backend %q %s %q: %w", b.Name(), action, key, err)
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
`pflag` in practice, since `CheckFieldNames` computes `pflagName` for every
field before any lookup runs, but the two call sites still deserve their
own distinct wording rather than colliding on one.

Because this pre-pass already has to compute `pflagName` for every field to
group collisions, have it also surface invalid-tag errors here rather than
deferring every one of them to the per-field `lookupField` call later: this
matches the aggregation principle in
[populate-error-handling.md](populate-error-handling.md#aggregate-every-failure-dont-stop-at-the-first)
— report everything the structural pass can already see in one `Populate`
failure, not one fix-rerun-fix cycle per concern.

```go
func (b *pflagBackend) CheckFieldNames(entries []confstruct.FieldPath) error {
	byName := make(map[string][]string, len(entries)) // resolved name -> field paths
	var errs []error
	for _, e := range entries {
		name, err := pflagName(e.Path, e.Chain)
		if err != nil {
			errs = append(errs, pflagBackendErr("name-check", b, e.Path, err))
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
`["A", "B"]` → `"A" and "B"`; `["A", "B", "C"]` → `"A", "B", and "C"`. This
lives in `pflag/pflag.go` alongside `CheckFieldNames`, not `confstruct.go`
— it's pflag-specific formatting, not a shared hook:

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

`CheckFieldNames` and `quotedJoin` add `"sort"` and `"strings"` to
whatever import block ends up in `pflag/pflag.go` — not shown in [Phase
3.2](pflag-plan-phase-3-backend.md#32-new-file-pflagpflaggo)'s current
import list, since that sketch predates this code landing in the same
file.

Map iteration order is nondeterministic; sort `paths` within each
collision group (done above, right before formatting — struct-declaration
order from the traversal makes this likely a no-op, but don't rely on
that) and sort the outer `byName` keys before appending to `errs` so
repeated runs produce byte-identical error text — this project has already
hit nondeterministic-output bugs once (see recent commit `91026bb`), so
treat map-iteration order as a footgun by default here, not an oversight
to catch later.

## 2.5 Tests — in `confstruct_test.go` or a new `pflag/pflag_test.go`

Now that `pflagBackend` lives in its own package (see the package-layout
note above), split by what's actually being tested: the generic pre-pass
wiring in `Populate` (2.2/2.3) belongs in `confstruct_test.go` alongside its
existing tests, while `pflagBackend`'s own name-collision logic (2.4)
belongs in `pflag/pflag_test.go`.

Directly from [the examples
table](pflag-integration.md#examples) and [the rules
section](pflag-integration.md#rules):

- Two fields with different derived names in the same `Populate` call: no
  error.
- One untagged field and one `cs.pflag`-tagged field resolving to the same
  name, same call: error naming both paths.
- The same tag-derived name in two *separate* `Meta`-rooted structs,
  populated by two separate `Populate` calls: no error (this is the
  subcommand scenario — write it as two structs, two `Populate` calls, both
  succeed).
- Two colliding fields plus a third, unrelated collision elsewhere in the
  same struct: both collisions reported in one error.
- The check fires even when the `*pflag.FlagSet` passed to `PFlag` doesn't
  define either colliding flag at all, and regardless of `Changed` — construct
  a `pflag.FlagSet` with neither flag registered and confirm `Populate` still
  fails structurally.
- An invalid `cs.pflag` tag on one field plus a genuine duplicate elsewhere:
  both surface in the same `errors.Join`.

Continue to [Phase 3 — `pflagBackend` core](pflag-plan-phase-3-backend.md).
