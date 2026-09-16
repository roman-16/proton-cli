package search

import "testing"

// A term is matched the way Proton's own local search matches one: literally,
// wherever it appears, with case and accents folded away on both sides.
func TestMatchesFoldsCaseAndAccents(t *testing.T) {
	for _, tc := range []struct {
		query string
		field string
		want  bool
	}{
		{"jurgen", "Jürgen Roe", true},
		{"JÜRGEN", "jurgen roe", true},
		{"invoice", "Your Invoice #2291 is ready", true},
		{"nvoic", "Your Invoice #2291 is ready", true},
		{"invoice 2291", "Your Invoice #2291 is ready", true},
		{"invoice receipt", "Your Invoice #2291 is ready", false},
		{"", "anything at all", true},
	} {
		if got := Matches(Terms(tc.query), tc.field); got != tc.want {
			t.Errorf("Matches(%q, %q) = %v, want %v", tc.query, tc.field, got, tc.want)
		}
	}
}

// Every word has to appear somewhere, and it may be a different somewhere for
// each: a search for a sender and a subject word is one query over one message.
func TestEveryTermHasToAppearSomewhere(t *testing.T) {
	fields := []string{"Quarterly numbers", "Jane Roe", "jane@example.com"}
	if !Matches(Terms("quarterly jane"), fields...) {
		t.Error("terms spread across fields should match")
	}
	if Matches(Terms("quarterly jürgen"), fields...) {
		t.Error("a term nothing holds should not match")
	}
}

// A quoted run is one term, so a phrase matches where the words in that order
// do and not where they merely both appear.
func TestAQuotedRunIsOneTerm(t *testing.T) {
	if got := Terms(`"parking permit" renewal`); len(got) != 2 || got[0] != "parking permit" {
		t.Fatalf("Terms = %q, want the phrase held together", got)
	}
	if !Matches(Terms(`"parking permit"`), "your parking permit is ready") {
		t.Error("a phrase should match the run it names")
	}
	if Matches(Terms(`"parking permit"`), "parking is free, and a permit is not needed") {
		t.Error("a phrase should not match its words scattered")
	}
}

// The quotation marks a phone or a word processor produces are the ones people
// paste, so they mean what the typed ones mean.
func TestCurlyQuotesAreQuotes(t *testing.T) {
	if got := Terms("\u201cparking permit\u201d"); len(got) != 1 || got[0] != "parking permit" {
		t.Errorf("Terms = %q, want one phrase", got)
	}
}

// Has is one term rather than a query, for the filters that take a single word.
func TestHasMatchesOneTermAcrossFields(t *testing.T) {
	if !Has("Jane Roe", "Jane Roe", "jane@example.com") {
		t.Error("a display name should match the name field")
	}
	if !Has("JANE@example.com", "Jane Roe", "jane@example.com") {
		t.Error("an address should match whatever its case")
	}
	if Has("jürgen", "Jane Roe", "jane@example.com") {
		t.Error("a name nobody has should not match")
	}
	if !Has("", "Jane Roe") {
		t.Error("an empty filter narrows nothing")
	}
}
