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

// This file is an example of the recommended unit test for validating that the
// lowest-precedence (defaults) layer covers every entry in the config struct.
package main

import (
	"context"
	"testing"

	cs "github.com/suyono3484/confstruct"
)

// defaultValues mirrors the Map backend in main(). Keeping it as a package-level
// variable lets both main() and the test reference the same set of defaults,
// so adding a new struct field forces a single update in one place.
var defaultValues = map[string]any{
	"Server.Host":           "0.0.0.0",
	"Server.Port":           8080,
	"Server.MaxConnections": 1000,
	"Database.Host":         "localhost",
	"Database.Port":         5432,
	"Database.Name":         "myapp",
	"Database.User":         "postgres",
	// Database.Password is intentionally absent: it has no safe default and must
	// be supplied at runtime via APP_DATABASE_PASSWORD. The application validates
	// this with an explicit IsSet() check after Populate.
	"Cache.Host": "localhost",
	"Cache.Port": 6379,
	"Cache.TTL":  300,
	"Debug":      false,
}

// TestDefaultsAreComplete verifies that every entry with a safe default is
// covered by the lowest layer. Register only the Map backend so that
// higher-priority sources (file, env) cannot mask a missing default.
func TestDefaultsAreComplete(t *testing.T) {
	var cfg AppConfig
	cfg.AddLayer(cs.Map(defaultValues))
	if err := cs.Populate(context.Background(), &cfg); err != nil {
		t.Fatal(err)
	}

	entries := []struct {
		name  string
		isSet bool
	}{
		{"Server.Host", cfg.Server.Host.IsSet()},
		{"Server.Port", cfg.Server.Port.IsSet()},
		{"Server.MaxConnections", cfg.Server.MaxConnections.IsSet()},
		{"Database.Host", cfg.Database.Host.IsSet()},
		{"Database.Port", cfg.Database.Port.IsSet()},
		{"Database.Name", cfg.Database.Name.IsSet()},
		{"Database.User", cfg.Database.User.IsSet()},
		{"Cache.Host", cfg.Cache.Host.IsSet()},
		{"Cache.Port", cfg.Cache.Port.IsSet()},
		{"Cache.TTL", cfg.Cache.TTL.IsSet()},
		{"Debug", cfg.Debug.IsSet()},
	}

	for _, e := range entries {
		if !e.isSet {
			t.Errorf("%s has no default value in the lowest layer", e.name)
		}
	}
}
