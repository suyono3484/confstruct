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
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
	"gopkg.in/yaml.v3"
)

type fileFormat int

const (
	fileFormatUnknown fileFormat = iota
	fileFormatYAML
	fileFormatJSON
	fileFormatTOML
)

// FileOption configures a File backend.
type FileOption func(*fileBackend)

// WithFormat explicitly sets the file format, overriding extension-based detection.
// Accepted values: "yaml", "yml", "json", "toml".
func WithFormat(format string) FileOption {
	return func(b *fileBackend) {
		b.format = parseFileFormat(format)
	}
}

// File returns a Backend that reads configuration from a YAML, JSON, or TOML
// file. The format is inferred from the file extension (.yaml, .yml, .json,
// .toml); use WithFormat to override. Nested struct paths map to nested file
// keys via case-insensitive matching: Lookup("Database.Host") matches
// database.host or DATABASE.HOST in the file. The file is read once at
// construction time.
func File(path string, opts ...FileOption) (Backend, error) {
	b := &fileBackend{}
	for _, o := range opts {
		o(b)
	}

	if b.format == fileFormatUnknown {
		b.format = parseFileFormat(filepath.Ext(path))
	}
	if b.format == fileFormatUnknown {
		return nil, fmt.Errorf("confstruct: file backend: unsupported format for %q; use WithFormat or a .yaml/.json/.toml extension", path)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("confstruct: file backend: %w", err)
	}

	values, err := unmarshalFileData(data, b.format)
	if err != nil {
		return nil, fmt.Errorf("confstruct: file backend %q: %w", path, err)
	}

	b.path = path
	b.values = values
	return b, nil
}

type fileBackend struct {
	path   string
	format fileFormat
	values map[string]any
}

const FileBackendName = "file"

func (f *fileBackend) Name() string { return FileBackendName }

func (f *fileBackend) Describe() string {
	return f.path + ", " + f.formatString()
}

func (f *fileBackend) formatString() string {
	switch f.format {
	case fileFormatYAML:
		return "yaml"
	case fileFormatJSON:
		return "json"
	case fileFormatTOML:
		return "toml"
	default:
		return "unknown"
	}
}

func (f *fileBackend) Lookup(path string) (any, bool, error) {
	return lookupNested(f.values, strings.Split(path, "."))
}

// lookupNested navigates a nested map[string]any using case-insensitive key
// matching at each level.
func lookupNested(m map[string]any, parts []string) (any, bool, error) {
	if len(parts) == 0 || m == nil {
		return nil, false, nil
	}
	target := strings.ToLower(parts[0])
	for k, v := range m {
		if strings.ToLower(k) != target {
			continue
		}
		if len(parts) == 1 {
			return v, true, nil
		}
		nested, ok := v.(map[string]any)
		if !ok {
			return nil, false, nil
		}
		return lookupNested(nested, parts[1:])
	}
	return nil, false, nil
}

func parseFileFormat(s string) fileFormat {
	switch strings.ToLower(strings.TrimPrefix(s, ".")) {
	case "yaml", "yml":
		return fileFormatYAML
	case "json":
		return fileFormatJSON
	case "toml":
		return fileFormatTOML
	}
	return fileFormatUnknown
}

func unmarshalFileData(data []byte, format fileFormat) (map[string]any, error) {
	var m map[string]any
	switch format {
	case fileFormatYAML:
		if err := yaml.Unmarshal(data, &m); err != nil {
			return nil, err
		}
	case fileFormatJSON:
		if err := json.Unmarshal(data, &m); err != nil {
			return nil, err
		}
	case fileFormatTOML:
		if _, err := toml.Decode(string(data), &m); err != nil {
			return nil, err
		}
	}
	if m == nil {
		m = make(map[string]any)
	}
	return m, nil
}
