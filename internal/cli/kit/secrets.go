package kit

import (
	"io"
	"os"
	"strings"

	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/spf13/cobra"
)

// Secrets declares where the secret parts of a stored item may be read from.
//
// A password, a card number, a TOTP URI and a hidden field are all things argv
// must never carry: it is readable by every user on the machine through ps, and
// it survives in shell history and in unit files. So they arrive the way the
// account password does, from a file or from standard input.
//
// They name their field because an item has several - a card has three - and
// standard input can only be read once, which is why one pair of flags carries
// all of them rather than a pair per field.
type Secrets struct {
	files      []string
	stdinField string
	stdin      io.Reader
}

// The one thing each of these says, wherever it appears.
const (
	SecretFileUsage  = "Read a secret field from a file, as NAME=FILE (repeatable)"
	SecretStdinUsage = "Read the named secret field from stdin"
)

// Declare adds the flags to a command. Call Supply from its steps.
func (s *Secrets) Declare(c *cobra.Command) {
	f := c.Flags()
	f.StringArrayVar(&s.files, "secret-file", nil, SecretFileUsage)
	f.StringVar(&s.stdinField, "secret-stdin", "", SecretStdinUsage)
}

// Supply claims standard input if it was asked for, before anything else can
// drain it.
func (s *Secrets) Supply(c *Invocation) error {
	if s.stdinField == "" {
		return nil
	}
	r, err := c.App.Stdin("--secret-stdin")
	if err != nil {
		return err
	}
	s.stdin = r
	return nil
}

// Values reads every secret that was named, keyed by its field.
//
// A file that is not there or holds nothing is an error rather than an empty
// secret: somebody meant to put one in it, and writing an empty password over a
// real one is the mistake worth refusing.
func (s *Secrets) Values() (map[string]string, error) {
	out := make(map[string]string, len(s.files)+1)
	for _, pair := range s.files {
		field, path, ok := strings.Cut(pair, "=")
		field, path = strings.TrimSpace(field), strings.TrimSpace(path)
		if !ok || field == "" || path == "" {
			return nil, errs.Problemf("--secret-file takes NAME=FILE, and %q is not one.", pair).
				Hint("--secret-file password=/run/secrets/github")
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return nil, errs.Problemf("Could not read %s: %v", path, err)
		}
		v := strings.TrimSpace(string(b))
		if v == "" {
			return nil, errs.Problemf("%s is empty.", path)
		}
		out[field] = v
	}
	if s.stdinField == "" {
		return out, nil
	}
	b, err := io.ReadAll(s.stdin)
	if err != nil {
		return nil, errs.Problemf("Could not read from stdin: %v", err)
	}
	v := strings.TrimSpace(string(b))
	if v == "" {
		return nil, errs.Problemf("Nothing arrived on stdin.")
	}
	out[strings.TrimSpace(s.stdinField)] = v
	return out, nil
}
