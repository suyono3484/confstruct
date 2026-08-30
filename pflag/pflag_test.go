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
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	spfpflag "github.com/spf13/pflag"
	"github.com/suyono3484/confstruct"
)

func TestPFlag_ChangedFlagOverridesLowerLayers(t *testing.T) {
	type cfgT struct {
		confstruct.Meta
		Name confstruct.StringEntry
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("name: from-file\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fb, err := confstruct.File(path)
	if err != nil {
		t.Fatal(err)
	}

	t.Setenv("PFLAGTEST_NAME", "from-env")
	eb, err := confstruct.Env(confstruct.WithPrefix("PFLAGTEST"))
	if err != nil {
		t.Fatal(err)
	}

	flags := spfpflag.NewFlagSet("test", spfpflag.ContinueOnError)
	flags.String("name", "flag-default", "")
	if err := flags.Parse([]string{"--name=from-flag"}); err != nil {
		t.Fatal(err)
	}

	var cfg cfgT
	cfg.AddLayer(confstruct.Map(map[string]any{"Name": "from-map"}))
	cfg.AddLayer(fb)
	cfg.AddLayer(eb)
	cfg.AddLayer(PFlag(flags))

	if err := confstruct.Populate(context.Background(), &cfg); err != nil {
		t.Fatal(err)
	}
	if got := cfg.Name.Value(); got != "from-flag" {
		t.Errorf("Name = %q, want %q", got, "from-flag")
	}
	if got := cfg.Name.SourceName(); got != PFlagBackendName {
		t.Errorf("SourceName = %q, want %q", got, PFlagBackendName)
	}
}

func TestPFlag_UnchangedFlagFallsThrough(t *testing.T) {
	type cfgT struct {
		confstruct.Meta
		Verbose confstruct.BoolEntry
	}
	flags := spfpflag.NewFlagSet("test", spfpflag.ContinueOnError)
	flags.Bool("verbose", true, "") // declared default true, never set via Parse
	if err := flags.Parse(nil); err != nil {
		t.Fatal(err)
	}

	var cfg cfgT
	cfg.AddLayer(confstruct.Map(map[string]any{"Verbose": false}))
	cfg.AddLayer(PFlag(flags))
	if err := confstruct.Populate(context.Background(), &cfg); err != nil {
		t.Fatal(err)
	}
	if got := cfg.Verbose.Value(); got != false {
		t.Errorf("Verbose = %v, want false (map layer; flag unchanged despite differing default)", got)
	}
	if got := cfg.Verbose.SourceName(); got != confstruct.MapBackendName {
		t.Errorf("SourceName = %q, want %q", got, confstruct.MapBackendName)
	}
}

func TestPFlag_ExplicitZeroValuesWinOverLowerLayer(t *testing.T) {
	type cfgT struct {
		confstruct.Meta
		Verbose confstruct.BoolEntry
		Port    confstruct.IntEntry
		Name    confstruct.StringEntry
	}
	flags := spfpflag.NewFlagSet("test", spfpflag.ContinueOnError)
	flags.Bool("verbose", true, "")
	flags.Int("port", 9999, "")
	flags.String("name", "flag-default", "")
	if err := flags.Parse([]string{"--verbose=false", "--port=0", "--name="}); err != nil {
		t.Fatal(err)
	}

	var cfg cfgT
	cfg.AddLayer(confstruct.Map(map[string]any{"Verbose": true, "Port": 42, "Name": "from-map"}))
	cfg.AddLayer(PFlag(flags))
	if err := confstruct.Populate(context.Background(), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Verbose.Value() != false {
		t.Errorf("Verbose = %v, want false", cfg.Verbose.Value())
	}
	if cfg.Port.Value() != 0 {
		t.Errorf("Port = %d, want 0", cfg.Port.Value())
	}
	if cfg.Name.Value() != "" {
		t.Errorf("Name = %q, want empty string", cfg.Name.Value())
	}
	if !cfg.Name.IsSet() {
		t.Error("Name should be IsSet=true even though its value is the empty string")
	}
}

func TestPFlag_DerivedAndTaggedNamesViaPopulate(t *testing.T) {
	type cfgT struct {
		confstruct.Meta
		ListenAddr confstruct.StringEntry
		Database   struct {
			HTTP2ServerPort confstruct.IntEntry
		}
		IPv6Address confstruct.StringEntry `cs.pflag:"ipv6-address"`
	}

	flags := spfpflag.NewFlagSet("test", spfpflag.ContinueOnError)
	flags.String("listen-addr", "", "")
	flags.Int("database-http2-server-port", 0, "")
	flags.String("ipv6-address", "", "")
	if err := flags.Parse([]string{
		"--listen-addr=0.0.0.0:8080",
		"--database-http2-server-port=9090",
		"--ipv6-address=::1",
	}); err != nil {
		t.Fatal(err)
	}

	var cfg cfgT
	cfg.AddLayer(PFlag(flags))
	if err := confstruct.Populate(context.Background(), &cfg); err != nil {
		t.Fatal(err)
	}

	if got := cfg.ListenAddr.Value(); got != "0.0.0.0:8080" {
		t.Errorf("ListenAddr = %q", got)
	}
	if got := cfg.Database.HTTP2ServerPort.Value(); got != 9090 {
		t.Errorf("Database.HTTP2ServerPort = %d", got)
	}
	if got := cfg.IPv6Address.Value(); got != "::1" {
		t.Errorf("IPv6Address = %q", got)
	}
}

func TestPFlag_LookupDirect(t *testing.T) {
	flags := spfpflag.NewFlagSet("test", spfpflag.ContinueOnError)
	flags.String("database-port", "", "")
	if err := flags.Parse([]string{"--database-port=5432"}); err != nil {
		t.Fatal(err)
	}

	b := PFlag(flags)

	v, ok, err := b.Lookup("Database.Port")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok || v != "5432" {
		t.Errorf("Lookup(%q) = (%v, %v), want (\"5432\", true)", "Database.Port", v, ok)
	}

	_, ok, err = b.Lookup("Nonexistent.Path")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Error("Lookup for an unregistered flag should return ok=false")
	}
}

// TestPFlag_LookupDirectIgnoresTag confirms that direct Backend.Lookup use
// -- which has no reflect.StructField chain -- always uses the derived
// name, never a cs.pflag tag. A struct field populated through Populate
// with a tag would resolve to "ipv6-address"; direct Lookup use of the
// same logical path has no field to read a tag from, so it resolves to
// the mechanically-derived "i-pv6-address" instead.
func TestPFlag_LookupDirectIgnoresTag(t *testing.T) {
	flags := spfpflag.NewFlagSet("test", spfpflag.ContinueOnError)
	flags.String("ipv6-address", "", "")  // the tagged name a struct field would use via Populate
	flags.String("i-pv6-address", "", "") // the bare-derived name Lookup uses directly
	if err := flags.Parse([]string{"--i-pv6-address=::1", "--ipv6-address=should-not-be-seen"}); err != nil {
		t.Fatal(err)
	}
	b := PFlag(flags)
	v, ok, err := b.Lookup("IPv6Address")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok || v != "::1" {
		t.Errorf("Lookup(%q) = (%v, %v), want (\"::1\", true) -- direct Lookup must use the derived name, not a tag", "IPv6Address", v, ok)
	}
}

func TestPFlag_MissingFlagFallsThrough(t *testing.T) {
	type cfgT struct {
		confstruct.Meta
		Name confstruct.StringEntry
	}
	flags := spfpflag.NewFlagSet("test", spfpflag.ContinueOnError) // "name" never registered
	var cfg cfgT
	cfg.AddLayer(confstruct.Map(map[string]any{"Name": "from-map"}))
	cfg.AddLayer(PFlag(flags))
	if err := confstruct.Populate(context.Background(), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Name.Value() != "from-map" {
		t.Errorf("Name = %q, want %q", cfg.Name.Value(), "from-map")
	}
}

func TestPFlag_AllEntryTypesCoerce(t *testing.T) {
	type cfgT struct {
		confstruct.Meta
		StringField  confstruct.StringEntry
		BoolField    confstruct.BoolEntry
		IntField     confstruct.IntEntry
		Int8Field    confstruct.Int8Entry
		Int16Field   confstruct.Int16Entry
		Int32Field   confstruct.Int32Entry
		Int64Field   confstruct.Int64Entry
		UintField    confstruct.UintEntry
		Uint8Field   confstruct.Uint8Entry
		Uint16Field  confstruct.Uint16Entry
		Uint32Field  confstruct.Uint32Entry
		Uint64Field  confstruct.Uint64Entry
		Float32Field confstruct.Float32Entry
		Float64Field confstruct.Float64Entry
	}

	flags := spfpflag.NewFlagSet("test", spfpflag.ContinueOnError)
	flags.String("string-field", "", "")
	flags.Bool("bool-field", false, "")
	flags.Int("int-field", 0, "")
	flags.Int8("int8-field", 0, "")
	flags.Int16("int16-field", 0, "")
	flags.Int32("int32-field", 0, "")
	flags.Int64("int64-field", 0, "")
	flags.Uint("uint-field", 0, "")
	flags.Uint8("uint8-field", 0, "")
	flags.Uint16("uint16-field", 0, "")
	flags.Uint32("uint32-field", 0, "")
	flags.Uint64("uint64-field", 0, "")
	flags.Float32("float32-field", 0, "")
	flags.Float64("float64-field", 0, "")

	args := []string{
		"--string-field=hello",
		"--bool-field=true",
		"--int-field=-42",
		"--int8-field=-8",
		"--int16-field=-16",
		"--int32-field=-32",
		"--int64-field=-64",
		"--uint-field=42",
		"--uint8-field=8",
		"--uint16-field=16",
		"--uint32-field=32",
		"--uint64-field=64",
		"--float32-field=1.5",
		"--float64-field=2.5",
	}
	if err := flags.Parse(args); err != nil {
		t.Fatal(err)
	}

	var cfg cfgT
	cfg.AddLayer(PFlag(flags))
	if err := confstruct.Populate(context.Background(), &cfg); err != nil {
		t.Fatal(err)
	}

	if got := cfg.StringField.Value(); got != "hello" {
		t.Errorf("StringField = %q", got)
	}
	if got := cfg.BoolField.Value(); got != true {
		t.Errorf("BoolField = %v", got)
	}
	if got := cfg.IntField.Value(); got != -42 {
		t.Errorf("IntField = %d", got)
	}
	if got := cfg.Int8Field.Value(); got != -8 {
		t.Errorf("Int8Field = %d", got)
	}
	if got := cfg.Int16Field.Value(); got != -16 {
		t.Errorf("Int16Field = %d", got)
	}
	if got := cfg.Int32Field.Value(); got != -32 {
		t.Errorf("Int32Field = %d", got)
	}
	if got := cfg.Int64Field.Value(); got != -64 {
		t.Errorf("Int64Field = %d", got)
	}
	if got := cfg.UintField.Value(); got != 42 {
		t.Errorf("UintField = %d", got)
	}
	if got := cfg.Uint8Field.Value(); got != 8 {
		t.Errorf("Uint8Field = %d", got)
	}
	if got := cfg.Uint16Field.Value(); got != 16 {
		t.Errorf("Uint16Field = %d", got)
	}
	if got := cfg.Uint32Field.Value(); got != 32 {
		t.Errorf("Uint32Field = %d", got)
	}
	if got := cfg.Uint64Field.Value(); got != 64 {
		t.Errorf("Uint64Field = %d", got)
	}
	if got := cfg.Float32Field.Value(); got != 1.5 {
		t.Errorf("Float32Field = %v", got)
	}
	if got := cfg.Float64Field.Value(); got != 2.5 {
		t.Errorf("Float64Field = %v", got)
	}
}

func TestPFlag_Int8BoundaryOverflowRejected(t *testing.T) {
	type cfgT struct {
		confstruct.Meta
		Small confstruct.Int8Entry
	}
	flags := spfpflag.NewFlagSet("test", spfpflag.ContinueOnError)
	// A plain int flag, not int8: pflag's own int8 flag type would reject
	// an out-of-range value at Parse time, before confstruct ever sees it.
	// Using int here lets the out-of-range value reach confstruct's own
	// coercion, exercising the same overflow-rejection path
	// TestPopulate_numericOverflowRejected already covers for other backends.
	flags.Int("small", 0, "")
	if err := flags.Parse([]string{"--small=300"}); err != nil {
		t.Fatal(err)
	}

	var cfg cfgT
	cfg.AddLayer(PFlag(flags))
	if err := confstruct.Populate(context.Background(), &cfg); err == nil {
		t.Fatal("expected Populate error for int8 overflow, got nil")
	}
	if cfg.Small.IsSet() {
		t.Error("Small should not be set after overflow")
	}
}

func TestPFlag_IncompatibleStringRejected(t *testing.T) {
	type cfgT struct {
		confstruct.Meta
		Port confstruct.IntEntry
	}
	flags := spfpflag.NewFlagSet("test", spfpflag.ContinueOnError)
	flags.String("port", "", "")
	if err := flags.Parse([]string{"--port=not-a-number"}); err != nil {
		t.Fatal(err)
	}

	var cfg cfgT
	cfg.AddLayer(PFlag(flags))
	if err := confstruct.Populate(context.Background(), &cfg); err == nil {
		t.Fatal("expected Populate error for incompatible string, got nil")
	}
	if cfg.Port.IsSet() {
		t.Error("Port should not be set after a coercion failure")
	}
}

func TestPFlag_SourceNameDescAndOnResolve(t *testing.T) {
	type cfgT struct {
		confstruct.Meta
		Name confstruct.StringEntry
	}
	flags := spfpflag.NewFlagSet("myapp", spfpflag.ContinueOnError)
	flags.String("name", "", "")
	if err := flags.Parse([]string{"--name=from-flag"}); err != nil {
		t.Fatal(err)
	}

	var cfg cfgT
	var resolveCount atomic.Int64
	var gotBackendName, gotBackendDesc string
	cfg.OnResolve(func(key string, value any, backendName, backendDesc string) {
		resolveCount.Add(1)
		gotBackendName = backendName
		gotBackendDesc = backendDesc
	})
	cfg.AddLayer(PFlag(flags))
	if err := confstruct.Populate(context.Background(), &cfg); err != nil {
		t.Fatal(err)
	}

	if got := cfg.Name.SourceName(); got != PFlagBackendName {
		t.Errorf("SourceName = %q, want %q", got, PFlagBackendName)
	}
	if got := cfg.Name.SourceDesc(); got != "myapp" {
		t.Errorf("SourceDesc = %q, want %q", got, "myapp")
	}
	if got := resolveCount.Load(); got != 1 {
		t.Errorf("OnResolve fired %d times, want 1", got)
	}
	if gotBackendName != PFlagBackendName {
		t.Errorf("OnResolve backendName = %q, want %q", gotBackendName, PFlagBackendName)
	}
	if gotBackendDesc != "myapp" {
		t.Errorf("OnResolve backendDesc = %q, want %q", gotBackendDesc, "myapp")
	}
}

func TestPFlag_DescribeCommandLine(t *testing.T) {
	// spfpflag.CommandLine is shared global state; register a flag name
	// unique to this test so it can't collide with anything else that
	// might touch the same FlagSet.
	spfpflag.CommandLine.String("pflag-test-describe-command-line-only", "", "")
	b := PFlag(spfpflag.CommandLine)
	if got := b.Describe(); got != "command-line" {
		t.Errorf("Describe() = %q, want %q", got, "command-line")
	}
}

func TestPFlag_AcceptedAsLowestLayer(t *testing.T) {
	type cfgT struct {
		confstruct.Meta
		Name confstruct.StringEntry
	}
	flags := spfpflag.NewFlagSet("test", spfpflag.ContinueOnError)
	flags.String("name", "", "")
	if err := flags.Parse([]string{"--name=only-layer"}); err != nil {
		t.Fatal(err)
	}

	var cfg cfgT
	cfg.AddLayer(PFlag(flags)) // sole layer => also the lowest layer
	if err := confstruct.Populate(context.Background(), &cfg); err != nil {
		t.Fatalf("PFlag as lowest (and only) layer should be accepted: %v", err)
	}
	if got := cfg.Name.Value(); got != "only-layer" {
		t.Errorf("Name = %q, want %q", got, "only-layer")
	}
}

// TestPFlag_DuplicateNameFiresEvenWithoutRegisteredFlags covers the one
// duplicate-name case explicitly deferred from Phase 2 -- see
// docs/pflag-plan-phase-2-duplicate-detection.md#25-tests--in-confstruct_testgo-and-pflagpflag_collision_testgo:
// the check is structural and fires even when the FlagSet doesn't define
// either colliding flag at all.
func TestPFlag_DuplicateNameFiresEvenWithoutRegisteredFlags(t *testing.T) {
	type cfgT struct {
		confstruct.Meta
		WithKey confstruct.StringEntry `cs.pflag:"with-key"`
		AltKey  confstruct.StringEntry `cs.pflag:"with-key"`
	}
	flags := spfpflag.NewFlagSet("test", spfpflag.ContinueOnError) // neither flag registered at all
	var cfg cfgT
	cfg.AddLayer(PFlag(flags))

	err := confstruct.Populate(context.Background(), &cfg)
	if err == nil {
		t.Fatal("expected Populate to fail due to duplicate flag name")
	}
	if !strings.Contains(err.Error(), "with-key") {
		t.Errorf("err = %q, want it to mention %q", err.Error(), "with-key")
	}
	if cfg.WithKey.IsSet() || cfg.AltKey.IsSet() {
		t.Error("no field should be set when Populate fails structurally")
	}
}
