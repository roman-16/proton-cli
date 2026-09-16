package search

import (
	"strings"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

// How a search term is matched against what is indexed.
//
// It is Proton's own local search rather than Proton's server search: a term is
// matched literally wherever it appears, every term has to appear somewhere, and
// a quoted run is one term. The server's operators - alternation, prefixes,
// negation - belong to the index Proton keeps, which this is not, and reading
// one of them as text is better than pretending to honour it.
//
// Case and accents are folded away on both sides, so "Jürgen" is found by
// "jurgen" and the other way round. The quotation marks a phrase may be typed
// with are folded too, because a shell and a phone keyboard disagree about which
// ones they produce.

// Fold reduces text to what it is compared as: decomposed, stripped of the
// marks that decomposition separates out, and lower-cased.
func Fold(s string) string {
	folded, _, err := transform.String(
		transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC), s)
	if err != nil {
		folded = s
	}
	return strings.ToLower(foldQuotes(folded))
}

// foldQuotes turns the quotation marks a keyboard produces into the ones a
// command line is typed with.
func foldQuotes(s string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case '\u2018', '\u2019', '\u201b', '\u2032':
			return '\''
		case '\u201c', '\u201d', '\u201f', '\u2033':
			return '"'
		}
		return r
	}, s)
}

// Terms splits a query into the parts that all have to match: words, and
// anything in quotes as one phrase.
func Terms(query string) []string {
	var out []string
	var current strings.Builder
	quoted := false
	flush := func() {
		if term := strings.TrimSpace(current.String()); term != "" {
			out = append(out, Fold(term))
		}
		current.Reset()
	}
	for _, r := range foldQuotes(query) {
		switch {
		case r == '"':
			quoted = !quoted
			if !quoted {
				flush()
			}
		case unicode.IsSpace(r) && !quoted:
			flush()
		default:
			current.WriteRune(r)
		}
	}
	flush()
	return out
}

// Matches reports whether every term appears somewhere in the fields.
//
// The fields are folded here rather than stored folded, because what is indexed
// is what the account holds: a subject is shown as it was written, and a folded
// copy beside it would be a second thing to keep in step.
func Matches(terms []string, fields ...string) bool {
	if len(terms) == 0 {
		return true
	}
	folded := make([]string, 0, len(fields))
	for _, f := range fields {
		if f != "" {
			folded = append(folded, Fold(f))
		}
	}
	for _, term := range terms {
		if !inAny(folded, term) {
			return false
		}
	}
	return true
}

// Has reports whether one term appears in any of the fields, for a filter that
// is one term rather than a query.
func Has(term string, fields ...string) bool {
	if term == "" {
		return true
	}
	return Matches([]string{Fold(term)}, fields...)
}

func inAny(folded []string, term string) bool {
	for _, f := range folded {
		if strings.Contains(f, term) {
			return true
		}
	}
	return false
}
