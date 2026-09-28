package tidyurl

import (
	"encoding/json"
	"os"
	"slices"
	"testing"
)

func TestCleanAnswersAsTheWebClientDoes(t *testing.T) {
	data, err := os.ReadFile("testdata/cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Link    string  `json:"link"`
		Cleaned string  `json:"cleaned"`
		Tracked bool    `json:"tracked"`
		Removed []Param `json:"removed"`
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) < 500 {
		t.Fatalf("testdata/cases.json holds %d cases; `just tidyurl` writes them", len(cases))
	}
	for _, c := range cases {
		got, tracked := Clean(c.Link)
		removed := got.Removed
		if removed == nil {
			removed = []Param{}
		}
		if got.URL != c.Cleaned || tracked != c.Tracked || !slices.Equal(removed, c.Removed) {
			t.Errorf("Clean(%q)\n got  %q tracked=%v removed=%v\n want %q tracked=%v removed=%v",
				c.Link, got.URL, tracked, removed, c.Cleaned, c.Tracked, c.Removed)
		}
	}
}

func TestEveryRuleCompiles(t *testing.T) {
	if len(rules) < 200 {
		t.Fatalf("rules.json holds %d rules", len(rules))
	}
}
