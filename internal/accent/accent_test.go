package accent

import "testing"

func TestResolveTakesEitherSpellingAndNothingElse(t *testing.T) {
	for _, given := range []string{"pacific", "PACIFIC", "#179FD9", "#179fd9"} {
		if got := Resolve(given); got != "#179FD9" {
			t.Errorf("Resolve(%q) = %q, want #179FD9", given, got)
		}
	}
	for _, given := range []string{"", "tomato", "#123456", "not-a-colour"} {
		if got := Resolve(given); got != "" {
			t.Errorf("Resolve(%q) = %q, want nothing", given, got)
		}
	}
}

func TestNameAnswersOnlyForThePalette(t *testing.T) {
	if got := Name("#ec3e7c"); got != "strawberry" {
		t.Errorf("Name(#ec3e7c) = %q, want strawberry", got)
	}
	if got := Name("#123456"); got != "" {
		t.Errorf("a colour off the palette was named %q", got)
	}
}

// A file may name any colour CSS has, and every one of them has to arrive at one
// of Proton's twenty.
func TestNearestSnapsAnyColourOntoThePalette(t *testing.T) {
	for _, tc := range []struct{ given, want string }{
		{"tomato", "#EC3E7C"},
		{"turquoise", "#179FD9"},
		{"#F00", "#C44800"},
		{"#179FD9", "#179FD9"},
		{"  #0F735A  ", "#0F735A"},
		{"BLACK", "#54473F"},
	} {
		if got := Nearest(tc.given); got != tc.want {
			t.Errorf("Nearest(%q) = %q, want %q", tc.given, got, tc.want)
		}
	}
}

// Every accent is its own nearest, or the snapping would move a colour that is
// already on the palette.
func TestEveryAccentIsItsOwnNearest(t *testing.T) {
	for _, c := range Palette {
		if got := Nearest(c.Hex); got != c.Hex {
			t.Errorf("Nearest(%s) = %s, want the colour itself", c.Hex, got)
		}
	}
}

// A COLOR another client wrote may hold anything, including something that is
// not a colour. Such a value has no nearest, and the event keeps none.
//
// Proton's own names are not colours here either: a file is written in the
// vocabulary CSS has, and "purple" in one is not the purple in the other.
func TestNearestAnswersNothingForWhatIsNotAColour(t *testing.T) {
	for _, given := range []string{"", "pacific", "chartreuse-ish", "#12345", "#GGGGGG", "rgb(1,2,3)"} {
		if got := Nearest(given); got != "" {
			t.Errorf("Nearest(%q) = %q, want nothing", given, got)
		}
	}
}

// The palette is what the API takes, so a typo in one of its hexes would be a
// colour every write is refused for.
func TestPaletteHoldsTwentyReadableColours(t *testing.T) {
	if len(Palette) != 20 {
		t.Errorf("the palette has %d colours, want Proton's twenty", len(Palette))
	}
	seen := map[string]bool{}
	for _, c := range Palette {
		if _, _, _, ok := parse(c.Hex); !ok {
			t.Errorf("%s is stored as %q, which is not a colour", c.Name, c.Hex)
		}
		if seen[c.Name] || seen[c.Hex] {
			t.Errorf("%s appears twice in the palette", c.Name)
		}
		seen[c.Name], seen[c.Hex] = true, true
	}
	if Name(Default) == "" {
		t.Errorf("the default colour %s is not on the palette", Default)
	}
}
