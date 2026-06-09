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
)

// Primitive returns a Backend backed by the given key-value map.
// Keys must be dot-separated field paths matching the config struct layout.
// Register it first via AddLayer to act as a defaults layer; register it last
// to act as an override layer.
func Primitive(values map[string]any) Backend {
	m := make(map[string]any, len(values))
	maps.Copy(m, values)
	return &primitiveBackend{values: m}
}

type primitiveBackend struct {
	values map[string]any
}

// PrimitiveBackendName is the Name() identifier for a [Primitive] backend.
// Use it to compare against [ResolveHook] arguments without hard-coding the string.
const PrimitiveBackendName = "primitive"

func (p *primitiveBackend) Lookup(path string) (any, bool, error) {
	v, ok := p.values[path]
	return v, ok, nil
}

func (p *primitiveBackend) Name() string { return PrimitiveBackendName }

func (p *primitiveBackend) Describe() string {
	return fmt.Sprintf("%d keys", len(p.values))
}
