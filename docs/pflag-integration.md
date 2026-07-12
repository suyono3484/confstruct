# Working draft: `spf13/pflag` backend

## Status

Exploration only. This document proposes an integration shape; it does not
commit the public API or add `pflag` as a dependency.

## Problem

Applications using `confstruct` often already define command-line flags with
[`spf13/pflag`](https://pkg.go.dev/github.com/spf13/pflag), directly or through
Cobra. They need a conventional final configuration layer where an explicitly
provided CLI value overrides defaults, files, and environment variables, while
application code continues to read only from the populated config struct.

Viper's related API is `BindPFlag` / `BindPFlags`: it binds pflag names to its
string-keyed registry and considers a flag an override only after it has
changed. This project should adopt the useful *presence* behaviour, not the
registry model. In particular, it must not add `Get`, `Set`, or other
string-keyed configuration reads.

## Recommended direction

Add an optional static backend, tentatively named `PFlag`:

```go
func PFlag(flags *pflag.FlagSet) Backend
```

The backend owns no parsing and performs no writes. The application defines
and parses its flags, adds the resulting backend as its highest-precedence
layer, and then calls `Populate`.

```go
type Config struct {
    confstruct.Meta

    ListenAddr confstruct.StringEntry
    Database   struct {
        Host confstruct.StringEntry
        Port confstruct.IntEntry
    }
}

func main() {
    flags := pflag.NewFlagSet("myapp", pflag.ExitOnError)
    flags.String("listen-addr", "", "address to listen on")
    flags.String("db-host", "", "database host")
    flags.Int("db-port", 0, "database port")
    flags.Parse(os.Args[1:])

    var cfg Config
    cfg.AddLayer(confstruct.Map(defaultValues))
    cfg.AddLayer(fileBackend)
    cfg.AddLayer(envBackend)
    cfg.AddLayer(confstruct.PFlag(flags)) // explicitly supplied CLI flags win

    if err := confstruct.Populate(context.Background(), &cfg); err != nil {
        log.Fatal(err)
    }
}
```

`PFlag` must be registered after the non-CLI layers when command-line values
are intended to win. Like `Map`, it is static and may technically be used as
the first layer, but that will normally leave entries unset because an
unprovided flag is absent rather than a default.

## Semantics

### Presence and precedence

For a field whose matching pflag has `Changed == false`, `PFlag.Lookup` returns
`(nil, false, nil)`. The entry therefore resolves from the next lower layer.
The flag's declared default is *not* a configuration value.

For a field whose matching pflag has `Changed == true`, the backend returns
the flag's current `Value.String()` and `ok == true`. The normal entry coercion
then parses that string into the entry's type. An explicitly supplied zero
value, such as `--db-port=0` or `--verbose=false`, remains set and overrides a
lower value. This preserves confstruct's set-versus-zero distinction.

The backend is not watchable. CLI arguments are parsed once, before
`Populate`, and do not change during the process lifetime.

### Parsing lifecycle

The caller remains responsible for calling `flags.Parse` (or for letting Cobra
parse its command) before `Populate`. Constructing `PFlag` before parsing is
fine as long as `Populate` occurs afterwards, because lookup happens during
population. Calling `Populate` before parsing would correctly see every flag
as unchanged and defer to lower layers, but is almost certainly an application
bug; documentation and tests should make the required ordering prominent.

### Type support

The first implementation should support exactly the entry types the package
already exposes: strings, bools, signed and unsigned integers, and floats.
It can return a flag's string representation and reuse the existing coercion
path. Slice, duration, IP, map, custom `pflag.Value`, and other pflag-specific
types must not silently acquire an interpretation merely because pflag accepts
them. They are future entry-type work, not requirements for this backend.

Malformed or incompatible explicit values currently follow the package's
existing coercion behaviour: the value cannot populate the typed entry. Before
shipping, decide whether initial population should instead report a clear
error that identifies both the struct field and flag name; that would improve
the CLI experience but is a broader `Populate` error-handling decision.

## Mapping flag names to fields

Viper's `BindPFlags` uses the flag long name as a configuration key. That does
not translate directly: confstruct's canonical identity is the Go struct path
(`Database.Port`), while CLI names are normally kebab-case (`db-port`).

The recommended mapping is deliberately small and source-specific:

1. By default, derive the long flag name from the complete field path: split
   Go identifiers at word boundaries, lowercase the words, join words in a
   segment with `-`, then join segments with `-`.
2. Allow the entry field to override that derived name with
   `cs.pflag:"name"`.
3. An empty tag is invalid and should cause `Populate` to return an error;
   unlike an omitted tag, it is most likely a mistake.

Examples:

| Struct field path | Default flag name | Tagged flag name |
| --- | --- | --- |
| `Port` | `port` | `cs.pflag:"listen-port"` → `listen-port` |
| `ListenAddr` | `listen-addr` | `cs.pflag:"addr"` → `addr` |
| `Database.Host` | `database-host` | `cs.pflag:"db-host"` → `db-host` |
| `HTTP.ServerPort` | `http-server-port` | `cs.pflag:"http-port"` → `http-port` |

The tag belongs on the entry field, not the containing struct. That keeps the
binding local to the value that receives it and matches `cs.env`.

```go
type Config struct {
    confstruct.Meta

    ListenAddr confstruct.StringEntry `cs.pflag:"addr"`
    Database   struct {
        Host confstruct.StringEntry `cs.pflag:"db-host"`
        Port confstruct.IntEntry    `cs.pflag:"db-port"`
    }
}
```

The initial scope should not add a general name-mapper callback, arbitrary
path aliases, or an API equivalent to Viper's `BindPFlag(key, flag)`. Those
would make CLI setup another string-keyed configuration interface. A narrowly
defined convention plus one field tag makes the struct remain the authoritative
schema and accommodates the common short-name cases.

## Proposed implementation outline

`PFlag` can reuse the existing private `fieldAwareBackend` hook, as `Env` and
`File` do:

```go
type pflagBackend struct {
    flags *pflag.FlagSet
}

func (b *pflagBackend) Lookup(path string) (any, bool, error) {
    // Fallback for direct Backend use: derive a flag name from path.
}

func (b *pflagBackend) lookupField(
    path string,
    fields []reflect.StructField,
) (any, bool, error) {
    name, err := pflagName(path, fields)
    if err != nil {
        return nil, false, err
    }
    flag := b.flags.Lookup(name)
    if flag == nil || !flag.Changed {
        return nil, false, nil
    }
    return flag.Value.String(), true, nil
}
```

Suggested exported diagnostic identity:

```go
const PFlagBackendName = "pflag"
```

`Name()` would return that constant. `Describe()` should identify the flag set
without exposing argument values—`"command-line"` for `pflag.CommandLine`, or
the flag set name for a custom set, is enough.

### Dependency and compatibility

Import `github.com/spf13/pflag` in a new `pflag.go` production file and add it
as a direct module requirement. No Cobra dependency is needed: Cobra exposes
`*pflag.FlagSet`. Standard-library flags remain usable when the caller imports
them into a pflag set with `AddGoFlagSet`, which is pflag's existing bridge.

## Decisions

Every open question originally raised for this backend has a recorded
decision below, each expanded in its own subsection or linked document.
"Exploration only" in [Status](#status) still applies to the backend as a
whole — none of this is implemented yet — but the design questions
themselves are no longer open.

1. **Name conversion.** Decided: derive flag names with the word-boundary
   rules in [Identifier-to-flag-name
   conversion](#identifier-to-flag-name-conversion) (initialism-aware
   segmentation, digits stay attached to their word), with `cs.pflag` as the
   escape hatch for names the rules get wrong. Table-driven tests must cover
   the rule table before the convention is made public; a small internal
   helper is preferable to an undocumented dependency.
2. **Duplicate names.** Decided: `Populate` fails before injecting any value,
   naming every colliding field path, whenever two fields resolve to the
   same flag name *within one `Populate` call*. See [Duplicate flag name
   detection](#duplicate-flag-name-detection) for the exact scope — the same
   name across separate `Meta`-rooted structs (e.g. per-subcommand config,
   populated independently) is explicitly not a collision.
3. **Missing flags.** Decided: absence, not error. This supports one config
   struct — or per-subcommand structs, per decision 2 — with lower layers
   supplying values a given command's flag set doesn't define. A typo is
   caught by a test that passes the intended flag set and asserts `IsSet` or
   source precedence for the fields meant to be CLI-overridable, not by
   `Populate` itself.
4. **Invalid tag.** Decided: an invalid `cs.pflag` tag is an error. See
   [Pflag tag validation](#pflag-tag-validation) for the exact grammar and
   error behaviour.
5. **Error handling.** Decided: yes, `Populate` should be improved so a
   failed initial conversion returns an error, before this backend ships —
   see [`populate-error-handling.md`](populate-error-handling.md) for the
   design (scope, aggregation, and error format). This is a `Populate`-level
   change, not `pflag`-specific, so it lands as a prerequisite rather than
   part of this backend's own diff.

### Identifier-to-flag-name conversion

The automatic convention is part of the public contract, even though its
implementation is small. Once an application uses an untagged field such as
`HTTPServerPort`, renaming the derived flag from `http-server-port` to
`h-t-t-p-server-port` would break command-line compatibility. The conversion
therefore needs an explicit specification and table-driven tests, rather than
being an incidental sequence of regular-expression replacements.

The ambiguity comes from Go's identifier style. A capital letter can mean the
start of a normal word (`ListenAddr`), be part of an initialism (`HTTPServer`),
or precede a digit (`HTTP2Server`). The spelling alone does not identify the
author's intended CLI spelling in every case.

Recommended rules for the initial implementation:

1. Treat an identifier as a sequence of ASCII letters and digits. The result
   contains lowercase ASCII letters, digits, and `-` only.
2. Start a new word at a lower-to-upper transition: `ListenAddr` becomes
   `listen-addr`.
3. Treat a run of capitals as one initialism when it is followed by a normal
   word. In `HTTPServer`, the `S` starts `Server` because it is followed by a
   lowercase letter; the preceding `HTTP` remains one word. Thus it becomes
   `http-server`, not `h-t-t-p-server`.
4. Keep digits attached to their adjacent word. A digit-to-capital transition
   begins a new word, but a letter-to-digit transition does not. Thus
   `HTTP2Server` becomes `http2-server` and `Server2Port` becomes
   `server2-port`.
5. Join the words within one Go field name with `-`, and join every nested
   field segment with `-` as well. `Database.HTTP2ServerPort` becomes
   `database-http2-server-port`.
6. A name containing non-ASCII letters, punctuation, or an otherwise undesired
   spelling must use `cs.pflag`. The automatic conversion should not silently
   invent a lossy transliteration.

With those rules, the following cases define the expected behavior:

| Go field path | Derived flag | Why |
| --- | --- | --- |
| `Port` | `port` | A single word. |
| `ListenAddr` | `listen-addr` | Lowercase `n` followed by uppercase `A`. |
| `TLS` | `tls` | An all-capital initialism is one word. |
| `TLSConfig` | `tls-config` | The `C` begins a normal word because `o` follows it. |
| `HTTPServerPort` | `http-server-port` | `HTTP` remains an initialism. |
| `HTTP2Server` | `http2-server` | The version digit stays attached to `HTTP`. |
| `Server2Port` | `server2-port` | The version digit stays attached to `Server`. |
| `IPv6Address` | `i-pv6-address` | This is mechanically consistent but probably not the desired CLI spelling; tag it as `ipv6-address`. |
| `Database.HTTP2ServerPort` | `database-http2-server-port` | Each nested field contributes a hyphen-separated segment. |

`IPv6Address` illustrates why a tag escape hatch is necessary. No general
algorithm can reliably know whether `IPv6`, `OAuth`, `iOS`, a product name, or
an organisation's initialism should be treated as one word. Attempting a
large built-in dictionary of special cases would make the convention harder to
learn and would still fail for application-specific names. The documented tag
is the explicit, local way to state intent:

```go
IPv6Address confstruct.StringEntry `cs.pflag:"ipv6-address"`
```

The implementation should scan bytes once and emit word boundaries; it does
not need a dependency. Tests should cover the table above, empty paths, and
tag precedence. The package need not expose the helper: only the resulting
convention is API surface.

### Duplicate flag name detection

Two distinct entry fields can resolve to the same flag long name, whether by
coincidence of the derivation rules or because both carry the same explicit
`cs.pflag` tag. Silently letting that stand would mean a single CLI value
configures two unrelated struct fields, which is exactly the kind of
accidental cross-wiring `confstruct`'s struct-first model exists to prevent.
`Populate` must therefore reject it — but the check has to be scoped
correctly, or it produces false positives for a legitimate pattern.

#### Scope: per `Populate` call, not per application

The check only considers entry fields reachable from the struct passed
*directly* to `Populate` — the one embedding `Meta` — including entries in
plain nested structs within that tree. It does not look at any other
`Meta`-rooted struct in the application, even one of the same Go type,
because a separate struct populated by a separate `Populate` call never
shares a backend lookup with this one.

This scoping is deliberate, not incidental. A common and legitimate shape is
multiple CLI subcommands (for example, via `spf13/cobra`) that each expose a
flag with the same long name — `--with-key` — bound to a different part of
the application's configuration depending on which subcommand runs:

```go
type SvcAConfig struct {
    confstruct.Meta
    WithKey confstruct.StringEntry `cs.pflag:"with-key"`
}

type SvcBConfig struct {
    confstruct.Meta
    WithKey confstruct.StringEntry `cs.pflag:"with-key"`
}
```

Because `SvcAConfig` and `SvcBConfig` each own their `Meta` and are populated
independently — `Populate(ctx, &cfg.SvcA)` inside `hit-svc-a`'s `RunE`, using
that subcommand's own `*pflag.FlagSet`, and likewise for `SvcB` — the two
`with-key` bindings are never evaluated in the same call and never collide.
Ownership of which flag feeds which field is expressed by ordinary Go field
paths and by where in the command tree `Populate` is called, not by a name
comparison at runtime. Only when two fields *within the same `Populate`
call* resolve to the same name is there a genuine ambiguity to reject.

#### Why `Meta` lives on the sub-struct, not the container

`Populate` looks for a `Meta` field on the exact struct pointer it is given;
it does not search up through parent structs. A struct that does not embed
`Meta` directly is not a valid `Populate` target and fails with `"struct
must embed confstruct.Meta"`. Consequently, a container struct that groups
several independently-populated units — `App` below — does not need `Meta`
at all: it is never passed to `Populate` itself, only its fields are. Each
field that *is* independently populated, with its own backend set and its
own call site, needs its own `Meta`. That is precisely what makes the
per-subcommand pattern work: `SvcAConfig` and `SvcBConfig` are separate
`Populate` targets because they are separate `Meta` owners, not because of
anything special about their field names.

A single, shared top-level `Meta` cannot express this. One `Meta` means one
backend list and one `Populate` call covering every field beneath it, so
`SvcA.WithKey` and `SvcB.WithKey` would necessarily be resolved together —
reintroducing the exact collision this section exists to reject.

#### Worked example: per-subcommand `Meta` plus a shared `GlobalConfig`

Fields that are genuinely common to every subcommand — log level, a config
file path — don't fit either extreme: they shouldn't be duplicated per
subcommand, and they shouldn't be forced into the same `Meta` as
subcommand-specific fields. Give them their own small struct and `Meta`,
populated once at the root via `PersistentPreRunE`, alongside the
per-subcommand structs populated in each leaf's `RunE`:

```go
type App struct {
    Global GlobalConfig // populated once, regardless of which subcommand runs
    SvcA   SvcAConfig   // populated only when hit-svc-a runs
    SvcB   SvcBConfig   // populated only when hit-svc-b runs
}

type GlobalConfig struct {
    confstruct.Meta
    LogLevel confstruct.StringEntry `cs.pflag:"log-level"`
}

type SvcAConfig struct {
    confstruct.Meta
    WithKey confstruct.StringEntry `cs.pflag:"with-key"`
}

type SvcBConfig struct {
    confstruct.Meta
    WithKey confstruct.StringEntry `cs.pflag:"with-key"`
}

func main() {
    var app App
    rootCmd := &cobra.Command{
        Use: "myapp",
        PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
            app.Global.AddLayer(confstruct.PFlag(cmd.Flags()))
            return confstruct.Populate(context.Background(), &app.Global)
        },
    }
    rootCmd.PersistentFlags().String("log-level", "info", "log verbosity")

    svcACmd := &cobra.Command{
        Use: "hit-svc-a",
        RunE: func(cmd *cobra.Command, args []string) error {
            app.SvcA.AddLayer(confstruct.PFlag(cmd.Flags()))
            if err := confstruct.Populate(context.Background(), &app.SvcA); err != nil {
                return err
            }
            return hitSvcA(app.Global, app.SvcA)
        },
    }
    svcACmd.Flags().String("with-key", "", "key for svc-a")

    svcBCmd := &cobra.Command{
        Use: "hit-svc-b",
        RunE: func(cmd *cobra.Command, args []string) error {
            app.SvcB.AddLayer(confstruct.PFlag(cmd.Flags()))
            if err := confstruct.Populate(context.Background(), &app.SvcB); err != nil {
                return err
            }
            return hitSvcB(app.Global, app.SvcB)
        },
    }
    svcBCmd.Flags().String("with-key", "", "key for svc-b")

    rootCmd.AddCommand(svcACmd, svcBCmd)
    log.Fatal(rootCmd.Execute())
}
```

`cmd.Flags()` inside `RunE` (and inside the root's `PersistentPreRunE`,
which cobra runs with the invoked leaf command) already merges that
command's local flags with every inherited persistent flag, so
`app.Global`, `app.SvcA`, and `app.SvcB` each see exactly the flags relevant
to them without any extra wiring. `App` itself is never passed to
`Populate` — it is a plain grouping struct, not a `confstruct` target — and
`hit-svc-a`'s `--with-key` and `hit-svc-b`'s `--with-key` never appear in
the same `Populate` call, so the duplicate-name check never sees them as a
pair.

##### Alternative: an inline anonymous struct instead of `GlobalConfig`

`Populate` and `AddLayer` reach `Meta` and the entries around it by
reflection and method promotion; neither cares whether the enclosing struct
type has a name. A one-off unit like `Global`, never referenced outside
`main`, can skip the named type and embed `Meta` directly in the field:

```go
type App struct {
    Global struct {
        confstruct.Meta
        LogLevel confstruct.StringEntry `cs.pflag:"log-level"`
    }
    SvcA SvcAConfig
    SvcB SvcBConfig
}
```

`app.Global.AddLayer(...)` and `confstruct.Populate(ctx, &app.Global)` work
exactly as they do with a named `GlobalConfig`. This trims a type
declaration for a shape used in exactly one place, at the cost of losing a
name to reuse if that shape is ever needed elsewhere — for example, as a
parameter type. That tradeoff is why `SvcAConfig` and `SvcBConfig` stay
named above: `hitSvcA` and `hitSvcB` take them as parameters, so the named
type is pulling its weight. Prefer the named form once a unit's shape is
referenced anywhere beyond the single field declaration and its `Populate`
call.

#### Rejected alternative: a discriminator tag

A tag such as `cs.command:"hit-svc-a"`, letting two same-named fields coexist
in one shared struct and disambiguating them by the active CLI command, was
considered and rejected. It would require `Populate` to accept a runtime
"which command is active" input that does not otherwise exist in this
package's model, and it reintroduces string-keyed dispatch resembling
`viper.BindPFlag(key, flag)` — the registry-style API this integration
explicitly declines to add (see [Explicit non-goals](#explicit-non-goals)).
The struct-per-unit pattern above already expresses the same intent through
types and call sites the compiler checks, so the tag would add a second,
weaker way to say the same thing.

#### Rules

1. **What counts as the same name.** Compare each field's fully resolved
   name — the trimmed, validated `cs.pflag` tag if present, otherwise the
   derived name — using an exact, case-sensitive match. The grammar in
   [Pflag tag validation](#pflag-tag-validation) already restricts every
   resolved name to lowercase kebab-case, so no further normalization is
   needed before comparing.
2. **When the check runs.** It is structural: derived purely from field
   paths and tags, before any `flags.Lookup` call. It fails whether or not
   the `*pflag.FlagSet` passed to `PFlag` actually defines a flag by that
   name, and regardless of `Changed`. The goal is to prevent an ambiguous
   binding from existing at all, not to catch it only when a user happens to
   trigger it.
3. **Failure mode.** `Populate` returns an error and injects no values into
   any field for that call — a duplicate anywhere in the struct invalidates
   the whole population, not just the colliding fields.
4. **Error content.** Name the colliding flag and every field path that
   resolved to it, for example:

   ```text
   confstruct: backend "pflag": duplicate flag name "with-key": fields
   "SvcA.WithKey" and "SvcA.AltKey" both resolve to it
   ```

   If a struct has more than one colliding group, report all of them in one
   error rather than stopping at the first, so a fix does not require
   re-running `Populate` once per collision.

#### Examples

| Field A | Field B | Same `Populate` call? | Result |
| --- | --- | --- | --- |
| `Database.Port` (derived `database-port`) | `Cache.Port` (derived `cache-port`) | yes | Valid — different resolved names. |
| `Database.Port` (no tag) | `Cache.Port` `cs.pflag:"database-port"` | yes | Error — both resolve to `database-port`. |
| `SvcA.WithKey` `cs.pflag:"with-key"` | `SvcB.WithKey` `cs.pflag:"with-key"`, in a separate `SvcBConfig` `Meta` | no — separate `Populate` calls | Valid — never compared. |
| `SvcA.WithKey` `cs.pflag:"with-key"` | `SvcA.AltKey` `cs.pflag:"with-key"`, same `SvcAConfig` | yes | Error — both resolve to `with-key` in the same call. |

### Pflag tag validation

`cs.pflag` is an explicit public command-line name, not a hint. A malformed
tag must make `Populate` fail rather than quietly falling back to the derived
name or treating the field as absent. Silent fallback is particularly harmful
here: an application can advertise `--db-port` in its help output while the
configuration field accidentally looks for some other name.

The following states are distinct:

| Field annotation | Meaning | Result |
| --- | --- | --- |
| no `cs.pflag` tag | Use the derived flag name. | Valid. |
| `cs.pflag:"db-port"` | Use `db-port`. | Valid. |
| `cs.pflag:"  db-port  "` | Outer whitespace is insignificant. | Valid; resolves as `db-port`. |
| `cs.pflag:""` or `cs.pflag:"   "` | The author explicitly supplied no usable name. | Error. |
| `cs.pflag:"--db-port"` | The tag contains CLI syntax, rather than the long name. | Error. |
| `cs.pflag:"db host"` | Whitespace appears inside the name. | Error. |
| `cs.pflag:"db_host"`, `cs.pflag:"DBPort"` | The spelling depends on a normalizer or is not canonical. | Error. |
| `cs.pflag:"db--port"`, `cs.pflag:"-db-port"`, or `cs.pflag:"db-port-"` | It contains an empty hyphen-separated word. | Error. |

A valid tag, after trimming outer whitespace, must match this ASCII grammar
(shown in Go regular-expression form):

```text
^[a-z][a-z0-9]*(-[a-z0-9]+)*$
```

In other words: it starts with a lowercase letter; subsequent words contain
lowercase letters or digits; words are separated by one hyphen; and it has no
leading/trailing hyphen, underscore, uppercase letter, space, or non-ASCII
character. This is intentionally narrower than the set of names a particular
`pflag.FlagSet` may accept. `pflag` permits custom normalization, so accepting
its broader input would make tag behavior depend on an application-owned
normalization function. The narrow grammar gives confstruct a stable,
documentable mapping and matches every automatically derived name.

Applications that already expose a non-canonical flag name must handle that
compatibility in their CLI layer—for example, by migrating the declaration or
mapping a deprecated flag to the canonical one before `Populate`. The first
integration should not let one field silently match several long names.

The error should be returned from `Populate`, using its existing backend and
field-path context. For example:

```text
confstruct: backend "pflag" lookup "Database.Port": invalid cs.pflag tag "--db-port": expected a lowercase kebab-case long flag name
```

This makes a mistake actionable without reporting a flag value, which may be
sensitive. Validation must occur before lookup and before the backend returns
a value. Tests should cover every invalid form in the table, acceptance after
outer-whitespace trimming, and the fact that an omitted tag still uses the
derived name.

## Test matrix for an implementation

- A changed scalar flag overrides Map, File, and Env layers.
- An unchanged flag falls through, even if its pflag default differs from the
  lower-layer value.
- `--flag=false`, `--flag=0`, and `--flag=""` are present and win.
- Derived top-level and nested names resolve correctly; `cs.pflag` overrides
  them.
- A missing flag falls through without error.
- Duplicate tag-derived names within one `Populate` call fail with a useful
  error naming all colliding field paths.
- The same tag-derived name in two separate `Meta`-rooted structs, populated
  by separate `Populate` calls, is not an error.
- Values are parsed into each currently supported entry type, including
  boundaries and incompatible strings.
- `SourceName()` and `OnResolve` report `PFlagBackendName` for a winning flag.
- The backend is accepted as the lowest layer (static) but rejected nowhere as
  watchable; an example documents why it is normally last.

## Explicit non-goals

- **A global config object or a Viper-style key registry.** `PFlag` is
  consulted only through `Populate`/`Meta`, exactly like every other
  backend. Introducing a shared object other code could query independently
  would recreate the `Get`-from-anywhere pattern this library exists to move
  away from (see [Prior art context](../AGENTS.md#prior-art-context)) — it
  would give the application two ways to read the same value instead of one.

- **Config reads by flag name or struct-path string.** There is no
  `PFlag(flags).Get("db-port")` or equivalent. The only sanctioned read path
  is the populated struct field (`cfg.Database.Port.Value()`). This is the
  same principle behind rejecting the `cs.command` discriminator-tag idea in
  [Duplicate flag name detection](#duplicate-flag-name-detection): a
  string-keyed lookup, even a narrow one, is a second interface to the same
  data and a second place for it to drift from the struct.

- **Mutating flags from configuration values, which would make precedence
  and ownership unclear.** This backend is read-only in the other
  direction too: nothing in `confstruct` ever writes a resolved value back
  into the `pflag.FlagSet` (e.g. to make a lower-layer default show up as
  the flag's current value). Writing back would blur who owns a flag's
  value — `pflag`'s own parse state, or `confstruct`'s resolved entry — and
  risks update loops or a value that silently disagrees with its own
  source. Runtime writes have exactly one sanctioned path in this library,
  `Override.Set`/`Unset`, and it is a deliberate exception scoped to a
  single backend built for that purpose — see the carve-out note in
  [AGENTS.md](../AGENTS.md). `PFlag` has no comparable write-side use case,
  so it doesn't get one.

- **Automatic flag definition from a config struct.** This is the inverse
  of what `PFlag` does: it consumes flags the application already declared
  and parsed, it does not generate `flags.String(...)`-style calls from a
  struct's shape. A flag declaration carries help text, a shorthand letter,
  which command it belongs to, and its own validation — none of which has a
  natural home as a struct tag, and guessing at them would mean `confstruct`
  quietly taking over a responsibility that belongs to the application or
  to Cobra's own command definitions.

- **Live flag updates and a `WatchableBackend` implementation.** CLI
  arguments are fixed for the life of a process once `flags.Parse` returns
  — there is no later event for a `WatchableBackend` to watch for. Adding
  the interface anyway would be machinery built for something that
  structurally cannot happen, and doing so via some other mechanism (a
  push driven by, say, a subcommand's `PreRun`) is the "watchable `PFlag`"
  idea already walked through and rejected while designing [duplicate flag
  name detection](#duplicate-flag-name-detection) — the answer there was
  ordinary control flow (call `Populate` at the right point), not a new
  backend capability.

- **Supporting every pflag value type before confstruct has matching typed
  entry types.** `pflag` has value types — slices, durations, IPs, custom
  `pflag.Value` implementations — that `confstruct` has no `Entry` type for
  yet (see [Type support](#type-support): only strings, bools, and the
  numeric families are in scope for the first implementation). Special-casing
  those inside `PFlag` would let this one backend's surface area outrun the
  rest of the library's type system, and would mean inventing new entry
  types under the pressure of "make this backend work" rather than as a
  deliberate addition considered on its own.

## Reference behaviour

- [Viper's flag documentation](https://pkg.go.dev/github.com/spf13/viper#hdr-Working_with_Flags)
  describes `BindPFlag` / `BindPFlags` and shows that binding is lazy.
- [Viper's `flags.go`](https://github.com/spf13/viper/blob/master/flags.go)
  adapts `pflag.Flag.Changed` into its `HasChanged` abstraction; Viper's lookup
  uses that signal before treating a pflag as an override.
- [pflag documentation](https://pkg.go.dev/github.com/spf13/pflag) describes
  flag parsing and the `AddGoFlagSet` bridge for standard-library flags.
