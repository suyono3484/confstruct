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
	"reflect"
	"strings"

	"github.com/BurntSushi/toml"
	"gopkg.in/yaml.v3"
)

const fileSegmentAliasTag = "cs.file.segment-alias"

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
// database.host or DATABASE.HOST in the file. During Populate, any struct field
// tagged with `cs.file.segment-alias:"..."` may also be matched by that alias
// segment, so Database.Host can resolve from db.host. If both the canonical and
// aliased segment are present at the same level, Populate fails with a conflict
// error. The file is read once at construction time.
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

// FileBackendName is the Name() identifier for a [File] backend.
// Use it to compare against [ResolveHook] arguments without hard-coding the string.
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

func (f *fileBackend) lookupField(path string, fields []reflect.StructField) (any, bool, error) {
	if len(fields) == 0 {
		return f.Lookup(path)
	}
	return f.lookupFieldRecursive(f.values, fields, 0, "")
}

// lookupNested navigates a nested map[string]any using case-insensitive key
// matching at each level.
func lookupNested(m map[string]any, parts []string) (any, bool, error) {
	if len(parts) == 0 || m == nil {
		return nil, false, nil
	}
	v, ok, err := lookupCaseInsensitive(m, parts[0])
	if err != nil {
		return nil, false, err
	}
	if !ok {
		return nil, false, nil
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

func (f *fileBackend) lookupFieldRecursive(m map[string]any, fields []reflect.StructField, index int, consumed string) (any, bool, error) {
	if len(fields) == 0 || m == nil || index >= len(fields) {
		return nil, false, nil
	}
	field := fields[index]
	segmentPath := field.Name
	if consumed != "" {
		segmentPath = consumed + "." + field.Name
	}
	alias, hasAlias := fileSegmentAlias(field)
	canonicalValue, canonicalOK, err := lookupCaseInsensitive(m, field.Name)
	if err != nil {
		return nil, false, err
	}
	aliasValue, aliasOK := any(nil), false
	if hasAlias {
		aliasValue, aliasOK, err = lookupCaseInsensitive(m, alias)
		if err != nil {
			return nil, false, err
		}
	}
	if hasAlias && canonicalOK && aliasOK {
		return nil, false, fmt.Errorf("conflicting file keys for %q: both %q and alias %q are present", segmentPath, field.Name, alias)
	}
	v, ok := canonicalValue, canonicalOK
	if aliasOK {
		v, ok = aliasValue, true
	}
	if !ok {
		return nil, false, nil
	}
	if index == len(fields)-1 {
		return v, true, nil
	}
	nested, ok := v.(map[string]any)
	if !ok {
		return nil, false, nil
	}
	return f.lookupFieldRecursive(nested, fields, index+1, segmentPath)
}

func lookupCaseInsensitive(m map[string]any, target string) (any, bool, error) {
	lowerTarget := strings.ToLower(target)
	var matchKey string
	var matchValue any
	found := false
	for k, v := range m {
		if strings.ToLower(k) != lowerTarget {
			continue
		}
		if found {
			return nil, false, fmt.Errorf("confstruct: ambiguous file keys %q and %q both match %q case-insensitively", matchKey, k, target)
		}
		matchKey = k
		matchValue = v
		found = true
	}
	if !found {
		return nil, false, nil
	}
	return matchValue, true, nil
}

func fileSegmentAlias(field reflect.StructField) (string, bool) {
	alias, ok := field.Tag.Lookup(fileSegmentAliasTag)
	if !ok {
		return "", false
	}
	alias = strings.TrimSpace(alias)
	if alias == "" || strings.EqualFold(alias, field.Name) {
		return "", false
	}
	return alias, true
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
