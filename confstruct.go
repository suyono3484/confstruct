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
// Backends receive canonical field paths as dot-separated chains of Go struct
// field names: "Host", "DB.Name", "DB.Pool.Max". The path is derived from the
// Go field names in the struct definition. Some built-in backends may
// additionally consult struct tags while resolving their own source-specific
// keys; for example, the Env backend recognizes `cs.env` and the File backend
// recognizes `cs.file.segment-alias`.
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
	"errors"
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
// The context passed to Watch is derived from the context passed to Populate: it
// is cancelled when that context is cancelled, and also cancelled immediately if
// Populate itself fails, so a failed Populate call never leaves watches running.
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
	setSlot(index int, v any, ok bool) error
	resolvedState() (value any, backendName, backendDesc string, isSet bool)
	hasChangedSinceNotify() (value any, name, desc string, isSet bool, changed bool)
}

var (
	layerManagerType = reflect.TypeFor[layerManager]()
	metaType         = reflect.TypeFor[Meta]()
)

type fieldAwareBackend interface {
	lookupField(path string, fields []reflect.StructField) (any, bool, error)
}

// populateState tracks the lifecycle of a single Meta across Populate calls.
// A struct starts stateIdle, moves to stateRunning while a Populate call owns
// it, and only reaches the terminal stateDone once that call fully succeeds.
// A failed call releases the claim back to stateIdle instead of permanently
// locking the struct — only a successful call does that. This is not an
// automatic-retry mechanism: most callers should treat a Populate error as
// fatal. The allowance exists for callers that construct config
// incrementally (a test isolating one backend, a tool letting a user correct
// a bad source in place) and want to call Populate again on the same struct
// instance once the cause is fixed.
const (
	stateIdle uint32 = iota
	stateRunning
	stateDone
)

// Meta holds globally registered backends. Embedding it in a config struct
// promotes AddLayer and OnResolve onto the struct directly.
type Meta struct {
	backends     []Backend
	resolveHooks []ResolveHook
	state        atomic.Uint32
	watchCancel  context.CancelFunc
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
	mu            sync.RWMutex
	slots         []layerSlot[T]
	resolved      T
	isSet         bool
	resolvedName  string
	resolvedDesc  string
	notifiedValue T
	notifiedName  string
	notifiedDesc  string
	notifiedIsSet bool
}

func (e *entry[T]) initSlots(n int) {
	e.slots = make([]layerSlot[T], n)
}

func (e *entry[T]) initSlotMeta(index int, name, desc string) {
	e.slots[index].backendName = name
	e.slots[index].backendDesc = desc
}

func (e *entry[T]) setSlot(index int, v any, ok bool) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if ok {
		coerced, err := coerce[T](v)
		if err != nil {
			return err
		}
		e.slots[index].value = coerced
		e.slots[index].ok = true
	} else {
		e.slots[index].value = *new(T)
		e.slots[index].ok = false
	}
	e.resolveUnderLock()
	return nil
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

// hasChangedSinceNotify reports the current resolved state along with whether
// it differs from the state last reported through this method. It only
// reports changed=true when isSet is true and either this is the first time
// reporting or the resolved value/backend name/backend desc differs from what
// was last reported; once it reports changed=true, it remembers that state so
// a subsequent identical resolution reports changed=false.
func (e *entry[T]) hasChangedSinceNotify() (value any, name, desc string, isSet bool, changed bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	value, name, desc, isSet = e.resolved, e.resolvedName, e.resolvedDesc, e.isSet
	if !isSet {
		return value, name, desc, isSet, false
	}
	changed = !e.notifiedIsSet || name != e.notifiedName || desc != e.notifiedDesc ||
		!reflect.DeepEqual(any(e.resolved), any(e.notifiedValue))
	if changed {
		e.notifiedValue = e.resolved
		e.notifiedName = name
		e.notifiedDesc = desc
		e.notifiedIsSet = true
	}
	return
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
		// Route through the same bounds-checked parseString/strconv.Parse*
		// path used for string-sourced values, rather than calling
		// rv.Convert(target) directly: Convert silently truncates/wraps on
		// narrowing or sign-changing conversions and on float64->float32
		// magnitude overflow, since that's the defined (and desired, for
		// plain Go code) behavior of a static numeric conversion. Formatting
		// rv to its canonical decimal string and re-parsing it with the
		// target's explicit bit size gets range checking for free from
		// strconv, with a single implementation shared by both call sites.
		result, err := parseString[T](formatNumericValue(rv), target)
		if err != nil {
			return zero, fmt.Errorf("value %v overflows %s", v, target)
		}
		return result, nil
	}
	if rv.Kind() == reflect.String {
		return parseString[T](rv.String(), target)
	}
	return zero, fmt.Errorf("cannot convert %T to %s", v, target)
}

