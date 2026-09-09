package fixture

import (
	"slices"
	"strings"
	"testing"
)

// Every collection has to be readable, identifiable and reconcilable, or the
// accessor that reads it fails somewhere far from the declaration.
func TestEveryCollectionCanBeReadAndReconciled(t *testing.T) {
	for _, c := range append(Free("/work"), Paid("owner@example.com")...) {
		where := c.What + " " + strings.Join(c.List, " ")
		if c.What == "" {
			t.Errorf("%s: no noun to report it by", where)
		}
		if !slices.Contains(c.List, "list") && !slices.Contains(c.List, "options") {
			t.Errorf("%s: List is not a listing", where)
		}
		if c.Key == "" {
			t.Errorf("%s: no field holds a row's identity", where)
		}
		if len(c.IDKeys) == 0 && c.Parent == "" {
			t.Errorf("%s: a row can be neither addressed by ID nor by path", where)
		}
		if len(c.Pins) == 0 {
			t.Errorf("%s: declares nothing", where)
		}
	}
}

// Every pin says how it comes about, or nothing can bring it about.
func TestEveryPinSaysHowItComesAbout(t *testing.T) {
	for _, c := range append(Free("/work"), Paid("owner@example.com")...) {
		for _, p := range c.Pins {
			if p.ID == "" {
				t.Errorf("%s: a pin with no name cannot be found", c.What)
			}
			if len(p.Create) == 0 {
				t.Errorf("%s: %s declares no way to be made", c.What, p.ID)
			}
		}
	}
}

// The fixtures read like somebody's account, and never like the suite's own
// artifacts - which Sweep deletes.
func TestNoFixtureIsNamedLikeSomethingTheSuiteSweeps(t *testing.T) {
	for _, c := range append(Free("/work"), Paid("owner@example.com")...) {
		for _, p := range c.Pins {
			if strings.HasPrefix(p.ID, TestPrefix) {
				t.Errorf("%s: %s carries the swept prefix, so a run would delete its own fixture", c.What, p.ID)
			}
		}
	}
	for _, m := range AllMail() {
		if strings.HasPrefix(m.Subject, TestPrefix) {
			t.Errorf("the %q fixture carries the swept prefix", m.Subject)
		}
	}
	for _, name := range []string{AliasName, PaidAlias, PaidForwarder} {
		if strings.HasPrefix(name, TestPrefix) {
			t.Errorf("the alias %q carries the swept prefix", name)
		}
	}
}

// A secret never reaches argv, so a pin that has one names the field it belongs
// to and the value is written to a file for the one command that reads it.
func TestASecretIsNeverAFlagValue(t *testing.T) {
	for _, c := range Free("/work") {
		for _, p := range c.Pins {
			for field, value := range p.Secrets {
				if field == "" || value == "" {
					t.Errorf("%s: %s has an empty secret", c.What, p.ID)
				}
				if slices.Contains(p.Create, value) {
					t.Errorf("%s: %s carries its %s secret in argv", c.What, p.ID, field)
				}
			}
		}
	}
}

// The paid account holds two fixtures, an alias and an address, and neither
// declares a way to remove itself: they are the two things on it that cannot be
// given back, which is the whole reason they are made once and kept.
func TestThePaidAccountKeepsWhatItCannotGiveBack(t *testing.T) {
	paid := Paid("owner@example.com")
	if len(paid) != 2 {
		t.Fatalf("the paid account declares %d collections, want the alias and the address", len(paid))
	}
	want := map[string]bool{PaidAlias: true, PaidForwarder: true}
	for _, c := range paid {
		if len(c.Pins) != 1 {
			t.Errorf("%s declares %d pins, want one", c.What, len(c.Pins))
			continue
		}
		if !want[c.Pins[0].ID] {
			t.Errorf("the paid account declares a fixture nothing names: %q", c.Pins[0].ID)
		}
		delete(want, c.Pins[0].ID)
		if len(c.Remove) != 0 {
			t.Errorf("%s declares a way to remove itself, which is exactly what must not happen to it", c.What)
		}
	}
	for id := range want {
		t.Errorf("the paid account no longer declares %q", id)
	}
	// An unpaged listing, because a real account can hold more items than a page
	// and a fixture that fell off the end of one would be minted again every run.
	if slices.Contains(paid[0].List, "items") {
		t.Errorf("the paid alias is looked up with %v, which pages; use the unpaged aliases listing",
			paid[0].List)
	}
}

