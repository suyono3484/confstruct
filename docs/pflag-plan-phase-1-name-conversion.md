# Phase 1 — Identifier-to-flag-name conversion + tag grammar

Part of [the `pflag` implementation plan](pflag-implementation-plan.md).
Previous: Phase 0 — `Populate` conversion-error aggregation, shipped; see
[populate-error-handling.md](populate-error-handling.md) for the settled
design rationale. Next:
[Phase 2 — struct-wide duplicate-name
validation](pflag-plan-phase-2-duplicate-detection.md).

Source: [Identifier-to-flag-name
conversion](pflag-integration.md#identifier-to-flag-name-conversion) and
[Pflag tag validation](pflag-integration.md#pflag-tag-validation). Pure
logic, no `pflag` dependency yet — this can be written and fully unit-tested
before touching `go.mod`.

## Tracker

| Step | Status | Notes |
| --- | --- | --- |
| [1.1 New file `pflag_name.go`](#11-new-file-pflag_namego) | Not started | |
| [1.2 Word-splitting algorithm](#12-word-splitting-algorithm-state-machine) | Not started | |
| [1.3 Tests](#13-tests--pflag_name_testgo) | Not started | |

Status values: `Not started`, `In progress`, `Done`.

## 1.1 New file `pflag_name.go`

```go
package confstruct

// pflagNameTag is the struct tag an entry field uses to override its
// derived long flag name.
const pflagNameTag = "cs.pflag"

// derivePFlagWord splits a single Go identifier into lowercase, hyphenless
// words following the rules in docs/pflag-integration.md
// #identifier-to-flag-name-conversion.
func splitIdentifierWords(name string) []string { ... }

// derivedPFlagName joins every segment of a field-chain path into one
// derived flag name: word-split each Go field name, lowercase, hyphen-join
// within a segment, hyphen-join across segments.
func derivedPFlagName(fields []reflect.StructField) string { ... }

// pflagTagRe is the accepted grammar for an explicit cs.pflag tag value,
// after trimming outer whitespace: ^[a-z][a-z0-9]*(-[a-z0-9]+)*$
var pflagTagRe = regexp.MustCompile(`^[a-z][a-z0-9]*(-[a-z0-9]+)*$`)

// pflagName resolves the final flag name for one entry field: the trimmed
// cs.pflag tag if present (validated against pflagTagRe), otherwise the
// derived name. Returns an error identifying the offending tag value if
// validation fails.
func pflagName(path string, fields []reflect.StructField) (string, error) { ... }
```

Keep `path` in the `pflagName` signature even though the current
`derivedPFlagName` sketch only needs `fields` — the pflag-integration doc's
error format includes the struct field path (`"Database.Port"`), and `path`
is the cheapest way to render it without re-deriving it from `fields`.

## 1.2 Word-splitting algorithm (state machine)

Scan each Go identifier once, byte by byte, tracking the previous
character's class (lower, upper, digit). Emit a word boundary:

1. Lower → Upper (`ListenAddr` → `Listen`, `Addr`).
2. Upper → Upper-then-lower: the *last* upper of a run starts a new word
   when followed by a lowercase letter (`HTTPServer` → `HTTP`, `Server`;
   the boundary is before the `S`, not before each capital).
3. Digit → Upper (`HTTP2Server` → boundary before `Server`, not before `2`).
4. No boundary on letter → digit (`Server2` stays one word: `server2`).

Lowercase every emitted word; join words within one field name with `-`;
join field-chain segments with `-`.

Implement this as a small explicit-state loop (three states: `lower`,
`upper`, `digit`) rather than a regexp cascade — the table in the source doc
depends on lookahead (upper-then-lower vs. upper-then-upper) that's awkward
to express as independent regex substitutions and easy to get subtly wrong
(the doc's own worked example, `HTTPServerPort`, is exactly the case a naive
"insert hyphen before every capital" implementation gets wrong).

## 1.3 Tests — `pflag_name_test.go`

Table-driven, directly encoding [the conversion
table](pflag-integration.md#identifier-to-flag-name-conversion):

| Input path | Expected derived name |
| --- | --- |
| `Port` | `port` |
| `ListenAddr` | `listen-addr` |
| `TLS` | `tls` |
| `TLSConfig` | `tls-config` |
| `HTTPServerPort` | `http-server-port` |
| `HTTP2Server` | `http2-server` |
| `Server2Port` | `server2-port` |
| `IPv6Address` | `i-pv6-address` (documents the case that needs a tag) |
| `Database.HTTP2ServerPort` | `database-http2-server-port` |

Plus, for `pflagName` / the tag grammar:

- Every row in [the tag validation
  table](pflag-integration.md#pflag-tag-validation): empty tag, whitespace-only
  tag, `--db-port`, `db host`, `db_host`, `DBPort`, `db--port`, `-db-port`,
  `db-port-` → all error.
- `"  db-port  "` → valid, resolves to `db-port` (outer whitespace trimmed).
- No tag → falls back to the derived name.
- Empty `fields` slice / empty `path` — decide and test the degenerate case
  (should not panic; likely returns an empty string derived name, which is
  only reachable via direct `Backend.Lookup` use on the empty path, not
  through `Populate`).

This phase ships as an internal, untested-by-external-API helper — no
exported function. Land it with its own PR and full table coverage before
[Phase 2](pflag-plan-phase-2-duplicate-detection.md) depends on it.
