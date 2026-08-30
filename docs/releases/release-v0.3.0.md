# v0.3.0 — PFlag Backend

## Highlights

- New `PFlag` backend, in its own package
  `github.com/suyono3484/confstruct/pflag`, reads explicitly-provided
  command-line flags from an already-parsed `spf13/pflag` `*FlagSet`.
- Flag long names are derived automatically from struct field paths,
  word-boundary- and initialism-aware (`Database.HTTP2ServerPort` →
  `database-http2-server-port`), with `cs.pflag` as a per-field override
  tag for names the derivation gets wrong (`IPv6Address` would otherwise
  derive to `i-pv6-address`).
- Presence, not defaults: only a flag with `Changed == true` is treated as
  set. An unprovided flag's declared default is never mistaken for a
  configuration value, so `--verbose=false` and `--db-port=0` remain
  explicitly set and still override a lower-precedence layer.
- Two entry fields resolving to the same flag name — by derivation or by
  an explicit `cs.pflag` tag — within one `Populate` call now cause
  `Populate` to fail structurally, before any flag is looked up. The
  check is scoped per `Populate` call, so the same flag name reused
  across two independently-populated structs (e.g. per-subcommand
  configs via `cobra`) is not a conflict.

## New public API

- `github.com/suyono3484/confstruct/pflag`: `PFlag(flags *pflag.FlagSet) confstruct.Backend`.
- `confstruct.FieldPath`, `confstruct.NameCollisionChecker`/
  `NameCollisionSeal`/`NewNameCollisionSeal`, and
  `confstruct.FieldLookuper`/`FieldLookupSeal`/`NewFieldLookupSeal`:
  exported adapter types that let a backend defined *outside* package
  `confstruct` participate in per-field tag-aware lookup and
  struct-wide duplicate-name validation — mechanisms that were previously
  only reachable via unexported hooks usable within package `confstruct`
  itself. `PFlag` is the first consumer; these exist for any future
  out-of-package backend with the same needs. See
  [docs/backend-shapes.md](../backend-shapes.md) for the underlying
  "pure key-value vs. hybrid field-aware" backend model these support.

## Behavior changes

- None for existing backends (`Map`, `Override`, `MapFromTags`, `Env`,
  `File`) — this release is purely additive.
- `Populate` gains one more structural pre-pass, checking for
  backend-declared name collisions, before its usual per-field walk. It
  is a no-op unless a registered backend implements the new
  `NameCollisionChecker` hook, which today only `PFlag` does.

## Notes

- `github.com/spf13/pflag` is now a direct dependency.
- See the README's [PFlag](../../README.md#pflag) section for usage, and
  [docs/pflag-integration.md](../pflag-integration.md) for the full design
  rationale (name derivation rules, duplicate-detection scoping, the
  cross-package adapter mechanism, and everything explicitly out of
  scope for this backend).
- An example program and its tests are available under `example/pflag`
  with the `example` build tag.
