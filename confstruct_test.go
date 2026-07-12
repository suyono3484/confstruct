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

package confstruct

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type testConfig struct {
	Meta
	Name     StringEntry
	Port     IntEntry
	Debug    BoolEntry
	Database struct {
		Host StringEntry
		Port Int32Entry
	}
}

func TestPopulate_singleLayer(t *testing.T) {
	var cfg testConfig
	cfg.AddLayer(Map(map[string]any{
		"Name":          "myapp",
		"Port":          8080,
		"Debug":         true,
		"Database.Host": "localhost",
		"Database.Port": int32(5432),
	}))
	if err := Populate(context.Background(), &cfg); err != nil {
		t.Fatal(err)
	}

	checks := []struct {
		name string
		set  bool
		got  any
		want any
	}{
		{"Name", cfg.Name.IsSet(), cfg.Name.Value(), "myapp"},
		{"Port", cfg.Port.IsSet(), cfg.Port.Value(), 8080},
		{"Debug", cfg.Debug.IsSet(), cfg.Debug.Value(), true},
		{"Database.Host", cfg.Database.Host.IsSet(), cfg.Database.Host.Value(), "localhost"},
		{"Database.Port", cfg.Database.Port.IsSet(), cfg.Database.Port.Value(), int32(5432)},
	}
	for _, c := range checks {
		if !c.set {
			t.Errorf("%s: IsSet=false, want true", c.name)
		}
		if c.got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, c.got, c.want)
		}
	}
}

func TestPopulate_unsetFields(t *testing.T) {
	var cfg testConfig
	cfg.AddLayer(Map(map[string]any{
		"Name": "myapp",
	}))
	if err := Populate(context.Background(), &cfg); err != nil {
		t.Fatal(err)
	}

	if !cfg.Name.IsSet() {
		t.Error("Name: IsSet=false, want true")
	}
	if cfg.Port.IsSet() {
		t.Errorf("Port: IsSet=true, want false (got %v)", cfg.Port.Value())
	}
	if cfg.Debug.IsSet() {
		t.Errorf("Debug: IsSet=true, want false (got %v)", cfg.Debug.Value())
	}
}

func TestPopulate_layerPrecedence(t *testing.T) {
	var cfg testConfig
	cfg.AddLayer(Map(map[string]any{
		"Name": "default",
		"Port": 3000,
	}))
	cfg.AddLayer(Map(map[string]any{
		"Name": "override",
	}))
	if err := Populate(context.Background(), &cfg); err != nil {
		t.Fatal(err)
	}

	if cfg.Name.Value() != "override" {
		t.Errorf("Name: got %q, want %q", cfg.Name.Value(), "override")
	}
	if cfg.Port.Value() != 3000 {
		t.Errorf("Port: got %d, want 3000", cfg.Port.Value())
	}
}

func TestPopulate_noLayers(t *testing.T) {
	var cfg testConfig
	if err := Populate(context.Background(), &cfg); err == nil {
		t.Error("expected error when no backends are registered")
	}
}

func TestPopulate_numericCoercion(t *testing.T) {
	var cfg testConfig
	cfg.AddLayer(Map(map[string]any{
		"Port":          int64(9090),
		"Database.Port": int(5432),
	}))
	if err := Populate(context.Background(), &cfg); err != nil {
		t.Fatal(err)
	}

	if cfg.Port.Value() != 9090 {
		t.Errorf("Port: got %d, want 9090", cfg.Port.Value())
	}
	if cfg.Database.Port.Value() != 5432 {
		t.Errorf("Database.Port: got %d, want 5432", cfg.Database.Port.Value())
	}
}

