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
	"reflect"
	"strings"
	"testing"

	"github.com/suyono3484/confstruct"
)

func TestCheckFieldNames_noCollision(t *testing.T) {
	entries := []confstruct.FieldPath{
		{Path: "Database.Port", Chain: []reflect.StructField{field("Database"), field("Port")}},
		{Path: "Cache.Port", Chain: []reflect.StructField{field("Cache"), field("Port")}},
	}
	if err := checkFieldNames(entries); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCheckFieldNames_untaggedAndTaggedCollide(t *testing.T) {
	entries := []confstruct.FieldPath{
		{Path: "Database.Port", Chain: []reflect.StructField{field("Database"), field("Port")}},
		{Path: "Cache.Port", Chain: []reflect.StructField{field("Cache"), fieldWithTag("Port", "database-port")}},
	}
	err := checkFieldNames(entries)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	for _, want := range []string{"database-port", "Database.Port", "Cache.Port"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err.Error(), want)
		}
	}
}

func TestCheckFieldNames_separateCallsNoCollision(t *testing.T) {
	entriesA := []confstruct.FieldPath{
		{Path: "SvcA.WithKey", Chain: []reflect.StructField{field("SvcA"), fieldWithTag("WithKey", "with-key")}},
	}
	entriesB := []confstruct.FieldPath{
		{Path: "SvcB.WithKey", Chain: []reflect.StructField{field("SvcB"), fieldWithTag("WithKey", "with-key")}},
	}
	if err := checkFieldNames(entriesA); err != nil {
		t.Fatalf("unexpected error for entriesA: %v", err)
	}
	if err := checkFieldNames(entriesB); err != nil {
		t.Fatalf("unexpected error for entriesB: %v", err)
	}
}

func TestCheckFieldNames_multipleCollisionGroups(t *testing.T) {
	entries := []confstruct.FieldPath{
		{Path: "SvcA.WithKey", Chain: []reflect.StructField{field("SvcA"), fieldWithTag("WithKey", "with-key")}},
		{Path: "SvcA.AltKey", Chain: []reflect.StructField{field("SvcA"), fieldWithTag("AltKey", "with-key")}},
		{Path: "Database.Host", Chain: []reflect.StructField{field("Database"), fieldWithTag("Host", "shared-name")}},
		{Path: "Cache.Host", Chain: []reflect.StructField{field("Cache"), fieldWithTag("Host", "shared-name")}},
	}
	err := checkFieldNames(entries)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	for _, want := range []string{"with-key", "shared-name", "SvcA.WithKey", "SvcA.AltKey", "Database.Host", "Cache.Host"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err.Error(), want)
		}
	}
}

func TestCheckFieldNames_invalidTagPlusDuplicate(t *testing.T) {
	entries := []confstruct.FieldPath{
		{Path: "Bad.Field", Chain: []reflect.StructField{field("Bad"), fieldWithTag("Field", "--not-valid")}},
		{Path: "SvcA.WithKey", Chain: []reflect.StructField{field("SvcA"), fieldWithTag("WithKey", "with-key")}},
		{Path: "SvcA.AltKey", Chain: []reflect.StructField{field("SvcA"), fieldWithTag("AltKey", "with-key")}},
	}
	err := checkFieldNames(entries)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	for _, want := range []string{"name-check", "Bad.Field", "invalid cs.pflag tag", "duplicate flag name", "with-key"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err.Error(), want)
		}
	}
}

func TestCheckFieldNames_duplicateErrorText_twoFields(t *testing.T) {
	entries := []confstruct.FieldPath{
		{Path: "SvcA.WithKey", Chain: []reflect.StructField{field("SvcA"), fieldWithTag("WithKey", "with-key")}},
		{Path: "SvcA.AltKey", Chain: []reflect.StructField{field("SvcA"), fieldWithTag("AltKey", "with-key")}},
	}
	err := checkFieldNames(entries)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	want := `confstruct: backend "pflag": duplicate flag name "with-key": fields "SvcA.AltKey" and "SvcA.WithKey" resolve to it`
	if err.Error() != want {
		t.Errorf("err = %q, want %q", err.Error(), want)
	}
}

func TestCheckFieldNames_duplicateErrorText_threeFields(t *testing.T) {
	entries := []confstruct.FieldPath{
		{Path: "C", Chain: []reflect.StructField{fieldWithTag("C", "shared")}},
		{Path: "A", Chain: []reflect.StructField{fieldWithTag("A", "shared")}},
		{Path: "B", Chain: []reflect.StructField{fieldWithTag("B", "shared")}},
	}
	err := checkFieldNames(entries)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	want := `confstruct: backend "pflag": duplicate flag name "shared": fields "A", "B", and "C" resolve to it`
	if err.Error() != want {
		t.Errorf("err = %q, want %q", err.Error(), want)
	}
}

func TestQuotedJoin(t *testing.T) {
	cases := []struct {
		name  string
		paths []string
		want  string
	}{
		{"one", []string{"A"}, `"A"`},
		{"two", []string{"A", "B"}, `"A" and "B"`},
		{"three", []string{"A", "B", "C"}, `"A", "B", and "C"`},
		{"four", []string{"A", "B", "C", "D"}, `"A", "B", "C", and "D"`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := quotedJoin(c.paths)
			if got != c.want {
				t.Errorf("quotedJoin(%v) = %q, want %q", c.paths, got, c.want)
			}
		})
	}
}
