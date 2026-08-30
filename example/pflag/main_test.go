//go:build example

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

// This file exercises the same defaults-completeness pattern as
// example/map/defaults_test.go, plus the precedence and fallthrough
// behavior this example exists to demonstrate.
package main

import (
	"context"
	"testing"

	"github.com/spf13/pflag"
	cs "github.com/suyono3484/confstruct"
	cspflag "github.com/suyono3484/confstruct/pflag"
)

// TestDefaultsAreComplete verifies that every entry is covered by the Map
// layer. Register only the Map backend so no other source can mask a
// missing key.
func TestDefaultsAreComplete(t *testing.T) {
	var cfg AppConfig
	cfg.AddLayer(cs.Map(defaultValues))
	if err := cs.Populate(context.Background(), &cfg); err != nil {
		t.Fatal(err)
	}
	unset, err := cs.UnsetFields(&cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(unset) > 0 {
		t.Errorf("fields with no default value in the Map layer: %v", unset)
	}
}

// TestUnprovidedFlagFallsThroughToDefault is the test-backed version of the
// fallthrough comment in main(): with no flags parsed at all, every field
// must resolve from the Map layer, not from pflag's own declared flag
// defaults (which differ from the Map defaults for ListenAddr/Database.Host
// on purpose, so a bug that let the flag's default leak through would be
// visible here).
func TestUnprovidedFlagFallsThroughToDefault(t *testing.T) {
	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	flags.String("listen-addr", "not-the-map-default", "address to listen on")
	flags.Bool("verbose", false, "enable verbose logging")
	flags.String("db-host", "not-the-map-default", "database host")
	flags.Int("db-port", 9999, "database port")
	if err := flags.Parse(nil); err != nil {
		t.Fatal(err)
	}

	var cfg AppConfig
	cfg.AddLayer(cs.Map(defaultValues))
	cfg.AddLayer(cspflag.PFlag(flags))
	if err := cs.Populate(context.Background(), &cfg); err != nil {
		t.Fatal(err)
	}

	if got := cfg.ListenAddr.Value(); got != defaultValues["ListenAddr"] {
		t.Errorf("ListenAddr = %q, want the Map default %q", got, defaultValues["ListenAddr"])
	}
	if got := cfg.Database.Host.Value(); got != defaultValues["Database.Host"] {
		t.Errorf("Database.Host = %q, want the Map default %q", got, defaultValues["Database.Host"])
	}
	if got := cfg.ListenAddr.SourceName(); got != cs.MapBackendName {
		t.Errorf("ListenAddr.SourceName() = %q, want %q", got, cs.MapBackendName)
	}
}

// TestProvidedFlagOverridesDefault confirms the other half of the story:
// a flag that is provided wins over the Map layer, using the cs.pflag-tagged
// Database.Host/Database.Port fields specifically, since those are the ones
// this example uses to demonstrate the tag override.
func TestProvidedFlagOverridesDefault(t *testing.T) {
	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	flags.String("listen-addr", "", "address to listen on")
	flags.Bool("verbose", false, "enable verbose logging")
	flags.String("db-host", "", "database host")
	flags.Int("db-port", 0, "database port")
	if err := flags.Parse([]string{"--db-host=db.internal", "--db-port=6543"}); err != nil {
		t.Fatal(err)
	}

	var cfg AppConfig
	cfg.AddLayer(cs.Map(defaultValues))
	cfg.AddLayer(cspflag.PFlag(flags))
	if err := cs.Populate(context.Background(), &cfg); err != nil {
		t.Fatal(err)
	}

	if got := cfg.Database.Host.Value(); got != "db.internal" {
		t.Errorf("Database.Host = %q, want %q", got, "db.internal")
	}
	if got := cfg.Database.Port.Value(); got != 6543 {
		t.Errorf("Database.Port = %d, want 6543", got)
	}
	if got := cfg.Database.Host.SourceName(); got != cspflag.PFlagBackendName {
		t.Errorf("Database.Host.SourceName() = %q, want %q", got, cspflag.PFlagBackendName)
	}
	// ListenAddr was never passed on the command line, so it should still
	// fall through to the Map default even though other fields won from
	// the PFlag layer in the same Populate call.
	if got := cfg.ListenAddr.Value(); got != defaultValues["ListenAddr"] {
		t.Errorf("ListenAddr = %q, want the Map default %q", got, defaultValues["ListenAddr"])
	}
}
