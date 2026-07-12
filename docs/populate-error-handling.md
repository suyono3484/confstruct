# Working draft: `Populate` initial-conversion error handling

## Status

Exploration only. This document proposes a change to `Populate`'s existing
behavior; it does not commit the exact diff. Raised while designing the
[`pflag` backend](pflag-integration.md), whose "Error handling" decision item
explicitly deferred this question because it isn't scoped to one backend.

## Problem

When a backend supplies a value for a field but that value fails to coerce
into the entry's declared type, `Populate` does not report it. In
`entry[T].setSlot` (`confstruct.go:195-210`), a `coerce[T]` failure just
`return`s — the slot is left at its zero state and `resolveUnderLock` never
runs for that update:

```go
func (e *entry[T]) setSlot(index int, v any, ok bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if ok {
		coerced, err := coerce[T](v)
		if err != nil {
			return // <- silently discarded
		}
		e.slots[index].value = coerced
		e.slots[index].ok = true
	} else {
		e.slots[index].value = *new(T)
		e.slots[index].ok = false
	}
	e.resolveUnderLock()
}
```

The result is indistinguishable from a field no backend ever addressed:
`IsSet()` is `false`, `Value()` is the zero value, and the entry silently
falls through to whatever the next lower-precedence layer provides. A
misconfigured value and an absent one produce the same observable state.

This was already flagged during review (see `docs/code-review-2026-07-11.md`,
"Numeric coercion silently overflows"): fixing the overflow-detection bug in
`coerce` made the failure detectable *inside* `coerce`, but `setSlot` still
throws that detection away before it reaches `Populate`'s caller.

This matters most for backends whose values are always strings needing
parsing — `Env`, `File`, and the planned `PFlag` — where a single typo
(`--db-port=abc`, a malformed env var, a wrong-typed YAML value) is a
plausible, everyday mistake. `Map`'s Go-typed values rarely trigger this
path, since `coerce`'s fast path (`v.(T)`) matches them directly.

## Recommended direction

Make `Populate` return a non-nil error when any field's *initial* value
fails to coerce during the synchronous walk it performs. Because coercion is
centralized in `entry[T].setSlot`/`coerce[T]`, fixing it there covers `Map`,
`File`, `Env`, `Override`, and the future `PFlag` simultaneously — no
backend-specific code is needed. This is what makes "affects every backend"
(the reason this was deferred) tractable rather than five separate patches.

## Scope: initial population only

