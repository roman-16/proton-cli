package pass

import (
	"testing"

	passsvc "github.com/roman-16/proton-cli/internal/service/pass"
)

func historyValue(it passsvc.Item, label string) string {
	for _, f := range historyFields(it) {
		if f.Label == label {
			return f.Value
		}
	}
	return ""
}

// Only what a Pass app fills in has a last use, so a note shows none rather than
// "never" - and a login nobody has used yet says so.
func TestLastUsedIsShownForWhatPassFillsIn(t *testing.T) {
	for _, tc := range []struct {
		kind string
		used int64
		want string
	}{
		{"login", 0, "never"},
		{"credit-card", 0, "never"},
		{"identity", 0, "never"},
		{"note", 0, ""},
		{"alias", 0, ""},
	} {
		it := passsvc.Item{Type: tc.kind, LastUseTime: tc.used}
		if got := historyValue(it, "Last Used"); got != tc.want {
			t.Errorf("%s used at %d shows %q, want %q", tc.kind, tc.used, got, tc.want)
		}
	}
	if got := historyValue(passsvc.Item{Type: "login", LastUseTime: 1790323380}, "Last Used"); got == "" || got == "never" {
		t.Errorf("a login used once shows %q", got)
	}
}

func TestAnExcludedItemSaysSo(t *testing.T) {
	if got := historyValue(passsvc.Item{Type: "login", Excluded: true}, "Monitor"); got != "excluded" {
		t.Errorf("an excluded login shows Monitor %q", got)
	}
	if got := historyValue(passsvc.Item{Type: "login"}, "Monitor"); got != "" {
		t.Errorf("a login Pass Monitor checks shows Monitor %q", got)
	}
}
