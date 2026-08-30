# v0.2.0 — Populate Error Handling

## Highlights

- `Populate` now reports coercion failures instead of silently discarding
  them. A value a backend supplies but that fails to convert into the
  entry's declared Go type — a malformed string, a numeric value that
  overflows the target type — causes `Populate` to return a non-nil error.
- Every conversion failure is collected across the whole struct and every
  backend layer, then returned together as one `errors.Join`ed error,
  rather than stopping at the first one found.
- A backend layer's bad value is reported even when a higher-precedence
  layer would have overridden it anyway — a layer claiming to have a value
  for a field but supplying one that doesn't coerce is a genuine problem
  with that layer, independent of what ultimately wins resolution.

## Behavior changes

- **Breaking:** a `Populate` call that would previously have returned `nil`
  while silently leaving a badly-typed field at its zero value now returns
  a non-nil error for that field. Callers that treat any `Populate` error
  as fatal are unaffected; callers relying on the old silent-discard
  behavior should validate against the new error instead.
- Partial results are kept on failure, not rolled back: a failed `Populate`
  call can leave some fields resolved (`IsSet` true, values populated)
  while others remain unset. The only contract is "don't trust the struct
  until `err == nil`" — not "an error means nothing was touched."
- `Populate` remains retryable after a failure, as before: a non-nil error
  does not permanently lock the struct, and the same instance can be
  passed to `Populate` again once the cause is fixed.
- `WatchableBackend` push-time coercion failures are unchanged and stay
  silent after a successful `Populate`, preserving a running application's
  stability against one malformed live update.

## Notes

- See [Error handling](../../README.md#error-handling) in the README for
  the user-facing contract, and
  [docs/populate-error-handling.md](../populate-error-handling.md) for the
  full design rationale.
- Error messages follow the `confstruct: backend %q field %q: %w` shape,
  matching the existing `confstruct: backend %q lookup %q: %w` convention
  used for `Backend.Lookup` errors.
