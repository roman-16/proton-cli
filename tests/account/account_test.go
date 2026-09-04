package account

import (
	"slices"
	"strings"
	"testing"
)

// Every account is required, and the list says so once.
//
// An optional credential produces a run that exits 0 having never tried what it
// could not reach, which is the same wrong answer as a listing that
// under-reports. So the list is every secret of every account, plus the mailbox
// outside Proton, and nothing decides at runtime which subset applies.
func TestRequiredNamesEverySecretOfEveryAccount(t *testing.T) {
	required := Required()
	for _, a := range All() {
		for _, v := range a.Secrets() {
			if !slices.Contains(required, v) {
				t.Errorf("%s needs %s and it is not required", a.Profile, v)
			}
		}
	}
	if !slices.Contains(required, ExternalRecipient) {
		t.Errorf("%s is not required", ExternalRecipient)
	}
	if !slices.IsSorted(required) {
		t.Errorf("the required list should be sorted, so a report of what is unset reads the same every time: %v", required)
	}
	if got := len(required); got != 9 {
		t.Errorf("nine variables are required, the list holds %d: %v", got, required)
	}
}

// Every variable is named for the account it belongs to and kept clear of
// anything the binary itself reads.
func TestEveryVariableIsPrefixed(t *testing.T) {
	for _, v := range Required() {
		if !strings.HasPrefix(v, "PROTON_CLI_TEST_") {
			t.Errorf("%s is not prefixed, so it could collide with something the CLI reads", v)
		}
	}
}

// Two accounts are the suite's own and one is not, which is what decides
// whether anything may fill it with test data.
func TestOnlyTheSuitesOwnAccountsAreFree(t *testing.T) {
	free := Free()
	if len(free) != 2 {
		t.Fatalf("Free() holds %d accounts, want the two kept for the suite", len(free))
	}
	for _, a := range free {
		if a.Profile == Paid {
			t.Error("the paid account is somebody's, and filling it with test data is not a thing to do to it")
		}
	}
	if len(All()) != len(free)+1 {
		t.Errorf("All() holds %d accounts, want the free ones and the paid one", len(All()))
	}
}

// Only the account in two-password mode carries the secrets that mode needs, and
// a sign-in wants them in the order Proton asks for them.
func TestSecretsComeInTheOrderASignInWantsThem(t *testing.T) {
	if got := Get(Primary).Secrets(); !slices.Equal(got, []string{
		"PROTON_CLI_TEST_PRIMARY_USER", "PROTON_CLI_TEST_PRIMARY_PASSWORD",
	}) {
		t.Errorf("the primary account's secrets are %v", got)
	}
	if got := Get(Secondary).Secrets(); len(got) != 4 {
		t.Errorf("the secondary account is in two-password mode with an extra password on Pass, so it has four secrets: %v", got)
	}
	if got := Get(Paid).Secrets(); len(got) != 2 {
		t.Errorf("the paid account has no second or extra password: %v", got)
	}
}

func TestGetAnUnknownProfileIsEmpty(t *testing.T) {
	if got := Get("nobody"); got.Profile != "" {
		t.Errorf("Get(%q) = %+v, want the zero account", "nobody", got)
	}
}

func TestMissingNamesWhatIsUnset(t *testing.T) {
	for _, v := range Required() {
		t.Setenv(v, "set")
	}
	if got := Missing(); len(got) != 0 {
		t.Errorf("everything is set and Missing() says %v", got)
	}
	t.Setenv(Get(Paid).Password, "")
	if got := Missing(); !slices.Equal(got, []string{Get(Paid).Password}) {
		t.Errorf("Missing() = %v, want just the paid password", got)
	}
}

// A password put in the address variable is a failed login attempt against a
// real account, and failed attempts are what Proton counts before it starts
// demanding a CAPTCHA. Neither value may appear in the complaint.
func TestSwappedCredentialsAreRefusedWithoutPrintingThem(t *testing.T) {
	for _, v := range Required() {
		t.Setenv(v, "set")
	}
	primary := Get(Primary)
	t.Setenv(primary.User, "hunter2")
	t.Setenv(primary.Password, "somebody@proton.me")

	err := Swapped()
	if err == nil {
		t.Fatal("an address in the password variable was accepted")
	}
	for _, name := range []string{primary.User, primary.Password} {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("the complaint does not name %s: %v", name, err)
		}
	}
	for _, secret := range []string{"hunter2", "somebody@proton.me"} {
		if strings.Contains(err.Error(), secret) {
			t.Errorf("the complaint printed a credential: %v", err)
		}
	}
}

// Proton takes a bare username as well as an address, so an address is not
// required - only the pair being the wrong way round is.
func TestABareUsernameIsNotSwapped(t *testing.T) {
	for _, v := range Required() {
		t.Setenv(v, "set")
	}
	primary := Get(Primary)
	t.Setenv(primary.User, "somebody")
	t.Setenv(primary.Password, "hunter2")
	if err := Swapped(); err != nil {
		t.Errorf("a bare username was reported as swapped: %v", err)
	}
}

func TestIsAddress(t *testing.T) {
	for _, v := range []string{"a@b.com", "somebody@proton.me", "x.y@sub.example.co.uk"} {
		if !isAddress(v) {
			t.Errorf("%q should read as an address", v)
		}
	}
	for _, v := range []string{"", "somebody", "hunter2", "@proton.me", "a@b", "no-at-sign.com"} {
		if isAddress(v) {
			t.Errorf("%q should not read as an address", v)
		}
	}
}
