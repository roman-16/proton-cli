package kit

import (
	"io"
	"os"
	"strings"

	"github.com/roman-16/proton-cli/internal/app"
	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/spf13/cobra"
)

// Secrets declares where the secret parts of a stored item may be read from.
//
// A password, a card number, a TOTP URI and a hidden field are all things argv
// must never carry: it is readable by every user on the machine through ps, and
// it survives in shell history and in unit files. So they arrive the way the
// account password does, as a path, with `-` for standard input.
//
// They name their field because an item has several - a card has three - which
// is why one flag carries all of them rather than one per field. Standard input
// can only be read once, so at most one of them may ask for it.
type Secrets struct {
	files      []string
	stdinField string
	stdin      io.Reader
}

// The one thing this says, wherever it appears.
const SecretFileUsage = "Read a secret field from a file, as NAME=FILE; - is stdin (repeatable)"

// Declare adds the flag to a command. Call Supply from its steps.
func (s *Secrets) Declare(c *cobra.Command) {
	c.Flags().StringArrayVar(&s.files, "secret-file", nil, SecretFileUsage)
}

// Supply claims standard input if a path says so, before anything else can
// drain it. Two fields reading the same stream would give the second an empty
// one, so that is refused here rather than discovered later.
func (s *Secrets) Supply(c *Invocation) error {
	for _, pair := range s.files {
		field, path, err := splitSecret(pair)
		if err != nil {
			return err
		}
		if path != app.Stdin {
			continue
		}
		if s.stdinField != "" {
			return errs.Problemf("%s and %s both read standard input, which can only be read once.",
				"--secret-file "+s.stdinField+"=-", "--secret-file "+field+"=-").
				Hint("read one of them from a path rather than -")
		}
		r, err := c.App.Stdin("--secret-file " + field + "=" + app.Stdin)
		if err != nil {
			return err
		}
		s.stdinField, s.stdin = field, r
	}
	return nil
}

// splitSecret reads one NAME=FILE pair.
func splitSecret(pair string) (field, path string, err error) {
	field, path, ok := strings.Cut(pair, "=")
	field, path = strings.TrimSpace(field), strings.TrimSpace(path)
	if !ok || field == "" || path == "" {
		return "", "", errs.Problemf("--secret-file takes NAME=FILE, and %q is not one.", pair).
			Hint("--secret-file password=/run/secrets/github")
	}
	return field, path, nil
}

// Values reads every secret that was named, keyed by its field.
//
// A file that is not there or holds nothing is an error rather than an empty
// secret: somebody meant to put one in it, and writing an empty password over a
// real one is the mistake worth refusing.
func (s *Secrets) Values() (map[string]string, error) {
	out := make(map[string]string, len(s.files))
	for _, pair := range s.files {
		field, path, err := splitSecret(pair)
		if err != nil {
			return nil, err
		}
		if path == app.Stdin {
			b, err := io.ReadAll(s.stdin)
			if err != nil {
				return nil, errs.Problemf("Could not read from stdin: %v", err)
			}
			v := strings.TrimSpace(string(b))
			if v == "" {
				return nil, errs.Problemf("Nothing arrived on stdin.")
			}
			out[field] = v
			continue
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
	return out, nil
}
