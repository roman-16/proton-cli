package calendar

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"time"

	"github.com/roman-16/proton-cli/internal/accent"
	"github.com/roman-16/proton-cli/internal/app"
	"github.com/roman-16/proton-cli/internal/cli/kit"
	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/roman-16/proton-cli/internal/proton"
	calsvc "github.com/roman-16/proton-cli/internal/service/calendar"
	"github.com/roman-16/proton-cli/internal/ui"
	"github.com/roman-16/proton-cli/internal/units"
	"github.com/spf13/cobra"
)

const settingsPath = "/settings/calendar"

// Calendar's General page takes a partial object, so every key writes to the same
// endpoint.
var specs = map[string]kit.Setting{
	"auto-detect-timezone": {
		Path: settingsPath, Field: "AutoDetectPrimaryTimezone",
		Page: "General", Desc: "Follow the system time zone", Enum: kit.OnOffNumbers(),
	},
	"auto-import-invite": {
		Path: settingsPath, Field: "AutoImportInvite",
		Page: "General", Desc: "Add emailed invitations to your calendar automatically",
		Enum: kit.OnOffNumbers(),
	},
	"invite-locale": {
		Path: settingsPath, Field: "InviteLocale",
		Page: "General", Desc: "Language of outgoing invitations, e.g. en_US",
	},
	"primary-timezone": {
		Path: settingsPath, Field: "PrimaryTimezone",
		Page: "General", Desc: "IANA time zone the grid is drawn in, e.g. Europe/Vienna",
	},
	"secondary-timezone": {
		Path: settingsPath, Field: "SecondaryTimezone",
		Page: "General", Desc: "IANA time zone shown alongside the primary one",
	},
	"show-secondary-timezone": {
		Path: settingsPath, Field: "DisplaySecondaryTimezone",
		Page: "General", Desc: "Show the secondary time zone", Enum: kit.OnOffNumbers(),
	},
	"view": {
		Path: settingsPath, Field: "ViewPreference",
		Page: "General", Desc: "Which view the web client opens on",
		Enum: kit.Ordered("day", "week", "month", "year", "planning"),
	},
	"week-numbers": {
		Path: settingsPath, Field: "DisplayWeekNumber",
		Page: "General", Desc: "Show week numbers", Enum: kit.OnOffNumbers(),
	},
}

type settingsView struct {
	View               string `json:"view"`
	WeekNumbers        string `json:"week_numbers"`
	PrimaryTimezone    string `json:"primary_timezone"`
	AutoDetectTimezone string `json:"auto_detect_timezone"`
	SecondaryTimezone  string `json:"secondary_timezone,omitempty"`
	ShowSecondary      string `json:"show_secondary_timezone"`
	AutoImportInvite   string `json:"auto_import_invite"`
	InviteLocale       string `json:"invite_locale,omitempty"`
	DefaultCalendar    string `json:"default_calendar,omitempty"`
}

