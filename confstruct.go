// Copyright 2026 Suyono
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package confstruct is a struct-first Go configuration library.
//
// The central rule: the struct is the config. Each leaf field is a typed entry —
// a live, thread-safe value populated from one or more [Backend] implementations
// and resolved to the highest-precedence winner. There is no global key-value
// store and no unmarshaling step; the struct is the only interface callers use.
//
// # Quickstart
//
//	type Config struct {
//	    confstruct.Meta             // embeds AddLayer and Populate machinery
//	    Host confstruct.StringEntry
//	    Port confstruct.IntEntry
//	    DB   struct {
//	        Name confstruct.StringEntry
//	    }
//	}
//
//	var cfg Config
//
//	// Lowest-priority layer: hard-coded defaults. Must be a static backend.
//	cfg.AddLayer(confstruct.Map(map[string]any{
//	    "Host":    "localhost",
//	    "Port":    8080,
//	    "DB.Name": "myapp",
//	}))
//
//	// Higher-priority layer: env vars (APP_HOST, APP_PORT, APP_DB_NAME, …).
//	env, _ := confstruct.Env(confstruct.WithPrefix("APP"))
//	cfg.AddLayer(env)
//
//	// Populate reads all backends once and registers watchers for live updates.
//	if err := confstruct.Populate(ctx, &cfg); err != nil {
//	    log.Fatal(err)
//	}
//
//	fmt.Println(cfg.Host.Value())  // "localhost" (or whatever a higher layer set)
//	fmt.Println(cfg.Port.IsSet()) // true
//
// # Field paths
//
// Backends receive field paths as dot-separated chains of Go struct field names:
// "Host", "DB.Name", "DB.Pool.Max". The path is derived from the Go field names
// in the struct definition, not from any struct tag.
//
// # Layering
//
// Layers are registered in ascending precedence order via [Meta.AddLayer].
// The first layer added is the lowest-priority source and must not be a
// [WatchableBackend]. When a [WatchableBackend] pushes an update, the entry
// re-resolves across all layers immediately; removing a key causes the entry to
// fall back to the next-lower layer automatically.
//
// # Thread safety
//
// Each entry is guarded by its own [sync.RWMutex]. Remote backends may push
// updates from any goroutine without racing against concurrent reads.
package confstruct

import (
	"context"
	"fmt"
	"reflect"
	"strconv"
	"sync"
	"sync/atomic"
)

// Backend is the extension point for configuration sources. Lookup is called
// with the field's dot-separated struct path and returns the raw value for that
// field, if the backend has one. Name returns the backend type identifier and
// Describe returns instance-specific detail; both are for debug and diagnostic use.
type Backend interface {
	Lookup(path string) (any, bool, error)
	Name() string
	Describe() string
}

// WatchableBackend extends Backend for remote sources that push updates.
// Watch is called once per field path during Populate; the backend must invoke
// hook whenever the value at path changes, passing ok=false if the key is removed.
type WatchableBackend interface {
	Backend
	Watch(ctx context.Context, path string, hook func(v any, ok bool))
}

// ResolveHook is called whenever a key's resolved value is determined or
// updated. key is the dot-separated struct field path, value is the resolved
// value, backendName and backendDesc identify the winning backend.
type ResolveHook func(key string, value any, backendName, backendDesc string)

// layerManager is satisfied by all entry types and lets Populate initialize
// and write into per-layer slot storage without knowing the concrete type T.
type layerManager interface {
	initSlots(n int)
	initSlotMeta(index int, name, desc string)
	setSlot(index int, v any, ok bool)
	resolvedState() (value any, backendName, backendDesc string, isSet bool)
}

var (
	layerManagerType = reflect.TypeFor[layerManager]()
	metaType         = reflect.TypeFor[Meta]()
)

// Meta holds globally registered backends. Embedding it in a config struct
// promotes AddLayer and OnResolve onto the struct directly.
type Meta struct {
	backends     []Backend
	resolveHooks []ResolveHook
	populated    atomic.Bool
}

// AddLayer registers a backend as a configuration layer. Layers added later
// have higher precedence. The lowest-priority backend (first added) must not
// implement WatchableBackend.
func (m *Meta) AddLayer(b Backend) {
	m.backends = append(m.backends, b)
}

