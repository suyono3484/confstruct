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
	"bufio"
	"os"
	"reflect"
	"strings"
)

// EnvOption configures an Env backend.
type EnvOption func(*envBackend)

// WithPrefix sets a prefix prepended to every derived env var key (uppercased).
// Example: WithPrefix("APP") maps "Database.Host" → "APP_DATABASE_HOST".
func WithPrefix(prefix string) EnvOption {
	return func(e *envBackend) {
		e.prefix = strings.ToUpper(prefix)
	}
}

// WithDotEnv loads key-value pairs from a .env file. OS environment variables
// take precedence over values from the file. A missing file is silently ignored;
// a file that exists but cannot be parsed returns an error from Env.
func WithDotEnv(path string) EnvOption {
	return func(e *envBackend) {
		e.dotEnvPath = path
	}
}

// Env returns a Backend that reads from environment variables. Struct field
// paths are mapped to env var names by uppercasing and replacing dots with
// underscores (e.g., "Database.Host" → "DATABASE_HOST"). During Populate, an
// entry field tagged with `cs.env:"..."` uses that env var name instead of the
// derived path name; any prefix configured via WithPrefix is still prepended to
// the tag value. Tag-derived keys are uppercased exactly like auto-derived path
// keys before lookup, so `cs.env:"db_port"` and `cs.env:"DB_PORT"` are
// equivalent — case in the tag value is never significant. All values are
// returned as strings; confstruct parses them into the target field type.
func Env(opts ...EnvOption) (Backend, error) {
	b := &envBackend{}
	for _, o := range opts {
		o(b)
	}
	if b.dotEnvPath != "" {
		m, err := parseDotEnv(b.dotEnvPath)
		if err != nil {
			return nil, err
		}
		b.dotEnv = m
	}
	return b, nil
}

type envBackend struct {
	prefix     string
	dotEnvPath string
	dotEnv     map[string]string
}

const envNameTag = "cs.env"

// EnvBackendName is the Name() identifier for an [Env] backend.
// Use it to compare against [ResolveHook] arguments without hard-coding the string.
const EnvBackendName = "env"

func (e *envBackend) Name() string { return EnvBackendName }

func (e *envBackend) Describe() string {
	switch {
	case e.prefix != "" && e.dotEnvPath != "":
		return "prefix=" + e.prefix + ", dotenv=" + e.dotEnvPath
	case e.prefix != "":
		return "prefix=" + e.prefix
	case e.dotEnvPath != "":
		return "dotenv=" + e.dotEnvPath
	default:
		return ""
	}
}

func (e *envBackend) Lookup(path string) (any, bool, error) {
	key := e.pathToKey(path)
	return e.lookupKey(key)
}

func (e *envBackend) lookupField(path string, fields []reflect.StructField) (any, bool, error) {
	key := e.pathToKey(path)
	if name, ok := envName(fields); ok {
		key = e.prefixKey(name)
	}
	return e.lookupKey(key)
}

func (e *envBackend) lookupKey(key string) (any, bool, error) {
	if val, ok := os.LookupEnv(key); ok {
		return val, true, nil
	}
	if val, ok := e.dotEnv[key]; ok {
		return val, true, nil
	}
	return nil, false, nil
}

func (e *envBackend) pathToKey(path string) string {
	key := strings.ToUpper(strings.ReplaceAll(path, ".", "_"))
	return e.prefixKey(key)
}

func (e *envBackend) prefixKey(key string) string {
	if e.prefix != "" {
		return e.prefix + "_" + key
	}
	return key
}

// envName returns the entry field's `cs.env` tag value, if any. The returned
// name is normalized to uppercase before being used as a lookup key — this is
// an explicit, documented contract, not an incidental side effect: it
// guarantees that `cs.env:"db_port"` and `cs.env:"DB_PORT"` are equivalent,
// matching the same uppercase normalization [envBackend.pathToKey] already
// applies to auto-derived path keys. Callers must not rely on the tag value's
// original casing being preserved.
func envName(fields []reflect.StructField) (string, bool) {
	if len(fields) == 0 {
		return "", false
	}
	name, ok := fields[len(fields)-1].Tag.Lookup(envNameTag)
	if !ok {
		return "", false
	}
	name = strings.ToUpper(strings.TrimSpace(name))
	if name == "" {
		return "", false
	}
	return name, true
}

// parseDotEnv reads a .env file and returns its key-value pairs.
// Supported: KEY=VALUE, KEY="VALUE", KEY='VALUE', export KEY=VALUE.
// Inline comments (# ...) are stripped from unquoted values.
// Lines beginning with # and blank lines are skipped.
func parseDotEnv(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()

	result := make(map[string]string)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || line[0] == '#' {
			continue
		}
		line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		rawKey, rawVal, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		key := strings.TrimSpace(rawKey)
		val := strings.TrimSpace(rawVal)
		if len(val) >= 2 {
			q := val[0]
			if (q == '"' || q == '\'') && val[len(val)-1] == q {
				val = val[1 : len(val)-1]
			} else if before, _, found := strings.Cut(val, "#"); found {
				val = strings.TrimSpace(before)
			}
		} else if before, _, found := strings.Cut(val, "#"); found {
			val = strings.TrimSpace(before)
		}
		if key != "" {
			result[key] = val
		}
	}
	return result, scanner.Err()
}