// formatNumericValue formats a numeric reflect.Value to its canonical decimal
// string form, dispatching on the value's own native kind (not the target
// type). Callers are expected to have already confirmed rv.Kind() is one of
// the kinds isNumericKind recognizes.
func formatNumericValue(rv reflect.Value) string {
	switch {
	case rv.CanInt():
		return strconv.FormatInt(rv.Int(), 10)
	case rv.CanUint():
		return strconv.FormatUint(rv.Uint(), 10)
	default:
		return strconv.FormatFloat(rv.Float(), 'g', -1, 64)
	}
}

func parseString[T any](s string, target reflect.Type) (T, error) {
	var zero T
	switch target.Kind() {
	case reflect.Bool:
		b, err := strconv.ParseBool(s)
		if err != nil {
			return zero, fmt.Errorf("cannot parse %q as bool", s)
		}
		return any(b).(T), nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n, err := strconv.ParseInt(s, 10, target.Bits())
		if err != nil {
			return zero, fmt.Errorf("cannot parse %q as %s", s, target)
		}
		return reflect.ValueOf(n).Convert(target).Interface().(T), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		n, err := strconv.ParseUint(s, 10, target.Bits())
		if err != nil {
			return zero, fmt.Errorf("cannot parse %q as %s", s, target)
		}
		return reflect.ValueOf(n).Convert(target).Interface().(T), nil
	case reflect.Float32, reflect.Float64:
		n, err := strconv.ParseFloat(s, target.Bits())
		if err != nil {
			return zero, fmt.Errorf("cannot parse %q as %s", s, target)
		}
		return reflect.ValueOf(n).Convert(target).Interface().(T), nil
	}
	return zero, fmt.Errorf("cannot convert string to %s", target)
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
// WatchableBackend. A successful Populate call may only happen once per config
// struct. A call that returns an error does not permanently lock the struct
// the way a successful call does, so the same struct remains valid to pass to
// Populate again after the cause of the failure has been addressed. This is
// not an automatic-retry mechanism, and most callers — e.g. a service loading
// its config once at startup — should treat a non-nil error as fatal. The
// allowance exists for callers that construct config incrementally, such as a
// test that isolates one backend or a tool that lets a user correct a bad
// source in place, without being forced to start over with a new struct
// instance.
//
// A failed call does not undo work it already did: any field whose value was
// successfully resolved before the failure is left resolved and readable
// (IsSet and Value reflect it) even though Populate returns a non-nil error.
// The only contract callers can rely on is that a non-nil error means the
// struct as a whole is not fully and correctly populated — not that no field
// in it was touched. Do not read cfgStruct after a failed call without first
// checking the error.
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

	if !meta.state.CompareAndSwap(stateIdle, stateRunning) {
		return fmt.Errorf("confstruct: Populate called more than once")
	}

	if len(meta.backends) == 0 {
		meta.state.Store(stateIdle)
		return fmt.Errorf("confstruct: no backends registered")
	}

	if _, ok := meta.backends[0].(WatchableBackend); ok {
		meta.state.Store(stateIdle)
		return fmt.Errorf("confstruct: lowest-priority backend must not be a WatchableBackend")
	}

	watchCtx, cancelWatches := context.WithCancel(ctx)
	meta.watchCancel = cancelWatches

	var errs []error
	var pending []func()
	err := walkAndInject(watchCtx, sv, meta, "", nil, &errs, &pending)
	if err != nil || len(errs) > 0 {
		cancelWatches()
		meta.watchCancel = nil
		meta.state.Store(stateIdle)
		return errors.Join(append(errs, err)...)
	}

	for _, notify := range pending {
		notify()
	}

	meta.state.Store(stateDone)
	return nil
}

