// Package mail is the `proton mail` tree.
//
// The mailbox is `messages`, `conversations`, `drafts` and their attachments;
// everything you configure lives under `settings`, one subcommand per page of
// Proton's own mail settings. `protected` is the message that is in no mailbox:
// one somebody sent behind a password, read by its link.
package mail

import (
	"errors"

	"github.com/roman-16/proton-cli/internal/cli/kit"
	mailsvc "github.com/roman-16/proton-cli/internal/service/mail"
	"github.com/spf13/cobra"
)

func New() *cobra.Command {
	c := &cobra.Command{
		Use:   "mail",
		Short: "Read, write and organize mail",
	}
	c.AddCommand(messagesCmd(), conversationsCmd(), draftsCmd(), protectedCmd(), settingsCmd())
	return c
}

// wrongTable turns "that ID belongs to the other table" into a pointer at the
// command that would have worked.
//
// Messages and threads have separate ID spaces that look identical, so pasting
// one where the other belongs is an ordinary mistake rather than a misuse. Saying
// which command to run makes it a two-second correction.
func wrongTable(err error, verb string) error {
	if err == nil {
		return nil
	}
	var wte *mailsvc.WrongTableError
	if !errors.As(err, &wte) {
		return err
	}
	var tree string
	switch wte.Kind {
	case "conversation":
		tree = "mail conversations"
	case "message":
		tree = "mail messages"
	default:
		return err
	}
	return kit.Fail("That ID is a %s, not a %s.", wte.Kind, mailsvc.OppositeKind(wte.Kind)).
		Hint("proton " + tree + " " + verb + " " + wte.ID).Exit(3)
}
