package calendar

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/roman-16/proton-cli/internal/cli/kit"
	calsvc "github.com/roman-16/proton-cli/internal/service/calendar"
	"github.com/roman-16/proton-cli/internal/ui"
)

// Giving a calendar to somebody else.
//
// What is handed over is the key that opens the calendar, encrypted to their
// key and signed with yours - so Proton passes it along without being able to
// read it, and they can tell it came from you. That is also why this only works
// with another Proton account: an address Proton holds no keys for has nothing
// to encrypt to.

func calendarsShareCmd() *cobra.Command {
	c := &cobra.Command{Use: "share", Short: "Who else can see a calendar"}
	c.AddCommand(shareAddCmd(), shareListCmd(), shareRemoveCmd())
	return c
}

func memberColumns() []ui.Column[calsvc.CalendarMember] {
	return []ui.Column[calsvc.CalendarMember]{
		{Header: "ID", ID: true, Cell: func(m calsvc.CalendarMember) string { return m.ID }},
		{Header: "EMAIL", Flex: true, Cell: func(m calsvc.CalendarMember) string { return m.Email }},
		{Header: "ACCESS", Cell: func(m calsvc.CalendarMember) string { return m.Access }},
		{Header: "STATUS", Cell: func(m calsvc.CalendarMember) string { return m.Status }},
		{Header: "OWNER", Cell: func(m calsvc.CalendarMember) string {
			if m.Owner {
				return "yes"
			}
			return "no"
		}},
	}
}

func shareAddCmd() *cobra.Command {
	var edit bool
	c := &cobra.Command{
		Use:   "add REF EMAIL",
		Short: "Give somebody a calendar",
		Long: "Give somebody a calendar.\n\n" +
			"They are sent an invitation and see nothing until they accept. What travels\n" +
			"is the key that opens the calendar, encrypted to their key - so it has to be\n" +
			"another Proton account.\n\n" +
			"They can read the calendar; --edit lets them change it too.",
		Args: cobra.ExactArgs(2),
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			cal, err := calendarList(c).Find(c.Ctx, c.Args[0])
			if err != nil {
				return err
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Invited, Kind: "members", Count: 1, Name: c.Args[1],
				Detail: "to " + cal.Name,
			}, func() error {
				return c.App.Calendar.CalendarShare(c.Ctx, cal.ID, c.Args[1], edit)
			})
		}),
	}
	c.Flags().BoolVar(&edit, "edit", false, "Let them change the calendar, not just see it")
	return c
}

func shareListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list REF",
		Short: "List who has a calendar",
		Long: "List who has a calendar.\n\n" +
			"Somebody who has not answered yet is listed as pending: they were sent an\n" +
			"invitation and cannot see anything until they take it.",
		Args: cobra.ExactArgs(1),
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			cal, err := calendarList(c).Find(c.Ctx, c.Args[0])
			if err != nil {
				return err
			}
			rows, err := c.App.Calendar.CalendarMembers(c.Ctx, cal.ID)
			if err != nil {
				return err
			}
			return kit.List(c, ui.TableSpec[calsvc.CalendarMember]{
				Noun: "members", Columns: memberColumns(), Total: len(rows), Page: ui.Unpaged,
			}, rows, func(m calsvc.CalendarMember) []string { return []string{m.ID} })
		}),
	}
}

func shareRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remove REF EMAIL",
		Short: "Take somebody's access to a calendar away",
		Long: "Take somebody's access to a calendar away.\n\n" +
			"It works whether they accepted or not: an invitation nobody answered is\n" +
			"withdrawn, and a membership somebody is using is ended.",
		Args: cobra.ExactArgs(2),
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			cal, err := calendarList(c).Find(c.Ctx, c.Args[0])
			if err != nil {
				return err
			}
			member, err := memberList(c, cal.ID).Find(c.Ctx, c.Args[1])
			if err != nil {
				return err
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Removed.WithConsent(), Kind: "members", Count: 1, Name: member.Email,
				Detail: "from " + cal.Name, IDs: []string{member.ID},
			}, func() error {
				return c.App.Calendar.CalendarUnshare(c.Ctx, cal.ID, member)
			})
		}),
	}
}

func memberList(c *kit.Invocation, calendarID string) *kit.Lookup[calsvc.CalendarMember] {
	return &kit.Lookup[calsvc.CalendarMember]{
		Kind: "member",
		Load: func(ctx context.Context) ([]calsvc.CalendarMember, error) {
			return c.App.Calendar.CalendarMembers(ctx, calendarID)
		},
		ID:     func(m calsvc.CalendarMember) string { return m.ID },
		Handle: func(m calsvc.CalendarMember) string { return m.Email },
	}
}
