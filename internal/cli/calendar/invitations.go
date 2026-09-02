package calendar

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/roman-16/proton-cli/internal/cli/kit"
	calsvc "github.com/roman-16/proton-cli/internal/service/calendar"
	"github.com/roman-16/proton-cli/internal/ui"
	"github.com/roman-16/proton-cli/internal/units"
)

// Calendars other people have offered you.
//
// An invitation carries the calendar's passphrase, encrypted to the address it
// was sent to. Accepting opens it and signs it back, which is how Proton knows
// the offer reached somebody who could read it. Until then you see the calendar's
// name and who sent it, and nothing that is on it.

func invitationsCmd() *cobra.Command {
	c := &cobra.Command{Use: "invitations", Short: "Calendars other people have offered you"}
	c.AddCommand(invitationsListCmd(), invitationsAcceptCmd(), invitationsDeclineCmd())
	return c
}

func invitationList(c *kit.Invocation) *kit.Lookup[calsvc.CalendarInvitation] {
	return &kit.Lookup[calsvc.CalendarInvitation]{
		Kind: "invitation",
		Load: func(ctx context.Context) ([]calsvc.CalendarInvitation, error) {
			return c.App.Calendar.CalendarInvitations(ctx)
		},
		ID:     func(i calsvc.CalendarInvitation) string { return i.ID },
		Handle: func(i calsvc.CalendarInvitation) string { return i.Name },
	}
}

func invitationColumns() []ui.Column[calsvc.CalendarInvitation] {
	return []ui.Column[calsvc.CalendarInvitation]{
		{Header: "ID", ID: true, Cell: func(i calsvc.CalendarInvitation) string { return i.ID }},
		{Header: "CALENDAR", Flex: true, Cell: func(i calsvc.CalendarInvitation) string { return i.Name }},
		{Header: "FROM", Cell: func(i calsvc.CalendarInvitation) string { return i.Sender }},
		{Header: "ACCESS", Cell: func(i calsvc.CalendarInvitation) string { return i.Access }},
		{Header: "STATUS", Cell: func(i calsvc.CalendarInvitation) string { return i.Status }},
		{Header: "EXPIRES", Cell: func(i calsvc.CalendarInvitation) string { return units.Time(i.Expires) }},
	}
}

func invitationsListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List calendars other people have offered you",
		Args:  cobra.NoArgs,
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			rows, err := c.App.Calendar.CalendarInvitations(c.Ctx)
			if err != nil {
				return err
			}
			return kit.List(c, ui.TableSpec[calsvc.CalendarInvitation]{
				Noun: "invitations", Columns: invitationColumns(),
				Total: len(rows), Page: ui.Unpaged,
			}, rows, func(i calsvc.CalendarInvitation) []string { return []string{i.ID} })
		}),
	}
}

func invitationsAcceptCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "accept REF...",
		Short: "Take a calendar somebody offered you",
		Long: "Take a calendar somebody offered you.\n\n" +
			"The invitation carries the calendar's key, encrypted to the address it was\n" +
			"sent to. Accepting re-encrypts that key to your own, after which the\n" +
			"calendar behaves like any other of yours.",
		Args: cobra.MinimumNArgs(1),
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			return answerInvitations(c, ui.Accepted, c.App.Calendar.CalendarInvitationAccept)
		}),
	}
}

func invitationsDeclineCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "decline REF...",
		Short: "Turn down a calendar somebody offered you",
		Args:  cobra.MinimumNArgs(1),
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			return answerInvitations(c, ui.Declined, c.App.Calendar.CalendarInvitationDecline)
		}),
	}
}

// answerInvitations says yes or no to the ones named. Both answers select the
// same way and report the same shape, so they share this.
func answerInvitations(c *kit.Invocation, action ui.Action, answer func(context.Context, string) error) error {
	sel, err := kit.SelectFrom(c, "invitations", invitationColumns(), invitationList(c))
	if err != nil {
		return err
	}
	return kit.Mutate(c, ui.ResultSpec{
		Action: action, Kind: "invitations", Count: sel.Len(), IDs: sel.IDs,
		Name: kit.Sole(sel.Rows, func(i calsvc.CalendarInvitation) string { return i.Name }),
	}, func() error {
		for _, id := range sel.IDs {
			if err := answer(c.Ctx, id); err != nil {
				return err
			}
		}
		return nil
	})
}
