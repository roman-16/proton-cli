// Package account declares the Proton accounts this repository's tooling acts
// as, and nothing else.
//
// One declaration, because signing in, filling an account with fixtures and
// running tests against it are three different jobs with three different
// callers, and the thing they have in common is which accounts exist and what
// each one's secrets are called.
package account

import (
	"fmt"
	"os"
	"slices"
	"strings"
)

// Name is a profile, which is also how the account is referred to throughout.
type Name = string

const (
	// Primary is what almost every test acts as.
	Primary Name = "primary"
	// Secondary is the second Proton user, for the scenarios that genuinely
	// need two - accepting a share, receiving mail, answering an invitation.
	//
	// It is in Proton's two-password mode, which is the only way the suite
	// reaches that mode at all: the secret that opens its keys is not the one
	// that signs it in. Its Pass carries an extra password for the same reason.
	Secondary Name = "secondary"
	// Paid is an account somebody actually uses, for what Proton gates behind a
	// subscription. What may be done to it is far narrower than the other two;
	// see the tests/paid package.
	Paid Name = "paid"
)

// Account is one account and the variables its secrets arrive in. No secret is
// held here - only the name of the variable carrying it - so this declaration
// can be read, printed and tested against freely.
type Account struct {
	Profile  Name
	User     string
	Password string
	// Second names the account's second password, set for the account in
	// two-password mode and empty for the rest.
	Second string
	// Extra names the password that account's Pass is protected with, set for
	// the account that has one and empty for the rest.
	Extra string
}

// Secrets are the variables this account needs, in the order a sign-in wants
// them, skipping the ones it has none of.
func (a Account) Secrets() []string {
	out := []string{a.User, a.Password}
	for _, v := range []string{a.Second, a.Extra} {
		if v != "" {
			out = append(out, v)
		}
	}
	return out
}

// Address is the account's own address, which is also how it signs in.
func (a Account) Address() string { return os.Getenv(a.User) }

var (
	primary = Account{
		Profile:  Primary,
		User:     "PROTON_CLI_TEST_PRIMARY_USER",
		Password: "PROTON_CLI_TEST_PRIMARY_PASSWORD",
	}
	secondary = Account{
		Profile:  Secondary,
		User:     "PROTON_CLI_TEST_SECONDARY_USER",
		Password: "PROTON_CLI_TEST_SECONDARY_PASSWORD",
		Second:   "PROTON_CLI_TEST_SECONDARY_SECOND_PASSWORD",
		Extra:    "PROTON_CLI_TEST_SECONDARY_EXTRA_PASSWORD",
	}
	paid = Account{
		Profile:  Paid,
		User:     "PROTON_CLI_TEST_PAID_USER",
		Password: "PROTON_CLI_TEST_PAID_PASSWORD",
	}
)

// ExternalRecipient names a mailbox outside Proton.
//
// It is required for the reason the accounts are: encrypting to somebody with
// no Proton account, and emailing an invitation to an attendee with no Proton
// calendar, are branches no Proton account can enter. A run without it would
// pass having never tried them. It has to be a mailbox that accepts mail - a
// fake @example.com address bounces and litters the inbox with MAILER-DAEMON
// returns.
const ExternalRecipient = "PROTON_CLI_TEST_EXTERNAL_RECIPIENT"

// Free are the two accounts kept for this suite and nothing else. They may be
// created in, written to, filled with fixtures and swept.
//
// The seed acts on these and only these: a paid account belongs to somebody,
// and filling it with test data is not a thing to do to it.
func Free() []Account { return []Account{primary, secondary} }

// All are every account the suite acts as. Each is as required as the others,
// because the suite holds tests that act as each one and a run that skipped
// them would report success for having done nothing.
func All() []Account { return append(Free(), paid) }

// Get returns one account by profile.
func Get(name Name) Account {
	for _, a := range All() {
		if a.Profile == name {
			return a
		}
	}
	return Account{}
}

// Required is every variable the suite cannot run without, sorted, so a report
// of what is unset reads the same every time.
func Required() []string {
	out := []string{ExternalRecipient}
	for _, a := range All() {
		out = append(out, a.Secrets()...)
	}
	slices.Sort(out)
	return out
}

// Missing are the required variables that are unset.
func Missing() []string {
	var out []string
	for _, v := range Required() {
		if os.Getenv(v) == "" {
			out = append(out, v)
		}
	}
	return out
}

// Swapped catches an address put in a password variable and a password put in
// the address one.
//
// Signing in with them the wrong way round is a failed login attempt against a
// real account, which is what Proton's anti-abuse system counts before it
// starts demanding a CAPTCHA - so it is worth refusing to start over. Neither
// value is ever returned: the message says which variables to look at.
func Swapped() error {
	for _, a := range All() {
		user, password := os.Getenv(a.User), os.Getenv(a.Password)
		if user == "" || password == "" {
			continue
		}
		// Proton takes a bare username as well as an address, so an address is
		// not required. Only the pair being the wrong way round is.
		if !isAddress(user) && isAddress(password) {
			return fmt.Errorf("%s and %s look swapped: the password holds an address and the user does not",
				a.User, a.Password)
		}
	}
	return nil
}

// isAddress reports whether a value is shaped like an email address.
func isAddress(v string) bool {
	at := strings.LastIndex(v, "@")
	return at > 0 && strings.Contains(v[at:], ".")
}
