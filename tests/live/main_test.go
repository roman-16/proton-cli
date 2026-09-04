// Package live is the suite that runs the real binary against the live Proton
// API. Everything here needs an account; everything decidable without one lives
// in a sibling package and runs in `just test-fast`.
//
// There are no mocks. Every test creates real data, verifies it, and cleans up.
package live

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/roman-16/proton-cli/tests/account"
)

// The three accounts are primary, secondary and paid.
//
// The suite creates, mutates and deletes real data. Most tests act as the
// primary; the ones that genuinely need two Proton users bring in the
// secondary; the ones Proton gates behind a subscription act as the paid one,
// under the rules in tests/paid.
//
// Each is as required as the others. An account is required because the binary
// holds tests that act as it, and a run that skipped them would report success
// for having done nothing - so there is one credential list and nothing decides
// at runtime which part of it applies.

// binaryPath is the binary under test, built once. Nothing outside runAs starts
// it: the runner is the one place a target account is chosen, so it is the one
// place the choice can be enforced or refused.
var binaryPath string

// workDir is the per-run temp directory, where the password files live.
var workDir string

type testAccount struct {
	account.Account
	// The files each of the account's secrets is written to for the run, so a
	// command that has to be handed one can be.
	passwordFile string
	secondFile   string
	extraFile    string
}

// accounts is the shared declaration, given somewhere to remember the files
// this run wrote each secret to.
var accounts = func() map[string]*testAccount {
	out := map[string]*testAccount{}
	for _, a := range account.All() {
		out[a.Profile] = &testAccount{Account: a}
	}
	return out
}()

func TestMain(m *testing.M) {
	requireCredentials()

	dir, err := os.MkdirTemp("", "proton-cli-test-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create temp dir: %v\n", err)
		os.Exit(1)
	}
	workDir = dir

	binaryPath = filepath.Join(dir, "proton")
	build := exec.Command("go", "build", "-o", binaryPath, "../../cmd/proton")
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		_ = os.RemoveAll(dir)
		fmt.Fprintf(os.Stderr, "failed to build binary: %v\n", err)
		os.Exit(1)
	}

	writePasswordFiles()
	openTrace()
	// Nothing is seeded. What an account has to hold is brought about by the
	// test that reads it, so a run pays only for the fixtures it asks for and
	// one that cannot be made fails those tests rather than all of them.
	// `just seed` fills the free accounts by hand, through the same declaration.
	signIn()
	// The paid account is somebody's, so a run settles what it must already
	// hold, records what it looked like before anything touches it, and checks
	// it came back afterwards.
	requirePaidFixtures()
	photographPaid()

	code := m.Run()

	if !paidCameBack() {
		code = 1
	}

	// os.Exit skips deferred funcs, so the temp-dir removal is explicit.
	closeTrace()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

// requireCredentials verifies the suite is configured before any test runs,
// exiting instantly - ahead of the expensive binary build - if it is not.
func requireCredentials() {
	if missing := account.Missing(); len(missing) > 0 {
		fmt.Fprintf(os.Stderr, "the live suite requires all of these: %s\n", strings.Join(missing, ", "))
		os.Exit(1)
	}
	if err := account.Swapped(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// writePasswordFiles puts each account's secrets where a command that has to be
// handed one can read it: the password for the commands Proton guards behind an
// elevated session, and the second and extra passwords for the sign-in.
//
// A session cannot carry elevation: Proton re-authenticates over SRP, and the
// key blob sealed at login is a one-way derivation of the password rather than
// the password itself.
func writePasswordFiles() {
	for _, a := range accounts {
		for _, secret := range []struct {
			kind  string
			value string
			into  *string
		}{
			{"password", os.Getenv(a.Password), &a.passwordFile},
			{"second", os.Getenv(a.Second), &a.secondFile},
			{"extra", os.Getenv(a.Extra), &a.extraFile},
		} {
			if secret.value == "" {
				continue
			}
			path := filepath.Join(workDir, a.Profile+"."+secret.kind)
			if err := os.WriteFile(path, []byte(secret.value), 0o600); err != nil {
				fmt.Fprintf(os.Stderr, "failed to write the %s %s file: %v\n", a.Profile, secret.kind, err)
				os.Exit(1)
			}
			*secret.into = path
		}
	}
}

// signIn brings every account to a signed-in session, which every test needs and
// nothing else can arrange for itself.
//
// A session already in place costs one read: `account login` does no SRP
// exchange while the saved one still works, so an ordinary run pays almost
// nothing for this and recovers by itself from a session that expired.
//
// Establishing one from nothing is a different matter. Proton may raise a
// CAPTCHA, `go test` gives this binary /dev/null on standard input, and a
// challenge cannot be solved by anything but a person with a browser - so a run
// that has to sign in from scratch says which command can, rather than failing
// with a page nobody is watching for.
func signIn() {
	for _, a := range account.All() {
		name := a.Profile
		a := accounts[name]
		args := []string{"account", "login", "--user", a.Address()}
		for _, secret := range []struct{ flag, file string }{
			{"--password-file", a.passwordFile},
			{"--second-password-file", a.secondFile},
			{"--extra-password-file", a.extraFile},
		} {
			if secret.file != "" {
				args = append(args, secret.flag, secret.file)
			}
		}
		_, stderr, code, err := runAs(name, nil, args...)
		if err == nil && code == 0 {
			continue
		}
		fmt.Fprint(os.Stderr, stderr)
		fmt.Fprintf(os.Stderr,
			"\ncould not sign %s in without asking anything.\n"+
				"Run `just login` once - it can open Proton's CAPTCHA page and wait for you -\n"+
				"then run this again.\n", name)
		os.Exit(1)
	}
}

// selfEmail is the primary account's address.
func selfEmail() string { return accounts[account.Primary].Address() }

// secondaryEmail is the second account's address.
func secondaryEmail() string { return accounts[account.Secondary].Address() }

// externalRecipient is a mailbox outside Proton. requireCredentials has already
// refused to start without one, so this never skips.
func externalRecipient(t *testing.T) string {
	t.Helper()
	return os.Getenv(account.ExternalRecipient)
}
