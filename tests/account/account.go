// Package account declares the Proton accounts this repository's tooling acts
// as, and nothing else.
//
// One declaration, because there were two: the suite's and the seed's, each
// naming the same accounts and the same variables, and each free to drift from
// the other. Whichever of them a reader found first looked authoritative.
//
// Signing in, filling an account with fixtures, and running tests against it are
// three different jobs with three different callers, and the thing they have in
// common is which accounts exist and what each one's secrets are called.
package account

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
	// see tests/paid_test.go.
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

// Free are the two accounts kept for this suite and nothing else. They may be
// created in, written to, filled with fixtures and swept.
//
// The seed acts on these and only these, which is what
// TestNothingSeedsThePaidAccount checks: a paid account belongs to somebody, and
// filling it with test data is not a thing to do to it.
func Free() []Account { return []Account{primary, secondary} }

// All are every account the tooling can sign in, which is what `just login`
// establishes sessions for. Signing an account in is not acting on it.
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
