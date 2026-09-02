package offline

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// What a stored secret arrives as is judgeable from the command line, so it is
// judged there: a token that is not NAME=FILE, a file that is not there, and a
// file with nothing in it - writing an empty password over a real one being the
// mistake worth refusing rather than performing.

func TestASecretFileIsJudgedBeforeTheNetwork(t *testing.T) {
	dir := t.TempDir()
	empty := filepath.Join(dir, "empty")
	if err := os.WriteFile(empty, []byte("\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	refuses(t, 1, []string{"pass", "items", "create", "--name", "GitHub",
		"--secret-file", "/run/secrets/github"}, "NAME=FILE")
	refuses(t, 1, []string{"pass", "items", "create", "--name", "GitHub",
		"--secret-file", "password=" + filepath.Join(dir, "nope")}, "Could not read")
	refuses(t, 1, []string{"pass", "items", "create", "--name", "GitHub",
		"--secret-file", "password=" + empty}, "is empty")
}

// A password may be made or given, and asking for both leaves it unsaid which
// one would have been stored.
func TestAPasswordIsEitherMadeOrGiven(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "pw")
	if err := os.WriteFile(file, []byte("hunter2"), 0o600); err != nil {
		t.Fatal(err)
	}
	refuses(t, 1, []string{"pass", "items", "create", "--name", "GitHub",
		"--generate-password", "--secret-file", "password=" + file}, "both set the password")
}

// A secret is never a flag value, so the flags that used to carry one are words
// this CLI does not know - and it says so the way it does for any other.
func TestASecretIsNotAFlagValue(t *testing.T) {
	for _, flag := range []string{"--password", "--cvv", "--pin", "--number", "--private-key",
		"--totp-uri", "--hidden", "--totp-field"} {
		_, stderr, code := run(t, "pass", "items", "create", "--name", "GitHub", flag, "hunter2")
		if code != 1 {
			t.Errorf("%s: exit %d, want 1", flag, code)
		}
		if !strings.Contains(stderr, "Unknown flag: "+flag) {
			t.Errorf("%s: stderr says %q", flag, truncate(stderr))
		}
	}
	refuses(t, 1, []string{"drive", "items", "share", "link", "/Documents", "--password", "hunter2"},
		"Unknown flag: --password")
	refuses(t, 1, []string{"mail", "messages", "send", "--to", "jane@example.com",
		"--subject", "Hi", "--body", "text", "--eo-password", "hunter2"},
		"Unknown flag: --eo-password")
}

// The password a stranger types is judged where every other value is: against
// the bounds Proton's own clients hold it to, before a request goes anywhere.
func TestAPasswordSetOnSomethingIsJudgedBeforeTheNetwork(t *testing.T) {
	dir := t.TempDir()
	write := func(name, contents string) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	empty := write("empty", "\n")
	tooLong := write("long", strings.Repeat("x", 51))
	tooShort := write("short", "secret7")

	link := []string{"drive", "items", "share", "link", "/Documents"}
	refuses(t, 1, append(link, "--link-password-file", filepath.Join(dir, "nope")), "Could not read")
	refuses(t, 1, append(link, "--link-password-file", empty), "is empty")
	refuses(t, 1, append(link, "--link-password-file", tooLong), "at most 50 characters")
	refuses(t, 1, append(link, "--clear-link-password", "--link-password-file", tooLong),
		"none of the others")
	// A duration of nothing is what an arithmetic mistake produces, so it is
	// refused rather than read as the word for no expiry.
	refuses(t, 1, append(link, "--expires", "0s"), "longer than nothing", "--expires never")
	refuses(t, 1, append(link, "--expires", "soon"), "--expires", "--expires never")

	send := []string{"mail", "messages", "send", "--to", "jane@example.com", "--subject", "Hi",
		"--body", "text"}
	refuses(t, 1, append(send, "--eo-password-file", tooShort), "at least 8 characters")
	refuses(t, 1, append(send, "--eo-password-file", empty), "is empty")
}

// Standard input has one reader, and the way out of a collision is the file the
// same secret can be read from - which is a different flag for each of them.
func TestAStdinCollisionNamesTheFlagThatMovesOffTheStream(t *testing.T) {
	_, stderr, code := runWithStdin(t, "long enough to be a password",
		"mail", "messages", "send", "--to", "jane@example.com",
		"--subject", "Hi", "--body", "-", "--eo-password-stdin")
	if code != 1 {
		t.Errorf("exit %d, want 1\nstderr: %s", code, truncate(stderr))
	}
	for _, phrase := range []string{"both read standard input", "--eo-password-file"} {
		if !strings.Contains(stderr, phrase) {
			t.Errorf("stderr does not say %q: %s", phrase, truncate(stderr))
		}
	}
}

// A password made here never has to travel, and the flags that shape it are the
// ones `pass generate` takes.
func TestAGeneratedPasswordIsShapedLikeAnyOther(t *testing.T) {
	stdout, _, code := run(t, "pass", "generate", "--words", "4", "--separator", "space", "--no-digits")
	if code != 0 {
		t.Fatalf("generate --words: exit %d", code)
	}
	_, made, ok := strings.Cut(strings.TrimSpace(stdout), ":")
	if !ok {
		t.Fatalf("generate --words printed %q", strings.TrimSpace(stdout))
	}
	if words := strings.Fields(made); len(words) != 4 {
		t.Errorf("--words 4 made %q", strings.TrimSpace(made))
	}
	if strings.ContainsAny(made, "0123456789") {
		t.Errorf("--no-digits made %q", strings.TrimSpace(made))
	}
	refuses(t, 1, []string{"pass", "generate", "--separator", "dots"}, "separator", "hyphen")
}
