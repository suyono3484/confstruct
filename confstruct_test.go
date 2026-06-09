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
	"testing"
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

func TestPopulate_typeMismatchSilent(t *testing.T) {
	var cfg testConfig
	cfg.AddLayer(Map(map[string]any{
		"Name": 123, // int into StringEntry — mismatch
	}))
	if err := Populate(context.Background(), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Name.IsSet() {
		t.Error("Name: IsSet=true after type mismatch, want false")
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

// fakeWatchable is a test WatchableBackend that lets tests trigger hook calls directly.
type fakeWatchable struct {
	values map[string]any
	hooks  map[string]func(any, bool)
}

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
