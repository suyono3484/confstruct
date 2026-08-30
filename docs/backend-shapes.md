# Backend shapes: pure key-value vs. hybrid field-aware

## Status

Settled design rationale, describing behavior already implemented by every
built-in backend (`Map`, `Override`, `Env`, `File`, `PFlag`). Not a plan —
there is nothing left to build here. This document exists because the
distinction it describes was worked out in conversation while unblocking
[`pflag-plan-phase-3-backend.md`](pflag-plan-phase-3-backend.md), never
written down on its own, and applies to every backend, not just `pflag`.

## The two shapes

Every backend implements the base [`Backend`](../confstruct.go) interface:

```go
type Backend interface {
    Lookup(path string) (any, bool, error)
    Name() string
    Describe() string
}
```

`Lookup` takes a plain, dot-separated struct path (`"Database.Port"`) and
nothing else. That is enough for a backend whose source has no concept of
Go struct tags — but not enough for one that wants to let an entry field
override its default resolution via a tag, since a tag lives on a
`reflect.StructField`, not on a path string. Backends therefore split into
two shapes:

- **Pure key-value.** The source is inherently a flat or nested map with no
  tag concept: `Map` and `Override`. `Lookup` is their only resolution
  mode, and it is genuinely the *only* method `Populate` ever calls on
  them.
- **Hybrid, field-aware.** The source supports tag-driven customization on
  top of a derived default: `Env` (`cs.env`), `File`
  (`cs.file.segment-alias`), and `PFlag` (`cs.pflag`). Each of these
  implements `Lookup` *and* the unexported `fieldAwareBackend` hook
  (`confstruct.go:129`):

  ```go
  type fieldAwareBackend interface {
      lookupField(path string, fields []reflect.StructField) (any, bool, error)
  }
  ```

  `lookupField` receives the full chain of `reflect.StructField`s leading
  to the entry, which is what lets `Env` read a `cs.env` tag, `File` read
  `cs.file.segment-alias`, and `PFlag` read `cs.pflag` — none of which a
  bare path string could carry.

This is not "some backends are smarter than others." A hybrid backend
deliberately keeps two independent entry points into the same underlying
resolution logic — one that needs the full `Populate` machinery to supply
a field chain, and one that works standalone off nothing but a path
string. `File`'s plain `Lookup` still does real structural work (nested
map traversal, case-insensitive segment matching) — "key-value" here means
"no tag awareness," not "no structure at all."

## The dispatch mechanism

`Populate` never chooses a mode by asking what kind of backend it has. It
always tries the enhanced mode first and falls back automatically —
`lookupBackendValue` (`confstruct.go:817`):

```go
func lookupBackendValue(b Backend, path string, fields []reflect.StructField) (any, bool, error) {
    if fb, ok := b.(fieldAwareBackend); ok {
        return fb.lookupField(path, fields)
    }
    return b.Lookup(path)
}
```

For `Map`/`Override`, the type assertion always fails, so `Populate`
always calls `Lookup`. For `Env`/`File`/`PFlag`, it always succeeds, so
`Populate` never calls their `Lookup` at all — `lookupField` wins every
time a struct is actually being populated.

| Backend | Implements `fieldAwareBackend`? | Tag | `Populate` ever calls plain `Lookup`? |
| --- | --- | --- | --- |
| `Map` | No | — | Yes — always |
| `Override` | No | — | Yes — always |
| `Env` | Yes | `cs.env` | No — `lookupField` always wins |
| `File` | Yes | `cs.file.segment-alias` | No — `lookupField` always wins |
| `PFlag` | Yes | `cs.pflag` | No — `lookupField` always wins |

## Why a hybrid backend's plain `Lookup` still has to work correctly

If `Populate` never calls it, it would be easy to assume `Lookup` on a
hybrid backend is dead weight — a method that exists only because Go
interfaces are all-or-nothing. It isn't. Two things depend on it working
correctly on its own:

1. **Direct, `Populate`-independent backend use.** `file_test.go` calls
   `b.Lookup(path)` directly in most of its tests (`TestFile_YAML`,
   `TestFile_CaseInsensitive`, and others) to exercise a backend's own
   resolution logic in isolation, without constructing a whole config
   struct. `pflag_test.go`'s `TestPFlag_LookupDirect` and
   `TestPFlag_LookupDirectIgnoresTag` follow the same pattern. This is also
   the tool available to an application that wants a quick ad-hoc check
   ("does this `pflag.FlagSet` actually have `--listen-addr` set?") outside
   the struct-first flow.
2. **The base `Backend` contract itself.** The
   [README](../README.md#backend-interfaces) documents `Lookup` as "called
   once per field during `Populate`" — that's the whole public contract a
   reader sees. `fieldAwareBackend` is deliberately unexported and
   undocumented there; a third-party `Backend` implementation has no way to
   participate in the enhanced mode at all; from the outside, `Lookup`
   *is* the interface.

**This is not a second way to read config, and does not conflict with
[AGENTS.md](../AGENTS.md#design-constraints)'s "one canonical way to read
config: through the populated struct."** Calling `backend.Lookup(path)`
directly returns one backend's raw contribution to a field, with no
coercion into the entry's declared type, no precedence resolution against
other layers, and no `IsSet`/`Value` semantics — it is a tool for testing
or inspecting a single backend, not an alternative to `Populate` for
application code. Nothing in this library exposes it as such: it has no
mention in the README's [Usage](../README.md#usage) section, and every
built-in backend's own doc comment frames it as a `Populate`-internal
detail, not a feature to build application logic on.

Because `Lookup` still has to produce a *correct* answer on its own, a
hybrid backend generally needs a second, string-only derivation path
alongside its field-aware one, since it cannot fabricate a
`reflect.StructField` chain out of nothing just to reuse the field-aware
code (see `pflag`'s `derivedPFlagNameFromPath`, next to the field-aware
`derivedPFlagName`, in `pflag/pflag_name.go`) — and, having no chain,
`Lookup` can never honor a tag override. `TestPFlag_LookupDirectIgnoresTag`
pins this down explicitly: the tagged and untagged forms of the same
logical field resolve to different flag names depending on which path you
use to reach it.

## Cross-package hybrid backends

A hybrid backend implemented outside package `confstruct` — currently only
`pflag` — cannot implement `fieldAwareBackend` directly, because Go
requires an unexported interface method to be declared in the same
package as the interface. That problem, and the seal/adapter mechanism
(`FieldLookupSeal`/`FieldLookuper`) that solves it, is a separate concern
from the shape distinction described here — see
[pflag-integration.md#cross-package-hook-mechanism-decided](pflag-integration.md#cross-package-hook-mechanism-decided)
for that mechanism in full. In short: the seal lets an out-of-package type
still be a hybrid backend; it doesn't change what a hybrid backend *is*.

## When implementing a new backend

Ask whether the source has anything a bare path string can't express —
a per-field naming convention, an escape hatch for names a derivation
algorithm gets wrong, anything that would naturally live on a struct tag.

- If no: implement `Backend` alone. `Map` and `Override` are the reference
  shape.
- If yes: implement `Backend` plus the field-aware hook (`fieldAwareBackend`
  directly if the backend lives in package `confstruct`, or via a seal if
  it doesn't), and make sure the plain `Lookup` path still resolves
  correctly — just without any tag it would otherwise honor. `Env`, `File`,
  and `PFlag` are the reference shape; `pflag/pflag_name.go`'s
  `derivedPFlagName`/`derivedPFlagNameFromPath` pair is the reference for
  keeping the two paths' logic in sync without one depending on the other.
