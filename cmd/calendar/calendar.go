// Package calendar implements the `calendar` subcommand tree.
package calendar

import (
	"fmt"
	"time"

	"github.com/roman-16/proton-cli/cmd/shared"
	"github.com/roman-16/proton-cli/internal/app"
	"github.com/roman-16/proton-cli/internal/ical"
	"github.com/roman-16/proton-cli/internal/render"
	calsvc "github.com/roman-16/proton-cli/internal/services/calendar"
	"github.com/spf13/cobra"
)

// NewCmd returns the root `calendar` command.
func NewCmd() *cobra.Command {
	c := &cobra.Command{Use: "calendar", Short: "Calendar operations"}
	c.AddCommand(calendarsCmd(), eventsCmd())
	return c
}

// ── calendar calendars ──

func calendarsCmd() *cobra.Command {
	c := &cobra.Command{Use: "calendars", Short: "Manage calendars"}
	c.AddCommand(&cobra.Command{
		Use: "list", Short: "List calendars",
		RunE: func(cmd *cobra.Command, args []string) error {
			a := app.From(cmd.Context())
			if err := a.Authenticate(cmd.Context()); err != nil {
				return err
			}
			cals, err := a.Calendar.CalendarsList(cmd.Context())
			if err != nil {
				return err
			}
			if a.R.Format != render.FormatText {
				return a.R.Object(cals)
			}
			headers := []string{"ID", "NAME", "COLOR", "MEMBERS"}
			var rows [][]string
			for _, c := range cals {
				rows = append(rows, []string{c.ID, c.Name, c.Color, fmt.Sprintf("%d", c.MemberCount)})
			}
			render.Table(a.R.Stdout, headers, rows)
			return nil
		},
	})
	var cName, cColor string
	create := &cobra.Command{
		Use: "create", Short: "Create a calendar",
		RunE: func(cmd *cobra.Command, args []string) error {
			a := app.From(cmd.Context())
			if err := a.Authenticate(cmd.Context()); err != nil {
				return err
			}
			if cName == "" {
				return fmt.Errorf("--name is required")
			}
			if a.DryRun {
				a.R.Info(fmt.Sprintf("dry-run: would create calendar %q", cName))
				return nil
			}
			u, err := a.Unlock(cmd.Context())
			if err != nil {
				return err
			}
			body, err := a.Calendar.CalendarCreate(cmd.Context(), u, cName, cColor)
			if err != nil {
				return err
			}
			id := shared.PickID(body, "Calendar", "ID")
			a.R.ID(id, fmt.Sprintf("Created calendar %q", cName))
			return nil
		},
	}
	create.Flags().StringVar(&cName, "name", "", "Calendar name")
	create.Flags().StringVar(&cColor, "color", "#8080FF", "Calendar color (hex)")
	c.AddCommand(create)

	c.AddCommand(&cobra.Command{
		Use: "delete CALENDAR_ID", Short: "Delete a calendar (requires password)",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a := app.From(cmd.Context())
			if err := a.Authenticate(cmd.Context()); err != nil {
				return err
			}
			if a.Creds.Password == "" {
				return fmt.Errorf("password is required for calendar delete")
			}
			if a.DryRun {
				a.R.Info(fmt.Sprintf("dry-run: would delete calendar %s", args[0]))
				return nil
			}
			a.R.Info("Unlocking password scope...")
			if err := a.API.UnlockPasswordScope(cmd.Context(), a.Creds.User, []byte(a.Creds.Password)); err != nil {
				return fmt.Errorf("unlock password scope: %w", err)
			}
			if err := a.Calendar.CalendarDelete(cmd.Context(), args[0]); err != nil {
				return err
			}
			a.R.Success("Calendar deleted.")
			return nil
		},
	})
	return c
}

// ── calendar events ──

