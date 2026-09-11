package kit

import (
	"os"

	"github.com/roman-16/proton-cli/internal/account/keys"
	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/spf13/cobra"
)

// Recovery declares where the secret that opens keys a password reset locked
// may come from: the password from before the reset, the recovery phrase, or a
// recovery file - the three things Proton's own recovery dialog accepts.
//
// They are one choice, so the flags of one method exclude the others, and a
// command line naming none of them means the previous password, asked for at a
// terminal. A phrase is asked for only when the command line says so, because
// the prompt for one secret must never be what somebody types the other into.
//
// Like every secret here, the password and the phrase are read from a pipe, a
// file or a prompt and never from a flag value: argv is readable by every user
// on the machine through ps, and it survives in shell history and in unit files.
// The recovery file is a path to something encrypted, which argv may carry.
type Recovery struct {
	previousFile  string
	previousStdin bool
	phrase        bool
	phraseFile    string
	phraseStdin   bool
	file          string
}

// The one thing each of these says, wherever it appears.
const (
	PreviousPasswordFileUsage  = "Read the password from before the reset from a file"
	PreviousPasswordStdinUsage = "Read the password from before the reset from stdin"
	RecoveryPhraseUsage        = "Recover with the recovery phrase, asked for at the prompt"
	RecoveryPhraseFileUsage    = "Read the recovery phrase from a file"
	RecoveryPhraseStdinUsage   = "Read the recovery phrase from stdin"
	RecoveryFileUsage          = "Recover with a recovery file downloaded from Proton"
)

// Declare adds the flags to a command. Call Supply from its body.
func (r *Recovery) Declare(c *cobra.Command) {
	f := c.Flags()
	f.StringVar(&r.previousFile, "previous-password-file", "", PreviousPasswordFileUsage)
	f.BoolVar(&r.previousStdin, "previous-password-stdin", false, PreviousPasswordStdinUsage)
	f.BoolVar(&r.phrase, "recovery-phrase", false, RecoveryPhraseUsage)
	f.StringVar(&r.phraseFile, "recovery-phrase-file", "", RecoveryPhraseFileUsage)
	f.BoolVar(&r.phraseStdin, "recovery-phrase-stdin", false, RecoveryPhraseStdinUsage)
	f.StringVar(&r.file, "recovery-file", "", RecoveryFileUsage)
	// One method per run, and one source per secret.
	methods := [][]string{
		{"previous-password-file", "previous-password-stdin"},
		{"recovery-phrase", "recovery-phrase-file", "recovery-phrase-stdin"},
		{"recovery-file"},
	}
	for i, chosen := range methods {
		for _, other := range methods[i+1:] {
			for _, a := range chosen {
				for _, b := range other {
					c.MarkFlagsMutuallyExclusive(a, b)
				}
			}
		}
	}
	c.MarkFlagsMutuallyExclusive("previous-password-file", "previous-password-stdin")
	c.MarkFlagsMutuallyExclusive("recovery-phrase-file", "recovery-phrase-stdin")
}

// Supply hands what was given to the invocation, before anything that might ask
// for it runs.
func (r *Recovery) Supply(c *Invocation) error {
	if err := c.App.Creds.SupplyPreviousPassword(r.previousFile, r.previousStdin); err != nil {
		return err
	}
	return c.App.Creds.SupplyRecoveryPhrase(r.phraseFile, r.phraseStdin)
}

// Offered reports whether the secret arrived on the command line, so a command
// can judge it before it asks Proton anything - and never prompts for one it is
// about to refuse.
func (r *Recovery) Offered() bool {
	return r.previousFile != "" || r.previousStdin || r.phraseFile != "" || r.phraseStdin || r.file != ""
}

// UsesPhrase reports whether the recovery phrase is the method chosen.
func (r *Recovery) UsesPhrase() bool { return r.phrase || r.phraseFile != "" || r.phraseStdin }

// UsesFile reports whether a recovery file is the method chosen.
func (r *Recovery) UsesFile() bool { return r.file != "" }

// Secret reads the chosen secret, asking for it if there is somebody to ask, and
// judges what can be judged here: a phrase that is not one is refused before
// anything is sent.
func (r *Recovery) Secret(c *Invocation) (keys.Secret, error) {
	switch {
	case r.UsesFile():
		b, err := os.ReadFile(r.file)
		if err != nil {
			return nil, errs.Problemf("Could not read %s: %v", r.file, err)
		}
		return keys.RecoveryFile(b), nil
	case r.UsesPhrase():
		phrase, err := c.App.Creds.RecoveryPhrase()
		if err != nil {
			return nil, err
		}
		return keys.Phrase(phrase)
	}
	password, err := c.App.Creds.PreviousPassword()
	if err != nil {
		return nil, err
	}
	return keys.PreviousPassword(password), nil
}
