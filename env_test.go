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
	"os"
	"path/filepath"
	"testing"
)

func TestEnv_basic(t *testing.T) {
	t.Setenv("NAME", "envapp")
	t.Setenv("PORT", "9090")
	t.Setenv("DEBUG", "true")
	t.Setenv("DATABASE_HOST", "db.example.com")
	t.Setenv("DATABASE_PORT", "5432")

	var cfg testConfig
	eb, err := Env()
	if err != nil {
		t.Fatal(err)
	}
	cfg.AddLayer(eb)
	if err := Populate(context.Background(), &cfg); err != nil {
		t.Fatal(err)
	}

	checks := []struct {
		name string
		set  bool
		got  any
		want any
	}{
		{"Name", cfg.Name.IsSet(), cfg.Name.Value(), "envapp"},
		{"Port", cfg.Port.IsSet(), cfg.Port.Value(), 9090},
		{"Debug", cfg.Debug.IsSet(), cfg.Debug.Value(), true},
		{"Database.Host", cfg.Database.Host.IsSet(), cfg.Database.Host.Value(), "db.example.com"},
		{"Database.Port", cfg.Database.Port.IsSet(), cfg.Database.Port.Value(), int32(5432)},
	}
	for _, c := range checks {
		if !c.set {
			t.Errorf("%s: IsSet=false, want true", c.name)
		}
		if c.got != c.want {
			t.Errorf("%s: got %v (%T), want %v (%T)", c.name, c.got, c.got, c.want, c.want)
		}
	}
}

func TestEnv_withPrefix(t *testing.T) {
	t.Setenv("APP_NAME", "prefixed")
	t.Setenv("APP_PORT", "7070")

	var cfg testConfig
	eb, err := Env(WithPrefix("APP"))
	if err != nil {
		t.Fatal(err)
	}
	cfg.AddLayer(eb)
	if err := Populate(context.Background(), &cfg); err != nil {
		t.Fatal(err)
	}

	if cfg.Name.Value() != "prefixed" {
		t.Errorf("Name: got %q, want %q", cfg.Name.Value(), "prefixed")
	}
	if cfg.Port.Value() != 7070 {
		t.Errorf("Port: got %d, want 7070", cfg.Port.Value())
	}
}

func TestEnv_tagOverride(t *testing.T) {
	t.Setenv("DB_PORT", "15432")

	type Config struct {
		Meta
		Database struct {
			Port Int32Entry `cs.env:"DB_PORT"`
		}
	}

	var cfg Config
	eb, err := Env()
	if err != nil {
		t.Fatal(err)
	}
	cfg.AddLayer(eb)
	if err := Populate(context.Background(), &cfg); err != nil {
		t.Fatal(err)
	}

	if !cfg.Database.Port.IsSet() {
		t.Fatal("Database.Port: IsSet=false, want true")
	}
	if got := cfg.Database.Port.Value(); got != 15432 {
		t.Errorf("Database.Port: got %d, want 15432", got)
	}
}

func TestEnv_tagOverrideWithPrefix(t *testing.T) {
	t.Setenv("APP_DB_PORT", "25432")

	type Config struct {
		Meta
		Database struct {
			Port Int32Entry `cs.env:"DB_PORT"`
		}
	}

	var cfg Config
	eb, err := Env(WithPrefix("APP"))
	if err != nil {
		t.Fatal(err)
	}
	cfg.AddLayer(eb)
	if err := Populate(context.Background(), &cfg); err != nil {
		t.Fatal(err)
	}

	if !cfg.Database.Port.IsSet() {
		t.Fatal("Database.Port: IsSet=false, want true")
	}
	if got := cfg.Database.Port.Value(); got != 25432 {
		t.Errorf("Database.Port: got %d, want 25432", got)
	}
}

func TestEnv_dotEnvFile(t *testing.T) {
	dir := t.TempDir()
	envFile := filepath.Join(dir, ".env")
	content := `
# comment
NAME=dotenvapp
PORT=4000
DEBUG=false
DATABASE_HOST=dotenv-db
DATABASE_PORT=3306
`
	if err := os.WriteFile(envFile, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	var cfg testConfig
	eb, err := Env(WithDotEnv(envFile))
	if err != nil {
		t.Fatal(err)
	}
	cfg.AddLayer(eb)
	if err := Populate(context.Background(), &cfg); err != nil {
		t.Fatal(err)
	}

	checks := []struct {
		name string
		got  any
		want any
	}{
		{"Name", cfg.Name.Value(), "dotenvapp"},
		{"Port", cfg.Port.Value(), 4000},
		{"Debug", cfg.Debug.Value(), false},
		{"Database.Host", cfg.Database.Host.Value(), "dotenv-db"},
		{"Database.Port", cfg.Database.Port.Value(), int32(3306)},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s: got %v (%T), want %v (%T)", c.name, c.got, c.got, c.want, c.want)
		}
	}
}