func eventsCmd() *cobra.Command {
	c := &cobra.Command{Use: "events", Short: "Manage calendar events"}

	var listCal, listStart, listEnd string
	list := &cobra.Command{
		Use: "list", Short: "List events",
		RunE: func(cmd *cobra.Command, args []string) error {
			a := app.From(cmd.Context())
			if err := a.Authenticate(cmd.Context()); err != nil {
				return err
			}
			u, err := a.Unlock(cmd.Context())
			if err != nil {
				return err
			}
			id, err := a.Calendar.ResolveCalendarID(cmd.Context(), listCal)
			if err != nil {
				return app.Exit(shared.ResolveExit(err), err)
			}
			start, end := calsvc.DefaultRange()
			if listStart != "" {
				t, err := time.Parse("2006-01-02", listStart)
				if err != nil {
					return fmt.Errorf("invalid --start: %w", err)
				}
				start = t
			}
			if listEnd != "" {
				t, err := time.Parse("2006-01-02", listEnd)
				if err != nil {
					return fmt.Errorf("invalid --end: %w", err)
				}
				end = t
			}
			events, err := a.Calendar.EventsList(cmd.Context(), u, id, start, end)
			if err != nil {
				return err
			}
			if a.R.Format != render.FormatText {
				return a.R.Object(events)
			}
			headers := []string{"DATE", "TIME", "DURATION", "TITLE", "LOCATION", "CALENDAR_ID", "EVENT_ID"}
			var rows [][]string
			for _, e := range events {
				st := e.Start.Local()
				rows = append(rows, []string{
					st.Format("2006-01-02"), st.Format("15:04"),
					render.Duration(e.End.Sub(e.Start)), e.Title, e.Location, e.CalendarID, e.ID,
				})
			}
			render.Table(a.R.Stdout, headers, rows)
			return nil
		},
	}
	list.Flags().StringVar(&listCal, "calendar", "", "Calendar ID or name")
	list.Flags().StringVar(&listStart, "start", "", "Start date YYYY-MM-DD")
	list.Flags().StringVar(&listEnd, "end", "", "End date YYYY-MM-DD")
	c.AddCommand(list)

	c.AddCommand(&cobra.Command{
		Use: "get {CALENDAR_ID EVENT_ID | TITLE}", Short: "Get an event (decrypted)",
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			a := app.From(cmd.Context())
			if err := a.Authenticate(cmd.Context()); err != nil {
				return err
			}
			u, err := a.Unlock(cmd.Context())
			if err != nil {
				return err
			}
			calID, eventID, err := a.Calendar.ResolveEvent(cmd.Context(), u, args)
			if err != nil {
				return app.Exit(shared.ResolveExit(err), err)
			}
			ev, err := a.Calendar.EventGet(cmd.Context(), u, calID, eventID)
			if err != nil {
				return err
			}
			if a.R.Format != render.FormatText {
				return a.R.Object(ev)
			}
			_, _ = fmt.Fprintf(a.R.Stdout, "Event:    %s\n", ev.Title)
			_, _ = fmt.Fprintf(a.R.Stdout, "Start:    %s\n", ev.Start.Local().Format("2006-01-02 15:04"))
			_, _ = fmt.Fprintf(a.R.Stdout, "End:      %s\n", ev.End.Local().Format("2006-01-02 15:04"))
			_, _ = fmt.Fprintf(a.R.Stdout, "Duration: %s\n", render.Duration(ev.End.Sub(ev.Start)))
			if ev.Location != "" {
				_, _ = fmt.Fprintf(a.R.Stdout, "Location: %s\n", ev.Location)
			}
			_, _ = fmt.Fprintf(a.R.Stdout, "ID:       %s\n", ev.ID)
			_, _ = fmt.Fprintf(a.R.Stdout, "Calendar: %s\n", ev.CalendarID)
			return nil
		},
	})

	var eCal, eTitle, eLocation, eStart, eDuration string
	var eAllDay bool
	create := &cobra.Command{
		Use: "create", Short: "Create an event",
		RunE: func(cmd *cobra.Command, args []string) error {
			a := app.From(cmd.Context())
			if err := a.Authenticate(cmd.Context()); err != nil {
				return err
			}
			if eTitle == "" || eStart == "" {
				return fmt.Errorf("--title and --start are required")
			}
			u, err := a.Unlock(cmd.Context())
			if err != nil {
				return err
			}
			calID, err := a.Calendar.ResolveCalendarID(cmd.Context(), eCal)
			if err != nil {
				return app.Exit(shared.ResolveExit(err), err)
			}
			start, err := ical.ParseTime(eStart)
			if err != nil {
				return fmt.Errorf("invalid --start: %w", err)
			}
			dur, err := time.ParseDuration(eDuration)
			if err != nil {
				return fmt.Errorf("invalid --duration: %w", err)
			}
			if a.DryRun {
				a.R.Info(fmt.Sprintf("dry-run: would create event %q in calendar %s", eTitle, calID))
				return nil
			}
			body, err := a.Calendar.EventCreate(cmd.Context(), u, calID, eTitle, eLocation, start, start.Add(dur), eAllDay)
			if err != nil {
				return err
			}
			id := shared.PickID(body, "Responses", 0, "Response", "Event", "ID")
			a.R.ID(id, fmt.Sprintf("Created event %q", eTitle))
			return nil
		},
	}
	create.Flags().StringVar(&eCal, "calendar", "", "Calendar ID or name")
	create.Flags().StringVar(&eTitle, "title", "", "Event title")
	create.Flags().StringVar(&eLocation, "location", "", "Event location")
	create.Flags().StringVar(&eStart, "start", "", "Start time (RFC3339 or YYYY-MM-DDTHH:MM)")
	create.Flags().StringVar(&eDuration, "duration", "1h", "Duration")
	create.Flags().BoolVar(&eAllDay, "all-day", false, "All-day event")
	c.AddCommand(create)

	var uTitle, uLocation, uStart, uDuration string
	update := &cobra.Command{
		Use: "update CALENDAR_ID EVENT_ID", Short: "Update an event",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			a := app.From(cmd.Context())
			if err := a.Authenticate(cmd.Context()); err != nil {
				return err
			}
			u, err := a.Unlock(cmd.Context())
			if err != nil {
				return err
			}
			var start, end time.Time
			if uStart != "" {
				t, err := ical.ParseTime(uStart)
				if err != nil {
					return fmt.Errorf("invalid --start: %w", err)
				}
				start = t
				if uDuration != "" {
					d, err := time.ParseDuration(uDuration)
					if err != nil {
						return fmt.Errorf("invalid --duration: %w", err)
					}
					end = start.Add(d)
				}
			}
			if a.DryRun {
				a.R.Info("dry-run: would update event")
				return nil
			}
			if err := a.Calendar.EventUpdate(cmd.Context(), u, args[0], args[1], uTitle, uLocation, start, end); err != nil {
				return err
			}
			a.R.Success("Event updated.")
			return nil
		},
	}
	update.Flags().StringVar(&uTitle, "title", "", "New title")
	update.Flags().StringVar(&uLocation, "location", "", "New location")
	update.Flags().StringVar(&uStart, "start", "", "New start time")
	update.Flags().StringVar(&uDuration, "duration", "", "New duration")
	c.AddCommand(update)

	c.AddCommand(&cobra.Command{
		Use: "delete {CALENDAR_ID EVENT_ID | TITLE}", Short: "Delete an event",
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			a := app.From(cmd.Context())
			if err := a.Authenticate(cmd.Context()); err != nil {
				return err
			}
			u, err := a.Unlock(cmd.Context())
			if err != nil {
				return err
			}
			calID, eventID, err := a.Calendar.ResolveEvent(cmd.Context(), u, args)
			if err != nil {
				return app.Exit(shared.ResolveExit(err), err)
			}
			if a.DryRun {
				a.R.Info(fmt.Sprintf("dry-run: would delete event %s in calendar %s", eventID, calID))
				return nil
			}
			if err := a.Calendar.EventDelete(cmd.Context(), u, calID, eventID); err != nil {
				return err
			}
			a.R.Success("Event deleted.")
			return nil
		},
	})
	return c
}
