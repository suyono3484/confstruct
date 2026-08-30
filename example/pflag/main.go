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

// Run this example with:
//
//	go run -tags=example .                                       # no flags: everything falls through to defaults
//	go run -tags=example . --listen-addr=0.0.0.0:9090 --verbose   # flags win where provided
//
// This example uses PFlag() to let explicitly-provided command-line flags
// override every other layer. Layers, in ascending precedence order:
//
//  1. Map   — hard-coded application defaults (always present).
//  2. PFlag — command-line flags, only when explicitly provided (highest priority).
//
// github.com/suyono3484/confstruct/pflag and github.com/spf13/pflag are both
// named "pflag" — this file aliases the confstruct one as cspflag so it can
// import both without a collision. A reader copying just the pflag import
// line and forgetting the alias will hit this immediately.
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/spf13/pflag"
	cs "github.com/suyono3484/confstruct"
	cspflag "github.com/suyono3484/confstruct/pflag"
)

// DatabaseConfig holds database connection settings.
type DatabaseConfig struct {
	// Untagged, Host/Port would derive to database-host/database-port (see
	// docs/pflag-integration.md#mapping-flag-names-to-fields). Tagged here
	// to demonstrate the cs.pflag override and to match the shorter flag
	// names registered in main below.
	Host cs.StringEntry `cs.pflag:"db-host"`
	Port cs.IntEntry    `cs.pflag:"db-port"`
}

// AppConfig is the top-level configuration struct.
type AppConfig struct {
	cs.Meta

	ListenAddr cs.StringEntry
	Verbose    cs.BoolEntry
	Database   DatabaseConfig
}

// defaultValues is the canonical set of hard-coded application defaults —
// the lowest-priority layer, always present regardless of which flags a
// caller happens to provide.
var defaultValues = map[string]any{
	"ListenAddr":    "0.0.0.0:8080",
	"Verbose":       false,
	"Database.Host": "localhost",
	"Database.Port": 5432,
}

func main() {
	flags := pflag.NewFlagSet("myapp", pflag.ExitOnError)
	flags.String("listen-addr", "", "address to listen on")
	flags.Bool("verbose", false, "enable verbose logging")
	flags.String("db-host", "", "database host")
	flags.Int("db-port", 0, "database port")
	if err := flags.Parse(os.Args[1:]); err != nil {
		log.Fatalf("parse flags: %v", err)
	}

	var cfg AppConfig

	// Layer 1 — Map: hard-coded application defaults (lowest priority).
	cfg.AddLayer(cs.Map(defaultValues))

	// Layer 2 — PFlag: explicitly-provided command-line flags (highest
	// priority). Only a flag with Changed == true is considered present —
	// an unprovided flag's declared default ("", false, 0 above) is not a
	// configuration value. Run this example with no flags at all and every
	// field falls through to the Map layer's default instead of picking up
	// the flag package's own zero-value default; run it with, say,
	// --verbose and only that field switches to the PFlag layer.
	cfg.AddLayer(cspflag.PFlag(flags))

	cfg.OnResolve(func(key string, value any, backendName, backendDesc string) {
		log.Printf("config: %-20s = %-20v  (from %s)", key, value, backendName)
	})

	if err := cs.Populate(context.Background(), &cfg); err != nil {
		log.Fatalf("populate: %v", err)
	}

	fmt.Printf("ListenAddr: %s\n", cfg.ListenAddr.Value())
	fmt.Printf("Verbose:    %v\n", cfg.Verbose.Value())
	fmt.Printf("Database:   %s:%d\n", cfg.Database.Host.Value(), cfg.Database.Port.Value())
}
