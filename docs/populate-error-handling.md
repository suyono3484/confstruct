# `Populate` error handling — design rationale

## Status

Implemented (Phase 0 of the `pflag` integration effort — see
[pflag-implementation-plan.md](pflag-implementation-plan.md)). This document
records the reasoning behind the behavior described in the README's [Error
handling](../README.md#error-handling) section. It is a design-rationale
reference, not a plan for pending work — read the README section first for
the user-facing contract; read this document for *why* it works that way.

## Background

Before this behavior shipped, when a backend supplied a value for a field
but that value failed to coerce into the entry's declared Go type,
`Populate` did not report it: the failure was discarded, the field was left
at its zero state, and `Populate` returned `nil`. That made a misconfigured
value indistinguishable from an absent one — `IsSet()` was `false`,
`Value()` was the zero value, and the entry silently fell through to
whatever the next lower-precedence layer provided.

This mattered most for backends whose values are always strings needing
parsing — `Env`, `File`, and the `pflag` backend — where a single typo
(`--db-port=abc`, a malformed env var, a wrong-typed YAML value) is a
plausible, everyday mistake. `Map`'s Go-typed values rarely triggered this
path, since coercion's fast path matches them directly without parsing.

Coercion is centralized in one place (`entry[T].setSlot`, which calls
`coerce[T]`), so fixing it there covers every backend — `Map`, `File`,
`Env`, `Override`, and `pflag` — simultaneously, with no backend-specific
code required.

## Scope: initial population only

This behavior covers only the synchronous walk performed inside a single
`Populate` call. It deliberately does **not** change how a `WatchableBackend`
push is handled after `Populate` has already returned successfully — a
badly-typed pushed value continues to be ignored, leaving the entry at its
last good resolved value. Failing a whole running application because one
live external update happened to be malformed would trade a bounded,
already-visible-at-startup problem for an unbounded, runtime one; that
tradeoff wasn't worth making as part of this change. See [Non-goals](#non-goals).

## Rationale

### Aggregate every failure, don't stop at the first

A config with several broken fields is reported in one `Populate` failure,
not discovered one fix-and-rerun cycle at a time. `walkAndInject`
accumulates every conversion failure across the whole struct tree and every
backend layer, and `Populate` returns them joined into a single error
(`errors.Join`) once the walk finishes. This mirrors the same reasoning
applied to duplicate flag-name detection in the `pflag` backend: report
everything a pass can already see in one failure, not one concern at a
time.

`Backend.Lookup` errors are a separate, pre-existing case and are
deliberately *not* folded into this aggregation: a `Lookup` error can
indicate a broken backend connection, not just a bad value, and keeps
returning immediately, exactly as before this change. Aggregation applies
only to conversion (coercion) failures.

### Every failing layer is reported, not just the one that would have won

A backend layer's `Lookup` can return `ok == true` for a field and still
supply an unparseable value, even if a higher-precedence layer would have
overridden it anyway. That layer is reported regardless. `Populate` only
ever calls a backend's `Lookup` for a path that corresponds to a real struct
field — the backend is specifically claiming "I have a value for this exact
field" — so a conversion failure there is a genuine problem with that
layer's configuration, independent of what ultimately wins the resolution.
Not reporting it because a higher layer happens to be valid this run would
leave a landmine for the day that higher layer's value is removed.

### Partial results are kept on failure, not rolled back

Because conversion errors aggregate instead of stopping the walk, a failed
`Populate` call can leave some fields resolved (`IsSet()==true`, `Value()`
populated) while others remain unset — whichever fields' own coercion
happened to succeed before the walk finished. This is intentional: the only
contract callers get is "don't trust the struct until `err == nil`," not
"an error means nothing was touched."

A rollback-to-zero-state alternative was considered and rejected: tracking
every field touched during a failed walk and resetting each one
(`isSet=false`, resolved value zeroed, slots cleared) before `Populate`
returns its error, for full atomicity. That would have discarded
correctly-resolved data for no benefit to the dominant abort-on-error usage
pattern (a caller that treats any `Populate` error as fatal never reads
`cfg` again, so it doesn't matter whether partial state was rolled back).
It would also have actively hurt the retry-capable case — a test that fixes
one broken backend and calls `Populate` again would have its already-correct
fields wiped and forced through unnecessary re-resolution, for no
corresponding upside.

### Same-field staleness: a failed higher-precedence override

The same kind of staleness can also happen within a single field, not just
across fields. If a higher-precedence layer's value fails coercion,
resolution never reaches that layer — the entry is left showing whichever
lower-precedence layer's value last resolved successfully, which is
indistinguishable from that higher-precedence layer never having addressed
the field at all.

Mechanically: a layer's slot is only marked resolved (`ok = true`) *after*
its value successfully coerces. A coercion failure returns before that
write happens, so the slot keeps its zero-value default, and resolution
(which scans from the highest-precedence slot down and stops at the first
`ok` one) simply skips it and lands on the next lower slot that succeeded.
Nothing in the entry's readable state records "a higher-precedence layer
attempted to set this field and failed" — from the caller's point of view,
`Value()` returns a real, previously-valid value, not an obviously-wrong
sentinel, which makes this staleness easy to mistake for the intended final
value rather than a rejected override's fallback.

This is a reasonable consequence of the "keep partial results" decision
above, not a separate bug: the same "don't trust the struct until `err ==
nil`" contract already covers it.

## Retry semantics

A failed `Populate` call does not permanently lock the struct — only a
successful call does that. The same struct instance remains valid to pass
to `Populate` again once the cause of the failure has been addressed, and a
retry starts clean: slot storage is re-initialized on every `Populate` call,
so a corrected backend value on the next attempt resolves normally with no
leftover state from the failed attempt.

This is not an automatic-retry mechanism, and most callers — a service
loading its config once at startup — should treat a non-nil `Populate` error
as fatal. The allowance exists for callers that construct config
incrementally: a test that isolates one backend, or a tool that lets a user
correct a bad source in place, without being forced to start over with a
new struct instance.

## Error message shape

A conversion failure is wrapped as `confstruct: backend %q field %q: %w`,
matching the existing `confstruct: backend %q lookup %q: %w` convention
already used for `Lookup` errors — both share one small helper so the
wording can't drift between the two call sites. The underlying coercion
error itself (`"value %v overflows %s"`, `"cannot parse %q as %s"`, etc.)
carries no `confstruct: ` prefix of its own, so wrapping it doesn't produce
a doubled prefix; the outer wrap supplies the single `confstruct: ` prefix
for the whole message, e.g.:

```
confstruct: backend "pflag" field "Database.Port": value abc overflows int8
```

## Non-goals

- Making `Backend.Lookup` errors participate in the same aggregation as
  conversion errors — see [Aggregate every failure](#aggregate-every-failure-dont-stop-at-the-first)
  above for why they stay separate.
- Changing `WatchableBackend` push-time coercion-failure handling — it stays
  silent, preserving a running application's stability against one bad live
  update. See [Scope: initial population only](#scope-initial-population-only).
- A way to distinguish, after a *successful* `Populate`, "unset because no
  backend addressed this field" from "unset because coercion failed." This
  is moot once a successful `Populate` can no longer have an unreported
  conversion failure — the two cases only ever looked alike because the
  failure case used to slip through silently.
