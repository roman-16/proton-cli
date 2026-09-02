package kit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A secret arrives as NAME=FILE, so what it is for travels with where it is.
func TestSecretsReadEveryFileItWasGiven(t *testing.T) {
	dir := t.TempDir()
	writeSecret(t, dir, "pan", "4111111111111111\n")
	writeSecret(t, dir, "cvv", "123")

	var s Secrets
	s.files = []string{
		"number=" + filepath.Join(dir, "pan"),
		"cvv=" + filepath.Join(dir, "cvv"),
	}
	got, err := s.Values()
	if err != nil {
		t.Fatalf("Values: %v", err)
	}
	if got["number"] != "4111111111111111" {
		t.Errorf("number = %q, and the trailing newline should be gone", got["number"])
	}
	if got["cvv"] != "123" {
		t.Errorf("cvv = %q", got["cvv"])
	}
}

// A name may be a custom field, section and all, which is the same token
// --field takes.
func TestASecretMayNameACustomField(t *testing.T) {
	dir := t.TempDir()
	writeSecret(t, dir, "wifi", "hunter2")

	var s Secrets
	s.files = []string{"Network/Key=" + filepath.Join(dir, "wifi")}
	got, err := s.Values()
	if err != nil {
		t.Fatalf("Values: %v", err)
	}
	if got["Network/Key"] != "hunter2" {
		t.Errorf("the field came back as %v", got)
	}
}

// The refusals: a token that is not NAME=FILE, a file that is not there, and a
// file with nothing in it. Writing an empty password over a real one is the
// mistake worth refusing rather than performing.
func TestSecretsRefuseWhatCannotBeASecret(t *testing.T) {
	dir := t.TempDir()
	writeSecret(t, dir, "empty", "   \n")

	for _, tc := range []struct{ name, arg, want string }{
		{"no field", "/run/secrets/github", "NAME=FILE"},
		{"no file", "password=", "NAME=FILE"},
		{"missing file", "password=" + filepath.Join(dir, "nope"), "Could not read"},
		{"empty file", "password=" + filepath.Join(dir, "empty"), "is empty"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var s Secrets
			s.files = []string{tc.arg}
			_, err := s.Values()
			if err == nil {
				t.Fatalf("%q was accepted", tc.arg)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("%q says %q, want something about %q", tc.arg, err, tc.want)
			}
		})
	}
}

// Standard input is claimed by name, so the field it belongs to is known before
// anything reads it.
func TestASecretMayComeFromStdin(t *testing.T) {
	var s Secrets
	s.stdinField = "password"
	s.stdin = strings.NewReader("hunter2\n")
	got, err := s.Values()
	if err != nil {
		t.Fatalf("Values: %v", err)
	}
	if got["password"] != "hunter2" {
		t.Errorf("the password came back as %v", got)
	}

	s = Secrets{stdinField: "password", stdin: strings.NewReader("\n")}
	if _, err := s.Values(); err == nil {
		t.Error("an empty stream was accepted as a password")
	}
}

func writeSecret(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
