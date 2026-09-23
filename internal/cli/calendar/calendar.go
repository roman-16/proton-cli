// Package calendar is the `proton calendar` tree.
//
// Events live under the app; the calendars they go in live under `settings`,
// matching where Proton puts them - "Calendars" is a page of Calendar's settings,
// not part of the grid.
package calendar

import (
	"github.com/roman-16/proton-cli/internal/cli/kit"
	mailsvc "github.com/roman-16/proton-cli/internal/service/mail"
	"github.com/spf13/cobra"
)

func New() *cobra.Command {
	c := &cobra.Command{
		Use:   "calendar",
		Short: "Calendars and events",
	}
	c.AddCommand(busyTimesCmd(), eventsCmd(), invitationsCmd(), remindersCmd(), settingsCmd())
	return c
}

// sendICS mails an invitation or a reply, attaching the ICS with the iTIP method
// that tells the recipient's client what to do with it.
//
// Invitations are system mail, so they carry no signature.
func sendICS(c *kit.Invocation, to []string, subject, body, ics, method string) error {
	sender, err := c.App.Mail.ResolveSender(c.Ctx, mailsvc.SenderRequest{})
	if err != nil {
		return err
	}
	_, err = c.App.Mail.Send(c.Ctx, mailsvc.Content{
		From:    sender,
		To:      mailsvc.ParseRecipients(to),
		Subject: subject,
		Body:    body,
		Attach: []mailsvc.LocalAttachment{{
			Filename: "invite.ics",
			MIMEType: "text/calendar; method=" + method,
			Data:     []byte(ics),
		}},
	}, mailsvc.Delivery{})
	return err
}
