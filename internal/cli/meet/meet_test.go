package meet

import (
	"testing"

	meetsvc "github.com/roman-16/proton-cli/internal/service/meet"
)

func TestAReferenceFindsByLinkThenByTitle(t *testing.T) {
	all := []meetsvc.Meeting{
		{Name: "865JZ8JMHG", Title: "Weekly sync"},
		{Name: "6MCF0BSHBR", Title: "Sync"},
		{Name: "E87W2DAJ60", Title: "Team room"},
	}
	for _, tc := range []struct {
		needle string
		want   []string
	}{
		{"865JZ8JMHG", []string{"865JZ8JMHG"}},
		{"865JZ8JM", []string{"865JZ8JMHG"}},
		{"Sync", []string{"6MCF0BSHBR"}},
		{"sync", []string{"6MCF0BSHBR"}},
		{"ync", []string{"865JZ8JMHG", "6MCF0BSHBR"}},
		{"865J", nil},
	} {
		got := matching(all, tc.needle)
		if len(got) != len(tc.want) {
			t.Errorf("%q matched %v, want %v", tc.needle, got, tc.want)
			continue
		}
		for i := range got {
			if got[i].Name != tc.want[i] {
				t.Errorf("%q matched %v, want %v", tc.needle, got, tc.want)
			}
		}
	}
}
