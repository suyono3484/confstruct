# v0.1.0 — Initial Release

`confstruct` is a struct-first Go configuration library built around one
principle: the populated struct is the configuration API.

## Highlights

- Define configuration with typed entry fields and read values directly from
  the populated struct.
- Compose nested configuration naturally with nested Go structs.
- Layer configuration sources in explicit precedence order, with eager value
  resolution and thread-safe reads.
- Distinguish unset fields from fields explicitly set to their zero value with
  `IsSet`, and validate required coverage with `UnsetFields`.
- Observe initial resolution and meaningful live updates with `OnResolve`.

## Built-in backends

- `Map` for Go values and application defaults.
- `MapFromTags` for struct-tag defaults.
- `Env` for OS environment variables and optional `.env` files, including
  prefixes and `cs.env` field aliases.
- `File` for YAML, JSON, and TOML configuration files, including
  case-insensitive matching and `cs.file.segment-alias` support.
- `Override` for narrow, write-side runtime overrides that re-resolve values
  immediately while reads remain struct-based.

## Notes

- Numeric values are safely coerced to compatible entry types; overflowing
  conversions are rejected.
- `Populate` may be retried after a failed attempt, but a successfully
  populated config struct remains one-shot.
- Example programs and their tests are available with the `example` build tag.
