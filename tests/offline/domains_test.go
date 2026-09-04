package offline

import (
	"strings"
	"testing"
)

// A value the interface declares a domain for is judged against that domain
// before anything is sent, and the refusal names the whole domain: somebody who
// guessed wrong needs the list, not the news that they were wrong.

func TestSettingValueOutsideItsDomainIsRefused(t *testing.T) {
	for _, tt := range []struct {
		args    []string
		phrases []string
	}{
		{[]string{"mail", "settings", "set", "view-mode", "threads"}, []string{"conversations", "messages"}},
		{[]string{"mail", "settings", "set", "view-mode", "7"}, []string{"conversations", "messages"}},
		{[]string{"mail", "settings", "set", "delay-send", "999"}, []string{"0-20 (seconds)"}},
		{[]string{"mail", "settings", "set", "page-size", "3"}, []string{"50", "100", "200"}},
		{[]string{"mail", "settings", "set", "draft-type", "text/markdown"}, []string{"text/html", "text/plain"}},
		{[]string{"account", "settings", "set", "week-start", "funday"},
			[]string{"week-start accepts", "monday", "sunday"}},
	} {
		refuses(t, 1, tt.args, tt.phrases...)
	}
}

// A key that does not exist is a reference that matched nothing, so it exits 3
// like every other miss and points at the list of the ones that do.
func TestUnknownSettingKeyIsRefusedAndPointsAtTheList(t *testing.T) {
	refuses(t, 3, []string{"mail", "settings", "set", "no-such-key", "1"},
		"no mail setting called", "mail settings list")
	refuses(t, 3, []string{"account", "settings", "set", "no-such-key", "on"},
		"no account setting called", "settings list")
	refuses(t, 3, []string{"calendar", "settings", "set", "no-such-key", "on"},
		"no calendar setting called", "calendar settings list")
	refuses(t, 3, []string{"drive", "settings", "set", "no-such-key", "on"},
		"no drive setting called", "drive settings list")
}

func TestSettingSomethingNeedsAKeyAndAValue(t *testing.T) {
	refuses(t, 1, []string{"account", "settings", "set"},
		"KEY and a VALUE", "account settings list")
}

func TestFlagValueOutsideItsDomainIsRefused(t *testing.T) {
	for _, tt := range []struct {
		args    []string
		phrases []string
	}{
		{[]string{"mail", "messages", "get", "--render", "wut", "any-ref"},
			[]string{"--render accepts:", "text", "html", "raw"}},
		{[]string{"calendar", "events", "respond", "--answer", "maybe", "any-ref"},
			[]string{"--answer accepts:", "accept", "tentative", "decline"}},
		{[]string{"drive", "photos", "list", "--tag", "bogus"},
			[]string{"--tag accepts:", "favorites"}},
		{[]string{"--output", "xml", "mail", "messages", "list"},
			[]string{"--output accepts:", "text", "json", "yaml"}},
		// A collection can only be ordered by a key it has, and which keys those
		// are differs, so each listing names its own.
		{[]string{"contacts", "list", "--sort", "nope"},
			[]string{"--sort accepts:", "name", "email"}},
		{[]string{"drive", "items", "list", "--sort", "nope"},
			[]string{"--sort accepts:", "name", "size", "modified"}},
		{[]string{"pass", "items", "list", "--sort", "nope"},
			[]string{"--sort accepts:", "name", "type", "modified", "created"}},
	} {
		refuses(t, 1, tt.args, tt.phrases...)
	}
}

// A tag is referenced by name only, so Proton's own number for it is refused
// too: the CLI neither accepts nor emits a raw enum.
func TestPhotoTagIsRefusedAsANumber(t *testing.T) {
	refuses(t, 1, []string{"drive", "photos", "list", "--tag", "2"}, "--tag accepts:")
}

// Proton allows only its own accent colours for a label, folder, calendar, event
// or contact group, so anything else is refused here rather than by the server.
func TestColourOffProtonsPaletteIsRefused(t *testing.T) {
	for _, args := range [][]string{
		{"mail", "settings", "labels", "create", "--name", "x", "--color", "#FFF000"},
		{"mail", "settings", "folders", "create", "--name", "x", "--color", "not-a-colour"},
		{"calendar", "settings", "calendars", "create", "--name", "x", "--color", "#123456"},
		{"calendar", "events", "create", "--title", "x", "--start", "2027-07-03T09:00",
			"--color", "#123456"},
	} {
		refuses(t, 1, args, "not a Proton accent color")
	}
}

