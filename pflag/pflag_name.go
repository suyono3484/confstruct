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
	"fmt"
	"reflect"
	"regexp"
	"strings"
)

// pflagNameTag is the struct tag an entry field uses to override its
// derived long flag name.
const pflagNameTag = "cs.pflag"

// pflagTagRe is the accepted grammar for an explicit cs.pflag tag value,
// after trimming outer whitespace: a lowercase-letter start, lowercase
// letters and digits within a word, and single hyphens between words.
var pflagTagRe = regexp.MustCompile(`^[a-z][a-z0-9]*(-[a-z0-9]+)*$`)

// splitIdentifierWords splits a single Go identifier into lowercase,
// hyphenless words following the rules in
// docs/pflag-integration.md#identifier-to-flag-name-conversion:
//
//  1. lower-to-upper starts a new word (ListenAddr -> Listen, Addr).
//  2. the last upper of an upper-run starts a new word when followed by a
//     lowercase letter (HTTPServer -> HTTP, Server).
//  3. digit-to-upper starts a new word (HTTP2Server -> HTTP2, Server).
//  4. letter-to-digit never starts a new word (Server2Port -> Server2, Port).
//
// A rune outside [a-zA-Z0-9] (an underscore, punctuation, or non-ASCII
// letter) is an error per Rule 6: the conversion must not silently invent a
// lossy transliteration, so such an identifier requires an explicit
// cs.pflag tag instead.
func splitIdentifierWords(name string) ([]string, error) {
	runes := []rune(name)
	n := len(runes)
	if n == 0 {
		return nil, nil
	}

	const (
		classLower = iota
		classUpper
		classDigit
		classOther
	)
	classOf := func(r rune) int {
		switch {
		case r >= 'a' && r <= 'z':
			return classLower
		case r >= 'A' && r <= 'Z':
			return classUpper
		case r >= '0' && r <= '9':
			return classDigit
		default:
			return classOther
		}
	}

	var words []string
	cur := make([]rune, 0, n)
	for i, r := range runes {
		cls := classOf(r)
		if cls == classOther {
			return nil, fmt.Errorf("character %q is not an ASCII letter or digit", r)
		}
		if len(cur) > 0 {
			prev := classOf(cur[len(cur)-1])
			boundary := false
			switch {
			case prev == classLower && cls == classUpper:
				boundary = true
			case prev == classDigit && cls == classUpper:
				boundary = true
			case prev == classUpper && cls == classUpper:
				boundary = i+1 < n && classOf(runes[i+1]) == classLower
			}
			if boundary {
				words = append(words, strings.ToLower(string(cur)))
				cur = cur[:0]
			}
		}
		cur = append(cur, r)
	}
	if len(cur) > 0 {
		words = append(words, strings.ToLower(string(cur)))
	}
	return words, nil
}

// derivedPFlagName joins every segment of a field-chain path into one
// derived flag name: word-split each Go field name, lowercase, hyphen-join
// within a segment, hyphen-join across segments.
func derivedPFlagName(fields []reflect.StructField) (string, error) {
	segments := make([]string, 0, len(fields))
	for _, f := range fields {
		words, err := splitIdentifierWords(f.Name)
		if err != nil {
			return "", fmt.Errorf("invalid field name %q: %w; use cs.pflag to specify an explicit flag name", f.Name, err)
		}
		if len(words) > 0 {
			segments = append(segments, strings.Join(words, "-"))
		}
	}
	return strings.Join(segments, "-"), nil
}

// pflagName resolves the final flag name for one entry field: the trimmed
// cs.pflag tag if present (validated against pflagTagRe), otherwise the
// derived name.
//
// path is unused here; it is kept in the signature only for call-convention
// parity with the pflagBackend.lookupField(path, fields) shape in
// docs/pflag-integration.md#proposed-implementation-outline, which Phase 3
// implements against this same function. The returned error deliberately
// does not mention path — see docs/pflag-plan-phase-1-name-conversion.md for
// why: the field path is added once by confstruct's own backend-error wrap,
// and repeating it here would duplicate it in the final message.
func pflagName(path string, fields []reflect.StructField) (string, error) {
	_ = path
	if len(fields) > 0 {
		if raw, ok := fields[len(fields)-1].Tag.Lookup(pflagNameTag); ok {
			trimmed := strings.TrimSpace(raw)
			if !pflagTagRe.MatchString(trimmed) {
				return "", fmt.Errorf("invalid cs.pflag tag %q: expected a lowercase kebab-case long flag name", raw)
			}
			return trimmed, nil
		}
	}
	return derivedPFlagName(fields)
}
