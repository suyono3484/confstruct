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
	"fmt"
	"maps"
	"reflect"
)

// Map returns a Backend backed by the given key-value map.
// Keys must be dot-separated field paths matching the config struct layout.
// Register it first via AddLayer to act as a defaults layer; register it last
// to act as an override layer.
func Map(values map[string]any) Backend {
	m := make(map[string]any, len(values))
	maps.Copy(m, values)
	return &mapBackend{values: m}
}

// MapFromTags returns a Backend that reads values from struct tag annotations on
// entry fields. The tag key is "cs." followed by suffix — for example, suffix
// "default" reads tags of the form `cs.default:"value"`. Values are always strings;
// confstruct coerces them into the target entry type using the same rules as the
// Env backend (numeric parsing, boolean parsing, and so on).
//
// MapFromTags panics if cfgStruct is not a pointer to a struct.
//
// Tag parsing produces no runtime signal on a missing or misspelled tag — a
// typo silently yields an unset entry. Write a unit test that registers only the
// MapFromTags backend and asserts IsSet() for every field that should be covered.
func MapFromTags(cfgStruct any, suffix string) Backend {
	rv := reflect.ValueOf(cfgStruct)
	if rv.Kind() != reflect.Pointer || rv.Elem().Kind() != reflect.Struct {
		panic("confstruct: MapFromTags requires a pointer to a struct")
	}
	tagKey := "cs." + suffix
	values := make(map[string]any)
	collectTagValues(rv.Elem().Type(), tagKey, "", values)
	return &mapBackend{values: values}
}

func collectTagValues(st reflect.Type, tagKey, prefix string, out map[string]any) {
	for f := range st.Fields() {
		if f.Type == metaType {
			continue
		}
		key := f.Name
		if prefix != "" {
			key = prefix + "." + f.Name
		}
		if reflect.PointerTo(f.Type).Implements(layerManagerType) {
			if v, ok := f.Tag.Lookup(tagKey); ok {
				out[key] = v
			}
			continue
		}
		if f.Type.Kind() == reflect.Struct {
			collectTagValues(f.Type, tagKey, key, out)
		}
	}
}

type mapBackend struct {
	values map[string]any
}

// MapBackendName is the Name() identifier for a [Map] or [MapFromTags] backend.
// Use it to compare against [ResolveHook] arguments without hard-coding the string.
const MapBackendName = "map"

func (p *mapBackend) Lookup(path string) (any, bool, error) {
	v, ok := p.values[path]
	return v, ok, nil
}

func (p *mapBackend) Name() string { return MapBackendName }

func (p *mapBackend) Describe() string {
	return fmt.Sprintf("%d keys", len(p.values))
}
