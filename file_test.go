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
	"strings"
	"testing"
)

func TestFile_YAML(t *testing.T) {
	content := `
database:
  host: localhost
  port: 5432
debug: true
name: myapp
`
	b := mustFileBackend(t, "config.yaml", content)

	cases := []struct {
		path string
		want any
	}{
		{"Database.Host", "localhost"},
		{"Database.Port", 5432},
		{"Debug", true},
		{"Name", "myapp"},
	}
	for _, c := range cases {
		v, ok, err := b.Lookup(c.path)
		if err != nil || !ok || v != c.want {
			t.Errorf("Lookup(%q) = %v, %v, %v; want %v", c.path, v, ok, err, c.want)
		}
	}

	if _, ok, _ := b.Lookup("Missing"); ok {
		t.Error("Lookup(Missing): want ok=false")
	}
}

func TestFile_YMLExtension(t *testing.T) {
	b := mustFileBackend(t, "config.yml", "host: localhost\n")
	v, ok, _ := b.Lookup("Host")
	if !ok || v != "localhost" {
		t.Errorf("Lookup(Host) = %v, %v; want localhost, true", v, ok)
	}
}

func TestFile_JSON(t *testing.T) {
	content := `{"database":{"host":"localhost","port":5432},"debug":true,"name":"myapp"}`
	b := mustFileBackend(t, "config.json", content)

	v, ok, _ := b.Lookup("Database.Host")
	if !ok || v != "localhost" {
		t.Errorf("Database.Host: got %v, %v; want localhost, true", v, ok)
	}
	// JSON numbers unmarshal to float64; coerce handles the conversion.
	v2, ok2, _ := b.Lookup("Database.Port")
	if !ok2 || v2 != float64(5432) {
		t.Errorf("Database.Port: got %v (%T), %v; want 5432.0, true", v2, v2, ok2)
	}
}

func TestFile_TOML(t *testing.T) {
	content := `
name = "myapp"
debug = true

[database]
host = "localhost"
port = 5432
`
	b := mustFileBackend(t, "config.toml", content)

	cases := []struct {
		path string
		want any
	}{
		{"Database.Host", "localhost"},
		{"Name", "myapp"},
		{"Debug", true},
	}
	for _, c := range cases {
		v, ok, err := b.Lookup(c.path)
		if err != nil || !ok || v != c.want {
			t.Errorf("Lookup(%q) = %v, %v, %v; want %v", c.path, v, ok, err, c.want)
		}
	}
}

func TestFile_CaseInsensitive(t *testing.T) {
	b := mustFileBackend(t, "config.yaml", "database:\n  host: localhost\n")
	for _, path := range []string{"database.host", "DATABASE.HOST", "Database.Host", "dAtAbAsE.hOsT"} {
		v, ok, _ := b.Lookup(path)
		if !ok || v != "localhost" {
			t.Errorf("Lookup(%q): got %v, %v; want localhost, true", path, v, ok)
		}
	}
}

func TestFile_WithFormat(t *testing.T) {
	content := `{"host":"localhost"}`
	dir := t.TempDir()
	path := filepath.Join(dir, "config.cfg") // non-standard extension
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	b, err := File(path, WithFormat("json"))
	if err != nil {
		t.Fatalf("File: %v", err)
	}
	v, ok, _ := b.Lookup("Host")
	if !ok || v != "localhost" {
		t.Errorf("Host: got %v, %v; want localhost, true", v, ok)
	}
}

func TestFile_UnknownFormat(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.cfg")
	os.WriteFile(path, []byte(""), 0o644)
	if _, err := File(path); err == nil {
		t.Error("expected error for unknown extension")
	}
}

func TestFile_MissingFile(t *testing.T) {
	if _, err := File("/nonexistent/path/config.yaml"); err == nil {
		t.Error("expected error for missing file")
	}
}

func TestFile_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	os.WriteFile(path, []byte("key: [unclosed"), 0o644)
	if _, err := File(path); err == nil {
		t.Error("expected error for invalid YAML")
	}
}

func TestFile_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	os.WriteFile(path, []byte("{bad json"), 0o644)
	if _, err := File(path); err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestFile_InvalidTOML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	os.WriteFile(path, []byte("= bad"), 0o644)
	if _, err := File(path); err == nil {
		t.Error("expected error for invalid TOML")
	}
}

func TestFile_EmptyFile(t *testing.T) {
	b := mustFileBackend(t, "config.yaml", "")
	if _, ok, _ := b.Lookup("Key"); ok {
		t.Error("empty file: Lookup should return ok=false")
	}
}

func TestFile_Populate_YAML(t *testing.T) {
	content := `
server:
  host: example.com
  port: 8080
workers: 4
`
	type Config struct {
		Meta
		Server struct {
			Host StringEntry
			Port IntEntry
		}
		Workers IntEntry
	}

	b := mustFileBackend(t, "config.yaml", content)
	var cfg Config
	cfg.AddLayer(b)
	if err := Populate(context.Background(), &cfg); err != nil {
		t.Fatalf("Populate: %v", err)
	}

	if got := cfg.Server.Host.Value(); got != "example.com" {
		t.Errorf("Server.Host: got %q; want example.com", got)
	}
	if got := cfg.Server.Port.Value(); got != 8080 {
		t.Errorf("Server.Port: got %d; want 8080", got)
	}
	if got := cfg.Workers.Value(); got != 4 {
		t.Errorf("Workers: got %d; want 4", got)
	}
}

