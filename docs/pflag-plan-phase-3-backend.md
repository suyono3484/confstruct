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
not in package `confstruct`. The sketch in [3.2](#32-new-file-pflagpflaggo) below
predates that decision and needs updating: it uses `package confstruct` and
calls the unexported `fieldAwareBackend`/`backendErr` hooks directly, none
of which a separate package can do. This phase depends on however
[Phase 2](pflag-plan-phase-2-duplicate-detection.md#package-layout-decided--blocker-for-this-phase-as-drafted)
resolves the cross-package mechanism question — treat the code below as the
shape of the intent, not a buildable sketch, until that's settled.

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
the package-layout note above. The unexported `backendErr` call below is a
placeholder for whatever cross-package mechanism Phase 2 settles on (an
exported equivalent, most likely); it will not compile as an unexported
`confstruct` symbol referenced from another package.

```go
package pflag

import (
	"fmt"
	"reflect"

	"github.com/suyono3484/confstruct"
	spfpflag "github.com/spf13/pflag"
)

// PFlagBackendName is the Name() identifier for a PFlag backend.
const PFlagBackendName = "pflag"

type pflagBackend struct {
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
	return &pflagBackend{flags: flags}
}

func (b *pflagBackend) Name() string { return PFlagBackendName }

func (b *pflagBackend) Describe() string {
	if b.flags == spfpflag.CommandLine {
		return "command-line"
	}
	return b.flags.Name()
}

// Lookup derives a flag name straight from path, for direct Backend use
// outside of Populate/Meta. Populate itself always calls lookupField
// instead, since only that path has the struct-field chain a cs.pflag tag
// lives on.
func (b *pflagBackend) Lookup(path string) (any, bool, error) {
	name := derivedPFlagName(splitPathIntoChainlessSegments(path))
	return b.lookupName(name)
}

func (b *pflagBackend) lookupField(path string, fields []reflect.StructField) (any, bool, error) {
	name, err := pflagName(path, fields)
	if err != nil {
		return nil, false, backendErr("lookup", b, path, err)
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

func (b *pflagBackend) checkNames(entries []fieldPath) error {
	// see Phase 2.4: docs/pflag-plan-phase-2-duplicate-detection.md#24-pflagbackendchecknames
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
- Duplicate-name and invalid-tag cases from [Phase
  2.5](pflag-plan-phase-2-duplicate-detection.md#25-tests--in-confstruct_testgo-or-a-new-pflag_testgo)
  (co-located here or there — pick one file and cross-reference, don't split
  the same feature's tests across two files).

## 3.5 Godoc

Add `pflag/pflag.go`'s own package-level doc comment (this package now
needs one, since it's a separate importable unit rather than a file inside
`confstruct`'s existing doc block). Also update the package-level doc
comment in `confstruct.go`'s doc block
(`confstruct.go:15-75`) mentioning `PFlag` alongside `Env`/`File` once it
ships, matching how `cs.env`/`cs.file.segment-alias` are already listed at
`confstruct.go:60-61`.

Continue to [Phase 4 — example and docs](pflag-plan-phase-4-example-docs.md).
