// Command login brings every test account to a signed-in session.
//
// It exists because signing in is the one thing here that may need a person.
// Proton's anti-abuse system raises a CAPTCHA at login often enough that any
// account left signed out will eventually meet one, and a challenge belongs to
// the run it was issued to - it cannot be solved in advance, reused, or answered
// by anything but a human with a browser. So the CLI opens the page, prints the
// address for any other device, and waits.
//
// Nothing else in this repository may wait like that. The suite and the seed run
// unattended and must fail rather than hang, and a session they find already in
// place costs them one read - `account login` does no SRP exchange when the
// saved session still works. That is the division of labour: this command is
// where a password and a CAPTCHA are answered, and everything else assumes the
// answer was given.
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/roman-16/proton-cli/tests/account"
	"golang.org/x/term"
)

func main() {
	only := flag.String("profile", "", "sign in one account rather than all of them")
	flag.Parse()

	wanted := account.All()
	if *only != "" {
		a := account.Get(*only)
		if a.Profile == "" {
			fail("there is no test account called %q", *only)
		}
		wanted = []account.Account{a}
	}

	if missing := missingSecrets(wanted); len(missing) > 0 {
		fail("set %s\n(.env.example lists every variable the accounts need)", strings.Join(missing, " "))
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		fail("`just login` needs a terminal: Proton may ask for a CAPTCHA, and only a person can answer one.\n" +
			"For an unattended run, sign in once here and let the session be found, or pass PROTON_VERIFIED.")
	}

	work, err := os.MkdirTemp("", "proton-cli-login-*")
	if err != nil {
		fail("%v", err)
	}
	defer func() { _ = os.RemoveAll(work) }()

	for _, a := range wanted {
		if err := signIn(work, a); err != nil {
			fail("%v", err)
		}
	}
	// What each profile ended up as, read off the disk rather than claimed.
	if err := show(); err != nil {
		fail("%v", err)
	}
}

// signIn attaches one account to its profile.
//
// Every secret goes in a file of its own and none down standard input, which is
// left free for the one answer only a person can give. Standard input has one
// reader, so a password sent that way is a password spent instead of a question
// asked - which is exactly how an unattended sign-in came to be unable to
// present a CAPTCHA.
//
// The streams are inherited rather than captured, because a command being waited
// on has to be able to say so. A page to open, held back until the run failed,
// is a run that looks hung.
func signIn(work string, a account.Account) error {
	args := []string{"account", "login", "--user", os.Getenv(a.User)}
	for _, secret := range []struct{ kind, flag, variable string }{
		{"password", "--password-file", a.Password},
		{"second", "--second-password-file", a.Second},
		{"extra", "--extra-password-file", a.Extra},
	} {
		if secret.variable == "" {
			continue
		}
		file, err := secretFile(work, a.Profile, secret.kind, os.Getenv(secret.variable))
		if err != nil {
			return err
		}
		args = append(args, secret.flag, file)
	}

	fmt.Fprintf(os.Stderr, "\n%s:\n", a.Profile)
	cmd := exec.Command(binary(), args...)
	cmd.Env = append(os.Environ(), "PROTON_PROFILE="+a.Profile)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stderr, os.Stderr
	if err := cmd.Run(); err != nil {
		// The CLI has said what happened, on the stream it happened on, and for
		// a verification it has printed the page and the token as well. Repeating
		// any of it would make one failure look like two.
		return fmt.Errorf("could not sign %s in", a.Profile)
	}
	return nil
}

// show lists what is now signed in, so the answer is what is on the disk rather
// than what this command believes it did.
func show() error {
	fmt.Fprintln(os.Stderr)
	cmd := exec.Command(binary(), "account", "profiles", "list")
	cmd.Env = append(os.Environ(), "PROTON_NO_INPUT=1")
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	return cmd.Run()
}

// secretFile puts one secret somewhere only this user can read it.
func secretFile(work, profile, kind, value string) (string, error) {
	path := filepath.Join(work, profile+"."+kind)
	return path, os.WriteFile(path, []byte(value), 0o600)
}

// missingSecrets names every variable these accounts need and nobody set.
func missingSecrets(wanted []account.Account) []string {
	var missing []string
	for _, a := range wanted {
		for _, v := range a.Secrets() {
			if os.Getenv(v) == "" {
				missing = append(missing, v)
			}
		}
	}
	return missing
}

// binary is the proton to drive. `just` builds it first.
func binary() string {
	if v := os.Getenv("PROTON_CLI"); v != "" {
		return v
	}
	return "./proton"
}

func fail(format string, a ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", a...)
	os.Exit(1)
}