// UnsetFields walks cfgStruct and returns the dot-separated paths of all entry
// fields for which IsSet() is false, in struct field order. An empty slice means
// every field has a value from at least one backend. Panics if cfgStruct is not a
// pointer to a struct embedding confstruct.Meta. Returns a non-nil error if the
// struct contains an unexported entry field, since such a field cannot legally be
// inspected via reflection.
//
// Typically called after Populate — before it, no backend has run and every field
// will appear in the result. Useful for startup validation and as a test helper
// that avoids hand-listing every field:
//
//	unset, err := confstruct.UnsetFields(&cfg)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	if len(unset) > 0 {
//	    log.Fatalf("required config fields are not set: %v", unset)
//	}
func UnsetFields(cfgStruct any) ([]string, error) {
	rv := reflect.ValueOf(cfgStruct)
	if rv.Kind() != reflect.Pointer || rv.Elem().Kind() != reflect.Struct {
		panic("confstruct: UnsetFields requires a pointer to a struct")
	}
	sv := rv.Elem()
	metaField := sv.FieldByName("Meta")
	if !metaField.IsValid() || metaField.Type() != metaType {
		panic("confstruct: struct must embed confstruct.Meta")
	}
	var unset []string
	if err := collectUnset(sv, "", &unset); err != nil {
		return nil, err
	}
	return unset, nil
}

func collectUnset(sv reflect.Value, prefix string, unset *[]string) error {
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
			if !f.IsExported() {
				return fmt.Errorf("confstruct: field %q is an unexported entry field; entry fields must be exported", key)
			}
			lm := fv.Addr().Interface().(layerManager)
			_, _, _, isSet := lm.resolvedState()
			if !isSet {
				*unset = append(*unset, key)
			}
			continue
		}

		if f.Type.Kind() == reflect.Struct {
			if err := collectUnset(fv, key, unset); err != nil {
				return err
			}
		}
	}
	return nil
}

func walkAndInject(ctx context.Context, sv reflect.Value, meta *Meta, prefix string, chain []reflect.StructField, errs *[]error, pending *[]func()) error {
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
		fieldChain := appendFieldChain(chain, f)

		if reflect.PointerTo(f.Type).Implements(layerManagerType) {
			if !f.IsExported() {
				return fmt.Errorf("confstruct: field %q is an unexported entry field; entry fields must be exported", key)
			}
			lm := fv.Addr().Interface().(layerManager)
			lm.initSlots(len(meta.backends))
			notify := func() {
				if len(meta.resolveHooks) == 0 {
					return
				}
				value, name, desc, isSet, changed := lm.hasChangedSinceNotify()
				if !isSet || !changed {
					return
				}
				for _, h := range meta.resolveHooks {
					h(key, value, name, desc)
				}
			}
			for idx, b := range meta.backends {
				lm.initSlotMeta(idx, b.Name(), b.Describe())
				v, ok, err := lookupBackendValue(b, key, fieldChain)
				if err != nil {
					return fmt.Errorf("confstruct: backend %q lookup %q: %w", b.Name(), key, err)
				}
				if err := lm.setSlot(idx, v, ok); err != nil {
					*errs = append(*errs, fmt.Errorf("confstruct: backend %q field %q: %w", b.Name(), key, err))
				}
				if len(*errs) == 0 {
					if wb, watchable := b.(WatchableBackend); watchable {
						wb.Watch(ctx, key, func(v any, ok bool) {
							// Live-update coercion failures are intentionally discarded:
							// Populate has already returned, so there's no error path to
							// report through. Entry keeps its last good value. See
							// docs/populate-error-handling.md#scope-initial-population-only.
							_ = lm.setSlot(idx, v, ok)
							notify()
						})
					}
				}
			}
			*pending = append(*pending, notify)
			continue
		}

		if f.Type.Kind() == reflect.Struct {
			if err := walkAndInject(ctx, fv, meta, key, fieldChain, errs, pending); err != nil {
				return err
			}
		}
	}
	return nil
}

func appendFieldChain(chain []reflect.StructField, field reflect.StructField) []reflect.StructField {
	next := make([]reflect.StructField, len(chain)+1)
	copy(next, chain)
	next[len(chain)] = field
	return next
}

func lookupBackendValue(b Backend, path string, fields []reflect.StructField) (any, bool, error) {
	if fb, ok := b.(fieldAwareBackend); ok {
		return fb.lookupField(path, fields)
	}
	return b.Lookup(path)
}
