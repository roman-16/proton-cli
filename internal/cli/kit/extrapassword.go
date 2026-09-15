package kit

import (
	"github.com/spf13/cobra"
)

// ExtraPassword declares where the password protecting Pass may be read from.
//
// It is neither of the account's own secrets: it does not sign anything in and it
// opens no keys. What it buys is the scope every Pass endpoint wants, which
// Proton then grants for the life of the session - so signing in is where it is
// handed over, and a Pass command that finds the session without it asks whoever
// is at the terminal.
//
// Like a password, it arrives as a path, with `-` for standard input, or from a
// prompt. It is never a flag value: argv is readable by every user on the
// machine through ps, and it survives in shell history and in unit files.
type ExtraPassword struct {
	file string
}

// The one thing this says, wherever it appears.
const ExtraPasswordFileUsage = "Read the Pass extra password from a file, or - for stdin"

// Declare adds the flag to a command. Call Supply from its body.
func (p *ExtraPassword) Declare(c *cobra.Command) {
	c.Flags().StringVar(&p.file, "extra-password-file", "", ExtraPasswordFileUsage)
}

// Supply hands what was given to the invocation, before anything that might ask
// for it runs.
func (p *ExtraPassword) Supply(c *Invocation) error {
	return c.App.Creds.SupplyExtraPassword(p.file)
}