func settingsCmd() *cobra.Command {
	c := kit.Settings("calendar", "How Calendar behaves", specs, settingsView{}, func(c *kit.Invocation) error {
		resp, err := c.App.API.Do(c.Ctx, proton.Request{Method: "GET", Path: settingsPath})
		if err != nil {
			return err
		}
		var env struct {
			CalendarUserSettings struct {
				ViewPreference            any
				DisplayWeekNumber         any
				PrimaryTimezone           string
				AutoDetectPrimaryTimezone any
				SecondaryTimezone         string
				DisplaySecondaryTimezone  any
				AutoImportInvite          any
				InviteLocale              string
				DefaultCalendarID         string
			}
		}
		if err := json.Unmarshal(resp.Body, &env); err != nil {
			return err
		}
		s := env.CalendarUserSettings
		view := settingsView{
			View:               specs["view"].Name(s.ViewPreference),
			WeekNumbers:        kit.OnOffText(kit.IntOf(s.DisplayWeekNumber)),
			PrimaryTimezone:    s.PrimaryTimezone,
			AutoDetectTimezone: kit.OnOffText(kit.IntOf(s.AutoDetectPrimaryTimezone)),
			SecondaryTimezone:  s.SecondaryTimezone,
			ShowSecondary:      kit.OnOffText(kit.IntOf(s.DisplaySecondaryTimezone)),
			AutoImportInvite:   kit.OnOffText(kit.IntOf(s.AutoImportInvite)),
			InviteLocale:       s.InviteLocale,
			DefaultCalendar:    s.DefaultCalendarID,
		}
		return kit.Show(c, ui.RecordSpec{
			Object: view,
			Fields: []ui.Field{
				{Label: "View", Value: view.View},
				{Label: "Week Numbers", Value: view.WeekNumbers, Always: true},
				{Label: "Primary Time Zone", Value: view.PrimaryTimezone},
				{Label: "Auto-detect Time Zone", Value: view.AutoDetectTimezone, Always: true},
				{Label: "Secondary Time Zone", Value: view.SecondaryTimezone},
				{Label: "Show Secondary", Value: view.ShowSecondary, Always: true},
				{Label: "Auto-import Invitations", Value: view.AutoImportInvite, Always: true},
				{Label: "Invitation Language", Value: view.InviteLocale},
				{Label: "Default Calendar", Value: view.DefaultCalendar, Ref: "calendar settings calendars"},
			},
		})
	})
	c.AddCommand(calendarsCmd(), holidaysCmd(), linksCmd())
	return c
}

// ── calendar settings calendars ──

func calendarsCmd() *cobra.Command {
	c := &cobra.Command{Use: "calendars", Short: "The calendars you keep events in"}
	c.AddCommand(calendarsListCmd(), calendarsGetCmd(), calendarsCreateCmd(), calendarsShareCmd(),
		calendarsUpdateCmd(), calendarsDeleteCmd(), calendarsLeaveCmd())
	return c
}

func calendarColumns() []ui.Column[calsvc.Calendar] {
	return []ui.Column[calsvc.Calendar]{
		{Header: "ID", ID: true, Cell: func(cal calsvc.Calendar) string { return cal.ID }},
		{Header: "NAME", Flex: true, Handle: true, Cell: func(cal calsvc.Calendar) string { return cal.Name }},
		kit.ColorColumn(func(cal calsvc.Calendar) string { return cal.Color }),
		{Header: "KIND", Cell: func(cal calsvc.Calendar) string { return cal.Kind }},
	}
}

func calendarList(c *kit.Invocation) *kit.Lookup[calsvc.Calendar] {
	return &kit.Lookup[calsvc.Calendar]{
		Kind:   "calendar",
		Load:   func(ctx context.Context) ([]calsvc.Calendar, error) { return c.App.Calendar.CalendarsList(ctx) },
		ID:     func(cal calsvc.Calendar) string { return cal.ID },
		Handle: func(cal calsvc.Calendar) string { return cal.Name },
	}
}

func calendarsListCmd() *cobra.Command {
	var held kit.Held[calsvc.Calendar]
	c := &cobra.Command{
		Use:   "list",
		Short: "List your calendars",
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			cals, err := calendarList(c).Rows(c.Ctx)
			if err != nil {
				return err
			}
			return held.Answer(c, ui.TableSpec[calsvc.Calendar]{
				Noun: "calendars", Columns: calendarColumns(),
			}, cals)
		}),
	}
	held.Register(c, "calendars",
		kit.Key[calsvc.Calendar]{Name: "name", Less: func(a, b calsvc.Calendar) int { return kit.Fold(a.Name, b.Name) }},
	)
	return c
}

