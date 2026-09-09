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
