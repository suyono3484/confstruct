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
	"testing"
)

func field(name string) reflect.StructField {
	return reflect.StructField{Name: name}
}

func fieldWithTag(name, tag string) reflect.StructField {
	return reflect.StructField{Name: name, Tag: reflect.StructTag(`cs.pflag:"` + tag + `"`)}
}

func TestDerivedPFlagName(t *testing.T) {
	cases := []struct {
		path   string
		fields []reflect.StructField
		want   string
	}{
		{"Port", []reflect.StructField{field("Port")}, "port"},
		{"ListenAddr", []reflect.StructField{field("ListenAddr")}, "listen-addr"},
		{"TLS", []reflect.StructField{field("TLS")}, "tls"},
		{"TLSConfig", []reflect.StructField{field("TLSConfig")}, "tls-config"},
		{"HTTPServerPort", []reflect.StructField{field("HTTPServerPort")}, "http-server-port"},
		{"HTTP2Server", []reflect.StructField{field("HTTP2Server")}, "http2-server"},
		{"Server2Port", []reflect.StructField{field("Server2Port")}, "server2-port"},
		{"IPv6Address", []reflect.StructField{field("IPv6Address")}, "i-pv6-address"},
		{
			"Database.HTTP2ServerPort",
			[]reflect.StructField{field("Database"), field("HTTP2ServerPort")},
			"database-http2-server-port",
		},
	}

	for _, c := range cases {
		t.Run(c.path, func(t *testing.T) {
			got, err := derivedPFlagName(c.fields)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != c.want {
				t.Errorf("derivedPFlagName(%q) = %q, want %q", c.path, got, c.want)
			}
		})
	}
}

func TestDerivedPFlagName_invalidCharacter(t *testing.T) {
	cases := []struct {
		name  string
		field string
	}{
		{"underscore", "DB_Host"},
		{"non-ASCII letter", "Café"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := derivedPFlagName([]reflect.StructField{field(c.field)})
			if err == nil {
				t.Fatalf("expected error for field name %q, got nil", c.field)
			}
		})
	}
}

func TestDerivedPFlagNameFromPath(t *testing.T) {
	cases := []struct {
		path string
		want string
	}{
		{"Port", "port"},
		{"ListenAddr", "listen-addr"},
		{"TLS", "tls"},
		{"TLSConfig", "tls-config"},
		{"HTTPServerPort", "http-server-port"},
		{"HTTP2Server", "http2-server"},
		{"Server2Port", "server2-port"},
		{"IPv6Address", "i-pv6-address"},
		{"Database.HTTP2ServerPort", "database-http2-server-port"},
		{"", ""},
	}

	for _, c := range cases {
		t.Run(c.path, func(t *testing.T) {
			got, err := derivedPFlagNameFromPath(c.path)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != c.want {
				t.Errorf("derivedPFlagNameFromPath(%q) = %q, want %q", c.path, got, c.want)
			}
		})
	}
}

func TestDerivedPFlagNameFromPath_invalidCharacter(t *testing.T) {
	cases := []struct {
		name string
		path string
	}{
		{"underscore", "DB_Host"},
		{"non-ASCII letter", "Café"},
		{"invalid segment nested", "Database.DB_Host"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := derivedPFlagNameFromPath(c.path)
			if err == nil {
				t.Fatalf("expected error for path %q, got nil", c.path)
			}
		})
	}
}

func TestPFlagName_derivedFallbackInvalidCharacter(t *testing.T) {
	_, err := pflagName("DB_Host", []reflect.StructField{field("DB_Host")})
	if err == nil {
		t.Fatal("expected error for field name containing an underscore, got nil")
	}
}

func TestPFlagName_derivedFallback(t *testing.T) {
	name, err := pflagName("ListenAddr", []reflect.StructField{field("ListenAddr")})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "listen-addr" {
		t.Errorf("name = %q, want %q", name, "listen-addr")
	}
}

func TestPFlagName_validTag(t *testing.T) {
	cases := []struct {
		name string
		tag  string
		want string
	}{
		{"plain", "db-port", "db-port"},
		{"outer whitespace trimmed", "  db-port  ", "db-port"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := pflagName("Database.Port", []reflect.StructField{fieldWithTag("Port", c.tag)})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != c.want {
				t.Errorf("name = %q, want %q", got, c.want)
			}
		})
	}
}

func TestPFlagName_invalidTag(t *testing.T) {
	cases := []struct {
		name string
		tag  string
	}{
		{"empty", ""},
		{"whitespace only", "   "},
		{"CLI dash prefix", "--db-port"},
		{"internal space", "db host"},
		{"underscore", "db_host"},
		{"uppercase", "DBPort"},
		{"double hyphen", "db--port"},
		{"leading hyphen", "-db-port"},
		{"trailing hyphen", "db-port-"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := pflagName("Database.Port", []reflect.StructField{fieldWithTag("Port", c.tag)})
			if err == nil {
				t.Fatalf("expected error for tag %q, got nil", c.tag)
			}
		})
	}
}

func TestPFlagName_invalidTagErrorText(t *testing.T) {
	_, err := pflagName("Database.Port", []reflect.StructField{fieldWithTag("Port", "--db-port")})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	want := `invalid cs.pflag tag "--db-port": expected a lowercase kebab-case long flag name`
	if err.Error() != want {
		t.Errorf("err = %q, want %q", err.Error(), want)
	}
}

func TestPFlagName_degenerateEmptyInput(t *testing.T) {
	name, err := pflagName("", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "" {
		t.Errorf("name = %q, want empty string", name)
	}
}
