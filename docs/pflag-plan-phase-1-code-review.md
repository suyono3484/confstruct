# Code review — Phase 1 (identifier-to-flag-name conversion)

Reviews the `add_pflag_phase1` branch: the new `pflag/` package
(`pflag_name.go`, `pflag_name_test.go`) implementing
[Phase 1](pflag-plan-phase-1-name-conversion.md), plus the accompanying
doc update marking that phase done.

This is a live tracker: update the Status column as findings are fixed,
deferred, or rejected, rather than editing history into the Notes.

## Tracker

| Finding | Status | Notes |
| --- | --- | --- |
| [F1: non-ASCII / underscore identifiers bypass the ASCII-only contract](#f1-non-ascii--underscore-identifiers-silently-bypass-the-ascii-only-contract) | Fixed | `splitIdentifierWords` now returns an error for any `classOther` rune; `derivedPFlagName`/`pflagName` propagate it. |

Status values: `Open`, `Fixed`, `Won't fix`, `Deferred`.

## Result

The implementation matches its spec closely. The word-splitting state
machine was hand-traced against every row in the identifier-conversion
table (`TLSConfig`, `HTTPServerPort`, `HTTP2Server`, `Server2Port`,
`IPv6Address`, nested paths) and the tag-validation grammar, and all check
out; tests pass, `gofmt`/`go vet` are clean, and no reuse, simplification,
efficiency, or convention issues turned up.

## Findings

### F1: non-ASCII / underscore identifiers silently bypass the ASCII-only contract

**Status:** Fixed — `splitIdentifierWords` now returns an error as soon as
it encounters a `classOther` rune (rather than silently folding it into the
current word), and `derivedPFlagName` wraps that into
`invalid field name %q: ...; use cs.pflag to specify an explicit flag
name`. `pflagName` propagates the error unchanged when no `cs.pflag` tag is
present to short-circuit derivation. Covered by
`TestDerivedPFlagName_invalidCharacter` (underscore and non-ASCII cases) and
`TestPFlagName_derivedFallbackInvalidCharacter`.

**File:** `pflag/pflag_name.go:55`

`classOf()` buckets underscores, non-ASCII letters, and any other
non-`[a-zA-Z0-9]` rune as `classOther`, but the boundary switch (lines
74–82) never handles a transition into or out of that class. Such
characters are silently folded into the current word (only lowercased)
instead of erroring or forcing a `cs.pflag` override.

**Failure scenario:** a legal Go field name like `DB_Host` derives to
`db_host`, and a Unicode field name like `Café` derives to `café` —
both contain characters outside `[a-z0-9-]`. This violates the
documented contract in
[pflag-integration.md#identifier-to-flag-name-conversion](pflag-integration.md#identifier-to-flag-name-conversion):

- Rule 1: "the result contains lowercase ASCII letters, digits, and `-`
  only."
- Rule 6: "a name containing non-ASCII letters, punctuation, or an
  otherwise undesired spelling must use `cs.pflag`... The automatic
  conversion should not silently invent a lossy transliteration."

No test in `pflag/pflag_name_test.go` exercises an underscore or
non-ASCII field name, so this silent contract violation ships
undetected, and `Populate` would register an invalid pflag long-flag
name with no error raised.

**Suggested fix direction:** either treat any `classOther` rune as a hard
error from `pflagName`/`derivedPFlagName` (forcing the author to use
`cs.pflag`), or explicitly strip/reject non-letter/non-digit runes —
whichever the maintainers prefer should also get a test case added
alongside the existing table-driven ones.

## Considered and not flagged

- **Raw vs. trimmed tag text in the invalid-tag error message**
  (`pflag/pflag_name.go:126` uses `raw`, not `trimmed`). Neither
  `pflag-plan-phase-1-name-conversion.md` nor
  `pflag-integration.md`'s tag-validation table specifies which of the
  two should be embedded when they diverge (whitespace-bearing invalid
  tags aren't in the table). Echoing `raw` is a defensible choice — it
  shows the author exactly what they wrote, including stray whitespace.
  Minor test-coverage gap only: `TestPFlagName_invalidTagErrorText` only
  exercises a tag where `raw == trimmed`.
- **Forward-looking risk notes about Phase 2/3 call-convention
  compatibility** (e.g. the unexported-interface cross-package problem,
  the unused `path` parameter in `pflagName`). These are already called
  out as open design questions in
  [pflag-integration.md#package-layout](pflag-integration.md#package-layout)
  and the Phase 2/3 plan docs, not new defects introduced by this diff.