func calendarsCreateCmd() *cobra.Command {
	var name, url, holidays, language string
	color := &kit.Color{Name: "color", Default: accent.Default}
	c := &cobra.Command{
		Use:   "create",
		Short: "Create a calendar, subscribe to one, or add public holidays",
		Long: "Create a calendar, subscribe to one, or add public holidays.\n\n" +
			"--url takes the address of an .ics file. Proton fetches it on a schedule\n" +
			"and fills the calendar from it, so those events are read-only. An address\n" +
			"Proton cannot read is refused before the calendar is made.\n\n" +
			"--holidays takes a country by name or code, as `settings holidays list`\n" +
			"shows them. The calendar takes the name Proton gives it, and its events\n" +
			"are read-only. A country with holidays in more than one language needs\n" +
			"--language.",
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			if language != "" && holidays == "" {
				return kit.Fail("--language picks a holidays calendar; pass --holidays with it.")
			}
			if holidays != "" {
				return addHolidays(c, holidays, language, color.Value())
			}
			if name == "" {
				return kit.Fail("A calendar needs a name.").Hint("--name Work")
			}
			return kit.Create(c, ui.ResultSpec{
				Action: ui.Created, Kind: "calendars", Name: name,
			}, func() (string, error) {
				return c.App.Calendar.CalendarCreate(c.Ctx, name, color.Value(), url)
			})
		}),
	}
	c.Flags().StringVar(&name, "name", "", "Name for the new calendar")
	c.Flags().StringVar(&url, "url", "",
		"Subscribe to the calendar published at this address instead of making an empty one")
	c.Flags().StringVar(&holidays, "holidays", "",
		"Add this country's public holidays instead of making an empty calendar")
	c.Flags().StringVar(&language, "language", "",
		"Which language the holidays are in, for a country with more than one")
	kit.Exclusive(c, "holidays", "name")
	kit.Exclusive(c, "holidays", "url")
	color.Register(c)
	return c
}

// calendarsGetCmd shows one calendar in full, including the defaults it applies
// to events made in it - which nothing else reports.
func calendarsGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get REF",
		Short: "Show one calendar, with the defaults it gives new events",
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			cal, err := calendarList(c).Find(c.Ctx, c.Args[0])
			if err != nil {
				return err
			}
			d, err := c.App.Calendar.CalendarDefaults(c.Ctx, cal.ID)
			if err != nil {
				return err
			}
			view := struct {
				calsvc.Calendar
				Defaults *calsvc.CalendarDefaults `json:"defaults"`
			}{cal, d}
			fields := []ui.Field{
				{Label: "Name", Value: cal.Name, Handle: true},
				kit.ColorField(cal.Color),
				{Label: "Kind", Value: cal.Kind},
				{Label: "Owner", Value: cal.Owner},
				{Label: "Access", Value: cal.Access},
				{Label: "Description", Value: cal.Description},
			}
			if cal.Kind == calsvc.KindPersonal {
				fields = append(fields, ui.Field{Label: "Default Duration", Value: units.Duration(
					time.Duration(d.Duration) * time.Minute), Always: true})
			}
			if cal.Kind != calsvc.KindHolidays {
				fields = append(fields, ui.Field{Label: "Default Reminders", Value: strings.Join(d.Reminders, ", ")})
			}
			fields = append(fields,
				ui.Field{Label: "All-day Reminders", Value: strings.Join(d.AllDayReminders, ", ")},
				ui.Field{Label: "Shows As Busy", Value: kit.OnOffText(boolInt(d.Busy)), Always: true},
				ui.Field{Label: "ID", Value: cal.ID, ID: true},
			)
			return kit.Show(c, ui.RecordSpec{Object: view, Fields: fields})
		}),
	}
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func calendarsUpdateCmd() *cobra.Command {
	var name, duration string
	var reminders, allDayReminders []string
	var noRemind, makeDefault bool
	color := &kit.Color{Name: "color", Usage: "New accent color, by name (purple) or hex (#8080FF)"}
	busy := &kit.Enum{
		Name: "busy", Usage: "Whether events here make you look busy to others",
		Values: []string{"on", "off"},
	}
	c := &cobra.Command{
		Use:   "update REF",
		Short: "Rename or recolor a calendar, or change what it gives new events",
		Long: "Rename or recolor a calendar, or change what it gives new events.\n\n" +
			"Defaults are set per calendar, so a work calendar can open half-hour\n" +
			"meetings with a reminder while a personal one does not. --default-duration\n" +
			"and --default apply only to a personal calendar of your own, and a holidays\n" +
			"calendar takes only --color, --remind-all-day, --no-remind and --busy.",
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			busyValue, err := busy.Value()
			if err != nil {
				return err
			}
			touchesDefaults := c.Changed("default-duration") || c.Changed("remind") ||
				c.Changed("remind-all-day") || c.Changed("no-remind") || busy.Set()
			if name == "" && !color.Set() && !touchesDefaults && !c.Changed("default") {
				return kit.Fail("Nothing to change.").
					Hint("pass --name, --color, --default-duration, --remind, --busy or --default.")
			}
			if c.Changed("default") && !makeDefault {
				return kit.Fail("A default calendar is replaced, not unset: make another calendar the default.")
			}
			patch, err := defaultsPatch(c, duration, reminders, allDayReminders, noRemind, busyValue)
			if err != nil {
				return err
			}
			cal, err := calendarList(c).Find(c.Ctx, c.Args[0])
			if err != nil {
				return err
			}
			if err := fitsKind(c, cal); err != nil {
				return err
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Updated, Kind: "calendars", Count: 1, Name: name,
				IDs: []string{cal.ID},
			}, func() error {
				if name != "" || color.Set() {
					if err := c.App.Calendar.CalendarRename(c.Ctx, cal.ID, name, color.Value()); err != nil {
						return err
					}
				}
				if touchesDefaults {
					if err := c.App.Calendar.CalendarDefaultsUpdate(c.Ctx, cal.ID, patch); err != nil {
						return err
					}
				}
				if !makeDefault {
					return nil
				}
				return c.App.Calendar.CalendarSetDefault(c.Ctx, cal.ID)
			})
		}),
	}
	c.Flags().StringVar(&name, "name", "", "New name")
	c.Flags().StringVar(&duration, "default-duration", "",
		"How long a new event lasts unless it says otherwise (e.g. 30m, 1h)")
	c.Flags().StringArrayVar(&reminders, "remind", nil,
		"Default reminder for a new event, as DURATION or DURATION:email (repeatable)")
	c.Flags().StringArrayVar(&allDayReminders, "remind-all-day", nil,
		"Default reminder for a new all-day event (repeatable)")
	c.Flags().BoolVar(&noRemind, "no-remind", false, "Give new events no reminder by default")
	c.Flags().BoolVar(&makeDefault, "default", false,
		"Make new events go here when nothing names another calendar")
	kit.Exclusive(c, "remind", "no-remind")
	kit.Exclusive(c, "remind-all-day", "no-remind")
	color.Register(c)
	busy.Register(c)
	return c
}