func TestPopulate_numericOverflowRejected(t *testing.T) {
	type overflowConfig struct {
		Meta
		Small  Int8Entry
		USmall Uint8Entry
		Neg    Uint16Entry
		Narrow Int32Entry
	}
	var cfg overflowConfig
	cfg.AddLayer(Map(map[string]any{
		"Small":  int64(300),
		"USmall": int64(300),
		"Neg":    int64(-1),
		"Narrow": int64(5_000_000_000),
	}))
	err := Populate(context.Background(), &cfg)
	if err == nil {
		t.Fatal("expected Populate error for overflowing numeric fields, got nil")
	}
	for _, field := range []string{"Small", "USmall", "Neg", "Narrow"} {
		if !strings.Contains(err.Error(), field) {
			t.Errorf("Populate error %q: missing mention of field %q", err.Error(), field)
		}
	}
	if cfg.Small.IsSet() {
		t.Error("Small: IsSet=true after overflow, want false")
	}
	if cfg.Small.Value() != 0 {
		t.Errorf("Small: got %v, want zero value", cfg.Small.Value())
	}
	if cfg.USmall.IsSet() {
		t.Error("USmall: IsSet=true after overflow, want false")
	}
	if cfg.USmall.Value() != 0 {
		t.Errorf("USmall: got %v, want zero value", cfg.USmall.Value())
	}
	if cfg.Neg.IsSet() {
		t.Error("Neg: IsSet=true after overflow, want false")
	}
	if cfg.Neg.Value() != 0 {
		t.Errorf("Neg: got %v, want zero value", cfg.Neg.Value())
	}
	if cfg.Narrow.IsSet() {
		t.Error("Narrow: IsSet=true after overflow, want false")
	}
	if cfg.Narrow.Value() != 0 {
		t.Errorf("Narrow: got %v, want zero value", cfg.Narrow.Value())
	}
}

func TestPopulate_float32OverflowRejected(t *testing.T) {
	type overflowConfig struct {
		Meta
		Big Float32Entry
	}
	var cfg overflowConfig
	cfg.AddLayer(Map(map[string]any{
		"Big": float64(1e40),
	}))
	err := Populate(context.Background(), &cfg)
	if err == nil {
		t.Fatal("expected Populate error for float32 overflow, got nil")
	}
	if !strings.Contains(err.Error(), "Big") {
		t.Errorf("Populate error %q: missing mention of field %q", err.Error(), "Big")
	}
	if cfg.Big.IsSet() {
		t.Error("Big: IsSet=true after float32 overflow, want false")
	}
	if cfg.Big.Value() != 0 {
		t.Errorf("Big: got %v, want zero value", cfg.Big.Value())
	}
}

func TestPopulate_numericInRangeStillWorks(t *testing.T) {
	type boundaryConfig struct {
		Meta
		MaxInt8  Int8Entry
		MinInt8  Int8Entry
		MaxUint8 Uint8Entry
		MaxInt32 Int32Entry
	}
	var cfg boundaryConfig
	cfg.AddLayer(Map(map[string]any{
		"MaxInt8":  int64(127),
		"MinInt8":  int64(-128),
		"MaxUint8": int64(255),
		"MaxInt32": int64(2147483647),
	}))
	if err := Populate(context.Background(), &cfg); err != nil {
		t.Fatal(err)
	}

	checks := []struct {
		name string
		set  bool
		got  any
		want any
	}{
		{"MaxInt8", cfg.MaxInt8.IsSet(), cfg.MaxInt8.Value(), int8(127)},
		{"MinInt8", cfg.MinInt8.IsSet(), cfg.MinInt8.Value(), int8(-128)},
		{"MaxUint8", cfg.MaxUint8.IsSet(), cfg.MaxUint8.Value(), uint8(255)},
		{"MaxInt32", cfg.MaxInt32.IsSet(), cfg.MaxInt32.Value(), int32(2147483647)},
	}
	for _, c := range checks {
		if !c.set {
			t.Errorf("%s: IsSet=false, want true", c.name)
		}
		if c.got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, c.got, c.want)
		}
	}
}

func TestPopulate_UnexportedEntryFieldFails(t *testing.T) {
	type unexportedConfig struct {
		Meta
		port IntEntry
	}
	var cfg unexportedConfig
	cfg.AddLayer(Map(map[string]any{"port": 8080}))

	err := Populate(context.Background(), &cfg)
	if err == nil {
		t.Fatal("expected error for unexported entry field, got nil")
	}
	if !strings.Contains(err.Error(), "unexported") {
		t.Errorf("Populate: got error %q, want it to mention \"unexported\"", err.Error())
	}
	if strings.Contains(err.Error(), "reflect: reflect.Value.Interface") {
		t.Errorf("Populate: got raw reflect panic message %q, want a clean confstruct error", err.Error())
	}
	if !strings.Contains(err.Error(), "port") {
		t.Errorf("Populate: got error %q, want it to name the field %q", err.Error(), "port")
	}
}

