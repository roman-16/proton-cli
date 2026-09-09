package mail

import (
	"strings"
	"testing"

	mailsvc "github.com/roman-16/proton-cli/internal/service/mail"
)

// What each verb takes is judged from the row, so an address a verb has nothing
// to do with - and the two Proton keeps whatever anybody asks - is refused
// before a request is made.

func address(email string, kind, status, order int) mailsvc.Address {
	return mailsvc.Address{
		ID: "id", Email: email, Type: kind, Status: status, Order: order,
		Send: 1, Receive: 1, HasKeys: true,
	}
}

func TestAddressVerbsTakeOnlyWhatTheyCanAct(t *testing.T) {
	tests := []struct {
		name  string
		takes func(mailsvc.Address) error
		a     mailsvc.Address
		// refusal is a fragment of the sentence, or "" when the verb takes it.
		refusal string
	}{
		{"disable an address of your own", disableTakes,
			address("work@example.com", mailsvc.TypeCustomDomain, mailsvc.StatusEnabled, 3), ""},
		{"disable one already off", disableTakes,
			address("work@example.com", mailsvc.TypeCustomDomain, mailsvc.StatusDisabled, 3),
			"is already disabled"},
		{"disable your first Proton address", disableTakes,
			address("me@proton.me", mailsvc.TypeOriginal, mailsvc.StatusEnabled, 1),
			"first Proton address, which cannot be disabled"},
		{"disable your short-domain address", disableTakes,
			address("me@pm.me", mailsvc.TypePremium, mailsvc.StatusEnabled, 2),
			"short-domain address, which cannot be disabled"},

		{"enable one that is off", enableTakes,
			address("work@example.com", mailsvc.TypeCustomDomain, mailsvc.StatusDisabled, 3), ""},
		{"enable one already on", enableTakes,
			address("work@example.com", mailsvc.TypeCustomDomain, mailsvc.StatusEnabled, 3),
			"is already enabled"},

		{"delete an address of your own", deleteTakes,
			address("work@example.com", mailsvc.TypeCustomDomain, mailsvc.StatusDisabled, 3), ""},
		{"delete your first Proton address", deleteTakes,
			address("me@proton.me", mailsvc.TypeOriginal, mailsvc.StatusEnabled, 1),
			"first Proton address, which cannot be deleted"},
		{"delete your short-domain address", deleteTakes,
			address("me@pm.me", mailsvc.TypePremium, mailsvc.StatusEnabled, 2),
			"short-domain address, which cannot be deleted"},
		{"delete the default address", deleteTakes,
			address("first@example.com", mailsvc.TypeCustomDomain, mailsvc.StatusEnabled, 1),
			"default address, which cannot be deleted"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.takes(tc.a)
			switch {
			case tc.refusal == "" && err != nil:
				t.Errorf("refused an address the verb takes: %v", err)
			case tc.refusal != "" && err == nil:
				t.Error("took an address it cannot act on")
			case tc.refusal != "" && !strings.Contains(err.Error(), tc.refusal):
				t.Errorf("the refusal reads %q, want it to say %q", err, tc.refusal)
			}
		})
	}
}

// Which addresses Proton will send from is judged from the row, so an address
// that cannot be the default is refused before the order is written.
func TestOnlyAnAddressProtonSendsFromCanBeTheDefault(t *testing.T) {
	working := address("work@example.com", mailsvc.TypeCustomDomain, mailsvc.StatusEnabled, 3)
	disabled := address("off@example.com", mailsvc.TypeCustomDomain, mailsvc.StatusDisabled, 3)
	external := address("me@gmail.com", mailsvc.TypeExternal, mailsvc.StatusEnabled, 3)
	receiveOnly := working
	receiveOnly.Send = 0
	unreachable := working
	unreachable.Receive = 0

	tests := []struct {
		name    string
		a       mailsvc.Address
		refusal string
	}{
		{"an address of your own", working, ""},
		{"one that is disabled", disabled, "a disabled address cannot be the default"},
		{"an external one", external, "external address, which cannot be the default"},
		{"one that cannot send", receiveOnly, "receive-only, so it cannot be the default"},
		{"one that cannot receive", unreachable, "cannot receive mail, so it cannot be the default"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := defaultTakes(tc.a)
			switch {
			case tc.refusal == "" && err != nil:
				t.Errorf("refused an address that can be the default: %v", err)
			case tc.refusal != "" && err == nil:
				t.Error("took an address Proton would not send from")
			case tc.refusal != "" && !strings.Contains(err.Error(), tc.refusal):
				t.Errorf("the refusal reads %q, want it to say %q", err, tc.refusal)
			}
		})
	}
}

// Naming some of the addresses moves those to the front and leaves the rest as
// they were, which is what makes one name mean "make this the default".
func TestReorderMovesTheNamedAddressesToTheFront(t *testing.T) {
	all := []mailsvc.Address{
		{ID: "a", Email: "a@example.com"},
		{ID: "b", Email: "b@example.com"},
		{ID: "c", Email: "c@example.com"},
	}

	order := reordered(all, []mailsvc.Address{all[2]})
	if got := emails(order); got != "c@example.com a@example.com b@example.com" {
		t.Errorf("one named address left the order %q", got)
	}

	order = reordered(all, []mailsvc.Address{all[1], all[2], all[0]})
	if got := emails(order); got != "b@example.com c@example.com a@example.com" {
		t.Errorf("a whole order came out as %q", got)
	}
}

// An order the account is already in is refused rather than reported as a
// change nobody made.
func TestReorderRefusesAnOrderTheAccountIsAlreadyIn(t *testing.T) {
	all := []mailsvc.Address{
		{ID: "a", Email: "a@example.com"},
		{ID: "b", Email: "b@example.com"},
	}

	named := []mailsvc.Address{all[0]}
	err := reorderTakes(all, reordered(all, named), named)
	if err == nil || !strings.Contains(err.Error(), "already the default address") {
		t.Errorf("naming the default address again = %v, want it to say so", err)
	}

	named = []mailsvc.Address{all[0], all[1]}
	err = reorderTakes(all, reordered(all, named), named)
	if err == nil || !strings.Contains(err.Error(), "already in that order") {
		t.Errorf("writing the order it is in = %v, want it to say so", err)
	}

	named = []mailsvc.Address{all[1]}
	if err := reorderTakes(all, reordered(all, named), named); err != nil {
		t.Errorf("a reorder that changes the default was refused: %v", err)
	}
}

func emails(addrs []mailsvc.Address) string {
	var out []string
	for _, a := range addrs {
		out = append(out, a.Email)
	}
	return strings.Join(out, " ")
}

// EMAIL is read the way Proton files an address, and a malformed one is wrong
// before anybody is signed in.
func TestSplitAddressReadsAnAddressOrRefusesIt(t *testing.T) {
	local, domain, err := splitAddress(" work@example.com ")
	if err != nil {
		t.Fatalf("splitAddress: %v", err)
	}
	if local != "work" || domain != "example.com" {
		t.Errorf("split into %q and %q", local, domain)
	}
	for _, bad := range []string{"work", "@example.com", "work@", "work@a@b", "wo rk@example.com", ""} {
		if _, _, err := splitAddress(bad); err == nil {
			t.Errorf("%q was read as an email address", bad)
		}
	}
}
