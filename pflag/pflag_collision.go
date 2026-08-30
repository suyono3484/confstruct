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

package pflag

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/suyono3484/confstruct"
)

// PFlagBackendName is the Name() identifier for a PFlag backend.
const PFlagBackendName = "pflag"

// pflagBackendErr mirrors confstruct's unexported backendErr, which this
// package cannot call directly across the package boundary (embedding a
// seal only solves interface-method satisfaction, not access to an
// unexported function). Keeps every hand-built pflag error in the same
// "confstruct: backend %q <action> %q: <cause>" shape. Takes no backend
// receiver: pflagBackend.Name() always returns the fixed PFlagBackendName
// constant, so there's nothing to look up on an instance.
func pflagBackendErr(action, key string, err error) error {
	return fmt.Errorf("confstruct: backend %q %s %q: %w", PFlagBackendName, action, key, err)
}

// checkFieldNames implements confstruct's name-collision hook for the pflag
// backend: two entry fields resolving to the same pflag long name within
// one Populate call is an error, checked structurally before any
// flags.Lookup. It ships as a standalone function, not a pflagBackend
// method, because pflagBackend doesn't exist until pflag's own backend
// core lands -- see
// docs/pflag-plan-phase-2-duplicate-detection.md#24-field-name-collision-detection-checkfieldnames.
func checkFieldNames(entries []confstruct.FieldPath) error {
	byName := make(map[string][]string, len(entries)) // resolved name -> field paths
	var errs []error
	for _, e := range entries {
		name, err := pflagName(e.Path, e.Chain)
		if err != nil {
			errs = append(errs, pflagBackendErr("name-check", e.Path, err))
			continue
		}
		byName[name] = append(byName[name], e.Path)
	}

	names := make([]string, 0, len(byName))
	for name := range byName {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		paths := byName[name]
		if len(paths) < 2 {
			continue
		}
		sort.Strings(paths)
		errs = append(errs, fmt.Errorf("confstruct: backend %q: duplicate flag name %q: fields %s resolve to it",
			PFlagBackendName, name, quotedJoin(paths)))
	}

	if len(errs) == 0 {
		return nil
	}
	return errors.Join(errs...)
}

// quotedJoin renders paths (already sorted by the caller) as a natural,
// comma-and-joined, quoted list: ["A","B"] -> `"A" and "B"`,
// ["A","B","C"] -> `"A", "B", and "C"`.
func quotedJoin(paths []string) string {
	switch len(paths) {
	case 1:
		return fmt.Sprintf("%q", paths[0])
	case 2:
		return fmt.Sprintf("%q and %q", paths[0], paths[1])
	default:
		quoted := make([]string, len(paths))
		for i, p := range paths {
			quoted[i] = fmt.Sprintf("%q", p)
		}
		last := len(quoted) - 1
		return strings.Join(quoted[:last], ", ") + ", and " + quoted[last]
	}
}