func TestPopulate_UnexportedEntryFieldFails_Nested(t *testing.T) {
	type innerConfig struct {
		Deeper struct {
			port IntEntry
		}
	}
	type nestedUnexportedConfig struct {
		Meta
		Inner innerConfig
	}
	var cfg nestedUnexportedConfig
	cfg.AddLayer(Map(map[string]any{"Inner.Deeper.port": 8080}))

	err := Populate(context.Background(), &cfg)
	if err == nil {
		t.Fatal("expected error for nested unexported entry field, got nil")
	}
	if !strings.Contains(err.Error(), "unexported") {
		t.Errorf("Populate: got error %q, want it to mention \"unexported\"", err.Error())
	}
	if strings.Contains(err.Error(), "reflect: reflect.Value.Interface") {
		t.Errorf("Populate: got raw reflect panic message %q, want a clean confstruct error", err.Error())
	}
	if !strings.Contains(err.Error(), "Inner.Deeper.port") {
		t.Errorf("Populate: got error %q, want it to name the nested field path %q", err.Error(), "Inner.Deeper.port")
	}
}

func TestUnsetFields_UnexportedEntryFieldFails(t *testing.T) {
	type unexportedConfig struct {
		Meta
		port IntEntry
	}
	var cfg unexportedConfig

	unset, err := UnsetFields(&cfg)
	if err == nil {
		t.Fatal("expected error for unexported entry field, got nil")
	}
	if unset != nil {
		t.Errorf("UnsetFields: got non-nil unset slice %v alongside an error, want nil", unset)
	}
	if !strings.Contains(err.Error(), "unexported") {
		t.Errorf("UnsetFields: got error %q, want it to mention \"unexported\"", err.Error())
	}
	if strings.Contains(err.Error(), "reflect: reflect.Value.Interface") {
		t.Errorf("UnsetFields: got raw reflect panic message %q, want a clean confstruct error", err.Error())
	}
	if !strings.Contains(err.Error(), "port") {
		t.Errorf("UnsetFields: got error %q, want it to name the field %q", err.Error(), "port")
	}
}

func TestPopulate_typeMismatchRejected(t *testing.T) {
	var cfg testConfig
	cfg.AddLayer(Map(map[string]any{
		"Name": 123, // int into StringEntry — mismatch
	}))
	err := Populate(context.Background(), &cfg)
	if err == nil {
		t.Fatal("expected Populate error for type mismatch, got nil")
	}
	if !strings.Contains(err.Error(), "Name") {
		t.Errorf("Populate error %q: missing mention of field %q", err.Error(), "Name")
	}
	if cfg.Name.IsSet() {
		t.Error("Name: IsSet=true after type mismatch, want false")
	}
}

func TestPopulate_multipleConversionErrorsAggregated(t *testing.T) {
	var cfg testConfig
	cfg.AddLayer(Map(map[string]any{
		"Name": 123,          // int into StringEntry — mismatch
		"Port": "not-a-port", // unparseable string into IntEntry
	}))
	err := Populate(context.Background(), &cfg)
	if err == nil {
		t.Fatal("expected Populate error for multiple broken fields, got nil")
	}
	for _, field := range []string{"Name", "Port"} {
		if !strings.Contains(err.Error(), field) {
			t.Errorf("Populate error %q: missing mention of field %q", err.Error(), field)
		}
	}
	var joined interface{ Unwrap() []error }
	if !errors.As(err, &joined) {
		t.Fatal("expected Populate error to unwrap as a joined error (errors.Join)")
	}
	if got := len(joined.Unwrap()); got != 2 {
		t.Errorf("Populate error: got %d joined errors, want 2", got)
	}
}

