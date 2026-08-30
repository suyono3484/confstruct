# Phase 3 — `pflagBackend` core

Part of [the `pflag` implementation plan](pflag-implementation-plan.md).
Previous: [Phase 2 — struct-wide duplicate-name
validation](pflag-plan-phase-2-duplicate-detection.md). Next:
[Phase 4 — example and docs](pflag-plan-phase-4-example-docs.md).

Source: [Recommended
direction](pflag-integration.md#recommended-direction), [Proposed
implementation outline](pflag-integration.md#proposed-implementation-outline),
[Semantics](pflag-integration.md#semantics), [Type
support](pflag-integration.md#type-support).

**Package layout (decided):** `pflagBackend` and this whole file live in the
new `github.com/suyono3484/confstruct/pflag` package (see
[pflag-integration.md#package-layout](pflag-integration.md#package-layout)),
not in package `confstruct`. `pflagBackend` satisfies `confstruct`'s
unexported `fieldAwareBackend` hook by embedding the exported
`confstruct.FieldLookupSeal` and implementing `LookupFieldValue`, per
[pflag-integration.md#cross-package-hook-mechanism-decided](pflag-integration.md#cross-package-hook-mechanism-decided).
It never calls `backendErr` directly — that unexported function isn't
reachable across the package boundary regardless of the seal, but
`walkAndInject` already wraps whatever plain error `lookupField` returns at
its own call site, exactly as it does today for `Env`/`File`. The sketch in
[3.2](#32-new-file-pflagpflaggo) below is updated to this shape.

**Prerequisite now done:** `confstruct.FieldLookuper`/`FieldLookupSeal`
were only documented in
[pflag-integration.md#cross-package-hook-mechanism-decided](pflag-integration.md#cross-package-hook-mechanism-decided)
until now — no phase actually added them to `confstruct.go`, even though
this file's sketch assumed they existed (mirroring how `NameCollisionSeal`
was added to `confstruct.go` in Phase 2, but its `FieldLookuper` sibling
was not). They're now implemented, next to `fieldAwareBackend`, with the
same nil-`impl` guard `NameCollisionSeal.checkNames` has. `pflagBackend`
can now actually embed `confstruct.FieldLookupSeal` as sketched below.

## Tracker

| Step | Status | Notes |
| --- | --- | --- |
| [3.1 Dependency](#31-dependency) | Not started | |
| [3.2 New file `pflag/pflag.go`](#32-new-file-pflagpflaggo) | Not started | |
| [3.3 Type coercion](#33-type-coercion) | Not started | |
| [3.4 Tests](#34-tests--pflag_testgo) | Not started | |
| [3.5 Godoc](#35-godoc) | Not started | |

Status values: `Not started`, `In progress`, `Done`.

## 3.1 Dependency

```
go get github.com/spf13/pflag
```

Adds a direct (non-indirect) requirement to `go.mod`. No Cobra dependency —
Cobra exposes `*pflag.FlagSet` directly via `cmd.Flags()`.

## 3.2 New file `pflag/pflag.go`

Mirrors `env.go`'s shape (license header, package, doc comment on the
constructor) as far as style goes, but note it is `package pflag`, in its
own directory, importing `confstruct` rather than being part of it — see
the package-layout note above. `pflagBackend` embeds both seal types from
[pflag-integration.md#cross-package-hook-mechanism-decided](pflag-integration.md#cross-package-hook-mechanism-decided)
and [Phase 2.1](pflag-plan-phase-2-duplicate-detection.md#21-new-optional-backend-interface),
which is why `PFlag` must be a real constructor rather than a bare struct
literal — the seals need a reference back to `b` that only exists once `b`
is allocated.

**`PFlagBackendName` already exists — do not redeclare it here.** Phase 2
needed it before `pflagBackend` did (`checkFieldNames`/`pflagBackendErr`
both reference it), so it's already declared in `pflag/pflag_collision.go`.
Declaring it again in this file would be a duplicate top-level declaration
in the same package and fail to compile. The sketch below omits it; use
the constant from `pflag_collision.go` as-is.

```go
package pflag

import (
	"fmt"
	"reflect"

	"github.com/suyono3484/confstruct"
	spfpflag "github.com/spf13/pflag"
)

type pflagBackend struct {
	confstruct.FieldLookupSeal
	confstruct.NameCollisionSeal
	flags *spfpflag.FlagSet
}

// PFlag returns a Backend that reads explicitly-provided command-line flags
// from an already-parsed *pflag.FlagSet. It owns no parsing and performs no
// writes: the application defines and parses its flags, adds the resulting
// backend as its highest-precedence layer, and then calls Populate.
//
// Only a flag with Changed == true is considered present; an unprovided
// flag's declared default is not a configuration value and the entry falls
// through to the next lower-precedence layer. See
// docs/pflag-integration.md for the full design rationale.
func PFlag(flags *spfpflag.FlagSet) confstruct.Backend {
	b := &pflagBackend{flags: flags}
	b.FieldLookupSeal = confstruct.NewFieldLookupSeal(b)
	b.NameCollisionSeal = confstruct.NewNameCollisionSeal(b)
	return b
}

func (b *pflagBackend) Name() string { return PFlagBackendName }

func (b *pflagBackend) Describe() string {
	if b.flags == spfpflag.CommandLine {
		return "command-line"
	}
	return b.flags.Name()
}

// Lookup derives a flag name straight from path, for direct Backend use
// outside of Populate/Meta. Populate itself always calls LookupFieldValue
// instead, since only that path has the struct-field chain a cs.pflag tag
// lives on.
func (b *pflagBackend) Lookup(path string) (any, bool, error) {
	name := derivedPFlagName(splitPathIntoChainlessSegments(path))
	return b.lookupName(name)
}

// LookupFieldValue satisfies confstruct.FieldLookuper; FieldLookupSeal
// forwards fieldAwareBackend.lookupField calls here. The returned error is
// deliberately unwrapped -- walkAndInject wraps it with backendErr at its
// own call site, exactly as it does for Env/File.
func (b *pflagBackend) LookupFieldValue(path string, fields []reflect.StructField) (any, bool, error) {
	name, err := pflagName(path, fields)
	if err != nil {
		return nil, false, err
	}
	return b.lookupName(name)
}

func (b *pflagBackend) lookupName(name string) (any, bool, error) {
	flag := b.flags.Lookup(name)
	if flag == nil || !flag.Changed {
		return nil, false, nil
	}
	return flag.Value.String(), true, nil
}

// CheckFieldNames satisfies confstruct.NameCollisionChecker; NameCollisionSeal
// forwards nameCollisionBackend.checkNames calls here. checkFieldNames is the
// standalone function Phase 2 already implemented and tested in
// pflag/pflag_collision.go -- see
// docs/pflag-plan-phase-2-duplicate-detection.md#24-field-name-collision-detection-checkfieldnames.
// This method has no logic of its own; it exists only because Phase 2 could
// not take a *pflagBackend receiver before this type existed.
func (b *pflagBackend) CheckFieldNames(entries []confstruct.FieldPath) error {
	return checkFieldNames(entries)
}
```

`derivedPFlagName` currently takes a `[]reflect.StructField` chain (Phase
1), but plain `Backend.Lookup(path)` only has a dot-separated string, with
no `reflect.StructField`s to inspect for a `cs.pflag` tag — which is exactly
why the doc calls this "Fallback for direct Backend use" and why the tag
only ever applies through `Populate`. Give `derivedPFlagName` a sibling that
accepts plain path segments (split on `.`) so `Lookup` can still derive a
name without needing a fabricated `reflect.StructField` chain — do not
special-case `Lookup` on top of `Populate`'s literal call path, since bare
`Backend.Lookup` is a legitimate direct-use entry point documented for every
other backend (`Env.Lookup`, `File.Lookup`).

## 3.3 Type coercion

No new work needed: `lookupName` returns `flag.Value.String()` and `ok ==
true`, exactly like `Env`/`File`; the existing `coerce[T]` path (now
error-returning, see
[populate-error-handling.md](populate-error-handling.md)) does the rest.
Confirm during testing that:

- `--db-port=abc` against `IntEntry` now surfaces a `Populate` error
  instead of silently leaving the field unset.
- `--verbose=false` and `--db-port=0` remain `Changed == true` and override
  a lower layer's `true`/non-zero value.

## 3.4 Tests — `pflag/pflag_test.go`

Run with `go test github.com/suyono3484/confstruct/pflag`, following the
project's per-package test convention in
[AGENTS.md](../AGENTS.md#testing-conventions) — this is now a second
package with its own test target, distinct from
`go test github.com/suyono3484/confstruct`.

Full matrix from [Test matrix for an
implementation](pflag-integration.md#test-matrix-for-an-implementation):

- A changed scalar flag overrides `Map`, `File`, and `Env` layers (four-layer
  `Populate` call, assert `SourceName() == PFlagBackendName`).
- An unchanged flag falls through even when its pflag default differs from
  the lower layer's value.
- `--flag=false`, `--flag=0`, `--flag=""` are present and win over a
  differing lower-layer value.
- Derived top-level and nested names resolve correctly; `cs.pflag` overrides
  them (reuse [Phase 1](pflag-plan-phase-1-name-conversion.md)'s table as
  fixtures, wired through an actual `pflag.FlagSet` + `Populate` this time,
  not just the naming helper in isolation).
- A missing flag (no flag by that name in the `FlagSet` at all) falls
  through without error.
- Values parse into every supported entry type — string, bool, every signed
  and unsigned int width, both float widths — including a boundary
  (`int8` max/min) and an incompatible string (post-Phase-0, expect a
  `Populate` error, not silent fallthrough).
- `SourceName()`/`SourceDesc()` report `PFlagBackendName`/`"command-line"`
  (or the custom `FlagSet.Name()`) for a winning flag; an `OnResolve` hook
  fires once with that backend identity.
- `PFlag` is accepted as the *lowest* layer (it's static, like `Map`) even
  though the doc recommends against that placement; it is never mistakenly
  rejected by the "lowest layer must not be watchable" check, since it does
  not implement `WatchableBackend`.
- The one duplicate-name case [Phase
  2.5](pflag-plan-phase-2-duplicate-detection.md#25-tests--in-confstruct_testgo-and-pflagpflag_collision_testgo)
  explicitly deferred here: the check fires even when the `*pflag.FlagSet`
  passed to `PFlag` doesn't define either colliding flag at all, and
  regardless of `Changed`. Every other duplicate-name/invalid-tag case is
  already covered as a pure `checkFieldNames` unit test in
  `pflag/pflag_collision_test.go` and doesn't need repeating here.

## 3.5 Godoc

Add `pflag/pflag.go`'s own package-level doc comment (this package now
needs one, since it's a separate importable unit rather than a file inside
`confstruct`'s existing doc block). Also update the package-level doc
comment in `confstruct.go`'s doc block
(`confstruct.go:15-75`) mentioning `PFlag` alongside `Env`/`File` once it
ships, matching how `cs.env`/`cs.file.segment-alias` are already listed at
`confstruct.go:60-61`.

Continue to [Phase 4 — example and docs](pflag-plan-phase-4-example-docs.md).
