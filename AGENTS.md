# Project Instructions

## Design philosophy

This is a struct-first Go configuration library. The central rule: **the struct is the config, not a view of it.**

- Config shape is defined by Go types. String-key access is not a first-class feature.
- Sub-configs compose naturally as nested or embedded structs.
- There is one canonical way to read config: through the populated struct.

## Design constraints

- Do not design APIs that expose a loose key-value store as a primary interface.
- Struct unmarshaling must be the core model, not a deserialization step bolted on at the end.
- Flexibility is a liability here. Prefer opinionated, consistent APIs over generic ones.

`Override`'s `Set` and `Unset` methods are a deliberately narrow, single-backend,
write-only exception for runtime override, hot-reload, and feature-flag use cases.
They exist because backends are constructed independently of the target struct and
therefore address fields by string path; reads still always go through the populated
struct. Do not generalize this pattern to other backends or add further string-keyed
write APIs without updating this carve-out.

## Testing conventions

When running tests, always target a specific package — never use `./...`. For example:

```
go test github.com/suyono3484/confstruct
```

The `example/` packages have their own tests but are opt-in. Run one explicitly
with its build tag when needed:

```
go test -tags=example github.com/suyono3484/confstruct/example/map
```

The same tag is required for `go run` in an example directory, for example
`go run -tags=example .`.

## Prior art context

This library exists because `spf13/viper` and `knadh/koanf` are too flexible — their flexibility makes it easy to lose consistency across a codebase, and both treat struct binding as an output step rather than a first-class contract.