func TestPopulate_lowerLayerConversionErrorReportedEvenWhenOverridden(t *testing.T) {
	var cfg testConfig
	cfg.AddLayer(Map(map[string]any{"Port": "bogus"})) // lower-precedence: unparseable
	cfg.AddLayer(Map(map[string]any{"Port": 9090}))    // higher-precedence: valid, would win resolution
	err := Populate(context.Background(), &cfg)
	if err == nil {
		t.Fatal("expected Populate error from the lower-precedence layer's bad value, got nil")
	}
	if !strings.Contains(err.Error(), "Port") {
		t.Errorf("Populate error %q: missing mention of field %q", err.Error(), "Port")
	}
}

func TestPopulate_higherLayerConversionErrorLeavesLowerLayerValue(t *testing.T) {
	var cfg testConfig
	cfg.AddLayer(Map(map[string]any{"Port": 8080}))         // lower-precedence: valid
	cfg.AddLayer(Map(map[string]any{"Port": "not-a-port"})) // higher-precedence: invalid, would have overridden
	err := Populate(context.Background(), &cfg)
	if err == nil {
		t.Fatal("expected Populate error from the higher-precedence layer's bad value, got nil")
	}
	if !cfg.Port.IsSet() {
		t.Error("Port: IsSet=false, want true (stale lower-layer value retained)")
	}
	if cfg.Port.Value() != 8080 {
		t.Errorf("Port: got %v, want stale lower-layer value 8080", cfg.Port.Value())
	}
}

func TestPopulate_retryAfterConversionFailure(t *testing.T) {
	var cfg testConfig
	cfg.AddLayer(Map(map[string]any{"Port": 0})) // non-watchable base layer, required below Override
	ob := Override(map[string]any{"Port": "not-a-port"})
	cfg.AddLayer(ob)

	if err := Populate(context.Background(), &cfg); err == nil {
		t.Fatal("expected first Populate call to fail due to conversion error")
	}

	ob.Set("Port", 9090)

	if err := Populate(context.Background(), &cfg); err != nil {
		t.Fatalf("expected retry on the same struct to succeed once the backend value is fixed, got %v", err)
	}
	if cfg.Port.Value() != 9090 {
		t.Errorf("Port: got %v, want 9090", cfg.Port.Value())
	}
}

