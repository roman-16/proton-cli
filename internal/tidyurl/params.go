package tidyurl

import (
	"strings"
)

type param struct{ name, value string }

// params is a query as URLSearchParams holds it: decoded, ordered, and
// serialized again only once something in it changes.
type params []param

func parseParams(query string) params {
	var p params
	for sequence := range strings.SplitSeq(query, "&") {
		if sequence == "" {
			continue
		}
		name, value, _ := strings.Cut(sequence, "=")
		p = append(p, param{formDecode(name), formDecode(value)})
	}
	return p
}

func formDecode(s string) string {
	return percentDecode(strings.ReplaceAll(s, "+", " "))
}

func (p params) get(name string) (string, bool) {
	for _, entry := range p {
		if entry.name == name {
			return entry.value, true
		}
	}
	return "", false
}

func (p params) without(name string) params {
	kept := make(params, 0, len(p))
	for _, entry := range p {
		if entry.name != name {
			kept = append(kept, entry)
		}
	}
	return kept
}

func (p params) lowercased() params {
	lower := make(params, len(p))
	for i, entry := range p {
		lower[i] = param{strings.ToLower(entry.name), entry.value}
	}
	return lower
}

func (p params) String() string {
	parts := make([]string, len(p))
	for i, entry := range p {
		parts[i] = formEncode(entry.name) + "=" + formEncode(entry.value)
	}
	return strings.Join(parts, "&")
}

func formEncode(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == ' ':
			b.WriteByte('+')
		case isAlpha(c) || isDigit(c) || c == '*' || c == '-' || c == '.' || c == '_':
			b.WriteByte(c)
		default:
			b.WriteByte('%')
			b.WriteByte(upperHex[c>>4])
			b.WriteByte(upperHex[c&0xF])
		}
	}
	return b.String()
}
