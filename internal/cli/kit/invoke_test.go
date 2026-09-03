package kit

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/roman-16/proton-cli/internal/app"
	"github.com/roman-16/proton-cli/internal/idcache"
)

// A compound reference is one pasteable token. The separator is safe because
// Proton's IDs are base64url, which has no slash - so a reference either carries
// one and is a pair, or does not and is a handle.
func TestPair(t *testing.T) {
	for _, tc := range []struct {
		name          string
		ref           string
		first, second string
	}{
		{"a pair", "shareABC==/itemXYZ==", "shareABC==", "itemXYZ=="},
		{"a bare id", "itemXYZ==", "", "itemXYZ=="},
		{"a human handle", "github.com", "", "github.com"},
		{"a handle with spaces", "Team sync", "", "Team sync"},
		{"an empty first half", "/itemXYZ==", "", "itemXYZ=="},
	} {
		t.Run(tc.name, func(t *testing.T) {
			first, second := Pair(tc.ref)
			if first != tc.first || second != tc.second {
				t.Errorf("Pair(%q) = (%q, %q), want (%q, %q)", tc.ref, first, second, tc.first, tc.second)
			}
		})
	}
}

// Joining and splitting have to agree, or an ID printed by one command would not
// be accepted by the next.
func TestPairRoundTrips(t *testing.T) {
	for _, tc := range [][2]string{
		{"shareABC==", "itemXYZ=="},
		{"", "itemXYZ=="},
	} {
		joined := JoinPair(tc[0], tc[1])
		first, second := Pair(joined)
		if first != tc[0] || second != tc[1] {
			t.Errorf("round trip of (%q, %q) gave (%q, %q) via %q", tc[0], tc[1], first, second, joined)
		}
	}
}

// A short ID has to work on either side of the slash, which is the promise the
// reference documentation makes. StepExpand cannot keep it: a slash is not part
// of an ID, so the whole token never looks short to it.
func TestExpandPairExpandsBothHalves(t *testing.T) {
	full := func(seed string) string {
		return seed + strings.Repeat("A", 86-len(seed)) + "=="
	}
	share, item := full("shareAB1"), full("itemXY23")

	dir := t.TempDir()
	a := &app.App{IDCache: idcache.New(filepath.Join(dir, "ids.json"))}
	if err := a.IDCache.Save(idcache.Entry{Collection: "pass items", Ref: JoinPair(share, item)}); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name          string
		ref           string
		first, second string
	}{
		{"both halves short", "shareAB1/itemXY23", share, item},
		{"only the second short", share + "/itemXY23", share, item},
		{"only the first short", "shareAB1/" + item, share, item},
		{"already full", share + "/" + item, share, item},
		{"an uncached half is left alone", "shareAB1/notcache", share, "notcache"},
		{"a handle is not a pair", "GitHub", "", "GitHub"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			first, second, err := ExpandPair(a, tc.ref)
			if err != nil {
				t.Fatalf("ExpandPair(%q): %v", tc.ref, err)
			}
			if first != tc.first || second != tc.second {
				t.Errorf("ExpandPair(%q) = (%q, %q), want (%q, %q)", tc.ref, first, second, tc.first, tc.second)
			}
		})
	}
}

func TestDedupePreservesOrder(t *testing.T) {
	got := Dedupe([]string{"c", "a", "c", "b", "a"})
	want := []string{"c", "a", "b"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("Dedupe = %v, want %v", got, want)
	}
}

// The verb vocabulary is closed, and the mutating subset has to be a subset of it:
// a verb that changes state but is not listed as a verb could never be reached.
func TestMutatingVerbsAreDeclaredVerbs(t *testing.T) {
	for verb := range Mutating {
		if _, ok := Verbs[verb]; !ok {
			t.Errorf("%q is marked mutating but is not in the vocabulary", verb)
		}
	}
}

// Every verb has to say what it means, or the vocabulary is just a list of words.
func TestEveryVerbIsExplained(t *testing.T) {
	for verb, meaning := range Verbs {
		if strings.TrimSpace(meaning) == "" {
			t.Errorf("%q has no meaning recorded", verb)
		}
		if verb != strings.ToLower(verb) {
			t.Errorf("%q should be lower case", verb)
		}
	}
}

func TestEveryPlaceholderIsExplained(t *testing.T) {
	for name, p := range Placeholders {
		if strings.TrimSpace(p.Means) == "" {
			t.Errorf("%q has no meaning recorded", name)
		}
		if name != strings.ToUpper(name) {
			t.Errorf("%q should be upper case", name)
		}
	}
}

func TestHintList(t *testing.T) {
	for _, tc := range []struct {
		in   []string
		want string
	}{
		{nil, ""},
		{[]string{"unread"}, "--unread"},
		{[]string{"unread", "from"}, "--unread or --from"},
		{[]string{"unread", "from", "older-than"}, "--unread, --from or --older-than"},
		{[]string{"--unread", "from"}, "--unread or --from"},
	} {
		if got := HintList(tc.in...); got != tc.want {
			t.Errorf("HintList(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
