package kit

import (
	"github.com/roman-16/proton-cli/internal/app"
	"github.com/spf13/cobra"
)

// NewPassword declares where the password a command is setting may be read
// from.
//
// It sits beside Reauth rather than inside it because the two secrets are
// different for the length of the command: one proves who this is, the other is
// what will be written. A single flag for both could only ever set a password
// to itself.
//
// Like every secret here it arrives as a path, with `-` for standard input, or
// from a prompt that asks twice. It is never a flag value: argv is readable by
// every user on the machine through ps, and it survives in shell history and in
// unit files.
type NewPassword struct {
	file string
}

// The one thing this says, wherever it appears.
const NewPasswordFileUsage = "Read the password being set from a file, or - for stdin"

// Declare adds the flag to a command. Call Supply from its body.
func (p *NewPassword) Declare(c *cobra.Command) {
	c.Flags().StringVar(&p.file, "new-password-file", "", NewPasswordFileUsage)
}

// Supply hands what was given to the invocation, before anything that might ask
// for it runs.
func (p *NewPassword) Supply(c *Invocation) error {
	return c.App.Creds.SupplyNewPassword(p.file)
}

// Offered reports whether the password arrived on the command line, so a
// command can judge it before it asks Proton anything - and never prompts for
// one it is about to refuse.
func (p *NewPassword) Offered() bool { return p.file != "" }

// Chosen is the password being set, held to the length Proton holds one to.
//
// Which secret it is decides what the prompt calls it, so the caller says: the
// account's password, the one it signs in with, or the one that opens its keys.
func (p *NewPassword) Chosen(c *Invocation, kind app.PasswordKind) (string, error) {
	password, err := c.App.Creds.ChoosePassword(kind)
	if err != nil {
		return "", err
	}
	if len([]rune(password)) < MinPassword {
		return "", Fail("A password needs at least eight characters.")
	}
	return password, nil
}

// MinPassword is the length Proton's own clients hold a new password to
// (passwordLengthValidator, packages/shared/lib/helpers/formValidators.ts).
const MinPassword = 8