// fitsKind refuses what a calendar of its kind does not have. Proton's clients
// offer a holidays calendar a colour, all-day reminders and whether it makes you
// look busy, and nothing else, and offer a default duration and the default
// calendar only among your own personal calendars.
func fitsKind(c *kit.Invocation, cal calsvc.Calendar) error {
	switch cal.Kind {
	case calsvc.KindHolidays:
		for _, flag := range []string{"name", "default-duration", "remind", "default"} {
			if c.Changed(flag) {
				return kit.Fail("A holidays calendar takes only --color, --remind-all-day, --no-remind and --busy.")
			}
		}
	case calsvc.KindPersonal:
		if c.Changed("default") && !cal.Active() {
			return errs.Naming(cal.Name, kit.Fail("%s is disabled, so it cannot be your default calendar.", cal.Name))
		}
	default:
		for _, flag := range []string{"default-duration", "default"} {
			if c.Changed(flag) {
				return kit.Fail("--%s applies only to a personal calendar of your own.", flag)
			}
		}
	}
	return nil
}

// defaultsPatch reads the per-calendar flags, judging what it can before the
// network: a duration that does not parse is wrong whoever is signed in.
func defaultsPatch(c *kit.Invocation, duration string, reminders, allDay []string,
	noRemind bool, busy string) (calsvc.DefaultsPatch, error) {
	var p calsvc.DefaultsPatch
	if c.Changed("default-duration") {
		d, err := units.ParseDuration(duration)
		if err != nil {
			return p, kit.Fail("--default-duration: %v", err)
		}
		minutes := int(d / time.Minute)
		if minutes <= 0 {
			return p, kit.Fail("--default-duration has to be at least a minute.")
		}
		p.Duration = &minutes
	}
	if c.Changed("remind") {
		p.Reminders = &reminders
	}
	if c.Changed("remind-all-day") {
		p.AllDayReminders = &allDay
	}
	if noRemind {
		none := []string{}
		p.Reminders, p.AllDayReminders = &none, &none
	}
	if busy != "" {
		on := busy == "on"
		p.Busy = &on
	}
	return p, nil
}

