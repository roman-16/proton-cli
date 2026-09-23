package kit

import (
	"reflect"
	"testing"
)

var (
	numbered = Setting{Enum: Ordered("conversations", "messages")}
	toggled  = Setting{Enum: OnOffBooleans()}
	nullable = Setting{Enum: []Choice{{Name: "off", Value: 1}, {Name: "on", Value: nil}}}
	fonts    = Setting{Enum: []Choice{{Name: "arial", Value: "Arial"}, {Name: "monospace", Value: "Menlo, Monospace"}}}
	ranged   = Setting{Range: &IntRange{Min: 0, Max: 20, Unit: "seconds"}}
	freeText = Setting{}
)

func TestParseFindsTheChoiceANameOrAStoredNumberNames(t *testing.T) {
	for _, tc := range []struct {
		name    string
		setting Setting
		raw     string
		want    Choice
	}{
		{"a name", numbered, "messages", Choice{Name: "messages", Value: 1}},
		{"a name in another case", numbered, "Messages", Choice{Name: "messages", Value: 1}},
		{"the number Proton stores", numbered, "1", Choice{Name: "messages", Value: 1}},
		{"a boolean", toggled, "on", Choice{Name: "on", Value: true}},
		{"a null", nullable, "on", Choice{Name: "on", Value: nil}},
		{"a number beside a null", nullable, "1", Choice{Name: "off", Value: 1}},
		{"a string", fonts, "monospace", Choice{Name: "monospace", Value: "Menlo, Monospace"}},
		{"a range", ranged, "10", Choice{Name: "10", Value: 10}},
		{"free text", freeText, "de_AT", Choice{Name: "de_AT", Value: "de_AT"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.setting.Parse("view-mode", tc.raw)
			if err != nil {
				t.Fatalf("Parse(%q): %v", tc.raw, err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("Parse(%q) = %#v, want %#v", tc.raw, got, tc.want)
			}
		})
	}
}

func TestParseRefusesWhatTheDomainDoesNotHold(t *testing.T) {
	for _, tc := range []struct {
		name    string
		setting Setting
		raw     string
		want    string
	}{
		{"an unknown name", numbered, "threads", "view-mode accepts: conversations, messages."},
		{"a number nothing stores", numbered, "7", "view-mode accepts: conversations, messages."},
		{"a number for a boolean", toggled, "1", "view-mode accepts: off, on."},
		{"a Proton font ID", fonts, "Menlo, Monospace", "view-mode accepts: arial, monospace."},
		{"a number past the range", ranged, "21", "view-mode accepts 0-20 (seconds)."},
		{"no free text at all", freeText, "", "view-mode needs a value."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tc.setting.Parse("view-mode", tc.raw)
			if err == nil {
				t.Fatalf("Parse(%q) was accepted", tc.raw)
			}
			if err.Error() != tc.want {
				t.Errorf("Parse(%q) error = %q, want %q", tc.raw, err.Error(), tc.want)
			}
		})
	}
}

func TestNameReadsBackWhatProtonStores(t *testing.T) {
	for _, tc := range []struct {
		name    string
		setting Setting
		stored  any
		want    string
	}{
		{"a decoded number", numbered, float64(1), "messages"},
		{"a boolean", toggled, false, "off"},
		{"a null", nullable, nil, "on"},
		{"a number beside a null", nullable, float64(1), "off"},
		{"a string", fonts, "Menlo, Monospace", "monospace"},
		{"a number nothing names", numbered, float64(7), "7"},
		{"a string nothing names", fonts, "Comic Sans", "Comic Sans"},
		{"a boolean nothing names", numbered, true, "true"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.setting.Name(tc.stored); got != tc.want {
				t.Errorf("Name(%#v) = %q, want %q", tc.stored, got, tc.want)
			}
		})
	}
}

func TestCompletionsOfferNamesAndNothingForAnOpenDomain(t *testing.T) {
	if got := nullable.Completions(); !reflect.DeepEqual(got, []string{"off", "on"}) {
		t.Errorf("Completions() = %q, want [off on]", got)
	}
	for _, s := range []Setting{ranged, freeText} {
		if got := s.Completions(); len(got) != 0 {
			t.Errorf("Completions() = %q, want nothing", got)
		}
	}
}
