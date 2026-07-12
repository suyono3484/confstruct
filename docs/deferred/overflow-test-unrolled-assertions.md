# Deferred: unify `TestPopulate_numericOverflowRejected`'s per-field assertions into a loop

**Status:** Won't fix for now — deferred, not abandoned.
**Where:** `confstruct_test.go`, `TestPopulate_numericOverflowRejected` and,
to a lesser extent, `TestPopulate_float32OverflowRejected`.

## Background

`TestPopulate_numericOverflowRejected` populates a struct with four numeric
fields deliberately set to values that overflow their target type
(`Small int8` from `300`, `USmall uint8` from `300`, `Neg uint16` from
`-1`, `Narrow int32` from `5_000_000_000`), and asserts that `Populate`
returns a single joined error mentioning every offending field, and that
none of those fields end up resolved:

```go
func TestPopulate_numericOverflowRejected(t *testing.T) {
	type overflowConfig struct {
		Meta
		Small  Int8Entry
		USmall Uint8Entry
		Neg    Uint16Entry
		Narrow Int32Entry
	}
	var cfg overflowConfig
	cfg.AddLayer(Map(map[string]any{
		"Small":  int64(300),
		"USmall": int64(300),
		"Neg":    int64(-1),
		"Narrow": int64(5_000_000_000),
	}))
	err := Populate(context.Background(), &cfg)
	if err == nil {
		t.Fatal("expected Populate error for overflowing numeric fields, got nil")
	}
	for _, field := range []string{"Small", "USmall", "Neg", "Narrow"} {
		if !strings.Contains(err.Error(), field) {
			t.Errorf("Populate error %q: missing mention of field %q", err.Error(), field)
		}
	}
	if cfg.Small.IsSet() {
		t.Error("Small: IsSet=true after overflow, want false")
	}
	if cfg.Small.Value() != 0 {
		t.Errorf("Small: got %v, want zero value", cfg.Small.Value())
	}
	if cfg.USmall.IsSet() {
		t.Error("USmall: IsSet=true after overflow, want false")
	}
	if cfg.USmall.Value() != 0 {
		t.Errorf("USmall: got %v, want zero value", cfg.USmall.Value())
	}
	if cfg.Neg.IsSet() {
		t.Error("Neg: IsSet=true after overflow, want false")
	}
	if cfg.Neg.Value() != 0 {
		t.Errorf("Neg: got %v, want zero value", cfg.Neg.Value())
	}
	if cfg.Narrow.IsSet() {
		t.Error("Narrow: IsSet=true after overflow, want false")
	}
	if cfg.Narrow.Value() != 0 {
		t.Errorf("Narrow: got %v, want zero value", cfg.Narrow.Value())
	}
}
```

Note it already loops over the field names once, for the error-message
substring check (the `for _, field := range []string{...}` block near the
top). But the `IsSet()`/`Value()` assertions immediately below are **not**
looped — they're eight hand-unrolled, near-identical two-line blocks, one
`IsSet`-check and one `Value`-check per field.

## Why it looks this way today

This shape is not a fresh mistake — it's the result of two prior review
passes layering fixes on top of each other, each addressing what it was
asked to and no more:

1. An early version of this test only asserted that `Populate()` returned a
   non-nil error mentioning each field name; it had no `IsSet()`/`Value()`
   checks at all, so nothing pinned down that overflowing fields actually
   stay unresolved (as opposed to, say, silently landing on a
   wrapped/truncated value). A review pass restored those assertions, but
   as two **OR-combined** checks — `cfg.Small.IsSet() || cfg.USmall.IsSet()
   || cfg.Neg.IsSet() || cfg.Narrow.IsSet()` guarding one generic
   `t.Error("expected all overflowing fields to remain IsSet=false")`, and
   likewise for `Value()` — functionally correct (it does catch a
   regression in any of the four fields), but a failure message didn't say
   *which* field regressed.
2. A follow-up review pass asked for per-field diagnostic specificity,
   pointing at the sibling test `TestPopulate_typeMismatchRejected` as the
   model to match:

   ```go
   func TestPopulate_typeMismatchRejected(t *testing.T) {
   	var cfg testConfig
   	cfg.AddLayer(Map(map[string]any{
   		"Name": 123, // int into StringEntry — mismatch
   	}))
   	err := Populate(context.Background(), &cfg)
   	if err == nil {
   		t.Fatal("expected Populate error for type mismatch, got nil")
   	}
   	if !strings.Contains(err.Error(), "Name") {
   		t.Errorf("Populate error %q: missing mention of field %q", err.Error(), "Name")
   	}
   	if cfg.Name.IsSet() {
   		t.Error("Name: IsSet=true after type mismatch, want false")
   	}
   }
   ```

   (`TestPopulate_typeMismatchRejected` only has one field to check, so its
   "per-field" style is trivially a single named assertion — it never had to
   solve the four-fields-at-once case.) The fix for
   `TestPopulate_numericOverflowRejected` satisfied that ask literally, by
   unrolling the OR-combined checks into one named block per field, without
   also converting the substring-check loop and the new per-field blocks
   into a single shared loop over `[]string{"Small", "USmall", "Neg",
   "Narrow"}`.

The net result: every assertion is individually correct and each names its
field on failure (matching what was asked for), but the test now has two
different iteration styles over the same four-element field list within a
few lines of each other — a loop for the substring check, eight unrolled
blocks for the `IsSet`/`Value` checks.

`TestPopulate_float32OverflowRejected` (the sibling test for `float32`
overflow) only has one field (`Big`), so it was never a target for this
same restructuring — it's mentioned here only because it shares the same
"per-field IsSet/Value assertions after an overflow" shape, at a scale
where the inconsistency doesn't arise.

## Why this matters (failure scenario)

Not a correctness bug — nothing here is wrong today, and every assertion
does what it's supposed to. The cost is maintainability:

- Adding or removing a field from this test means copy-pasting (or
  deleting) another two-line block, instead of adding one entry to the
  existing `[]string{"Small", "USmall", "Neg", "Narrow"}` slice already used
  by the substring-check loop three lines above.
- The stylistic inconsistency (loop for one check, eight unrolled blocks for
  the other, in the same test function) is visible to any reader scanning
  the function top to bottom, and invites a future edit to "fix" only one
  half of the pattern again.

## Suggested fix (deferred)

Unify both checks under the same `[]string{"Small", "USmall", "Neg",
"Narrow"}` loop, with a way to go from field name to the field's
`IsSet()`/`Value()` accessors. Two reasonable shapes:

1. A small per-field accessor table built once at the top of the test,
   e.g. `map[string]func() (bool, any){"Small": func() (bool, any) { return
   cfg.Small.IsSet(), cfg.Small.Value() }, ...}`, then one loop over field
   names driving all three checks (substring, `IsSet`, `Value`).
2. A tiny local struct/slice of `{name string; isSet func() bool; value
   func() any}` built once, iterated the same way.

Either removes the copy-paste and restores a single source of truth for
"the four overflow fields this test covers," matching the style already
used for the substring check.

## Why deferred rather than fixed now

Purely a test-hygiene / diagnostic-clarity concern with no correctness
impact — every assertion currently passes and correctly names its field on
failure. Low priority relative to other planned work. Revisit alongside
other test-hygiene cleanup, or opportunistically the next time this test is
touched for an unrelated reason (e.g. adding a new overflow-prone field).
