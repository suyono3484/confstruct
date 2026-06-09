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

func TestOverride_initialValues(t *testing.T) {
	var cfg testConfig
	overrides := Override(map[string]any{"Name": "user"})
	cfg.AddLayer(Map(map[string]any{
		"Name": "default",
		"Port": 8080,
	}))
	cfg.AddLayer(overrides)

	if err := Populate(context.Background(), &cfg); err != nil {
		t.Fatal(err)
	}

	if got := cfg.Name.Value(); got != "user" {
		t.Errorf("Name: got %q, want %q", got, "user")
	}
	if got := cfg.Name.SourceName(); got != OverrideBackendName {
		t.Errorf("Name source: got %q, want %q", got, OverrideBackendName)
	}
	if got := cfg.Port.Value(); got != 8080 {
		t.Errorf("Port: got %d, want 8080", got)
	}
}

func TestOverride_setAndUnset(t *testing.T) {
	var cfg testConfig
	overrides := Override(nil)
	cfg.AddLayer(Map(map[string]any{
		"Name":          "default",
		"Database.Host": "db-default",
	}))
	cfg.AddLayer(overrides)

	if err := Populate(context.Background(), &cfg); err != nil {
		t.Fatal(err)
	}

	overrides.Set("Name", "user")
	if got := cfg.Name.Value(); got != "user" {
		t.Errorf("Name after Set: got %q, want %q", got, "user")
	}

	overrides.Set("Database.Host", "db-user")
	if got := cfg.Database.Host.Value(); got != "db-user" {
		t.Errorf("Database.Host after Set: got %q, want %q", got, "db-user")
	}

	overrides.Unset("Name")
	if got := cfg.Name.Value(); got != "default" {
		t.Errorf("Name after Unset: got %q, want %q", got, "default")
	}
}

func TestOverride_typeMismatchKeepsPreviousValue(t *testing.T) {
	var cfg testConfig
	overrides := Override(nil)
	cfg.AddLayer(Map(map[string]any{"Port": 8080}))
	cfg.AddLayer(overrides)

	if err := Populate(context.Background(), &cfg); err != nil {
		t.Fatal(err)
	}

	overrides.Set("Port", 9090)
	if got := cfg.Port.Value(); got != 9090 {
		t.Fatalf("Port after valid Set: got %d, want 9090", got)
	}

	overrides.Set("Port", "not-a-number")
	if got := cfg.Port.Value(); got != 9090 {
		t.Errorf("Port after invalid Set: got %d, want 9090", got)
	}
}
