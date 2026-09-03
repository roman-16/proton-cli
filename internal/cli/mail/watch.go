package mail

import (
	"strings"
	"time"

	"github.com/roman-16/proton-cli/internal/cli/kit"
	mailsvc "github.com/roman-16/proton-cli/internal/service/mail"
	"github.com/roman-16/proton-cli/internal/ui"
	"github.com/spf13/cobra"
)

// senderWidth is how much of a line a sender may take before the subject
// begins. A stream is drawn a line at a time, so the width is declared rather
// than measured, and the subject lines up because of it.
const senderWidth = 20

func watchCmd() *cobra.Command {
	var folder, from, subject string
	c := &cobra.Command{
		Use:   "watch",
		Short: "Print each message as it arrives",
		Long: "Print each message as it arrives, until you stop it.\n\n" +
			"It reports what happens while it is watching, so nothing that arrived\n" +
			"beforehand comes up. A thread returning from snooze counts as arriving.\n\n" +
			"Without --folder it covers the inbox plus every folder whose notifications\n" +
			"are on, which `settings folders list` shows under NOTIFY.",
		Args: cobra.NoArgs,
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			in, err := c.App.Mail.WatchedIn(c.Ctx, folder)
			if err != nil {
				return err
			}
			opts := mailsvc.WatchOptions{In: in, From: from, Subject: subject}
			return kit.Watch(c, ui.StreamSpec[mailsvc.Message]{
				Columns: []ui.StreamColumn[mailsvc.Message]{
					{Width: 5, Cell: func(mailsvc.Message) string { return time.Now().Format("15:04") }},
					{Ref: "mail messages", Cell: func(m mailsvc.Message) string { return m.ID }},
					{Width: senderWidth, Cell: sender},
					{Handle: true, Cell: func(m mailsvc.Message) string { return m.Subject }},
				},
				Opening: "Watching " + ui.Listing(names(in)) + ". Ctrl+C to stop.",
			}, func(emit func(mailsvc.Message) error) error {
				return c.App.Mail.Watch(c.Ctx, opts, emit)
			})
		}),
	}
	registerFolder(c, &folder, "", "the ones that notify")
	c.Flags().StringVar(&from, "from", "", "Match the sender's address")
	c.Flags().StringVar(&subject, "subject", "", "Match text in the subject")
	return c
}

// sender is who a line is from: the name when there is one, since that is what
// a notification would show, and the address when there is not.
func sender(m mailsvc.Message) string {
	if strings.TrimSpace(m.FromName) != "" {
		return m.FromName
	}
	return m.FromAddress
}

func names(boxes []mailsvc.Mailbox) []string {
	out := make([]string, 0, len(boxes))
	for _, b := range boxes {
		out = append(out, b.Name)
	}
	return out
}
