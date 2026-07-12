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

**Package layout (decided) — blocker for this phase as drafted:** the
`pflag` backend now lives in its own package,
`github.com/suyono3484/confstruct/pflag`, not in package `confstruct` (see
[pflag-integration.md#package-layout](pflag-integration.md#package-layout)).
Everything below — `nameCollisionBackend` as an *unexported* interface in
`confstruct.go`, satisfied by `pflagBackend.checkNames` — assumes the two
types are declared in the same package. Go requires an unexported interface
method to be declared in the same package as the interface for a type to
satisfy it, so a `pflagBackend` defined in package `pflag` cannot implement
`nameCollisionBackend` as written here. This phase cannot proceed exactly as
drafted; it needs one of:

- exporting `nameCollisionBackend` (and `fieldPath`, since it appears in the
  method signature) from `confstruct`, or
- some other cross-package extension mechanism.

This is new open design work created by the package-layout decision, not
resolved by this document. The rest of this phase — the traversal shape,
the scoping rules, the test matrix — is unaffected and still describes the
intended behavior; only the *mechanism* by which `pflagBackend` hooks into
`Populate`'s pre-pass needs to change from what's sketched in
[2.1](#21-new-optional-backend-interface) and
[2.4](#24-pflagbackendchecknames) below.

## Tracker

| Step | Status | Notes |
| --- | --- | --- |
| [2.1 New optional `Backend` interface](#21-new-optional-backend-interface) | Not started | |
| [2.2 Collecting `[]fieldPath`](#22-collecting-fieldpath-before-the-value-walk) | Not started | |
| [2.3 Wiring in `Populate`](#23-wiring-in-populate-confstructgo425-465) | Not started | |
| [2.4 `pflagBackend.checkNames`](#24-pflagbackendchecknames) | Not started | |
| [2.5 Tests](#25-tests--in-confstruct_testgo-or-a-new-pflag_testgo) | Not started | |

Status values: `Not started`, `In progress`, `Done`.

## 2.1 New optional `Backend` interface

*(See the package-layout note above — this sketch predates the decision to
put `pflagBackend` in its own package, so it needs an exported or otherwise
cross-package-satisfiable mechanism instead of an unexported interface.)*

Add to `confstruct.go`, next to `fieldAwareBackend`:

```go
// fieldPath is one entry field reachable from a single Populate call: its
// dot-separated struct path and the reflect.StructField chain leading to it
// (same chain fieldAwareBackend.lookupField already receives per-field).
type fieldPath struct {
	path  string
	chain []reflect.StructField
}

// nameCollisionBackend is implemented by a backend that must validate,
// once per Populate call and before any Lookup runs, that no two entry
// fields reachable from the target struct resolve to the same
// backend-specific name. Returning a non-nil error fails the whole
// Populate call before any value is injected into any field.
type nameCollisionBackend interface {
	checkNames(entries []fieldPath) error
}
```

Only `pflagBackend` implements this initially. `Map`, `File`, `Env`,
`Override` are unaffected — the type assertion in `Populate` simply won't
match them.

## 2.2 Collecting `[]fieldPath` before the value walk

Add a pre-pass that mirrors `walkAndInject`'s traversal (skip `Meta`,
recurse into plain nested structs, error on unexported entry fields) but
only collects paths/chains — it must not touch backends, since the whole
point is to run before any `Lookup`:

```go
func collectFieldPaths(sv reflect.Value, prefix string, chain []reflect.StructField, out *[]fieldPath) error {
	// same field/prefix/chain bookkeeping as walkAndInject (confstruct.go:538-597),
	// minus backend interaction; append fieldPath{key, fieldChain} for each
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
var fieldPaths []fieldPath
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

## 2.4 `pflagBackend.checkNames`

*(Same caveat as [2.1](#21-new-optional-backend-interface): `pflagBackend`
now lives in package `pflag`, so this method can only exist as sketched if
`nameCollisionBackend` becomes satisfiable across a package boundary.)*

Because this pre-pass already has to compute `pflagName` for every field to
group collisions, have it also surface invalid-tag errors here rather than
deferring every one of them to the per-field `lookupField` call later: this
matches the aggregation principle in
[populate-error-handling.md](populate-error-handling.md#aggregate-every-failure-dont-stop-at-the-first)
— report everything the structural pass can already see in one `Populate`
failure, not one fix-rerun-fix cycle per concern.

```go
func (b *pflagBackend) checkNames(entries []fieldPath) error {
	byName := make(map[string][]string, len(entries)) // resolved name -> field paths
	var errs []error
	for _, e := range entries {
		name, err := pflagName(e.path, e.chain)
		if err != nil {
			errs = append(errs, backendErr("field", b, e.path, err))
			continue
		}
		byName[name] = append(byName[name], e.path)
	}
	for name, paths := range byName {
		if len(paths) < 2 {
			continue
		}
		errs = append(errs, fmt.Errorf("confstruct: backend %q: duplicate flag name %q: fields %s all resolve to it",
			PFlagBackendName, name, quotedJoin(paths)))
	}
	if len(errs) == 0 {
		return nil
	}
	return errors.Join(errs...)
}
```

`quotedJoin` — small local helper (`"%q" join with ", "`) — matches the
example error format in the source doc:

```
confstruct: backend "pflag": duplicate flag name "with-key": fields
"SvcA.WithKey" and "SvcA.AltKey" both resolve to it
```

Map iteration order is nondeterministic; sort `paths` (they're already in
struct declaration order from the traversal, so this is likely a no-op) and
sort the outer `name` keys before appending to `errs` so repeated runs
produce byte-identical error text — this project has already hit
nondeterministic-output bugs once (see recent commit `91026bb`), so treat
map-iteration order as a footgun by default here, not an oversight to catch
later.

## 2.5 Tests — in `confstruct_test.go` or a new `pflag/pflag_test.go`

Now that `pflagBackend` lives in its own package (see the package-layout
note above), split by what's actually being tested: the generic pre-pass
wiring in `Populate` (2.2/2.3) belongs in `confstruct_test.go` alongside its
existing tests, while `pflagBackend`'s own name-collision logic (2.4)
belongs in `pflag/pflag_test.go`, once Phase 2's cross-package mechanism is
settled.

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
