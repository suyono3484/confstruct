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

// This file is an example of the recommended unit test for validating that every
// expected cs.default struct tag is present and parses correctly. Because a missing
// or misspelled tag produces no compile-time or runtime error — only a silently
// unset entry — this test is the primary safety net for tag-based defaults.
package main

import (
	"context"
	"testing"

	cs "github.com/suyono3484/confstruct"
)

// TestTagDefaultsAreComplete verifies that every entry that should have a cs.default
// tag is actually set after MapFromTags runs. Register only the MapFromTags backend
// so that higher-priority sources (file, env) cannot mask a missing tag.
func TestTagDefaultsAreComplete(t *testing.T) {
	var cfg AppConfig
	cfg.AddLayer(cs.MapFromTags(&cfg, "default"))
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
			t.Errorf("%s has no cs.default tag or its value failed to parse", e.name)
		}
	}
}
