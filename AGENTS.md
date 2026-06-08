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

## Testing conventions

When running tests, always target a specific package — never use `./...`. For example:

```
go test github.com/suyono3484/confstruct
```

The `example/` package has its own tests but they are opt-in and must be run explicitly when needed. Using `./...` would pull them into every test run, which is not the intent.

## Prior art context

This library exists because `spf13/viper` and `knadh/koanf` are too flexible — their flexibility makes it easy to lose consistency across a codebase, and both treat struct binding as an output step rather than a first-class contract.
