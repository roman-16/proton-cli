package breach

import (
	"encoding/json"
	"slices"
	"testing"
)

func answer(t *testing.T, body string) Answer {
	t.Helper()
	var a Answer
	if err := json.Unmarshal([]byte(body), &a); err != nil {
		t.Fatal(err)
	}
	return a
}

func TestAPlanWithTheDetailReportsEveryBreachNewestFirst(t *testing.T) {
	report := answer(t, `{"IsEligible": true, "Count": 3, "Breaches": [
		{"ID": "a", "Name": "Adobe", "Severity": 0.1, "ResolvedState": 3, "PublishedAt": "2025-11-20T00:00:00Z",
		 "ExposedData": [{"Name": "Email"}], "Source": {"IsAggregated": true}},
		{"ID": "c", "Name": "Canva", "Severity": 0.9, "ResolvedState": 1, "PublishedAt": "2026-07-28T00:00:00Z",
		 "CreatedAt": "2026-08-02T00:00:00Z", "PasswordLastChars": "k2x9", "Size": 137000000,
		 "ExposedData": [{"Name": "Email"}, {"Name": "Password"}], "Actions": [{"Name": "Change your password"}],
		 "Source": {"Domain": "canva.com"}},
		{"ID": "d", "Name": "Dropbox", "Severity": 0.5, "ResolvedState": 2, "PublishedAt": "2026-03-14T00:00:00Z"}
	]}`).Report()

	if !report.Eligible || report.Withheld != 0 {
		t.Errorf("eligible %v, withheld %d; want the whole list", report.Eligible, report.Withheld)
	}
	var names, states, severities []string
	for _, b := range report.Breaches {
		names = append(names, b.Name)
		states = append(states, b.State)
		severities = append(severities, b.Severity)
	}
	if want := []string{"Canva", "Dropbox", "Adobe"}; !slices.Equal(names, want) {
		t.Errorf("order %v, want %v", names, want)
	}
	if want := []string{StateNew, StateOpen, StateResolved}; !slices.Equal(states, want) {
		t.Errorf("states %v, want %v", states, want)
	}
	if want := []string{"high", "medium", "low"}; !slices.Equal(severities, want) {
		t.Errorf("severities %v, want %v", severities, want)
	}
	canva := report.Breaches[0]
	if canva.Source != "canva.com" || canva.PasswordTail != "k2x9" || canva.Size != 137000000 ||
		canva.Found == 0 || !slices.Equal(canva.Actions, []string{"Change your password"}) {
		t.Errorf("Canva came back as %+v", canva)
	}
	if report.Breaches[2].Source != "several sources" {
		t.Errorf("an aggregated source reads %q", report.Breaches[2].Source)
	}
	if d := report.Breaches[1]; d.Exposed == nil || d.Actions == nil {
		t.Errorf("a breach with nothing listed has nil lists: %+v", d)
	}
}

func TestAPlanWithoutTheDetailNamesTheSamplesAndCountsTheRest(t *testing.T) {
	report := answer(t, `{"IsEligible": false, "Count": 4, "Breaches": [], "Samples": [
		{"ID": "c", "Name": "Canva", "Severity": 0.9, "ResolvedState": 1, "CreatedAt": "2026-08-02T00:00:00Z"}
	]}`).Report()
	if report.Eligible || len(report.Breaches) != 1 || report.Withheld != 3 {
		t.Errorf("report = %+v, want one sample and three withheld", report)
	}
}

func TestTheStateIsWrittenAsProtonNumbersIt(t *testing.T) {
	for state, want := range map[string]int{StateResolved: 3, StateOpen: 2, StateNew: 1} {
		if got := Code(state); got != want {
			t.Errorf("Code(%s) = %d, want %d", state, got, want)
		}
	}
}
