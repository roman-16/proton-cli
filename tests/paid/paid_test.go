package paid

import (
	"slices"
	"strings"
	"testing"
)

// Every rule says why, because that sentence is what somebody reads when a test
// they just wrote refuses to run.
func TestEveryRuleSaysWhy(t *testing.T) {
	for _, list := range [][]Restriction{Restrictions(), FixtureOnly()} {
		for _, r := range list {
			if len(r.Command) == 0 {
				t.Error("a rule matches nothing")
			}
			if r.Why == "" {
				t.Errorf("%v says nothing about why", r.Command)
			}
		}
	}
}

// A command the fixture needs cannot also be refused outright, or the fixture
// could never bring itself about.
func TestWhatTheFixtureNeedsIsNotAlsoRefused(t *testing.T) {
	for _, r := range FixtureOnly() {
		if why := OffLimits(r.Command); why != "" {
			t.Errorf("%v is the fixture's to run and refused to everything: %s", r.Command, why)
		}
	}
}

// A restriction has to be reachable: two naming the same command means the
// second one's reason is never read.
func TestNoRestrictionShadowsAnother(t *testing.T) {
	seen := map[string]bool{}
	for _, r := range Restrictions() {
		key := strings.Join(r.Command, " ") + "\x00" + r.Value
		if seen[key] {
			t.Errorf("%v is restricted twice", r.Command)
		}
		seen[key] = true
	}
	for _, r := range Restrictions() {
		if r.Value == "" {
			continue
		}
		for _, other := range Restrictions() {
			if other.Value == "" && slices.Equal(other.Command, r.Command) {
				t.Errorf("%v is refused outright, so narrowing it to %q says nothing", r.Command, r.Value)
			}
		}
	}
}

// The restrictions are in the order somebody would look them up.
func TestRestrictionsAreOrdered(t *testing.T) {
	var names []string
	for _, r := range Restrictions() {
		names = append(names, strings.Join(r.Command, " ")+" "+r.Value)
	}
	if !slices.IsSorted(names) {
		t.Errorf("the restrictions should read alphabetically: %v", names)
	}
}

// Each restriction is enforced, and the flags a caller puts around a command
// are not a way around it.
func TestEveryRestrictionIsEnforced(t *testing.T) {
	for _, r := range Restrictions() {
		args := slices.Clone(r.Command)
		if r.Value != "" {
			args = append(args, r.Value)
		}
		if OffLimits(args) == "" {
			t.Errorf("%v is declared off limits and allowed", args)
		}
		wrapped := append(append([]string{"--yes", "--output", "json"}, args...), "--some-flag", "value")
		if OffLimits(wrapped) == "" {
			t.Errorf("%v is allowed once flags are added", args)
		}
	}
}

// A narrowed restriction refuses the argument it names and leaves the rest of
// the command alone, or every setting on the account would be unwritable.
func TestANarrowedRestrictionRefusesOnlyWhatItNames(t *testing.T) {
	if why := OffLimits([]string{"drive", "settings", "set", "version-history", "off"}); why == "" {
		t.Error("lowering the version history was allowed")
	}
	if why := OffLimits([]string{"mail", "settings", "set", "view-mode", "messages"}); why != "" {
		t.Errorf("an unrelated mail setting is refused: %s", why)
	}
}

// Reading is always allowed, and so is anything a test can undo. A guard that
// refused everything would be safe and useless.
func TestWhatCanBeUndoneIsAllowed(t *testing.T) {
	for _, args := range [][]string{
		{"calendar", "events", "list"},
		{"calendar", "settings", "calendars", "create", "--name", "x"},
		{"contacts", "groups", "create", "--name", "x"},
		{"drive", "settings", "get"},
		{"mail", "messages", "send", "--to", "someone@example.com"},
		{"mail", "messages", "trash", "--", "SOMEREF"},
		{"mail", "settings", "addresses", "list"},
		{"mail", "settings", "autoreply", "get"},
		{"pass", "aliases", "create", "--prefix", "protoncli", "--name", "x"},
		{"pass", "export", "--dest", "-"},
		{"pass", "items", "list"},
	} {
		if why := OffLimits(args); why != "" {
			t.Errorf("%v is refused against the paid account: %s", args, why)
		}
	}
}

// The comparison has to notice a change, in both directions and in the count.
// An empty photograph compares equal to another empty one and would pass for
// ever, which is the failure mode of every guard that reads the world.
func TestDifferenceNoticesAChange(t *testing.T) {
	gone, added := Difference([]string{"a", "b"}, []string{"b", "c"})
	if !slices.Equal(gone, []string{"a"}) {
		t.Errorf("missing items came out as %v, want [a]", gone)
	}
	if !slices.Equal(added, []string{"c"}) {
		t.Errorf("new items came out as %v, want [c]", added)
	}
	if g, a := Difference([]string{"a"}, []string{"a"}); len(g) != 0 || len(a) != 0 {
		t.Errorf("an unchanged account reported %v / %v", g, a)
	}
	if _, a := Difference([]string{"a"}, []string{"a", "a"}); len(a) != 1 {
		t.Errorf("a duplicate went unnoticed: %v", a)
	}
	if g, _ := Difference([]string{"a", "a"}, []string{"a"}); len(g) != 1 {
		t.Errorf("one of a pair going missing went unnoticed: %v", g)
	}
}

// A photograph reports per collection, so what turned up is readable rather
// than an identifier nobody can place.
func TestComparePerCollection(t *testing.T) {
	before := Photograph{"vaults": {"Personal abc"}, "labels": {"Newsletters def"}}
	after := Photograph{"vaults": {"Personal abc", "Leftover xyz"}, "labels": {}}

	problems := before.Compare(after)
	if len(problems) != 2 {
		t.Fatalf("want two problems, got %v", problems)
	}
	joined := strings.Join(problems, "\n")
	for _, want := range []string{"vaults", "left behind", "Leftover xyz", "labels", "missing", "Newsletters def"} {
		if !strings.Contains(joined, want) {
			t.Errorf("the report does not mention %q:\n%s", want, joined)
		}
	}
	if !slices.IsSorted(problems) {
		t.Errorf("the report should be ordered: %v", problems)
	}
}

func TestAnUnchangedAccountReportsNothing(t *testing.T) {
	shot := Photograph{"vaults": {"Personal abc"}, "mail settings get": {"{}"}}
	if problems := shot.Compare(shot); len(problems) != 0 {
		t.Errorf("an unchanged account reported %v", problems)
	}
}

// A collection only one of the two photographs holds is a change, not something
// to skip: a listing that failed on the way out looks exactly like one that
// emptied.
func TestACollectionOnlyOnePhotographHoldsIsAChange(t *testing.T) {
	before := Photograph{"vaults": {"Personal abc"}}
	if problems := before.Compare(Photograph{}); len(problems) != 1 {
		t.Errorf("a collection that vanished reported %v", problems)
	}
	if problems := (Photograph{}).Compare(before); len(problems) != 1 {
		t.Errorf("a collection that appeared reported %v", problems)
	}
}

// Sweeping needs something to sweep for, and the subjects are what tell a
// notice this run caused from somebody's real mail.
func TestNoticesAreDeclared(t *testing.T) {
	if len(Notices()) == 0 {
		t.Fatal("a run that shares something makes Proton write to the account, and nothing here would clear it")
	}
	for _, n := range Notices() {
		if strings.TrimSpace(n) == "" {
			t.Error("an empty subject matches every message on the account")
		}
	}
}
