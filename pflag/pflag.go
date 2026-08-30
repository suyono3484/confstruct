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

// Package pflag is an optional [github.com/suyono3484/confstruct] backend
// that reads explicitly-provided command-line flags from
// [github.com/spf13/pflag]. It lives in its own package, not in the root
// confstruct package, because both it and spf13/pflag are named "pflag" --
// any file importing both needs an import alias for one of them. See
// https://pkg.go.dev/github.com/suyono3484/confstruct's docs/pflag-integration.md
// for the full design rationale.
//
// # Quickstart
//
//	import (
//	    "github.com/suyono3484/confstruct"
//	    cspflag "github.com/suyono3484/confstruct/pflag"
//	    "github.com/spf13/pflag"
//	)
//
//	flags := pflag.NewFlagSet("myapp", pflag.ExitOnError)
//	flags.String("listen-addr", "", "address to listen on")
//	flags.Parse(os.Args[1:])
//
//	var cfg Config
//	cfg.AddLayer(cspflag.PFlag(flags)) // explicitly supplied CLI flags win
//	confstruct.Populate(ctx, &cfg)
//
// # Presence, not defaults
//
// A flag whose Changed is false is treated as absent, not as a value: the
// entry falls through to the next lower-precedence layer instead of picking
// up the flag's declared default. This is why PFlag should normally be
// added as the highest-precedence layer, and why the caller must call
// flags.Parse before Populate -- lookup happens during population, so
// calling Populate first would see every flag as unchanged.
//
// # Flag name derivation
//
// By default, the long flag name is derived from the complete field path:
// split Go identifiers at word boundaries, lowercase, join words within a
// segment with "-", then join segments with "-" ("Database.HTTP2ServerPort"
// -> "database-http2-server-port"). An entry field may override the
// derived name with a `cs.pflag:"name"` tag; the tag must be lowercase
// kebab-case or Populate fails. This applies to every entry field, tagged
// or not -- the tag is an escape hatch for names the derivation gets
// wrong (e.g. "IPv6Address" would derive to "i-pv6-address"), not a gate on
// which fields participate.
package pflag

import (
	"reflect"

	spfpflag "github.com/spf13/pflag"
	"github.com/suyono3484/confstruct"
)

type pflagBackend struct {
	confstruct.FieldLookupSeal
	confstruct.NameCollisionSeal
	flags *spfpflag.FlagSet
}

// PFlag returns a Backend that reads explicitly-provided command-line flags
// from an already-parsed *pflag.FlagSet. It owns no parsing and performs no
// writes: the application defines and parses its flags, adds the resulting
// backend as its highest-precedence layer, and then calls Populate.
//
// Only a flag with Changed == true is considered present; an unprovided
// flag's declared default is not a configuration value and the entry falls
// through to the next lower-precedence layer. See
// docs/pflag-integration.md for the full design rationale.
func PFlag(flags *spfpflag.FlagSet) confstruct.Backend {
	b := &pflagBackend{flags: flags}
	b.FieldLookupSeal = confstruct.NewFieldLookupSeal(b)
	b.NameCollisionSeal = confstruct.NewNameCollisionSeal(b)
	return b
}

func (b *pflagBackend) Name() string { return PFlagBackendName }

func (b *pflagBackend) Describe() string {
	if b.flags == spfpflag.CommandLine {
		return "command-line"
	}
	return b.flags.Name()
}

// Lookup derives a flag name straight from path, for direct Backend use
// outside of Populate/Meta -- pflag's plain key-value mode, with no
// reflect.StructField chain and therefore no cs.pflag tag to honor.
// Populate itself always calls LookupFieldValue instead, since only that
// path has the struct-field chain a cs.pflag tag lives on.
func (b *pflagBackend) Lookup(path string) (any, bool, error) {
	name, err := derivedPFlagNameFromPath(path)
	if err != nil {
		return nil, false, err
	}
	return b.lookupName(name)
}

// LookupFieldValue satisfies confstruct.FieldLookuper; FieldLookupSeal
// forwards fieldAwareBackend.lookupField calls here. The returned error is
// deliberately unwrapped -- walkAndInject wraps it with backendErr at its
// own call site, exactly as it does for Env/File.
func (b *pflagBackend) LookupFieldValue(path string, fields []reflect.StructField) (any, bool, error) {
	name, err := pflagName(path, fields)
	if err != nil {
		return nil, false, err
	}
	return b.lookupName(name)
}

func (b *pflagBackend) lookupName(name string) (any, bool, error) {
	flag := b.flags.Lookup(name)
	if flag == nil || !flag.Changed {
		return nil, false, nil
	}
	return flag.Value.String(), true, nil
}

// CheckFieldNames satisfies confstruct.NameCollisionChecker; NameCollisionSeal
// forwards nameCollisionBackend.checkNames calls here. checkFieldNames is the
// standalone function Phase 2 already implemented and tested in
// pflag/pflag_collision.go -- see
// docs/pflag-plan-phase-2-duplicate-detection.md#24-field-name-collision-detection-checkfieldnames.
// This method has no logic of its own; it exists only because Phase 2 could
// not take a *pflagBackend receiver before this type existed.
func (b *pflagBackend) CheckFieldNames(entries []confstruct.FieldPath) error {
	return checkFieldNames(entries)
}
