package calendar

import (
	"github.com/roman-16/proton-cli/internal/cli/kit"
	"github.com/roman-16/proton-cli/internal/ical"
	calsvc "github.com/roman-16/proton-cli/internal/service/calendar"
	"github.com/roman-16/proton-cli/internal/ui"
	"github.com/spf13/cobra"
)

// Busy times are other people's calendars seen from outside: when somebody is
// busy, and nothing of what with.
//
// Proton's calendar shows them while an event with participants is being placed
// on the grid. A command line has no grid to place one on, so they are a listing
// of their own, asked for before `events create` - which is when they help.
//
// Somebody Proton will not say for is part of the answer rather than a gap in
// it: left out quietly, they would read as free all week. So they are named
// under the table and carried in the envelope.

func busyTimesCmd() *cobra.Command {
	c := &cobra.Command{Use: "busy-times", Short: "When other people are busy"}
	c.AddCommand(busyTimesListCmd())
	return c
}

func busyTimeColumns() []ui.Column[calsvc.BusyTime] {
	return []ui.Column[calsvc.BusyTime]{
		{Header: "EMAIL", Flex: true, Cell: func(b calsvc.BusyTime) string { return b.Email }},
		{Header: "DATE", Cell: func(b calsvc.BusyTime) string { return b.Start.Format("2006-01-02") }},
		{Header: "TIME", Cell: func(b calsvc.BusyTime) string {
			if b.AllDay {
				return "all day"
			}
			return b.Start.Format("15:04")
		}},
		{Header: "DURATION", Right: true, Cell: func(b calsvc.BusyTime) string {
			return length(b.Start, b.End, b.AllDay)
		}},
	}
}

func busyTimesListCmd() *cobra.Command {
	var days kit.DayRange
	var held kit.Held[calsvc.BusyTime]
	c := &cobra.Command{
		Use:   "list EMAIL...",
		Short: "List when people are busy in a date range",
		Long: "List when people are busy in a date range.\n\n" +
			"With neither --after nor --before it covers the next 7 days, starting today.\n" +
			"With one of them, it covers the 7 days starting or ending there.\n\n" +
			"It needs a Duo, Family, Visionary or business plan. Somebody whose busy times\n" +
			"Proton will not show, such as anyone outside Proton, is listed as unknown. A\n" +
			"busy time says when, never what.",
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			// The zone is settled before the days are read, because when this
			// machine names none it is the account's, and the days are read in it.
			zone, err := c.App.Zone(c.Ctx)
			if err != nil {
				return err
			}
			first, last := days.Or(calsvc.BusyDays())
			busy, unknown, err := c.App.Calendar.BusyTimes(c.Ctx, c.Args, zone, ical.Days(first, last))
			if err != nil {
				return err
			}
			if err := held.Answer(c, ui.TableSpec[calsvc.BusyTime]{
				Noun: "busy times", Columns: busyTimeColumns(),
				Extra: map[string]any{"unknown": unknown},
			}, busy); err != nil {
				return err
			}
			if len(unknown) > 0 {
				c.Warn("Availability unknown for %s: Proton will not say when they are busy.", ui.Listing(unknown))
			}
			return nil
		}),
	}
	days.Register(c)
	held.Register(c, "busy times")
	return c
}