func calendarsDeleteCmd() *cobra.Command {
	var reauth kit.Reauth
	c := &cobra.Command{
		Use:   "delete REF...",
		Short: "Delete calendars, and every event in them",
		Long: "Delete calendars, and every event in them.\n\n" +
			"Asks for your password even when you are signed in, except for a holidays\n" +
			"calendar. With no terminal to ask, pass --password-file, which takes - for\n" +
			"stdin. A calendar somebody shared with you is left with `leave` instead.",
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			if err := reauth.Supply(c); err != nil {
				return err
			}
			sel, err := kit.SelectFrom(c, "calendars", calendarColumns(), calendarList(c))
			if err != nil {
				return err
			}
			for _, cal := range sel.Rows {
				if cal.Kind == calsvc.KindShared {
					return errs.Naming(cal.Name, kit.Fail("%s was shared with you by %s, so it is left, not deleted.",
						cal.Name, cal.Owner).
						Hint(kit.Program+" calendar settings calendars leave "+shellArg(cal.Name)))
				}
			}
			next, moves, err := c.App.Calendar.DefaultAfter(c.Ctx, sel.IDs)
			if err != nil {
				return err
			}
			spec := ui.ResultSpec{
				Action: ui.Deleted, Kind: "calendars", Count: sel.Len(), IDs: sel.IDs,
				Name:    kit.Sole(sel.Rows, func(cal calsvc.Calendar) string { return cal.Name }),
				Preview: sel.Preview(),
			}
			var nextID string
			if moves {
				spec.Detail = "with no default calendar left"
				spec.Extra = map[string]any{"default_calendar": nil}
				if next != nil {
					nextID = next.ID
					spec.Detail = "with " + next.Name + " as your new default calendar"
					spec.Extra = map[string]any{"default_calendar": next.ID}
				}
			}
			return kit.Mutate(c, spec, func() error {
				// Nothing here arranges the elevation: the client does it when the
				// server asks, and drops the scope again afterwards. All this owes
				// the user is a reason for the prompt.
				ctx := app.WithScopeReason(c.Ctx, "delete a calendar")
				for _, cal := range sel.Rows {
					if err := c.App.Calendar.CalendarDelete(ctx, cal); err != nil {
						return err
					}
				}
				if !moves {
					return nil
				}
				if err := c.App.Calendar.CalendarSetDefault(c.Ctx, nextID); err != nil {
					c.Warn("The deletion went through, but your default calendar could not be moved: %v", err)
				}
				return nil
			})
		}),
	}
	reauth.Declare(c)
	return c
}

func calendarsLeaveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "leave REF...",
		Short: "Leave calendars somebody shared with you",
		Long: "Leave calendars somebody shared with you.\n\n" +
			"Only the calendar's owner can give it to you again.",
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			sel, err := kit.SelectFrom(c, "calendars", calendarColumns(), calendarList(c))
			if err != nil {
				return err
			}
			for _, cal := range sel.Rows {
				if cal.Kind != calsvc.KindShared {
					return errs.Naming(cal.Name, kit.Fail("%s was not shared with you, so it is deleted, not left.",
						cal.Name).
						Hint(kit.Program+" calendar settings calendars delete "+shellArg(cal.Name)))
				}
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Left, Kind: "calendars", Count: sel.Len(), IDs: sel.IDs,
				Name:    kit.Sole(sel.Rows, func(cal calsvc.Calendar) string { return cal.Name }),
				Preview: sel.Preview(),
			}, func() error {
				for _, cal := range sel.Rows {
					if err := c.App.Calendar.CalendarLeave(c.Ctx, cal); err != nil {
						return err
					}
				}
				return nil
			})
		}),
	}
}

var plainArg = regexp.MustCompile(`^[A-Za-z0-9._@/-]+$`)

// shellArg writes a calendar's name so that a command line pasted from a hint
// reads it back as one argument.
func shellArg(s string) string {
	if plainArg.MatchString(s) {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