// OnResolve registers a hook that is called once after Populate sets the
// initial resolved value for each key, and again whenever a watchable backend
// pushes an update that changes the winner. The hook is not called when no
// backend has a value for the key.
func (m *Meta) OnResolve(h ResolveHook) {
	m.resolveHooks = append(m.resolveHooks, h)
}

type layerSlot[T any] struct {
	value       T
	ok          bool
	backendName string
	backendDesc string
}

type entry[T any] struct {
	mu           sync.RWMutex
	slots        []layerSlot[T]
	resolved     T
	isSet        bool
	resolvedName string
	resolvedDesc string
}

func (e *entry[T]) initSlots(n int) {
	e.slots = make([]layerSlot[T], n)
}

func (e *entry[T]) initSlotMeta(index int, name, desc string) {
	e.slots[index].backendName = name
	e.slots[index].backendDesc = desc
}

func (e *entry[T]) setSlot(index int, v any, ok bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if ok {
		coerced, err := coerce[T](v)
		if err != nil {
			return
		}
		e.slots[index].value = coerced
		e.slots[index].ok = true
	} else {
		e.slots[index].value = *new(T)
		e.slots[index].ok = false
	}
	e.resolveUnderLock()
}

func (e *entry[T]) resolveUnderLock() {
	for i := len(e.slots) - 1; i >= 0; i-- {
		if e.slots[i].ok {
			e.resolved = e.slots[i].value
			e.isSet = true
			e.resolvedName = e.slots[i].backendName
			e.resolvedDesc = e.slots[i].backendDesc
			return
		}
	}
	var zero T
	e.resolved = zero
	e.isSet = false
	e.resolvedName = ""
	e.resolvedDesc = ""
}

// Value returns the resolved value from the highest-precedence backend that
// has one. Returns the zero value if no backend has a value for this field.
func (e *entry[T]) Value() T {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.resolved
}

// IsSet reports whether any backend has a value for this field.
func (e *entry[T]) IsSet() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.isSet
}

func (e *entry[T]) resolvedState() (any, string, string, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.resolved, e.resolvedName, e.resolvedDesc, e.isSet
}

// SourceName returns the Name() of the backend that provided the resolved value.
// Returns an empty string if no backend has a value for this field.
func (e *entry[T]) SourceName() string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.resolvedName
}

// SourceDesc returns the Describe() of the backend that provided the resolved value.
// Returns an empty string if no backend has a value for this field.
func (e *entry[T]) SourceDesc() string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.resolvedDesc
}

// IntEntry is a configuration entry whose resolved value is an int.
type IntEntry struct{ entry[int] }

// Int8Entry is a configuration entry whose resolved value is an int8.
type Int8Entry struct{ entry[int8] }

// Int16Entry is a configuration entry whose resolved value is an int16.
type Int16Entry struct{ entry[int16] }

// Int32Entry is a configuration entry whose resolved value is an int32.
type Int32Entry struct{ entry[int32] }

// Int64Entry is a configuration entry whose resolved value is an int64.
type Int64Entry struct{ entry[int64] }

// UintEntry is a configuration entry whose resolved value is a uint.
type UintEntry struct{ entry[uint] }

// Uint8Entry is a configuration entry whose resolved value is a uint8.
type Uint8Entry struct{ entry[uint8] }

// Uint16Entry is a configuration entry whose resolved value is a uint16.
type Uint16Entry struct{ entry[uint16] }

// Uint32Entry is a configuration entry whose resolved value is a uint32.
type Uint32Entry struct{ entry[uint32] }

// Uint64Entry is a configuration entry whose resolved value is a uint64.
type Uint64Entry struct{ entry[uint64] }

// Float32Entry is a configuration entry whose resolved value is a float32.
type Float32Entry struct{ entry[float32] }

// Float64Entry is a configuration entry whose resolved value is a float64.
type Float64Entry struct{ entry[float64] }

// StringEntry is a configuration entry whose resolved value is a string.
type StringEntry struct{ entry[string] }

// BoolEntry is a configuration entry whose resolved value is a bool.
type BoolEntry struct{ entry[bool] }

func coerce[T any](v any) (T, error) {
	if t, ok := v.(T); ok {
		return t, nil
	}
	var zero T
	rv := reflect.ValueOf(v)
	target := reflect.TypeFor[T]()
	if isNumericKind(rv.Kind()) && isNumericKind(target.Kind()) {
		return rv.Convert(target).Interface().(T), nil
	}
	if rv.Kind() == reflect.String {
		return parseString[T](rv.String(), target)
	}
	return zero, fmt.Errorf("confstruct: cannot convert %T to %s", v, target)
}

