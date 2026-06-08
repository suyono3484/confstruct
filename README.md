# confstruct

A Go configuration library designed around struct-first, consistency-first principles.

## Motivation

After years of using [`spf13/viper`](https://github.com/spf13/viper) and [`knadh/koanf`](https://github.com/knadh/koanf), a few recurring pain points drove the creation of this library:

### Flexibility is a double-edged sword

Both viper and koanf are intentionally unopinionated and highly flexible. That flexibility makes it easy for config access patterns to drift across a codebase — some code reads keys directly, some unmarshals into structs, some uses watchers, and so on. Over time, consistency erodes and config handling becomes hard to reason about.

### Struct binding is an afterthought

Both libraries treat the struct as a deserialization target: config lives in a loosely typed internal map, and the struct is just one way to read it out at the end. The struct is not the source of truth — the map is. Sub-configs cannot be bound to their own structs in a composable way, and type safety is opt-in rather than enforced.

### No way to distinguish unset from zero

When you unmarshal into a plain struct, a field that was never present in any config source is indistinguishable from one that was explicitly set to its zero value (`0`, `""`, `false`, etc.). There is no first-class signal for "this field was not configured" without resorting to pointer fields or sentinel values — workarounds that pollute the struct definition and leak the library's limitations into application code.

## Design

`confstruct` is built on the opposite premise: **the struct is the config**. Each field in the struct is a typed **entry** — a live, thread-safe value that is resolved from one or more backends at startup and kept up to date by remote backends as they push changes.

The lifecycle has three steps:

1. **Register** — call `AddLayer` on the struct to attach backends, in ascending precedence order. The first backend added is the lowest-priority layer and must be a local (non-watchable) backend, serving as the stable default.
2. **Wire** — call `Populate(ctx, &cfg)`. It walks every entry field, pulls the initial value from each backend, caches the resolved winner, and registers a watcher with any remote backends for future updates.
3. **Query** — call `Value()` and `IsSet()` on entry fields. Each call is a single read under a read-lock — no backend interaction, no reflection, no allocation.

**Values are resolved eagerly.** When `Populate` runs, or when a remote backend pushes an update, the winning value across all layers is computed immediately and cached in the entry. `Value()` reads that cache — it does not re-consult backends on every call.

**The entry is thread-safe.** Each entry holds a `sync.RWMutex`. Remote backends can push updates from their own goroutines at any time without racing against reads.

## Usage

```go
type Config struct {
    confstruct.Meta

    ListenAddr confstruct.StringEntry
    Database   struct {
        User confstruct.StringEntry
        Host confstruct.StringEntry
    }
}

var cfg Config

// Layer 1 — hard-coded defaults (lowest priority, must be a static backend).
cfg.AddLayer(confstruct.Primitive(map[string]any{
    "ListenAddr":    "localhost:8080",
    "Database.User": "app",
    "Database.Host": "localhost",
}))

// Layer 2 — config file overrides defaults.
fileBackend, err := confstruct.File("config.yaml")
if err != nil {
    log.Fatal(err)
}
cfg.AddLayer(fileBackend)

// Layer 3 — environment variables override the file.
envBackend, err := confstruct.Env(confstruct.WithPrefix("APP"))
if err != nil {
    log.Fatal(err)
}
cfg.AddLayer(envBackend)

// Layer 4 — remote backend wins over all; receives live updates.
cfg.AddLayer(consul.Backend("myapp/"))

if err := confstruct.Populate(ctx, &cfg); err != nil {
    log.Fatal(err)
}

// Read config through the struct — the only interface the caller ever uses.
fmt.Println(cfg.ListenAddr.Value())
if cfg.Database.Host.IsSet() {
    fmt.Println(cfg.Database.Host.Value())
}
```

`Populate` is called once at startup and may not be called again on the same struct. After that, all entry access goes through the struct fields — there is no other API.

## Entry types

Entry types are the building blocks of a config struct. Each one wraps a Go primitive and carries the per-layer slot storage needed to resolve its value.

| Entry type | Go type |
|---|---|
| `StringEntry` | `string` |
| `BoolEntry` | `bool` |
| `IntEntry`, `Int8Entry`, `Int16Entry`, `Int32Entry`, `Int64Entry` | `int`, `int8`, `int16`, `int32`, `int64` |
| `UintEntry`, `Uint8Entry`, `Uint16Entry`, `Uint32Entry`, `Uint64Entry` | `uint`, `uint8`, `uint16`, `uint32`, `uint64` |
| `Float32Entry`, `Float64Entry` | `float32`, `float64` |

Each entry type exposes two methods:

- `Value() T` — returns the pre-resolved value from the highest-precedence backend that has one, or the zero value if none do.
- `IsSet() bool` — reports whether any backend currently has a value for this field.

## Layering and precedence

There are no built-in or assumed layers. The caller registers each backend explicitly, in order. Each call to `AddLayer` appends a backend at a higher precedence than all previous ones.

```go
cfg.AddLayer(defaults)   // lowest precedence — must be a local backend
cfg.AddLayer(fileConfig) //    ↑
cfg.AddLayer(envVars)    //    ↑
cfg.AddLayer(cliFlags)   // highest precedence
```

**The lowest-priority backend must not be a `WatchableBackend`.** It serves as the stable default that is always present from the moment `Populate` returns. This guarantees every field has a valid baseline value even before any remote source responds.

**confstruct is explicit by design.** It is the caller's responsibility to cover every entry in the config struct with a value in the lowest layer. If a field has no value in any layer, `Value()` silently returns the Go zero value and `IsSet()` returns `false` — there is no error. This is intentional: the library does not guess at defaults.

**Recommended practice:** write a unit test that calls `Populate` with only the lowest layer registered and asserts that `IsSet()` is `true` for every entry field. This catches omissions before they reach production.

```go
func TestDefaultsAreComplete(t *testing.T) {
    var cfg AppConfig
    cfg.AddLayer(confstruct.Primitive(defaultValues))
    if err := confstruct.Populate(context.Background(), &cfg); err != nil {
        t.Fatal(err)
    }
    if !cfg.ListenAddr.IsSet() {
        t.Error("ListenAddr has no default")
    }
    if !cfg.Database.Host.IsSet() {
        t.Error("Database.Host has no default")
    }
    // ... one assertion per entry field
}
```

When a remote backend pushes an update for a field, the entry re-resolves across all layers immediately, under its write-lock. If the remote backend later signals that a key was removed, the next-highest layer that has a value becomes the winner — falling back to the local default if no other layer covers it.

Each backend is independent. `confstruct` does not know or care how a backend retrieves or formats its values. Backends are fully swappable and independently testable.

## Backend interfaces

### Static backends

```go
type Backend interface {
    Lookup(path string) (any, bool, error)
}
```

`Lookup` is called once per field during `Populate` and returns the value, a presence flag, and an error. The path is built from the chain of Go struct field names leading to the entry:

```
"ListenAddr"        // top-level field
"Database.User"     // nested struct field
"Database.Pool.Max" // doubly nested struct field
```

The `bool` return value distinguishes "this backend has a value of zero" from "this backend has no value at all", preserving the set/unset distinction at the source level.

### Watchable backends

```go
type WatchableBackend interface {
    Backend
    Watch(ctx context.Context, path string, hook func(v any, ok bool))
}
```

`WatchableBackend` extends `Backend` for remote sources that push updates. `Watch` is called once per field path during `Populate`. The backend must call `hook` whenever the value at `path` changes:

- `hook(v, true)` — the value changed to `v`.
- `hook(nil, false)` — the key was removed; the entry falls back to a lower-priority layer.

The context passed to `Populate` governs the lifetime of all watchers. When it is cancelled, backends should stop calling hooks and clean up their connections. **The goroutine that drives the watch loop lives inside the backend implementation** — confstruct does not manage it.

Backends have no knowledge of the target struct type. The entry type handles coercion of the returned `any` value into the correct Go type, allowing numeric conversions (e.g., `int64` from a backend filling an `Int32Entry`). Coercion failures on hook calls are silently ignored — the entry retains its previous value.

**Backends are never exposed to the caller.** Once registered via `AddLayer`, a backend is an internal detail. The caller only ever interacts with the struct.

## Built-in backends

### Primitive

`Primitive` is a map-backed backend for literal Go values. It is useful as a defaults layer (registered first) or a forced-override layer (registered last). It is a static backend and satisfies the lowest-priority constraint.

```go
cfg.AddLayer(confstruct.Primitive(map[string]any{
    "ListenAddr":    "localhost:8080",
    "Database.Host": "localhost",
    "Database.Port": 5432,
}))
```

Keys are dot-separated field paths matching the struct layout. Values must be of a type compatible with the target entry — exact match, or any numeric type (conversions are handled automatically).

### Env

`Env` reads from OS environment variables and an optional `.env` file. Field paths are mapped to env var names by uppercasing and replacing dots with underscores. An optional prefix is prepended.

```go
// Database.Host → APP_DATABASE_HOST
// ListenAddr    → APP_LISTEN_ADDR
envBackend, err := confstruct.Env(
    confstruct.WithPrefix("APP"),
    confstruct.WithDotEnv(".env"), // silently skipped if the file does not exist
)
```

OS environment variables take precedence over `.env` file values. All values are returned as strings; confstruct parses them into the target field type.

### File

`File` reads from a YAML, JSON, or TOML file. The format is inferred from the file extension; `WithFormat` overrides it for non-standard extensions.

```go
backend, err := confstruct.File("config.yaml")
backend, err := confstruct.File("config.json")
backend, err := confstruct.File("config.toml")
backend, err := confstruct.File("config.cfg", confstruct.WithFormat("toml"))
```

Nested struct paths map to nested file keys via case-insensitive matching at each level. `Lookup("Database.Host")` matches any of `database.host`, `DATABASE.HOST`, or `Database.Host` in the file.

```yaml
# config.yaml
database:
  host: localhost
  port: 5432
```

```json
// config.json
{"database": {"host": "localhost", "port": 5432}}
```

```toml
# config.toml
[database]
host = "localhost"
port = 5432
```

The file is read once at construction time. `File` is a static backend.

**Type notes:** JSON numbers unmarshal to `float64`; TOML integers unmarshal to `int64`; YAML integers unmarshal to `int`. All are handled by confstruct's numeric coercion — a `float64` from JSON fills an `Int32Entry` correctly, and so on. For very large integers (> 2^53), prefer YAML or TOML over JSON to avoid float64 precision loss.

## Other backend shapes

The table below shows the shapes of backends you might implement or source from third-party packages. confstruct does not provide these.

| Backend | Kind | Example source |
|---|---|---|
| Command-line flags | Static | `--port 8080` |
| Consul | Watchable | Live key-value updates |
| Vault | Watchable | Secret leases with renewal |
| AWS Parameter Store | Watchable | SSM parameter change events |
