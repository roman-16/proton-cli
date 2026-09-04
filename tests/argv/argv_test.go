package argv

import "testing"

func TestAtFindsAdjacentWordsInOrder(t *testing.T) {
	args := []string{"--yes", "--output", "json", "mail", "messages", "empty", "--folder", "trash"}
	for _, tc := range []struct {
		words []string
		want  int
	}{
		{[]string{"mail"}, 3},
		{[]string{"mail", "messages"}, 3},
		{[]string{"messages", "empty"}, 4},
		{[]string{"--folder", "trash"}, 6},
		{[]string{"--yes"}, 0},
		{[]string{"mail", "empty"}, -1},
		{[]string{"empty", "messages"}, -1},
		{[]string{"drive"}, -1},
		{[]string{"trash", "--extra"}, -1},
	} {
		if got := At(args, tc.words...); got != tc.want {
			t.Errorf("At(%v) = %d, want %d", tc.words, got, tc.want)
		}
	}
}

// The position is what lets a caller insert a flag directly after a command's
// own words, which is where a subcommand's flag has to go.
func TestAtIsWhereAFlagWouldBeInserted(t *testing.T) {
	args := []string{"--yes", "calendar", "settings", "calendars", "delete", "REF"}
	words := []string{"calendar", "settings", "calendars", "delete"}
	at := At(args, words...)
	if at < 0 {
		t.Fatal("the command was not found")
	}
	if got := args[at+len(words)]; got != "REF" {
		t.Errorf("the flag would land before %q", got)
	}
}

func TestAtWithNoWordsIsTheStart(t *testing.T) {
	if got := At([]string{"a"}); got != 0 {
		t.Errorf("At() = %d, want 0", got)
	}
	if got := At(nil); got != 0 {
		t.Errorf("At(nil) = %d, want 0", got)
	}
}

func TestAtWantingMoreThanThereIsFindsNothing(t *testing.T) {
	if got := At([]string{"mail"}, "mail", "messages"); got != -1 {
		t.Errorf("At = %d, want -1", got)
	}
}

func TestHasIsAtWithoutThePosition(t *testing.T) {
	args := []string{"pass", "aliases", "create"}
	if !Has(args, "pass", "aliases", "create") {
		t.Error("the command is there and Has said no")
	}
	if Has(args, "pass", "items", "create") {
		t.Error("Has matched words that are not adjacent")
	}
}
