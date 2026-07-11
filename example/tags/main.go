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
//	go run -tags=example .
//
// This example uses MapFromTags() to supply hard-coded defaults from cs.default
// struct tag annotations, keeping defaults co-located with the field declarations.
//
// Layers, in ascending precedence order:
//
//  1. MapFromTags — defaults read from cs.default struct tags (always present).
//  2. File        — config.yaml in the current directory (optional; skipped if absent).
//  3. Env         — .env file and APP_-prefixed environment variables (highest priority).
//
// Example config.yaml:
//
//	server:
//	  port: 9090
//	db:
//	  host: postgres.internal
//	  name: production
//	  user: appuser
//
// Example .env:
//
//	APP_DATABASE_PASSWORD=secret
//	APP_DEBUG=true
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"

	cs "github.com/suyono3484/confstruct"
)

// ServerConfig holds HTTP server settings.
type ServerConfig struct {
	Host           cs.StringEntry `cs.default:"0.0.0.0"`
	Port           cs.IntEntry    `cs.default:"8080"`
	MaxConnections cs.IntEntry    `cs.default:"1000"`
}

// DatabaseConfig holds relational database settings.
type DatabaseConfig struct {
	Host     cs.StringEntry `cs.default:"localhost"`
	Port     cs.IntEntry    `cs.default:"5432"`
	Name     cs.StringEntry `cs.default:"myapp"`
	User     cs.StringEntry `cs.default:"postgres"`
	Password cs.StringEntry // intentionally no default; validated after Populate
}

// CacheConfig holds Redis / in-process cache settings.
type CacheConfig struct {
	Host cs.StringEntry `cs.default:"localhost"`
	Port cs.IntEntry    `cs.default:"6379"`
	TTL  cs.IntEntry    `cs.default:"300"` // seconds
}

// AppConfig is the top-level configuration struct.
//
// YAML shape:
//
//	server:
//	  host: 0.0.0.0
//	  port: 8080
//	  max_connections: 1000
//	database:
//	  host: localhost
//	  port: 5432
//	  name: myapp
//	  user: postgres
//	cache:
//	  host: localhost
//	  port: 6379
//	  ttl: 300
//	debug: false
//
// File backend note: Database may also be written as "db" because the field
// below carries `cs.file.segment-alias:"db"`.
//
// Embedding cs.Meta wires up AddLayer and the Populate machinery.
type AppConfig struct {
	cs.Meta

	Server   ServerConfig
	Database DatabaseConfig `cs.file.segment-alias:"db"`
	Cache    CacheConfig
	Debug    cs.BoolEntry `cs.default:"false"`
}

func main() {
	var cfg AppConfig

	// Layer 1 — MapFromTags: defaults read from cs.default struct tags (lowest priority).
	// The tag key formula is cs.<suffix>, so MapFromTags(&cfg, "default") reads cs.default tags.
	// All tag values are strings; confstruct coerces them into the target entry type.
	cfg.AddLayer(cs.MapFromTags(&cfg, "default"))

	// Layer 2 — File: config.yaml in the current directory (optional).
	// Nested YAML keys map case-insensitively to struct field paths:
	//   db.host     → Database.Host  (via cs.file.segment-alias)
	//   server.port → Server.Port
	if fileBackend, err := cs.File("config.yaml"); err == nil {
		cfg.AddLayer(fileBackend)
	} else if !errors.Is(err, os.ErrNotExist) {
		log.Fatalf("config file: %v", err)
	}

	// Layer 3 — Env: environment variables and optional .env file (highest priority).
	// Field paths are mapped to env var names by uppercasing and replacing dots
	// with underscores, then prepending the APP_ prefix:
	//   Server.Host           → APP_SERVER_HOST
	//   Database.Password     → APP_DATABASE_PASSWORD
	//   Cache.TTL             → APP_CACHE_TTL
	//   Debug                 → APP_DEBUG
	envBackend, err := cs.Env(
		cs.WithPrefix("APP"),
		cs.WithDotEnv(".env"), // silently skipped if the file does not exist
	)
	if err != nil {
		log.Fatalf("env backend: %v", err)
	}
	cfg.AddLayer(envBackend)

	// Optional: log every resolved key and its winning backend for diagnostics.
	cfg.OnResolve(func(key string, value any, backendName, backendDesc string) {
		if backendDesc != "" {
			log.Printf("config: %-30s = %-20v  (from %s: %s)", key, value, backendName, backendDesc)
		} else {
			log.Printf("config: %-30s = %-20v  (from %s)", key, value, backendName)
		}
	})

	if err := cs.Populate(context.Background(), &cfg); err != nil {
		log.Fatalf("populate: %v", err)
	}

	// Validate required fields that have no safe default.
	if !cfg.Database.Password.IsSet() {
		log.Fatal("APP_DATABASE_PASSWORD is required but not set")
	}

	fmt.Printf("Server:   %s:%d (max-conn=%d)\n",
		cfg.Server.Host.Value(),
		cfg.Server.Port.Value(),
		cfg.Server.MaxConnections.Value(),
	)
	fmt.Printf("Database: %s:%d/%s (user=%s)\n",
		cfg.Database.Host.Value(),
		cfg.Database.Port.Value(),
		cfg.Database.Name.Value(),
		cfg.Database.User.Value(),
	)
	fmt.Printf("Cache:    %s:%d (ttl=%ds)\n",
		cfg.Cache.Host.Value(),
		cfg.Cache.Port.Value(),
		cfg.Cache.TTL.Value(),
	)
	fmt.Printf("Debug:    %v\n", cfg.Debug.Value())
}