func TestPopulate_watchableBadPushAfterSuccessIgnored(t *testing.T) {
	var cfg testConfig
	w := &fakeWatchable{values: map[string]any{"Name": "remote"}}
	cfg.AddLayer(Map(map[string]any{"Name": "default"}))
	cfg.AddLayer(w)
	if err := Populate(context.Background(), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Name.Value() != "remote" {
		t.Fatalf("Name: got %q, want %q", cfg.Name.Value(), "remote")
	}

	// push a badly-typed value (int into StringEntry) after Populate succeeded
	w.trigger("Name", 123, true)

	if !cfg.Name.IsSet() {
		t.Error("Name: IsSet=false after bad push, want true (unchanged)")
	}
	if cfg.Name.Value() != "remote" {
		t.Errorf("Name after bad push: got %v, want unchanged %q", cfg.Name.Value(), "remote")
	}
}

func TestPopulate_requiresPointer(t *testing.T) {
	n := 42
	if err := Populate(context.Background(), n); err == nil {
		t.Error("expected error for non-pointer argument")
	}
}

func TestPopulate_requiresMeta(t *testing.T) {
	type noMeta struct {
		Name StringEntry
	}
	if err := Populate(context.Background(), &noMeta{}); err == nil {
		t.Error("expected error for struct without Meta")
	}
}

func TestPopulate_calledTwice(t *testing.T) {
	var cfg testConfig
	cfg.AddLayer(Map(map[string]any{"Name": "first"}))
	if err := Populate(context.Background(), &cfg); err != nil {
		t.Fatal(err)
	}
	if err := Populate(context.Background(), &cfg); err == nil {
		t.Error("expected error on second Populate call")
	}
}

func TestPopulate_watchableAsLowestLayer(t *testing.T) {
	var cfg testConfig
	cfg.AddLayer(&fakeWatchable{values: map[string]any{"Name": "remote"}})
	if err := Populate(context.Background(), &cfg); err == nil {
		t.Error("expected error when lowest-priority backend is watchable")
	}
}

func TestPopulate_lookupError(t *testing.T) {
	var cfg testConfig
	cfg.AddLayer(errBackend{err: errors.New("boom")})
	err := Populate(context.Background(), &cfg)
	if err == nil {
		t.Fatal("expected error when backend Lookup fails")
	}
	if !strings.Contains(err.Error(), `backend "err" lookup "Name"`) {
		t.Fatalf("Populate: got error %q; want backend lookup context", err)
	}
}

func TestPopulate_retryAfterFailure(t *testing.T) {
	var cfg testConfig
	failErr := errors.New("boom")
	backend := &toggleErrBackend{field: "Name", err: &failErr}
	cfg.AddLayer(backend)

	if err := Populate(context.Background(), &cfg); err == nil {
		t.Fatal("expected first Populate call to fail")
	}

	*backend.err = nil

	if err := Populate(context.Background(), &cfg); err != nil {
		t.Fatalf("expected retry on the same struct to succeed once the backend is fixed, got %v", err)
	}
	if cfg.Name.Value() != "value" {
		t.Errorf("Name: got %q, want %q", cfg.Name.Value(), "value")
	}

	if err := Populate(context.Background(), &cfg); err == nil {
		t.Error("expected a third Populate call to fail: a successful Populate is still one-shot")
	}
}

func TestPopulate_failureCancelsWatches(t *testing.T) {
	var cfg testConfig
	ov := Override(nil)
	cfg.AddLayer(Map(map[string]any{"Name": "default", "Port": 8080}))
	cfg.AddLayer(ov)
	cfg.AddLayer(errFieldBackend{field: "Port", err: errors.New("boom")})

	// Deliberately never cancelled: proves cleanup is driven by Populate's own
	// failure, not by the caller tearing down its context.
	ctx := context.Background()

	if err := Populate(ctx, &cfg); err == nil {
		t.Fatal("expected Populate to fail on the Port lookup error")
	}

	deadline := time.Now().Add(time.Second)
	for {
		ov.mu.RLock()
		n := len(ov.watchers["Name"])
		ov.mu.RUnlock()
		if n == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("watcher for Name still registered (%d) after failed Populate", n)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestPopulate_watchableUpdate(t *testing.T) {
	var cfg testConfig
	w := &fakeWatchable{values: map[string]any{"Name": "remote"}}
	cfg.AddLayer(Map(map[string]any{"Name": "default", "Port": 8080}))
	cfg.AddLayer(w)
	if err := Populate(context.Background(), &cfg); err != nil {
		t.Fatal(err)
	}

	if cfg.Name.Value() != "remote" {
		t.Errorf("Name: got %q, want %q", cfg.Name.Value(), "remote")
	}

	// simulate a remote update via the registered hook
	w.trigger("Name", "updated", true)

	if cfg.Name.Value() != "updated" {
		t.Errorf("Name after update: got %q, want %q", cfg.Name.Value(), "updated")
	}

	// simulate key removal — lower layer (Map) takes over
	w.trigger("Name", nil, false)

	if !cfg.Name.IsSet() {
		t.Error("Name: IsSet=false after removal, want true (lower layer has default)")
	}
	if cfg.Name.Value() != "default" {
		t.Errorf("Name after removal: got %q, want %q (lower layer should win)", cfg.Name.Value(), "default")
	}
}

func TestPopulate_watchSkippedAfterEarlierCoercionFailure(t *testing.T) {
	var cfg testConfig
	cfg.AddLayer(Map(map[string]any{
		"Name": 123, // int into StringEntry — coercion failure, accumulated in errs
	}))
	w := &fakeWatchable{values: map[string]any{"Port": 8080}}
	cfg.AddLayer(w)

	err := Populate(context.Background(), &cfg)
	if err == nil {
		t.Fatal("expected Populate error from Name's type mismatch")
	}
	if _, watched := w.hooks["Port"]; watched {
		t.Error("Port: Watch registered on fakeWatchable after an earlier field's coercion failure, want skipped")
	}
}

// fakeWatchable is a test WatchableBackend that lets tests trigger hook calls directly.
type fakeWatchable struct {
	values map[string]any
	hooks  map[string]func(any, bool)
}

type errBackend struct {
	err error
}

func (e errBackend) Lookup(path string) (any, bool, error) {
	if path == "Name" {
		return nil, false, e.err
	}
	return nil, false, nil
}

func (e errBackend) Name() string     { return "err" }
func (e errBackend) Describe() string { return "" }

// errFieldBackend errors only for the configured field, otherwise reports no value.
type errFieldBackend struct {
	field string
	err   error
}

func (e errFieldBackend) Lookup(path string) (any, bool, error) {
	if path == e.field {
		return nil, false, e.err
	}
	return nil, false, nil
}

func (e errFieldBackend) Name() string     { return "errField" }
func (e errFieldBackend) Describe() string { return "" }

// toggleErrBackend errors for the configured field only while *err is
// non-nil, letting a test simulate a transient failure being fixed and
// retried against the same backend.
type toggleErrBackend struct {
	field string
	err   *error
}

func (b *toggleErrBackend) Lookup(path string) (any, bool, error) {
	if path != b.field {
		return nil, false, nil
	}
	if *b.err != nil {
		return nil, false, *b.err
	}
	return "value", true, nil
}

func (b *toggleErrBackend) Name() string     { return "toggle" }
func (b *toggleErrBackend) Describe() string { return "" }

func (f *fakeWatchable) Lookup(path string) (any, bool, error) {
	v, ok := f.values[path]
	return v, ok, nil
}

func (f *fakeWatchable) Watch(_ context.Context, path string, hook func(any, bool)) {
	if f.hooks == nil {
		f.hooks = make(map[string]func(any, bool))
	}
	f.hooks[path] = hook
}

func (f *fakeWatchable) Name() string     { return "fakeWatchable" }
func (f *fakeWatchable) Describe() string { return "" }

func (f *fakeWatchable) trigger(path string, v any, ok bool) {
	if h, exists := f.hooks[path]; exists {
		h(v, ok)
	}
}

// TestOnResolve_NotFiredOnIdenticalOverrideSet verifies that repeating an
// identical Set call on the current winning layer does not refire OnResolve:
// the hook's contract is "changes the winner", not "a watchable layer wrote
// something".
func TestOnResolve_NotFiredOnIdenticalOverrideSet(t *testing.T) {
	var cfg testConfig
	var count atomic.Int64
	cfg.OnResolve(func(key string, value any, backendName, backendDesc string) {
		count.Add(1)
	})

	cfg.AddLayer(Map(map[string]any{"Name": "default"}))
	overrides := Override(map[string]any{"Name": "user"})
	cfg.AddLayer(overrides)

	if err := Populate(context.Background(), &cfg); err != nil {
		t.Fatal(err)
	}

	if got := count.Load(); got != 1 {
		t.Fatalf("hook count after Populate: got %d, want 1", got)
	}

	overrides.Set("Name", "user")
	overrides.Set("Name", "user")

	if got := count.Load(); got != 1 {
		t.Fatalf("hook count after identical Set calls: got %d, want 1 (unchanged)", got)
	}
}

// TestOnResolve_NotFiredWhenWatchedBackendIsNotWinner verifies that updates on
// a watchable layer that isn't the current winner (because a higher-priority
// layer already has a value) never refire OnResolve, since the resolved
// struct field never actually changes.
func TestOnResolve_NotFiredWhenWatchedBackendIsNotWinner(t *testing.T) {
	var cfg testConfig
	var count atomic.Int64
	cfg.OnResolve(func(key string, value any, backendName, backendDesc string) {
		count.Add(1)
	})

	cfg.AddLayer(Map(map[string]any{"Name": "default"}))
	low := Override(map[string]any{"Name": "low"})
	high := Override(map[string]any{"Name": "high"})
	cfg.AddLayer(low)
	cfg.AddLayer(high)

	if err := Populate(context.Background(), &cfg); err != nil {
		t.Fatal(err)
	}

	if got := count.Load(); got != 1 {
		t.Fatalf("hook count after Populate: got %d, want 1", got)
	}
	if got := cfg.Name.Value(); got != "high" {
		t.Fatalf("Name after Populate: got %q, want %q", got, "high")
	}

	low.Set("Name", "low2")
	low.Set("Name", "low3")
	low.Set("Name", "low4")

	if got := count.Load(); got != 1 {
		t.Fatalf("hook count after low-layer Set calls: got %d, want 1 (unchanged)", got)
	}
	if got := cfg.Name.Value(); got != "high" {
		t.Fatalf("Name after low-layer Set calls: got %q, want %q (high should still win)", got, "high")
	}
}

// TestOnResolve_NotFiredWhenPopulateFails is the regression repro for
// docs/pflag-plan-phase-0-code-review.md finding 1: Name resolves
// successfully before Port's coercion fails later in the same walk. OnResolve
// must not fire for Name (or anything else) since Populate as a whole never
// reaches stateDone, even though Name itself stays resolved and readable —
// see the "partial results are kept, not rolled back" contract documented on
// Populate.
func TestOnResolve_NotFiredWhenPopulateFails(t *testing.T) {
	var cfg testConfig
	var count atomic.Int64
	cfg.OnResolve(func(key string, value any, backendName, backendDesc string) {
		count.Add(1)
	})

	cfg.AddLayer(Map(map[string]any{
		"Name": "good-value",
		"Port": "not-a-port",
	}))

	err := Populate(context.Background(), &cfg)
	if err == nil {
		t.Fatal("expected Populate error for unparseable Port, got nil")
	}

	if got := count.Load(); got != 0 {
		t.Fatalf("OnResolve fire count after failed Populate: got %d, want 0", got)
	}
	if !cfg.Name.IsSet() || cfg.Name.Value() != "good-value" {
		t.Fatalf("Name after failed Populate: IsSet=%v Value=%q, want IsSet=true Value=%q (partial results stay resolved)",
			cfg.Name.IsSet(), cfg.Name.Value(), "good-value")
	}
	if cfg.Port.IsSet() {
		t.Fatal("Port after failed Populate: IsSet=true, want false (coercion failed)")
	}
}

// blockingBackend blocks in Lookup until release is closed, simulating a
// slow later-field backend so a concurrent push on an earlier field's
// WatchableBackend can land while Populate is still walking, before
// success/failure is known.
type blockingBackend struct {
	field   string
	release chan struct{}
}

func (b *blockingBackend) Lookup(path string) (any, bool, error) {
	if path != b.field {
		return nil, false, nil
	}
	<-b.release
	return "not-a-port", true, nil
}

func (b *blockingBackend) Name() string     { return "blocking" }
func (b *blockingBackend) Describe() string { return "" }

// TestOnResolve_NotFiredOnConcurrentPushDuringFailedPopulate is the
// regression repro for docs/pflag-plan-phase-0-code-review-2.md finding 1: a
// WatchableBackend push landing while Populate is still walking later fields
// must not fire OnResolve ahead of Populate's own success/failure
// determination. Name's Override layer is already watched by the time the
// walk reaches the blocking Port lookup; a concurrent Set on it must not
// notify until Populate is known to have succeeded, and here it never does.
func TestOnResolve_NotFiredOnConcurrentPushDuringFailedPopulate(t *testing.T) {
	var cfg testConfig
	var count atomic.Int64
	cfg.OnResolve(func(key string, value any, backendName, backendDesc string) {
		count.Add(1)
	})

	cfg.AddLayer(Map(map[string]any{"Name": "default", "Port": 0}))
	ob := Override(map[string]any{"Name": "initial"})
	cfg.AddLayer(ob)

	release := make(chan struct{})
	cfg.AddLayer(&blockingBackend{field: "Port", release: release})

	var wg sync.WaitGroup
	wg.Add(1)
	var populateErr error
	go func() {
		defer wg.Done()
		populateErr = Populate(context.Background(), &cfg)
	}()

	// give Populate time to register Name's Override watch and block on Port's Lookup
	time.Sleep(50 * time.Millisecond)
	ob.Set("Name", "concurrent-update")
	close(release)
	wg.Wait()

	if populateErr == nil {
		t.Fatal("expected Populate to fail due to Port's bad value")
	}
	if got := count.Load(); got != 0 {
		t.Fatalf("OnResolve fire count during/before a failed Populate call: got %d, want 0 (err=%v)", got, populateErr)
	}
}
