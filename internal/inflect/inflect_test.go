package inflect

import "testing"

var nouns = map[string]string{
	"access token":      "access tokens",
	"address":           "addresses",
	"album":             "albums",
	"alias":             "aliases",
	"attachment":        "attachments",
	"body":              "bodies",
	"calendar":          "calendars",
	"contact":           "contacts",
	"conversation":      "conversations",
	"draft":             "drafts",
	"entry":             "entries",
	"event":             "events",
	"filter":            "filters",
	"folder":            "folders",
	"folder or label":   "folders or labels",
	"group":             "groups",
	"invitation":        "invitations",
	"item":              "items",
	"key":               "keys",
	"label":             "labels",
	"mailing list":      "mailing lists",
	"message":           "messages",
	"message body":      "message bodies",
	"photo":             "photos",
	"profile":           "profiles",
	"revision":          "revisions",
	"scheduled message": "scheduled messages",
	"session":           "sessions",
	"setting":           "settings",
	"vault":             "vaults",
	"watched address":   "watched addresses",
}

func TestPluralNamesEveryKindTheWayASentenceDoes(t *testing.T) {
	for singular, want := range nouns {
		if got := Plural(singular); got != want {
			t.Errorf("Plural(%q) = %q, want %q", singular, got, want)
		}
	}
}

func TestSingularUndoesPlural(t *testing.T) {
	for want, plural := range nouns {
		if got := Singular(plural); got != want {
			t.Errorf("Singular(%q) = %q, want %q", plural, got, want)
		}
	}
}

func TestSingularLeavesWhatIsNotAPluralAlone(t *testing.T) {
	for _, word := range []string{"folders/", "y", ""} {
		if got := Singular(word); got != word {
			t.Errorf("Singular(%q) = %q, want it unchanged", word, got)
		}
	}
}
