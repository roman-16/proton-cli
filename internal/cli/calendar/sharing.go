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
	c.AddCommand(shareAddCmd(), shareGetCmd(), shareUpdateCmd(), shareRemoveCmd())
	return c
}

func shareAddCmd() *cobra.Command {
	access := kit.Viewing()
	c := &cobra.Command{
		Use:   "add REF EMAIL",
		Short: "Give somebody a calendar",
		Long: "Give somebody a calendar.\n\n" +
			"Only another Proton account can be given one.\n\n" +
			"They are sent an invitation and see nothing until they accept. A viewer reads\n" +
			"the calendar; an editor changes it too.",
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			edit, err := kit.CanEdit(access)
			if err != nil {
				return err
			}
			cal, err := calendarList(c).Find(c.Ctx, c.Args[0])
			if err != nil {
				return err
			}
			if err := cal.Shareable(); err != nil {
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
	access.Register(c)
	return c
}

func shareGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get REF",
		Short: "Show who has a calendar",
		Long: "Show who has a calendar.\n\n" +
			"Somebody who has not answered yet is listed as pending. They can see nothing\n" +
			"until they accept.",
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			cal, err := calendarList(c).Find(c.Ctx, c.Args[0])
			if err != nil {
				return err
			}
			if err := cal.Shareable(); err != nil {
				return err
			}
			rows, err := c.App.Calendar.CalendarMembers(c.Ctx, cal.ID)
			if err != nil {
				return err
			}
			fields := []ui.Field{{Label: "Name", Value: cal.Name}}
			for _, m := range rows {
				label, detail := "Member", m.Access
				switch {
				case m.Owner:
					detail = "owner"
				case m.Status != "active":
					label, detail = "Invited", m.Access+", "+m.Status
				}
				fields = append(fields, ui.Field{Label: label, Value: m.Email + " (" + detail + ")"})
			}
			return kit.Show(c, ui.RecordSpec{
				Object: calsvc.CalendarShare{Name: cal.Name, Members: rows},
				Fields: fields,
			})
		}),
	}
}

func shareUpdateCmd() *cobra.Command {
	access := kit.Viewing()
	c := &cobra.Command{
		Use:   "update REF EMAIL",
		Short: "Change what somebody may do with a calendar",
		Long: "Change what somebody may do with a calendar.\n\n" +
			"Name them by address. It works whether they have accepted the calendar or\n" +
			"still have it pending.",
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			edit, err := kit.CanEdit(access)
			if err != nil {
				return err
			}
			cal, err := calendarList(c).Find(c.Ctx, c.Args[0])
			if err != nil {
				return err
			}
			if err := cal.Shareable(); err != nil {
				return err
			}
			member, err := memberList(c, cal.ID).Find(c.Ctx, c.Args[1])
			if err != nil {
				return err
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Updated, Kind: "members", Count: 1, Name: member.Email,
				Detail: "to " + kit.Access(edit) + " on " + cal.Name, IDs: []string{member.ID},
			}, func() error {
				return c.App.Calendar.CalendarSetAccess(c.Ctx, cal.ID, member, edit)
			})
		}),
	}
	access.Register(c)
	return c
}

func shareRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remove REF EMAIL",
		Short: "Take somebody's access to a calendar away",
		Long: "Take somebody's access to a calendar away.\n\n" +
			"Works whether they accepted or not. An unanswered invitation is withdrawn;\n" +
			"an accepted membership is ended.",
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			cal, err := calendarList(c).Find(c.Ctx, c.Args[0])
			if err != nil {
				return err
			}
			if err := cal.Shareable(); err != nil {
				return err
			}
			member, err := memberList(c, cal.ID).Find(c.Ctx, c.Args[1])
			if err != nil {
				return err
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Removed, Kind: "members", Count: 1, Name: member.Email,
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
