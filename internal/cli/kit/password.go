package kit

import (
	"io"
	"os"
	"strings"

	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/spf13/cobra"
)

// Password is a secret a command puts on the thing it writes, rather than one
// that opens an account: what somebody else will type to read a public link or a
// message sent outside Proton.
//
// Like every other secret here it is read from a file or from standard input and
// never from a flag value: argv is readable by every user on the machine through
// ps, and it survives in shell history and in unit files.
//
// It has flags of its own rather than naming a field the way Secrets does,
// because a link has one password and a message has one - NAME=FILE is what an
// item with three of them needs, and ceremony on a set of one.
//
// What a valid one looks like is Proton's answer, not this CLI's, so each is
// declared with the bounds its own web client enforces and refuses anything
// outside them before a request goes anywhere.
type Password struct {
	name  string
	label string
	min   int
	max   int
	// clearUsage is empty for a secret that cannot be taken off again, which is
	// what decides whether the clear flag exists at all.
	clearUsage string
	fileUsage  string
	stdinUsage string

	file   string
	stdin  bool
	clear  bool
	reader io.Reader
}

// The one thing each of these says, wherever it appears.
const (
	LinkPasswordFileUsage  = "Read the public link's password from a file"
	LinkPasswordStdinUsage = "Read the public link's password from stdin"
	ClearLinkPasswordUsage = "Remove the public link's password"
	EOPasswordFileUsage    = "Read the password for recipients outside Proton from a file"
	EOPasswordStdinUsage   = "Read the password for recipients outside Proton from stdin"
)

// LinkPassword is what somebody must type to open a Drive public link.
//
// Fifty characters is where Proton's own clients stop
// (MAX_SHARED_URL_PASSWORD_LENGTH), and an empty one means the link goes back to
// opening for anyone who has the URL - which is what the clear flag says out
// loud, since a value read from a file has no way to say it.
func LinkPassword() *Password {
	return &Password{
		name: "link-password", label: "A link password", max: 50,
		fileUsage: LinkPasswordFileUsage, stdinUsage: LinkPasswordStdinUsage,
		clearUsage: ClearLinkPasswordUsage,
	}
}

// EOPassword is what a recipient outside Proton types to read a message.
//
// Proton's composer asks for at least eight characters and sets no ceiling. It
// cannot be cleared, because every send chooses afresh: a message already gone
// is not editable, and one that is still a draft carries no password until it is
// sent.
func EOPassword() *Password {
	return &Password{
		name: "eo-password", label: "A password for recipients outside Proton", min: 8,
		fileUsage: EOPasswordFileUsage, stdinUsage: EOPasswordStdinUsage,
	}
}

// Declare adds the flags to a command. Call Supply from its steps.
func (p *Password) Declare(c *cobra.Command) {
	f := c.Flags()
	f.StringVar(&p.file, p.name+"-file", "", p.fileUsage)
	f.BoolVar(&p.stdin, p.name+"-stdin", false, p.stdinUsage)
	names := []string{p.name + "-file", p.name + "-stdin"}
	if p.clearUsage != "" {
		f.BoolVar(&p.clear, "clear-"+p.name, false, p.clearUsage)
		names = append(names, "clear-"+p.name)
	}
	c.MarkFlagsMutuallyExclusive(names...)
}

// Supply claims standard input if it was asked for, before anything else can
// drain it.
func (p *Password) Supply(c *Invocation) error {
	if !p.stdin {
		return nil
	}
	r, err := c.App.Stdin("--" + p.name + "-stdin")
	if err != nil {
		return err
	}
	p.reader = r
	return nil
}

// Wanted reports whether the password was spoken about at all, which is what
// decides whether the thing being written gets one.
func (p *Password) Wanted() bool { return p.file != "" || p.stdin || p.clear }

// Cleared reports whether the password is being taken off rather than set.
func (p *Password) Cleared() bool { return p.clear }

// Value reads the password from wherever it was told to look.
//
// A file that is not there or holds nothing is an error rather than an empty
// password: somebody meant to put one in it, and writing an empty password over
// a real one is the mistake worth refusing.
func (p *Password) Value() (string, error) {
	var v string
	switch {
	case p.clear:
		return "", nil
	case p.file != "":
		b, err := os.ReadFile(p.file)
		if err != nil {
			return "", errs.Problemf("Could not read %s: %v", p.file, err)
		}
		if v = strings.TrimSpace(string(b)); v == "" {
			return "", errs.Problemf("%s is empty.", p.file)
		}
	case p.reader != nil:
		b, err := io.ReadAll(p.reader)
		if err != nil {
			return "", errs.Problemf("Could not read from stdin: %v", err)
		}
		if v = strings.TrimSpace(string(b)); v == "" {
			return "", errs.Problemf("Nothing arrived on stdin.")
		}
	default:
		return "", errs.Problemf("%s is required.", p.label).
			Hint("--" + p.name + "-file FILE, or --" + p.name + "-stdin")
	}
	if p.min > 0 && len([]rune(v)) < p.min {
		return "", errs.Problemf("%s must be at least %d characters.", p.label, p.min)
	}
	if p.max > 0 && len([]rune(v)) > p.max {
		return "", errs.Problemf("%s may be at most %d characters.", p.label, p.max)
	}
	return v, nil
}
