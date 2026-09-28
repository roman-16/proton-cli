package tidyurl

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"
)

var errMalformedURI = errors.New("URI malformed")

func decodeURIComponent(s string) (string, error) { return decodeEscapes(s, "") }

func decodeURI(s string) (string, error) { return decodeEscapes(s, ";/?:@&=+$,#") }

// decodeEscapes is ECMAScript's Decode: every %XX sequence becomes the UTF-8 it
// spells, except single bytes in reserved, and anything malformed fails.
func decodeEscapes(s, reserved string) (string, error) {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '%' {
			b.WriteByte(s[i])
			continue
		}
		first, ok := escapedByte(s, i)
		if !ok {
			return "", errMalformedURI
		}
		if first < utf8.RuneSelf {
			if strings.IndexByte(reserved, first) >= 0 {
				b.WriteString(s[i : i+3])
			} else {
				b.WriteByte(first)
			}
			i += 2
			continue
		}
		n := sequenceLength(first)
		if n == 0 {
			return "", errMalformedURI
		}
		encoded := []byte{first}
		for k := 1; k < n; k++ {
			next, ok := escapedByte(s, i+3*k)
			if !ok || next&0xC0 != 0x80 {
				return "", errMalformedURI
			}
			encoded = append(encoded, next)
		}
		r, size := utf8.DecodeRune(encoded)
		if r == utf8.RuneError && size <= 1 {
			return "", errMalformedURI
		}
		b.WriteRune(r)
		i += 3*n - 1
	}
	return b.String(), nil
}

func escapedByte(s string, i int) (byte, bool) {
	if i+2 >= len(s) || s[i] != '%' || !isHex(s[i+1]) || !isHex(s[i+2]) {
		return 0, false
	}
	return unhex(s[i+1])<<4 | unhex(s[i+2]), true
}

func sequenceLength(first byte) int {
	switch {
	case first&0xE0 == 0xC0:
		return 2
	case first&0xF0 == 0xE0:
		return 3
	case first&0xF8 == 0xF0:
		return 4
	}
	return 0
}

// atob is the browser's forgiving base64 decode. Its answer is a binary string,
// one character per byte, which is what the rules go on to read as a link.
func atob(s string) (string, bool) {
	s = strings.Map(func(r rune) rune {
		if strings.ContainsRune("\t\n\f\r ", r) {
			return -1
		}
		return r
	}, s)
	if len(s)%4 == 0 {
		s = strings.TrimSuffix(s, "=")
		s = strings.TrimSuffix(s, "=")
	}
	if len(s)%4 == 1 {
		return "", false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !isAlpha(c) && !isDigit(c) && c != '+' && c != '/' {
			return "", false
		}
	}
	raw, err := base64.RawStdEncoding.DecodeString(s)
	if err != nil {
		return "", false
	}
	runes := make([]rune, len(raw))
	for i, c := range raw {
		runes[i] = rune(c)
	}
	return string(runes), true
}

func decodeBase64(s string) string {
	if decoded, ok := atob(s); ok {
		return decoded
	}
	return s
}

var knownEncodings = map[string]bool{"": true, "base64": true, "url": true, "urlc": true}

func decodeAs(s, encoding string) string {
	var decoded string
	var err error
	switch encoding {
	case "url":
		decoded, err = decodeURI(s)
	case "urlc":
		decoded, err = decodeURIComponent(s)
	default:
		return decodeBase64(s)
	}
	if err != nil {
		return s
	}
	return decoded
}

func parseJSONValue(s string) (any, bool) {
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		return nil, false
	}
	return v, true
}

// isObject is JavaScript's typeof v === "object", which null passes.
func isObject(v any) bool {
	switch v.(type) {
	case map[string]any, []any, nil:
		return true
	}
	return false
}