// The address the suite forwards from is the account's own, with a suffix: a
// domain Proton has already let it use, and a local part that is free wherever
// that domain is.
func TestTheForwarderAddressIsTheAccountsOwnDomain(t *testing.T) {
	if got := ForwarderAddress("owner@example.com"); got != "owner-protoncli-fwd@example.com" {
		t.Errorf("ForwarderAddress = %q", got)
	}
	if got := ForwarderAddress("nonsense"); got != "" {
		t.Errorf("ForwarderAddress of something that is not an address = %q, want nothing", got)
	}
}

// The panel's mail is what the README shows, so it has to be there and to read
// like mail.
func TestThePanelShowsMail(t *testing.T) {
	if len(Panel()) == 0 {
		t.Fatal("the README panel shows nothing")
	}
	for _, m := range Panel() {
		if m.Subject == "" || m.Body == "" {
			t.Errorf("a panel message is blank: %+v", m)
		}
		if m.Attach != "" && Files()[m.Attach] == "" && m.Attach != "panorama.jpg" {
			t.Errorf("the panel attaches %q, which is not one of the fixture's files", m.Attach)
		}
	}
}

// Every file a fixture attaches or uploads is one the seed and the suite both
// know how to write.
func TestEveryAttachedFileIsDeclared(t *testing.T) {
	for _, m := range AllMail() {
		for _, name := range []string{m.Attach, m.Inline} {
			if name == "" {
				continue
			}
			if _, ok := Files()[name]; !ok && name != Attachments.Inline {
				t.Errorf("the %q fixture attaches %q, which Files() does not declare", m.Subject, name)
			}
		}
	}
}

// A mutating test takes a message no other test is changing, so there have to
// be several and they have to be distinct.
func TestTheMutablePoolHoldsDistinctMessages(t *testing.T) {
	if len(Mutable) < 2 {
		t.Fatal("a pool of one is not a pool")
	}
	seen := map[string]bool{}
	for _, m := range Mutable {
		if seen[m.Subject] {
			t.Errorf("two mutable messages share the subject %q, so two tests would change the same one", m.Subject)
		}
		seen[m.Subject] = true
	}
}

// The plain fixture's body carries its subject, which is what lets a test tell
// a body it can read from a body it only thinks it can read.
func TestThePlainFixturesBodyNamesItself(t *testing.T) {
	if !strings.Contains(Plain.Body, "reading group") {
		t.Errorf("the plain fixture's body does not carry its subject: %q", Plain.Body)
	}
	if strings.Contains(Plain.Body, "wrote:") || strings.Contains(Plain.Body, "\n>") {
		t.Errorf("the plain fixture carries a quote, so --strip-quotes would change it: %q", Plain.Body)
	}
	if !strings.Contains(Quoted.Body, "wrote:") {
		t.Errorf("the quoted fixture carries no reply block, which is the whole point of it: %q", Quoted.Body)
	}
}

// The window a listing of the fixture's events uses has to reach the days they
// are on, or the seed and the suite disagree about whether they exist.
func TestTheEventWindowReachesTheFixturesEvents(t *testing.T) {
	if Today() >= InDays(1) {
		t.Errorf("InDays does not move forward: %s then %s", Today(), InDays(1))
	}
	if InDays(3) >= InDays(30) {
		t.Errorf("the fixture's events are three days out and the window is thirty: %s, %s", InDays(3), InDays(30))
	}
}
