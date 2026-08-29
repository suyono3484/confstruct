# Deferred: unify `collectUnset`, `walkAndInject`, and `collectFieldPaths` into one shared traversal helper

**Status:** Won't fix for now — deferred, not abandoned.
**Where:** `confstruct.go` — `collectUnset` (`:614`), `collectFieldPaths` (`:656`), `walkAndInject` (`:696`).

## Background

Three functions in `confstruct.go` independently walk a config struct's
field tree, each for a different caller:

- `collectUnset` (used by `UnsetFields`) — collects the dotted paths of
  every entry field whose `IsSet()` is false.
- `collectFieldPaths` (used by `Populate`'s Phase 2 name-collision
  pre-pass) — collects a `FieldPath` (dotted path + `reflect.StructField`
  chain) for every entry field, before any backend `Lookup` runs, so a
  `nameCollisionBackend` can validate structurally.
- `walkAndInject` (used by `Populate`) — the real value-injection walk:
  looks up each field's value from every registered backend, coerces it,
  sets the entry's slot, and registers `WatchableBackend` watches.

All three share the same skeleton: skip the `Meta` field, build the dotted
`key` (`prefix + "." + f.Name`), detect an entry field via
`reflect.PointerTo(f.Type).Implements(layerManagerType)`, reject an
unexported entry field with the identical error text
(`"confstruct: field %q is an unexported entry field; entry fields must be
exported"` — byte-identical in all three, confirmed via `grep -c` finding
exactly 3 occurrences), and recurse into fields of `reflect.Struct` kind.
`collectFieldPaths` and `walkAndInject` additionally share the
`appendFieldChain` call that builds the `reflect.StructField` chain;
`collectUnset` doesn't need it, since `UnsetFields` only ever returns
dotted-path strings.

This was first flagged as a code-review finding on the `add_pflag_phase1`
branch — see
[pflag-plan-phase-2-code-review.md, finding 1](../pflag-plan-phase-2-code-review.md#1-collectfieldpaths-duplicates-the-tree-walk-skeleton-already-implemented-twice) —
and is also called out in
[pflag-implementation-plan.md#open-follow-ups](../pflag-implementation-plan.md#open-follow-ups)
as anticipated, deliberately-deferred work: "worth doing once there's a
third real consumer justifying the abstraction." `collectFieldPaths`
(shipped in Phase 2) is that third consumer, so the trigger condition for
doing this refactor has now been met — it just hasn't been scheduled.

## Why this matters (failure scenario)

A future change to the shared traversal rules — e.g. how unexported
fields are rejected, or how `Meta`/embedded structs are skipped — only
gets applied to one or two of the three copies, silently making
`collectFieldPaths` (the name-collision pre-pass) see a different set of
fields than `walkAndInject` (actual value injection). A field
`walkAndInject` would reject as unexported could slip past
`collectFieldPaths`'s copy of the check if the two conditions/messages
ever drift apart, or vice versa — producing inconsistent validation
between the structural pre-pass and the real population pass, likely
without any test noticing.

## Partial mitigation already in place

`TestCollectFieldPathsAndWalkAndInjectAgreeOnUnexportedEntryField`
(`confstruct_test.go`) calls `collectFieldPaths` and `walkAndInject`
directly against the same struct value and asserts they reject an
unexported entry field with identical error text — added specifically
because no `Populate`-driven test could ever catch this drift structurally
(`collectFieldPaths` always runs before `walkAndInject`, and `Populate`
stops at the first failure, so `walkAndInject` never executes on a struct
`collectFieldPaths` already rejected).

This narrows the risk for the unexported-field-rejection check
specifically, but does not cover the rest of the shared skeleton — the
`Meta`-skip, dotted-key construction, and struct-kind recursion are still
three independently-maintained copies with nothing enforcing they stay in
sync. The underlying duplication this document tracks is still real; the
test only guards against the single sharpest failure mode identified so
far.

## Suggested fix (deferred)

Extract the shared skeleton into one traversal-with-callback helper, and
have each of the three current functions become a thin visitor over it:

```go
// entryVisitor is called once per entry field found by walkEntries, after
// the unexported-field check has already passed.
type entryVisitor func(fv reflect.Value, key string, chain []reflect.StructField) error

// walkEntries walks sv depth-first: skips Meta, builds the dotted key and
// reflect.StructField chain, rejects an unexported entry field, and calls
// visit for every entry field it finds. Recurses into plain nested structs.
func walkEntries(sv reflect.Value, prefix string, chain []reflect.StructField, visit entryVisitor) error {
	st := sv.Type()
	for i := 0; i < st.NumField(); i++ {
		f := st.Field(i)
		fv := sv.Field(i)

		if f.Type == metaType {
			continue
		}

		key := f.Name
		if prefix != "" {
			key = prefix + "." + f.Name
		}
		fieldChain := appendFieldChain(chain, f)

		if reflect.PointerTo(f.Type).Implements(layerManagerType) {
			if !f.IsExported() {
				return fmt.Errorf("confstruct: field %q is an unexported entry field; entry fields must be exported", key)
			}
			if err := visit(fv, key, fieldChain); err != nil {
				return err
			}
			continue
		}

		if f.Type.Kind() == reflect.Struct {
			if err := walkEntries(fv, key, fieldChain, visit); err != nil {
				return err
			}
		}
	}
	return nil
}
```

- `collectUnset` becomes a visitor that checks `resolvedState()` and
  appends to `unset` when `!isSet` (it can just ignore the `chain`
  parameter it doesn't need).
- `collectFieldPaths` becomes a visitor that appends
  `FieldPath{Path: key, Chain: chain}` to its output slice.
- `walkAndInject` needs more context per call (`ctx`, `meta`, `errs`,
  `pending`) than the visitor signature carries — the cleanest shape is
  likely for `walkAndInject` to remain the closure-constructing caller,
  passing a visitor that captures those over its own local variables,
  rather than widening `entryVisitor`'s signature for one consumer.

## Why deferred rather than fixed now

Purely a maintainability/reuse concern, not a correctness bug — the three
copies are presently byte-identical for their shared portions. Already
named in `pflag-implementation-plan.md`'s Open follow-ups as separable
from shipping the `pflag` backend, so it doesn't block Phases 3–4. Revisit
once Phase 4 ships (no more near-term new consumers of this traversal
shape expected), or sooner if a fourth consumer appears, or if the
partial-mitigation test above is ever seen to need a sibling for a
different part of the skeleton.
