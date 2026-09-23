package calendar

import (
	"errors"
	"strings"
	"testing"

	"github.com/roman-16/proton-cli/internal/errs"
	calsvc "github.com/roman-16/proton-cli/internal/service/calendar"
)

var directory = []calsvc.Holidays{
	{ID: "at", Country: "Austria", CountryCode: "AT", Language: "Deutsch", LanguageCode: "de"},
	{ID: "ch-de", Country: "Switzerland", CountryCode: "CH", Language: "Deutsch", LanguageCode: "de"},
	{ID: "ch-en", Country: "Switzerland", CountryCode: "CH", Language: "English", LanguageCode: "en"},
	{ID: "ch-fr", Country: "Switzerland", CountryCode: "CH", Language: "Français", LanguageCode: "fr"},
}

// A country is named the way a person would: by its name or its code, in any
// case, or by the ID a listing showed. A language is asked for only where the
// country has more than one.
func TestAHolidaysCalendarIsPickedByCountryAndLanguage(t *testing.T) {
	for _, tc := range []struct{ country, language, want string }{
		{"Austria", "", "at"},
		{"austria", "", "at"},
		{"AT", "", "at"},
		{"at", "", "at"},
		{"Austria", "Deutsch", "at"},
		{"ch-fr", "", "ch-fr"},
		{"Switzerland", "Français", "ch-fr"},
		{"CH", "en", "ch-en"},
		{"switzerland", "DEUTSCH", "ch-de"},
	} {
		got, err := pickHolidays(directory, tc.country, tc.language)
		if err != nil {
			t.Errorf("%s in %q: %v", tc.country, tc.language, err)
			continue
		}
		if got.ID != tc.want {
			t.Errorf("%s in %q picked %s, want %s", tc.country, tc.language, got.ID, tc.want)
		}
	}
}

// Every way of naming nothing, or too much, is refused with what would settle
// it, and none of them guesses.
func TestAHolidaysCalendarThatCannotBePickedIsRefused(t *testing.T) {
	for _, tc := range []struct {
		name, country, language string
		exit                    int
		says                    []string
	}{
		{"a country with several languages and none asked for", "Switzerland", "", 4,
			[]string{"Switzerland has holidays in Deutsch, English and Français.", "--language Deutsch"}},
		{"a language the country has no calendar in", "Austria", "English", 3,
			[]string{"Austria has no holidays calendar in English; it has Deutsch."}},
		{"a country Proton offers nothing for", "Atlantis", "", 3,
			[]string{`No holidays calendar matching "Atlantis".`, "calendar settings holidays list"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := pickHolidays(directory, tc.country, tc.language)
			if err == nil {
				t.Fatal("was accepted")
			}
			var coder errs.ExitCoder
			if !errors.As(err, &coder) || coder.ExitCode() != tc.exit {
				t.Errorf("exit code of %v, want %d", err, tc.exit)
			}
			said := err.Error()
			var hinted interface{ Hints() []string }
			if errors.As(err, &hinted) {
				said += "\n" + strings.Join(hinted.Hints(), "\n")
			}
			for _, want := range tc.says {
				if !strings.Contains(said, want) {
					t.Errorf("says %q, want it to say %q", said, want)
				}
			}
		})
	}
}
