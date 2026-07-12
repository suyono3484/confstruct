# Phase 0 — `Populate` conversion-error aggregation

Part of [the `pflag` implementation plan](pflag-implementation-plan.md).
Previous: none (this phase has no dependency). Next:
[Phase 1 — identifier-to-flag-name conversion](pflag-plan-phase-1-name-conversion.md).

Source: [`populate-error-handling.md`](populate-error-handling.md). This is
a prerequisite, not part of the `pflag` diff, because it fixes a bug that
already affects `Map`, `File`, `Env`, and `Override` today (silent coercion
failure). Do this first so the `pflag` backend is born reporting errors,
instead of adding a fifth silent-failure call site that then has to be
revisited.

## 0.1 `layerManager` interface change (`confstruct.go:114-120`)

```go
type layerManager interface {
	initSlots(n int)
	initSlotMeta(index int, name, desc string)
	setSlot(index int, v any, ok bool) error // was: no error return
	resolvedState() (value any, backendName, backendDesc string, isSet bool)
	hasChangedSinceNotify() (value any, name, desc string, isSet bool, changed bool)
}
```

## 0.2 `entry[T].setSlot` (`confstruct.go:195-210`)

Return the `coerce[T]` error instead of discarding it; keep the `ok == false`
branch as-is (absence is not a coercion failure).

## 0.3 `walkAndInject` (`confstruct.go:538-597`)

- Thread an accumulator through the recursive walk. Simplest option matching
  the existing signature style: add a `*[]error` parameter to
  `walkAndInject` and `appendFieldChain`'s sibling calls, e.g.:

  ```go
  func walkAndInject(ctx context.Context, sv reflect.Value, meta *Meta, prefix string, chain []reflect.StructField, errs *[]error) error
  ```

  `walkAndInject` keeps its own `error` return for the pre-existing fail-fast
  cases (missing `Meta` — not reachable recursively but keep the signature
  uniform — unexported entry field, `Lookup` error). Those still return
  immediately, unchanged. Only the new `setSlot` error path appends to
  `*errs` and continues the loop.

- Initial-population call site:

  ```go
  if err := lm.setSlot(idx, v, ok); err != nil {
  	*errs = append(*errs, fmt.Errorf("confstruct: backend %q field %q: %w", b.Name(), key, err))
  }
  ```

- `WatchableBackend` push closure explicitly discards the error, preserving
  live-update behavior (see [Scope: initial population
  only](populate-error-handling.md#scope-initial-population-only)):

  ```go
  wb.Watch(ctx, key, func(v any, ok bool) {
  	_ = lm.setSlot(idx, v, ok)
  	notify()
  })
  ```

## 0.4 `Populate` (`confstruct.go:425-465`)

```go
var errs []error
if err := walkAndInject(watchCtx, sv, meta, "", nil, &errs); err != nil {
	cancelWatches()
	meta.watchCancel = nil
	meta.state.Store(stateIdle)
	return err
}
if len(errs) > 0 {
	cancelWatches()
	meta.watchCancel = nil
	meta.state.Store(stateIdle)
	return errors.Join(errs...)
}
```

Add `"errors"` to the import block.

## 0.5 Strip the doubled `"confstruct: "` prefix

Five sites in `coerce`/`parseString` (`confstruct.go:351, 358, 382, 388,
394, 400, 404`) currently self-prefix. Change each to a plain fragment:

| Before | After |
| --- | --- |
| `"confstruct: value %v overflows %s"` | `"value %v overflows %s"` |
| `"confstruct: cannot convert %T to %s"` | `"cannot convert %T to %s"` |
| `"confstruct: cannot parse %q as bool"` | `"cannot parse %q as bool"` |
| `"confstruct: cannot parse %q as %s"` (×2) | `"cannot parse %q as %s"` |
| `"confstruct: cannot convert string to %s"` | `"cannot convert string to %s"` |

Before making this change, grep the test suite for any assertion on the
literal `"confstruct: "`-prefixed coercion string (per decision 4 in the
source doc) so the rewrite doesn't leave a stale assertion passing for the
wrong reason.

```
grep -n '"confstruct: value\|"confstruct: cannot' confstruct_test.go env_test.go file_test.go override_test.go
```

## 0.6 Tests

- Rewrite `TestPopulate_numericOverflowRejected`,
  `TestPopulate_float32OverflowRejected`, and `TestPopulate_typeMismatchSilent`
  (rename the last one — it's no longer silent) to assert a non-nil
  `Populate` error instead of a silently-unset field.
- New: a struct with two or more independently broken fields reports all of
  them in one `Populate` error (use `errors.Is`/`strings.Contains` per
  fragment, or unwrap via `errors.Join`'s `Unwrap() []error`).
- New: a lower-precedence layer's unparseable value errors even when a
  higher-precedence layer supplies a valid value for the same field.
- New: a `WatchableBackend` pushing a badly-typed value after a successful
  `Populate` does not error and leaves the resolved value unchanged
  (regression guard using the existing watchable test fixture in
  `confstruct_test.go`, if any, or `Override`).
- New: `Populate` is retryable after a conversion-error failure — fix the
  backend value, call `Populate` again on the same struct, confirm success.

## 0.7 Exit criteria

`go test github.com/suyono3484/confstruct` green, including the rewritten
tests, with no other behavioral change (same resolution semantics, same
watch behavior). This is the point at which `pflag.go` may start being
written — see [Phase 1](pflag-plan-phase-1-name-conversion.md).