This decision covers only the synchronous walk performed inside a single
`Populate` call. It deliberately does **not** change how a `WatchableBackend`
push handled after `Populate` has already returned successfully — a
badly-typed pushed value continues to be ignored, leaving the entry at its
last good resolved value, exactly as today. Failing a whole running
application because one live external update happened to be malformed would
trade a bounded, already-visible-at-startup problem for an unbounded,
runtime one; that tradeoff isn't worth making as part of this change, and
isn't what the "failed **initial** conversion" wording in the original
decision item was asking about. See [Explicit
non-goals](#explicit-non-goals).

## Design

### Aggregate every failure, don't stop at the first

Same reasoning as [duplicate flag name
detection](pflag-integration.md#duplicate-flag-name-detection): a config
with several broken fields should be reported in one `Populate` failure, not
discovered one fix-and-rerun cycle at a time. `walkAndInject` should
accumulate every conversion failure across the whole struct tree and every
backend layer, then `Populate` returns them joined into a single error
(`errors.Join`) once the walk finishes.

### Every failing layer is reported, not just the one that would have won

A backend layer's `Lookup` can return `ok == true` for a field and still
supply an unparseable value even if a higher-precedence layer would have
overridden it anyway. That layer is reported regardless. `Populate` only
ever calls a backend's `Lookup` for a path that corresponds to a real struct
field — the backend is specifically claiming "I have a value for this exact
field" — so a conversion failure there is a genuine problem with that
layer's configuration, independent of what ultimately wins the resolution.
Not reporting it because a higher layer happens to be valid this run would
leave a landmine for the day that higher layer's value is removed.

### Interface and call-site changes

`layerManager.setSlot` gains an error return:

```go
type layerManager interface {
	initSlots(n int)
	initSlotMeta(index int, name, desc string)
	setSlot(index int, v any, ok bool) error // was: setSlot(index int, v any, ok bool)
	resolvedState() (value any, backendName, backendDesc string, isSet bool)
	hasChangedSinceNotify() (value any, name, desc string, isSet bool, changed bool)
}

func (e *entry[T]) setSlot(index int, v any, ok bool) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if ok {
		coerced, err := coerce[T](v)
		if err != nil {
			return err
		}
		e.slots[index].value = coerced
		e.slots[index].ok = true
	} else {
		e.slots[index].value = *new(T)
		e.slots[index].ok = false
	}
	e.resolveUnderLock()
	return nil
}
```

`walkAndInject`'s initial-population loop captures that error into an
accumulator instead of discarding it; the `WatchableBackend` push closure
explicitly discards it, preserving today's live-update behavior:

```go
for idx, b := range meta.backends {
	lm.initSlotMeta(idx, b.Name(), b.Describe())
	v, ok, err := lookupBackendValue(b, key, fieldChain)
	if err != nil {
		return fmt.Errorf("confstruct: backend %q lookup %q: %w", b.Name(), key, err)
	}
	if err := lm.setSlot(idx, v, ok); err != nil {
		*errs = append(*errs, fmt.Errorf("confstruct: backend %q field %q: %w", b.Name(), key, err))
	}
	if wb, watchable := b.(WatchableBackend); watchable {
		wb.Watch(ctx, key, func(v any, ok bool) {
			_ = lm.setSlot(idx, v, ok) // live-update failures stay silent; see Scope
			notify()
		})
	}
}
```

`walkAndInject` needs an error accumulator threaded through its recursion
into nested structs (a `*[]error` parameter, or each call returning its own
slice for the caller to merge) so that a conversion failure three levels
deep doesn't get lost or short-circuit the rest of the walk. `Lookup`
errors are unaffected by this and keep returning immediately, exactly as
today — that's a pre-existing, already-surfaced error path and out of scope
here (see [Explicit non-goals](#explicit-non-goals)).

At the top of `Populate`, after the walk completes:

```go
if len(errs) > 0 {
	cancelWatches()
	meta.watchCancel = nil
	meta.state.Store(stateIdle)
	return errors.Join(errs...)
}
```

This reuses the exact cleanup `Populate` already performs on the existing
error paths (`confstruct.go:456-461`), so the documented retry contract —
"a call that returns an error does not consume that one-shot budget" — holds
for conversion errors with no further changes: `initSlots` re-runs on retry
and resets every slot, so a corrected backend value on the next `Populate`
call starts clean.

### Avoid a doubled `"confstruct: "` prefix

`coerce` and `parseString` already prefix their own errors (`confstruct.go:351,
358, 382, 388, 394, 400, 404`) — e.g. `"confstruct: value %v overflows %s"`.
Wrapping one of those again with `"confstruct: backend %q field %q: %w"`
would produce a doubled prefix (`confstruct: backend "pflag" field
"Database.Port": confstruct: value abc overflows int8`). Strip the
self-prefix from those five error sites so they read as plain fragments
(`"value %v overflows %s"`, `"cannot parse %q as %s"`, `"cannot convert %T
to %s"`), and let the new wrap in `walkAndInject` supply the single prefix —
matching the existing `Lookup`-error convention exactly:

```text
confstruct: backend "pflag" field "Database.Port": value abc overflows int8
```

## Decisions to settle before implementation

1. **Aggregate vs. fail-fast for conversion errors.** Recommended:
   aggregate via `errors.Join`, matching the duplicate-name precedent. A
   fail-fast alternative is simpler to implement but reintroduces the
   fix-one-rerun-fix-next friction this project has already rejected once.
2. **`Lookup` errors stay fail-fast.** This change only touches conversion
   (coercion) failures. Folding `Lookup` errors into the same aggregation
   model is a separate decision with its own tradeoffs (a `Lookup` error can
   indicate a broken backend connection, not just a bad value) and isn't
   part of what the original "failed initial conversion" item asked for.
3. **`WatchableBackend` push-time failures stay silent.** Confirmed as an
   explicit non-goal above — worth a second look only if a future backend's
   live-push failure mode turns out to need visibility (an `OnResolve`-style
   error hook, not a `Populate`-time change).
4. **Prefix stripping in `coerce`/`parseString`.** Five error strings change
   shape (prefix removed). Grep for any code asserting the literal
   `"confstruct: "`-prefixed coercion message before implementing, since
   these are the same call sites `docs/code-review-2026-07-11.md`'s overflow
   fix already touched once.

## Test matrix

- `TestPopulate_numericOverflowRejected`, `TestPopulate_float32OverflowRejected`,
  and `TestPopulate_typeMismatchSilent` currently assert `Populate` returns
  `nil` and the field is silently unset; rewrite them to assert a non-nil
  `Populate` error instead (and rename `TestPopulate_typeMismatchSilent`,
  since it will no longer be silent).
- A struct with two or more independently broken fields reports all of them
  in one `Populate` error, not just the first encountered.
- A lower-precedence layer's unparseable value errors even when a
  higher-precedence layer supplies a valid value for the same field —
  confirms failing layers are reported regardless of which layer would have
  won.
- A `WatchableBackend` pushing a badly-typed value after a successful
  `Populate` does not error and leaves the field's already-resolved value
  unchanged — regression guard for the live-update non-goal.
- `Populate` remains retryable after a conversion-error failure: fix the
  backend's value and call `Populate` again on the same struct; it succeeds
  and every field resolves normally.

## Explicit non-goals

- Making `Backend.Lookup` errors participate in the same aggregation as
  conversion errors.
- Changing `WatchableBackend` push-time coercion-failure handling — it stays
  silent, preserving a running application's stability against one bad live
  update.
- A way to distinguish, after a *successful* `Populate`, "unset because no
  backend addressed this field" from "unset because coercion failed" — moot
  once a successful `Populate` can no longer have an unreported conversion
  failure; the two cases only looked alike because the failure case used to
  slip through silently.
