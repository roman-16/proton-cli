package calendar

import (
	"github.com/roman-16/proton-cli/internal/cli/kit"
	"github.com/roman-16/proton-cli/internal/ical"
	calsvc "github.com/roman-16/proton-cli/internal/service/calendar"
	"github.com/roman-16/proton-cli/internal/ui"
	"github.com/spf13/cobra"
)

// Reminders are events seen from the other side.
//
// An event says it wants a quarter of an hour's warning; a reminder is that
// warning, at the moment it is due. They are a collection of their own because
// they are counted differently: an event with two reminders is two of these, an
// event with none is not one at all, and a recurring event is one per occurrence
// it has. `events list` answers what is on; this answers what will interrupt you.

func remindersCmd() *cobra.Command {
	c := &cobra.Command{Use: "reminders", Short: "The notifications your events will raise"}
	c.AddCommand(remindersListCmd(), remindersWatchCmd())
	return c
}

func reminderColumns() []ui.Column[calsvc.Reminder] {
	return []ui.Column[calsvc.Reminder]{
		{Header: "ID", Ref: "calendar events", Cell: func(r calsvc.Reminder) string { return eventRef(r.Event) }},
		{Header: "FIRES", Cell: func(r calsvc.Reminder) string { return r.Fires.Format("2006-01-02 15:04") }},
		{Header: "REMIND", Right: true, Cell: func(r calsvc.Reminder) string { return r.Remind }},
		{Header: "TITLE", Flex: true, Handle: true, Cell: func(r calsvc.Reminder) string { return r.Title }},
		{Header: "STARTS", Cell: func(r calsvc.Reminder) string {
			if r.AllDay {
				return r.Start.Format("2006-01-02") + " all day"
			}
			return r.Start.Format("2006-01-02 15:04")
		}},
		{Header: "LOCATION", Flex: true, Cell: func(r calsvc.Reminder) string { return r.Location }},
	}
}

func remindersListCmd() *cobra.Command {
	var calendar string
	var days kit.DayRange
	c := &cobra.Command{
		Use:   "list",
		Short: "List the reminders due in a date range",
		Long: "List every reminder your events will raise between two dates.\n\n" +
			"A reminder is listed on the day it goes off, not the day its event is on.\n" +
			"An event with two reminders is two rows; a recurring event is one row per\n" +
			"occurrence.\n\n" +
			"Emailed reminders are sent by Proton and are left out.",
		Args: cobra.NoArgs,
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			calIDs, err := listedCalendars(c, calendar)
			if err != nil {
				return err
			}
			first, last := days.Or(calsvc.DefaultDays())
			reminders, err := c.App.Calendar.RemindersList(c.Ctx, calIDs, ical.Days(first, last))
			if err != nil {
				return err
			}
			return kit.List(c, ui.TableSpec[calsvc.Reminder]{
				Noun: "reminders", Columns: reminderColumns(),
				Total: ui.Unknown, Page: ui.Unpaged,
			}, reminders)
		}),
	}
	c.Flags().StringVar(&calendar, "calendar", "", "Which calendar, by name or ID (default: all of them)")
	days.Register(c)
	return c
}

func remindersWatchCmd() *cobra.Command {
	var calendar string
	c := &cobra.Command{
		Use:   "watch",
		Short: "Print each reminder as it comes due",
		Long: "Print each reminder as it comes due, until you stop it.\n\n" +
			"Reminders land on the second. Calendars are re-read as it runs, so an event\n" +
			"added while it is watching still reminds you.\n\n" +
			"Each line says what a desktop notification would say.",
		Args: cobra.NoArgs,
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			calIDs, err := listedCalendars(c, calendar)
			if err != nil {
				return err
			}
			names, err := calendarNames(c, calIDs)
			if err != nil {
				return err
			}
			return kit.Watch(c, ui.StreamSpec[calsvc.Reminder]{
				Columns: []ui.StreamColumn[calsvc.Reminder]{
					{Width: 5, Cell: func(r calsvc.Reminder) string { return r.Fires.Format("15:04") }},
					{Ref: "calendar events", Cell: func(r calsvc.Reminder) string { return eventRef(r.Event) }},
					{Cell: func(r calsvc.Reminder) string { return r.Says }},
				},
				Opening: "Watching " + ui.Listing(names) + ". Ctrl+C to stop.",
			}, func(emit func(calsvc.Reminder) error) error {
				return c.App.Calendar.RemindersWatch(c.Ctx, calIDs, emit)
			})
		}),
	}
	c.Flags().StringVar(&calendar, "calendar", "", "Which calendar, by name or ID (default: all of them)")
	return c
}

func calendarNames(c *kit.Invocation, ids []string) ([]string, error) {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		name, err := c.App.Calendar.CalendarName(c.Ctx, id)
		if err != nil {
			return nil, err
		}
		out = append(out, name)
	}
	return out, nil
}