func TestEnv_osEnvTakesPrecedenceOverDotEnv(t *testing.T) {
	dir := t.TempDir()
	envFile := filepath.Join(dir, ".env")
	if err := os.WriteFile(envFile, []byte("NAME=fromfile\nPORT=1111\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NAME", "fromenv")

	var cfg testConfig
	eb, err := Env(WithDotEnv(envFile))
	if err != nil {
		t.Fatal(err)
	}
	cfg.AddLayer(eb)
	if err := Populate(context.Background(), &cfg); err != nil {
		t.Fatal(err)
	}

	if cfg.Name.Value() != "fromenv" {
		t.Errorf("Name: got %q, want %q (OS env should win)", cfg.Name.Value(), "fromenv")
	}
	if cfg.Port.Value() != 1111 {
		t.Errorf("Port: got %d, want 1111 (from .env)", cfg.Port.Value())
	}
}

func TestEnv_tagLowercaseUppercasedForLookup(t *testing.T) {
	t.Setenv("APP_DB_PORT", "45432")

	type Config struct {
		Meta
		Database struct {
			Port Int32Entry `cs.env:"db_port"`
		}
	}

	var cfg Config
	eb, err := Env(WithPrefix("APP"))
	if err != nil {
		t.Fatal(err)
	}
	cfg.AddLayer(eb)
	if err := Populate(context.Background(), &cfg); err != nil {
		t.Fatal(err)
	}

	if !cfg.Database.Port.IsSet() {
		t.Fatal("Database.Port: IsSet=false, want true")
	}
	if got := cfg.Database.Port.Value(); got != 45432 {
		t.Errorf("Database.Port: got %d, want 45432", got)
	}
}

func TestEnv_tagMixedCaseNormalized(t *testing.T) {
	t.Setenv("DB_PORT", "55432")

	type Config struct {
		Meta
		Database struct {
			Port Int32Entry `cs.env:"Db_Port"`
		}
	}

	var cfg Config
	eb, err := Env()
	if err != nil {
		t.Fatal(err)
	}
	cfg.AddLayer(eb)
	if err := Populate(context.Background(), &cfg); err != nil {
		t.Fatal(err)
	}

	if !cfg.Database.Port.IsSet() {
		t.Fatal("Database.Port: IsSet=false, want true")
	}
	if got := cfg.Database.Port.Value(); got != 55432 {
		t.Errorf("Database.Port: got %d, want 55432", got)
	}
}

func TestEnv_tagOverrideDotEnv(t *testing.T) {
	dir := t.TempDir()
	envFile := filepath.Join(dir, ".env")
	if err := os.WriteFile(envFile, []byte("APP_DB_PORT=35432\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	type Config struct {
		Meta
		Database struct {
			Port Int32Entry `cs.env:"DB_PORT"`
		}
	}

	var cfg Config
	eb, err := Env(WithPrefix("APP"), WithDotEnv(envFile))
	if err != nil {
		t.Fatal(err)
	}
	cfg.AddLayer(eb)
	if err := Populate(context.Background(), &cfg); err != nil {
		t.Fatal(err)
	}

	if !cfg.Database.Port.IsSet() {
		t.Fatal("Database.Port: IsSet=false, want true")
	}
	if got := cfg.Database.Port.Value(); got != 35432 {
		t.Errorf("Database.Port: got %d, want 35432", got)
	}
}

func TestEnv_missingDotEnvSilentlyIgnored(t *testing.T) {
	var cfg testConfig
	eb, err := Env(WithDotEnv("/nonexistent/.env"))
	if err != nil {
		t.Fatalf("expected no error for missing .env file, got: %v", err)
	}
	cfg.AddLayer(eb)
	if err := Populate(context.Background(), &cfg); err != nil {
		t.Fatal(err)
	}
}

func TestEnv_dotEnvFormats(t *testing.T) {
	dir := t.TempDir()
	envFile := filepath.Join(dir, ".env")
	content := `
export EXPORTED=yes
QUOTED_DOUBLE="hello world"
QUOTED_SINGLE='single quoted'
INLINE_COMMENT=value # this is a comment
`
	if err := os.WriteFile(envFile, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	b := &envBackend{}
	m, err := parseDotEnv(envFile)
	if err != nil {
		t.Fatal(err)
	}
	b.dotEnv = m

	cases := []struct{ key, want string }{
		{"EXPORTED", "yes"},
		{"QUOTED_DOUBLE", "hello world"},
		{"QUOTED_SINGLE", "single quoted"},
		{"INLINE_COMMENT", "value"},
	}
	for _, c := range cases {
		v, ok, _ := b.Lookup(c.key)
		if !ok {
			t.Errorf("%s: not found", c.key)
			continue
		}
		if v.(string) != c.want {
			t.Errorf("%s: got %q, want %q", c.key, v, c.want)
		}
	}
}

func TestEnv_layeredWithMap(t *testing.T) {
	t.Setenv("NAME", "from-env")

	var cfg testConfig
	cfg.AddLayer(Map(map[string]any{
		"Name": "default",
		"Port": 3000,
	}))
	eb, err := Env()
	if err != nil {
		t.Fatal(err)
	}
	cfg.AddLayer(eb)
	if err := Populate(context.Background(), &cfg); err != nil {
		t.Fatal(err)
	}

	if cfg.Name.Value() != "from-env" {
		t.Errorf("Name: got %q, want %q (env layer should win)", cfg.Name.Value(), "from-env")
	}
	if cfg.Port.Value() != 3000 {
		t.Errorf("Port: got %d, want 3000 (map fallback)", cfg.Port.Value())
	}
}

func TestParseString_types(t *testing.T) {
	cases := []struct {
		input string
		run   func(string) bool
	}{
		{"42", func(s string) bool {
			v, err := coerce[int](s)
			return err == nil && v == 42
		}},
		{"255", func(s string) bool {
			v, err := coerce[uint8](s)
			return err == nil && v == 255
		}},
		{"3.14", func(s string) bool {
			v, err := coerce[float64](s)
			return err == nil && v == 3.14
		}},
		{"true", func(s string) bool {
			v, err := coerce[bool](s)
			return err == nil && v == true
		}},
		{"1", func(s string) bool {
			v, err := coerce[bool](s)
			return err == nil && v == true
		}},
	}
	for _, c := range cases {
		if !c.run(c.input) {
			t.Errorf("coerce(%q) failed", c.input)
		}
	}
}
