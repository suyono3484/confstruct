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
// This example uses Map() to supply hard-coded defaults as the lowest-priority layer.
// Defaults are kept in a package-level variable so the companion defaults_test.go
// can reference the same map — adding a new struct field forces a single update.
//
// Layers, in ascending precedence order:
//
//  1. Map  — hard-coded application defaults (always present).
//  2. File — config.yaml in the current directory (optional; skipped if absent).
//  3. Env  — .env file and APP_-prefixed environment variables (highest priority).
//
// Example config.yaml:
//
//	server:
//	  port: 9090
//	database:
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
	Host           cs.StringEntry
	Port           cs.IntEntry
	MaxConnections cs.IntEntry
}

// DatabaseConfig holds relational database settings.
type DatabaseConfig struct {
	Host     cs.StringEntry
	Port     cs.IntEntry
	Name     cs.StringEntry
	User     cs.StringEntry
	Password cs.StringEntry
}

// CacheConfig holds Redis / in-process cache settings.
type CacheConfig struct {
	Host cs.StringEntry
	Port cs.IntEntry
	TTL  cs.IntEntry // seconds
}

// AppConfig is the top-level configuration struct.
// Embedding cs.Meta wires up AddLayer and the Populate machinery.
type AppConfig struct {
	cs.Meta

	Server   ServerConfig
	Database DatabaseConfig
	Cache    CacheConfig
	Debug    cs.BoolEntry
}

// defaultValues is the canonical set of hard-coded application defaults.
// Keeping it as a package-level variable lets both main() and defaults_test.go
// reference the same map — adding a new struct field forces a single update.
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

func main() {
	var cfg AppConfig

	// Layer 1 — Map: hard-coded application defaults (lowest priority).
	// Every key is the dot-separated struct field path.
	cfg.AddLayer(cs.Map(defaultValues))

	// Layer 2 — File: config.yaml in the current directory (optional).
	// Nested YAML keys map case-insensitively to struct field paths:
	//   database.host → Database.Host
	//   server.port   → Server.Port
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