func parseString[T any](s string, target reflect.Type) (T, error) {
	var zero T
	switch target.Kind() {
	case reflect.Bool:
		b, err := strconv.ParseBool(s)
		if err != nil {
			return zero, fmt.Errorf("confstruct: cannot parse %q as bool", s)
		}
		return any(b).(T), nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n, err := strconv.ParseInt(s, 10, target.Bits())
		if err != nil {
			return zero, fmt.Errorf("confstruct: cannot parse %q as %s", s, target)
		}
		return reflect.ValueOf(n).Convert(target).Interface().(T), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		n, err := strconv.ParseUint(s, 10, target.Bits())
		if err != nil {
			return zero, fmt.Errorf("confstruct: cannot parse %q as %s", s, target)
		}
		return reflect.ValueOf(n).Convert(target).Interface().(T), nil
	case reflect.Float32, reflect.Float64:
		n, err := strconv.ParseFloat(s, target.Bits())
		if err != nil {
			return zero, fmt.Errorf("confstruct: cannot parse %q as %s", s, target)
		}
		return reflect.ValueOf(n).Convert(target).Interface().(T), nil
	}
	return zero, fmt.Errorf("confstruct: cannot convert string to %s", target)
}

func isNumericKind(k reflect.Kind) bool {
	switch k {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return true
	}
	return false
}

// Populate initializes every entry in cfgStruct from the registered backends
// and registers watchers for any WatchableBackend layers. cfgStruct must be a
// pointer to a struct embedding confstruct.Meta with at least one backend
// registered; the lowest-priority backend (first added) must not be a
// WatchableBackend. Populate may only be called once per config struct.
func Populate(ctx context.Context, cfgStruct any) error {
	rv := reflect.ValueOf(cfgStruct)
	if rv.Kind() != reflect.Pointer || rv.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("confstruct: Populate requires a pointer to a struct")
	}

	sv := rv.Elem()
	metaField := sv.FieldByName("Meta")
	if !metaField.IsValid() || metaField.Type() != metaType {
		return fmt.Errorf("confstruct: struct must embed confstruct.Meta")
	}

	meta := metaField.Addr().Interface().(*Meta)

	if !meta.populated.CompareAndSwap(false, true) {
		return fmt.Errorf("confstruct: Populate called more than once")
	}

	if len(meta.backends) == 0 {
		return fmt.Errorf("confstruct: no backends registered")
	}

	if _, ok := meta.backends[0].(WatchableBackend); ok {
		return fmt.Errorf("confstruct: lowest-priority backend must not be a WatchableBackend")
	}

	return walkAndInject(ctx, sv, meta, "")
}

func walkAndInject(ctx context.Context, sv reflect.Value, meta *Meta, prefix string) error {
	st := sv.Type()
	for i := 0; i < st.NumField(); i++ {
		f := st.Field(i)
		fv := sv.Field(i)

		if f.Type == metaType {
			continue
		}

		key := f.Name
		if prefix != "" {
			key = prefix + "." + f.Name
		}

		if reflect.PointerTo(f.Type).Implements(layerManagerType) {
			lm := fv.Addr().Interface().(layerManager)
			lm.initSlots(len(meta.backends))
			notify := func() {
				if len(meta.resolveHooks) == 0 {
					return
				}
				value, name, desc, isSet := lm.resolvedState()
				if !isSet {
					return
				}
				for _, h := range meta.resolveHooks {
					h(key, value, name, desc)
				}
			}
			for idx, b := range meta.backends {
				lm.initSlotMeta(idx, b.Name(), b.Describe())
				v, ok, err := b.Lookup(key)
				if err != nil {
					ok = false
				}
				lm.setSlot(idx, v, ok)
				if wb, watchable := b.(WatchableBackend); watchable {
					wb.Watch(ctx, key, func(v any, ok bool) {
						lm.setSlot(idx, v, ok)
						notify()
					})
				}
			}
			notify()
			continue
		}

		if f.Type.Kind() == reflect.Struct {
			if err := walkAndInject(ctx, fv, meta, key); err != nil {
				return err
			}
		}
	}
	return nil
}