func TestFile_Populate_SegmentAlias(t *testing.T) {
	content := `
db:
  hostname: aliased.example.com
`
	type Config struct {
		Meta
		Database struct {
			Host StringEntry `cs.file.segment-alias:"hostname"`
		} `cs.file.segment-alias:"db"`
	}

	b := mustFileBackend(t, "config.yaml", content)
	var cfg Config
	cfg.AddLayer(b)
	if err := Populate(context.Background(), &cfg); err != nil {
		t.Fatalf("Populate: %v", err)
	}

	if !cfg.Database.Host.IsSet() {
		t.Fatal("Database.Host: want IsSet() = true")
	}
	if got := cfg.Database.Host.Value(); got != "aliased.example.com" {
		t.Errorf("Database.Host: got %q; want aliased.example.com", got)
	}
}

func TestFile_Populate_SegmentAliasFallbackToFieldName(t *testing.T) {
	content := `
database:
  host: canonical.example.com
`
	type Config struct {
		Meta
		Database struct {
			Host StringEntry `cs.file.segment-alias:"hostname"`
		} `cs.file.segment-alias:"db"`
	}

	b := mustFileBackend(t, "config.yaml", content)
	var cfg Config
	cfg.AddLayer(b)
	if err := Populate(context.Background(), &cfg); err != nil {
		t.Fatalf("Populate: %v", err)
	}

	if got := cfg.Database.Host.Value(); got != "canonical.example.com" {
		t.Errorf("Database.Host: got %q; want canonical.example.com", got)
	}
}

func TestFile_Populate_SegmentAliasConflictOnParentSegment(t *testing.T) {
	content := `
database:
  host: canonical.example.com
db:
  hostname: aliased.example.com
`
	type Config struct {
		Meta
		Database struct {
			Host StringEntry `cs.file.segment-alias:"hostname"`
		} `cs.file.segment-alias:"db"`
	}

	b := mustFileBackend(t, "config.yaml", content)
	var cfg Config
	cfg.AddLayer(b)
	err := Populate(context.Background(), &cfg)
	if err == nil {
		t.Fatal("Populate: expected conflict error, got nil")
	}
	if got := err.Error(); got == "" || !strings.Contains(got, `both "Database" and alias "db" are present`) {
		t.Fatalf("Populate: got error %q; want parent segment conflict", got)
	}
}

func TestFile_Populate_SegmentAliasConflictOnLeafSegment(t *testing.T) {
	content := `
database:
  host: canonical.example.com
  hostname: aliased.example.com
`
	type Config struct {
		Meta
		Database struct {
			Host StringEntry `cs.file.segment-alias:"hostname"`
		}
	}

	b := mustFileBackend(t, "config.yaml", content)
	var cfg Config
	cfg.AddLayer(b)
	err := Populate(context.Background(), &cfg)
	if err == nil {
		t.Fatal("Populate: expected conflict error, got nil")
	}
	if got := err.Error(); got == "" || !strings.Contains(got, `both "Host" and alias "hostname" are present`) {
		t.Fatalf("Populate: got error %q; want leaf segment conflict", got)
	}
}

func TestFile_Populate_JSON(t *testing.T) {
	content := `{"server":{"host":"json.example.com","port":9090}}`

	type Config struct {
		Meta
		Server struct {
			Host StringEntry
			Port IntEntry
		}
	}

	b := mustFileBackend(t, "config.json", content)
	var cfg Config
	cfg.AddLayer(b)
	if err := Populate(context.Background(), &cfg); err != nil {
		t.Fatalf("Populate: %v", err)
	}

	if got := cfg.Server.Host.Value(); got != "json.example.com" {
		t.Errorf("Server.Host: got %q; want json.example.com", got)
	}
	if got := cfg.Server.Port.Value(); got != 9090 {
		t.Errorf("Server.Port: got %d; want 9090", got)
	}
}

func TestFile_Populate_TOML(t *testing.T) {
	content := "[server]\nhost = \"toml.example.com\"\nport = 7070\n"

	type Config struct {
		Meta
		Server struct {
			Host StringEntry
			Port IntEntry
		}
	}

	b := mustFileBackend(t, "config.toml", content)
	var cfg Config
	cfg.AddLayer(b)
	if err := Populate(context.Background(), &cfg); err != nil {
		t.Fatalf("Populate: %v", err)
	}

	if got := cfg.Server.Host.Value(); got != "toml.example.com" {
		t.Errorf("Server.Host: got %q; want toml.example.com", got)
	}
	if got := cfg.Server.Port.Value(); got != 7070 {
		t.Errorf("Server.Port: got %d; want 7070", got)
	}
}

func TestFile_LayeredWithMap(t *testing.T) {
	// File provides defaults; Map overrides one field.
	type Config struct {
		Meta
		Host StringEntry
		Port IntEntry
	}

	b := mustFileBackend(t, "config.yaml", "host: file.example.com\nport: 3000\n")
	var cfg Config
	cfg.AddLayer(b)
	cfg.AddLayer(Map(map[string]any{"Port": 4000}))
	if err := Populate(context.Background(), &cfg); err != nil {
		t.Fatalf("Populate: %v", err)
	}

	if got := cfg.Host.Value(); got != "file.example.com" {
		t.Errorf("Host: got %q; want file.example.com", got)
	}
	if got := cfg.Port.Value(); got != 4000 {
		t.Errorf("Port: got %d; want 4000 (Map override)", got)
	}
}

func mustFileBackend(t *testing.T, name, content string) Backend {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("mustFileBackend: %v", err)
	}
	b, err := File(path)
	if err != nil {
		t.Fatalf("File(%q): %v", name, err)
	}
	return b
}