func TestWrongNumberOfArgumentsIsRefused(t *testing.T) {
	refuses(t, 1, []string{"api"}, "arg")
	refuses(t, 1, []string{"mail", "messages", "get"}, "arg")
}

// An event's length can be said as an end or as a duration, and saying it both
// ways is a command line that means something its author did not check. Saying
// it with no start to hang off is the same mistake in the other direction. Both
// are decided by the flags alone, so neither costs a request to discover.
func TestAnEventsLengthIsStatedOnce(t *testing.T) {
	refuses(t, 1, []string{"calendar", "events", "create",
		"--title", "X", "--start", "2026-04-16T14:00",
		"--end", "2026-04-16T15:00", "--duration", "1h"},
		"--end and --duration", "pass one of them")

	for _, flag := range [][]string{{"--end", "2026-04-16T15:00"}, {"--duration", "1h"}} {
		refuses(t, 1, append([]string{"calendar", "events", "update", "any-ref"}, flag...),
			"needs --start")
	}
}

// Which contact an address belongs to is a question only one contact can answer,
// so naming several with --email is decided by the flags alone.
func TestGroupingOneAddressNeedsOneContact(t *testing.T) {
	refuses(t, 1, []string{"contacts", "groups", "add", "GROUP", "one", "two",
		"--email", "a@example.com"},
		"--email applies to one contact", "drop --email")
}

// Making a password reaches no account and needs no session: it happens on this
// machine and may never leave it.
func TestGeneratingAPasswordNeedsNoAccount(t *testing.T) {
	stdout, _, code := run(t, "pass", "generate", "--length", "24")
	if code != 0 {
		t.Fatalf("exit %d, want 0 - generating a password should not need a session", code)
	}
	if !strings.Contains(stdout, "Password:") {
		t.Errorf("expected a password, got: %s", stdout)
	}
}

// A password too short to hold one of every kind asked for is refused rather
// than silently dropping a kind.
func TestAPasswordTooShortForItsKindsIsRefused(t *testing.T) {
	refuses(t, 1, []string{"pass", "generate", "--length", "2"}, "cannot hold one of each")
}

// A filter is described on the command line, so everything wrong with the
// description is answerable there - and worth answering there, because the
// alternative is a filter that silently moves the wrong mail.
func TestAFilterDescribedWronglyIsRefused(t *testing.T) {
	create := func(rest ...string) []string {
		return append([]string{"mail", "settings", "filters", "create", "--name", "X"}, rest...)
	}
	for _, tt := range []struct {
		args    []string
		phrases []string
	}{
		{create("--star"), []string{"needs something to match", "--if"}},
		{create("--if", "subject contains x"), []string{"does nothing with it", "--move-to", "--star"}},
		{create("--if", "topic contains x", "--star"),
			[]string{"nothing called \"topic\"", "attachments", "recipient", "sender", "subject"}},
		{create("--if", "subject holds x", "--star"),
			[]string{"no way to match called \"holds\"", "contains", "is", "matches", "starts", "ends"}},
		{create("--if", "subject contains", "--star"), []string{"but not what to match"}},
		{create("--if", "attachments contains nope", "--star"), []string{"takes no value"}},
		{create("--if", "subject contains x", "--match", "either", "--star"), []string{"all", "any"}},
	} {
		refuses(t, 1, tt.args, tt.phrases...)
	}
}

// The two ways to write a filter are not two halves of one.
func TestAFilterIsEitherDescribedOrWritten(t *testing.T) {
	refuses(t, 1, []string{"mail", "settings", "filters", "create", "--name", "X",
		"--if", "subject contains x", "--sieve", "keep;"}, "if", "sieve")
}

// The same description, judged the same way, whichever command takes it.
func TestARewrittenFilterIsDescribedByTheSameRules(t *testing.T) {
	update := func(rest ...string) []string {
		return append([]string{"mail", "settings", "filters", "update", "X"}, rest...)
	}
	for _, tt := range []struct {
		args    []string
		phrases []string
	}{
		{update("--if", "topic contains x", "--star"), []string{"nothing called \"topic\""}},
		{update("--if", "subject contains x"), []string{"does nothing with it"}},
		{update("--if", "subject contains x", "--sieve", "keep;"), []string{"if", "sieve"}},
		{update(), []string{"Nothing to change", "--name", "--if"}},
	} {
		refuses(t, 1, tt.args, tt.phrases...)
	}
}
