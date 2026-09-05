package calendar

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/roman-16/proton-cli/internal/cli/kit"
	"github.com/roman-16/proton-cli/internal/ical"
	calsvc "github.com/roman-16/proton-cli/internal/service/calendar"
	"github.com/roman-16/proton-cli/internal/ui"
	"github.com/roman-16/proton-cli/internal/units"
	"github.com/spf13/cobra"
)

func eventsCmd() *cobra.Command {
	c := &cobra.Command{Use: "events", Short: "Events in your calendars"}
	c.AddCommand(eventsListCmd(), eventsGetCmd(), eventsCreateCmd(), eventsUpdateCmd(),
		eventsRespondCmd(), eventsDeleteCmd(), eventsExportCmd(), eventsImportCmd())
	return c
}

// References to an event.
//
// An event needs two IDs to address it, because Proton stores it inside a
// calendar, and a recurring event needs a third thing: which occurrence. Both are
// written into the one REF every command takes - the two IDs separated by a
// slash, which is safe because Proton's IDs are base64url, and the occurrence's
// own start after an "@", which is safe because it has to parse as a time to
// count as one.
//
//	CALENDAR/EVENT                    the stored event; for a series, all of it
//	CALENDAR/EVENT@2026-04-16T09:00   one occurrence of it

// occurrenceShape is the set of forms an occurrence may be written in: the two
// the CLI prints, plus seconds or an offset for anything generated elsewhere.
var occurrenceShape = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}([T ]\d{2}:\d{2}(:\d{2})?(Z|[+-]\d{2}:\d{2})?)?$`)

// splitOccurrence separates a reference from the occurrence it names.
//
// The split only happens when what follows the last "@" is shaped like a time, so
// a title or an address containing one stays a whole reference.
func splitOccurrence(ref string) (base, occurrence string) {
	at := strings.LastIndexByte(ref, '@')
	if at < 0 {
		return ref, ""
	}
	if suffix := ref[at+1:]; occurrenceShape.MatchString(suffix) {
		return ref[:at], suffix
	}
	return ref, ""
}

// eventRef renders the reference a row is addressed by, which is the form a list
// prints and a command reads back.
func eventRef(e calsvc.Event) string {
	ref := kit.JoinPair(e.CalendarID, e.ID)
	if e.Occurrence == "" {
		return ref
	}
	return ref + "@" + e.Occurrence
}

// resolveEvent turns a reference into the parts the service needs. A title still
// works, and still resolves across every calendar.
func resolveEvent(c *kit.Invocation, ref string) (calendarID, eventID, occurrence string, err error) {
	base, occurrence := splitOccurrence(ref)
	if first, second, err := kit.ExpandPair(c.App, base); err != nil || first != "" {
		return first, second, occurrence, err
	}
	calendarID, eventID, resolved, err := c.App.Calendar.ResolveEvent(c.Ctx, base)
	if err != nil {
		return "", "", "", err
	}
	if occurrence == "" {
		occurrence = resolved
	}
	return calendarID, eventID, occurrence, nil
}

func eventColumns() []ui.Column[calsvc.Event] {
	return []ui.Column[calsvc.Event]{
		{Header: "ID", ID: true, Cell: eventRef},
		{Header: "DATE", Cell: func(e calsvc.Event) string { return e.Start.Format("2006-01-02") }},
		{Header: "TIME", Cell: func(e calsvc.Event) string {
			if e.AllDay {
				return "all day"
			}
			return e.Start.Format("15:04")
		}},
		{Header: "DURATION", Right: true, Cell: func(e calsvc.Event) string {
			return units.Duration(e.End.Sub(e.Start))
		}},
		{Header: "TITLE", Flex: true, Handle: true, Cell: func(e calsvc.Event) string { return e.Title }},
		{Header: "LOCATION", Flex: true, Cell: func(e calsvc.Event) string { return e.Location }},
	}
}

func eventsListCmd() *cobra.Command {
	var calendar string
	var days kit.DayRange
	c := &cobra.Command{
		Use:   "list",
		Short: "List events in a date range",
		Long: "List what is on your calendars between two dates.\n\n" +
			"--start and --end are whole days in your own zone, both included.\n\n" +
			"Each occurrence of a recurring event is listed on its own day, with a\n" +
			"reference that names that occurrence.\n\n" +
			"Covers every calendar unless --calendar narrows it to one.",
		Args: cobra.NoArgs,
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			calIDs, err := listedCalendars(c, calendar)
			if err != nil {
				return err
			}
			first, last := days.Or(calsvc.DefaultDays())
			events, err := c.App.Calendar.EventsList(c.Ctx, calIDs, ical.Days(first, last))
			if err != nil {
				return err
			}
			return kit.List(c, ui.TableSpec[calsvc.Event]{
				Noun: "events", Columns: eventColumns(),
				Total: ui.Unknown, Page: ui.Unpaged,
			}, events)
		}),
	}
	c.Flags().StringVar(&calendar, "calendar", "", "Which calendar, by name or ID (default: all of them)")
	days.Register(c)
	return c
}

func eventsGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get REF",
		Short: "Show one event, decrypted",
		Args:  cobra.ExactArgs(1),
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			calID, eventID, occurrence, err := resolveEvent(c, c.Args[0])
			if err != nil {
				return err
			}
			ev, err := c.App.Calendar.EventGet(c.Ctx, calID, eventID, occurrence)
			if err != nil {
				return err
			}
			name, err := c.App.Calendar.CalendarName(c.Ctx, ev.CalendarID)
			if err != nil {
				return err
			}
			when := ev.Start.Format("2006-01-02 15:04")
			until := ev.End.Format("2006-01-02 15:04")
			if ev.AllDay {
				when = ev.Start.Format("2006-01-02") + " (all day)"
				until = ""
			}
			return kit.Show(c, ui.RecordSpec{
				Object: ev,
				Fields: []ui.Field{
					{Label: "Title", Value: ev.Title, Handle: true},
					{Label: "Start", Value: when},
					{Label: "End", Value: until},
					{Label: "Duration", Value: units.Duration(ev.End.Sub(ev.Start))},
					{Label: "Location", Value: ev.Location},
					{Label: "Description", Value: ev.Description},
					{Label: "Recurrence", Value: ev.RRule},
					{Label: "Occurrence", Value: occurrenceLabel(*ev)},
					{Label: "Series", Value: seriesLabel(*ev)},
					{Label: "Zone", Value: ev.Zone},
					{Label: "Reminders", Value: strings.Join(ev.Reminders, ", ")},
					{Label: "Status", Value: ev.Status, Always: true},
					{Label: "Organizer", Value: ev.Organizer},
					{Label: "Attendees", Value: strings.Join(ev.Attendees, ", ")},
					kit.ColorField(ev.Color),
					{Label: "Calendar", Value: name},
					kit.SignatureField(string(ev.Signature)),
					{Label: "ID", Value: eventRef(*ev), ID: true},
				},
			})
		}),
	}
}

// occurrenceLabel says where an instance sits in its series, or how far a series
// runs when the series itself is being shown.
func occurrenceLabel(e calsvc.Event) string {
	switch {
	case e.Number > 0 && e.Count != nil:
		return fmt.Sprintf("%d of %d", e.Number, *e.Count)
	case e.Number > 0:
		return fmt.Sprintf("%d of a recurring series", e.Number)
	case e.Count == nil && e.RRule != "":
		return "the whole series, which has no end"
	case e.Count != nil && *e.Count > 1:
		return fmt.Sprintf("the whole series, %s", ui.Quantity(*e.Count, "occurrences"))
	}
	return ""
}

// seriesLabel names the series an instance belongs to, so a reader can address the
// whole thing from a row that addresses one occurrence.
func seriesLabel(e calsvc.Event) string {
	if e.Occurrence == "" {
		return ""
	}
	return kit.JoinPair(e.CalendarID, e.ID)
}

// colorParagraph is what both commands say about --color, since an event's
// colour behaves the same whether it is being given one or given another.
const colorParagraph = "--color gives an event a color of its own; without one it is drawn in its\n" +
	"calendar's. Once it has one there is no way back, in Proton's apps or here.\n" +
	"A color of its own is a paid feature: Proton stores one for a free account,\n" +
	"but its apps draw the calendar's."

// details are the fields an event carries. create and update share them, so the
// two commands cannot disagree about what an event is.
type details struct {
	title, location, description string
	start, duration, rrule       string
	end                          string
	status                       *kit.Enum
	color                        *kit.Color
	allDay                       bool
	reminders                    []string
	noReminders                  bool
}

func (d *details) register(c *cobra.Command, verb string) {
	d.status = &kit.Enum{
		Name: "status", Usage: verb + " whether it is going ahead",
		Values: calsvc.EventStatuses(),
	}
	d.status.Register(c)
	d.color = &kit.Color{
		Name: "color", Usage: verb + " the color, by name (purple) or hex (#8080FF)",
	}
	d.color.Register(c)
	f := c.Flags()
	f.StringVar(&d.title, "title", "", verb+" the title")
	f.StringVar(&d.start, "start", "", verb+" the start: a day, or a day and a time "+
		"(2026-04-16, 2026-04-16T14:00, or full RFC 3339)")
	f.StringVar(&d.end, "end", "", verb+" the end: the last day it runs through, or a day and a time")
	f.StringVar(&d.duration, "duration", "", verb+" how long it lasts (e.g. 15m, 1h, 2h30m, 3d)")
	f.StringVar(&d.location, "location", "", verb+" where it is")
	f.StringVar(&d.description, "description", "", verb+" the description")
	f.StringVar(&d.rrule, "rrule", "", verb+" the recurrence rule, e.g. FREQ=WEEKLY;COUNT=10")
	f.StringArrayVar(&d.reminders, "remind", nil,
		"Remind this long before the start, as DURATION or DURATION:email (repeatable)")
}

func eventsCreateCmd() *cobra.Command {
	var d details
	var calendar string
	var attendees []string
	c := &cobra.Command{
		Use:   "create",
		Short: "Create an event",
		Long: "Create an event.\n\n" +
			"--start takes a day and a time, as 2026-04-16T14:00. A bare day is an event\n" +
			"with no time of day, which --all-day has to say as well.\n\n" +
			"Without --end or --duration an event lasts as long as the calendar it is made\n" +
			"in says a new event lasts, which `settings calendars get` shows; an all-day\n" +
			"event lasts a day.\n\n" + colorParagraph,
		Args: cobra.NoArgs,
		RunE: kit.Run([]kit.Step{d.check(true)}, func(c *kit.Invocation) error {
			if d.title == "" || d.start == "" {
				return kit.Fail("An event needs a title and a start.").
					Hint(`--title Dentist --start 2026-04-16T14:00`)
			}
			calID, err := resolveCalendar(c, calendar)
			if err != nil {
				return err
			}
			zone, loc, err := workingZone(c)
			if err != nil {
				return err
			}
			written, err := ical.ParseTime(d.start, loc)
			if err != nil {
				return kit.Fail("--start: %v", err)
			}
			start := written.Anchored(zone)
			dur, fromCalendar, err := d.length(c, calID, start, loc)
			if err != nil {
				return err
			}
			status, err := d.status.Value()
			if err != nil {
				return err
			}
			var res *calsvc.EventResult
			if err := kit.Create(c, ui.ResultSpec{
				Action: ui.Created, Kind: "events", Name: d.title,
				Detail: clauses("for "+timeLabel(start), lengthClause(dur, fromCalendar)),
			}, func() (string, error) {
				var err error
				res, err = c.App.Calendar.EventCreate(c.Ctx, calID, calsvc.EventInput{
					Title: d.title, Location: d.location, Description: d.description,
					Start: start, Duration: dur,
					RRule: d.rrule, Reminders: d.reminders, Attendees: attendees,
					Status: calsvc.ICalStatus(status), Color: d.color.Value(),
				})
				if err != nil {
					return "", err
				}
				return kit.JoinPair(calID, res.ID), nil
			}); err != nil {
				return err
			}
			return tellAttendees(c, res)
		}),
	}
	d.register(c, "Set")
	c.Flags().StringVar(&calendar, "calendar", "", "Which calendar, by name or ID (default: your first)")
	c.Flags().BoolVar(&d.allDay, "all-day", false, "An event with no time of day")
	c.Flags().StringArrayVar(&attendees, "attendee", nil,
		"Invite someone, as EMAIL or EMAIL:optional; Proton users are added directly, "+
			"others are emailed (repeatable)")
	return c
}

func eventsUpdateCmd() *cobra.Command {
	var d details
	var onwards bool
	c := &cobra.Command{
		Use:   "update REF",
		Short: "Change an event's title, time, location, description or recurrence",
		Long: "Change an event.\n\n" +
			"Anything you do not mention is left alone, including the reminders and the\n" +
			"recurrence.\n\n" +
			"--start takes a day, which moves the event and leaves its time of day alone,\n" +
			"or a day and a time, which gives an all-day event a time of day and makes it\n" +
			"last what the calendar says a new event lasts. --all-day takes the time of day\n" +
			"away again; --all-day=false is the other direction and needs a --start saying\n" +
			"which time.\n\n" +
			"A reference that names one occurrence of a recurring event changes only that\n" +
			"occurrence. Add --onwards to change it and every later one, or drop the @ part\n" +
			"of the reference to change the whole series, which --dry-run will show you\n" +
			"before you do.\n\n" + colorParagraph,
		Args: cobra.ExactArgs(1),
		RunE: kit.Run([]kit.Step{d.check(false), kit.StepExpand}, func(c *kit.Invocation) error {
			calID, eventID, occurrence, err := resolveEvent(c, c.Args[0])
			if err != nil {
				return err
			}
			if onwards && occurrence == "" {
				return kit.Fail("--onwards needs a reference that names an occurrence.").
					Hint("`events list` prints one, as CALENDAR/EVENT@2026-04-16T09:00")
			}
			reached, err := reachOf(c, calID, eventID, occurrence)
			if err != nil {
				return err
			}
			patch, length, err := d.patch(c, calID, reached)
			if err != nil {
				return err
			}
			var res *calsvc.EventResult
			if err := kit.Mutate(c, ui.ResultSpec{
				Action: ui.Updated, Kind: "events", Count: 1, Name: reached.name(d.title),
				IDs: []string{c.Args[0]}, Preview: reached.preview(), Extra: reached.extra(),
				Detail: clauses(reached.clause(occurrence, onwards), reached.moved(patch), length),
			}, func() error {
				var err error
				switch {
				case occurrence == "":
					res, err = c.App.Calendar.EventUpdate(c.Ctx, calID, eventID, patch)
				case onwards:
					res, err = c.App.Calendar.SeriesSplit(c.Ctx, calID, eventID, occurrence, patch)
				default:
					res, err = c.App.Calendar.OccurrenceUpdate(c.Ctx, calID, eventID, occurrence, patch)
				}
				return err
			}); err != nil {
				return err
			}
			return tellAttendees(c, res)
		}),
	}
	d.register(c, "Replace")
	c.Flags().BoolVar(&d.allDay, "all-day", false, "Turn it into an event with no time of day")
	c.Flags().BoolVar(&d.noReminders, "no-remind", false, "Remove the reminders")
	c.Flags().BoolVar(&onwards, "onwards", false, "Also change every later occurrence of the series")
	return c
}

// patch turns the flags the user actually set into the change to make, and the
// clause a result needs when the length was not one of them. A flag left alone is
// absent rather than empty, which is what lets --description "" clear a
// description instead of meaning "leave it".
func (d *details) patch(c *kit.Invocation, calendarID string, reached *reach) (calsvc.EventPatch, string, error) {
	var p calsvc.EventPatch
	if c.Changed("title") {
		p.Title = &d.title
	}
	if c.Changed("location") {
		p.Location = &d.location
	}
	if c.Changed("description") {
		p.Description = &d.description
	}
	if c.Changed("rrule") {
		p.RRule = &d.rrule
	}
	if c.Changed("all-day") {
		p.AllDay = &d.allDay
	}
	if c.Changed("remind") && c.Changed("no-remind") {
		return p, "", kit.Fail("--remind and --no-remind contradict each other.")
	}
	if c.Changed("status") {
		word, err := d.status.Value()
		if err != nil {
			return p, "", err
		}
		status := calsvc.ICalStatus(word)
		p.Status = &status
	}
	if c.Changed("remind") {
		p.Reminders = &d.reminders
	}
	if c.Changed("no-remind") {
		none := []string{}
		p.Reminders = &none
	}
	if d.color.Set() {
		color := d.color.Value()
		p.Color = &color
	}

	if !c.Changed("start") {
		return p, "", nil
	}
	// A time is written against the zone the invocation works in, so re-timing an
	// event re-anchors it. That is only true when a time is being written: an
	// event whose location changed keeps the zone it was made in, wherever the
	// person changing it happens to be, and so does one moved by a bare day.
	zone, loc, err := workingZone(c)
	if err != nil {
		return p, "", err
	}
	written, err := ical.ParseTime(d.start, loc)
	if err != nil {
		return p, "", kit.Fail("--start: %v", err)
	}
	start := written.Anchored(zone)
	p.Start = &start

	// An event that had no time of day and is being given one needs a length to go
	// with it: the day it lasted is not one.
	gainsATimeOfDay := reached.allDay && !start.AllDay
	if !c.Changed("duration") && !c.Changed("end") && !gainsATimeOfDay {
		return p, "", nil
	}
	dur, fromCalendar, err := d.length(c, calendarID, start, loc)
	if err != nil {
		return p, "", err
	}
	p.Duration = &dur
	return p, lengthClause(dur, fromCalendar), nil
}

// ── the zone, and what a change reaches ──

// workingZone is the zone this invocation writes against, and the frame a
// wall-clock time given on the command line is read in.
func workingZone(c *kit.Invocation) (string, *time.Location, error) {
	zone, err := c.App.Zone(c.Ctx)
	if err != nil {
		return "", nil, err
	}
	loc, err := c.App.Location(c.Ctx)
	if err != nil {
		return "", nil, err
	}
	return zone, loc, nil
}

// timeLabel is when an event sits, said in the frame it is anchored to.
//
// The zone is on screen because it is the one part of a written time that the
// command line does not state and that nothing else would reveal until the event
// turned up an hour out in somebody else's calendar. A day has none to state: it
// has no time of day for a zone to move.
func timeLabel(d ical.DateTime) string {
	if d.AllDay {
		return d.Time.Format("2006-01-02") + " (all day)"
	}
	zone := d.TZID
	if zone == "" {
		zone = "UTC"
	}
	return d.Wall().Format("2006-01-02 15:04") + " " + zone
}

// lengthClause says how long an event lasts when the command line did not: the
// length is then the calendar's, which is the one part of the write that nothing
// on screen would otherwise reveal.
func lengthClause(dur time.Duration, fromCalendar bool) string {
	if !fromCalendar {
		return ""
	}
	return units.Duration(dur) + " long"
}

// previewSample is how many of a series' occurrences a preview draws.
//
// Enough to recognise the pattern and see where it starts, few enough that the
// question underneath is still on the screen. How many there are in all is in
// the sentence above the table, which is where a preview keeps its count.
const previewSample = 20

// reach is what a reference reaches: the event it names, and, when it names a
// series rather than one instance, the occurrences that come with it.
//
// It is read before the change is made, because a preview has to describe the
// same thing the change will do, and a series addressed by its bare reference is
// where those two are furthest apart - one reference, and every meeting it ever
// generates.
type reach struct {
	title  string
	allDay bool
	rows   []calsvc.Event
	// total is how many occurrences there are, or nil for a series with no end.
	total *int
	// series records that the reference named the whole of something recurring.
	series bool
}

func reachOf(c *kit.Invocation, calID, eventID, occurrence string) (*reach, error) {
	if occurrence != "" {
		ev, err := c.App.Calendar.EventGet(c.Ctx, calID, eventID, occurrence)
		if err != nil {
			return nil, err
		}
		one := 1
		return &reach{title: ev.Title, allDay: ev.AllDay, rows: []calsvc.Event{*ev}, total: &one}, nil
	}
	got, err := c.App.Calendar.Series(c.Ctx, calID, eventID, previewSample)
	if err != nil {
		return nil, err
	}
	r := &reach{rows: got.Rows, total: got.Total}
	if len(got.Rows) > 0 {
		first := got.Rows[0]
		r.title, r.allDay, r.series = first.Title, first.AllDay, first.RRule != ""
	}
	return r, nil
}

// name is what the sentence calls the event: what it is being renamed to, or
// what it is called now.
func (r *reach) name(renamed string) string {
	if renamed != "" {
		return renamed
	}
	return r.title
}

// clause says which occurrences the change reaches, so the preview, the
// confirmation and the result all describe the scope the reference asked for.
func (r *reach) clause(occurrence string, onwards bool) string {
	switch {
	case occurrence != "" && onwards:
		return "from " + occurrence + " onwards"
	case occurrence != "":
		return "on " + occurrence
	case !r.series:
		return ""
	case r.total == nil:
		return "and every occurrence of it, a series with no end"
	case *r.total > 1:
		return "and all " + ui.Quantity(*r.total, "occurrences") + " of it"
	}
	return ""
}

// at is where the event sits now, in the frame it is anchored to, which is what
// a change moves it from.
func (r *reach) at() (ical.DateTime, bool) {
	if len(r.rows) == 0 {
		return ical.DateTime{}, false
	}
	first := r.rows[0]
	if r.allDay {
		return ical.Day(first.Start), true
	}
	return ical.Timed(first.Start, first.Zone), true
}

// moved says where a change puts the event, whenever it puts it anywhere - a
// time of day taken away moves it as surely as a new date does.
func (r *reach) moved(p calsvc.EventPatch) string {
	at, ok := r.at()
	if !p.TouchesTimes() || !ok {
		return ""
	}
	return "now " + timeLabel(p.Lands(at))
}

func (r *reach) preview() func(*ui.UI) error {
	if !r.series {
		return nil
	}
	return kit.Preview("events", eventColumns(), r.rows)
}

func (r *reach) extra() map[string]any {
	if !r.series {
		return nil
	}
	return map[string]any{"occurrences": r.total}
}

// clauses joins the things a result has to say into one trailing sentence,
// leaving out whatever does not apply.
func clauses(parts ...string) string {
	kept := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" {
			kept = append(kept, part)
		}
	}
	return strings.Join(kept, ", ")
}

// check judges what the command line alone decides, before anything is asked of
// Proton: whether an event's length was stated twice, whether a length was stated
// with no beginning to hang off, and whether what was written agrees with itself
// about the one thing an event either has or has not - a time of day.
//
// It is a step so that it runs before the calendar is resolved. Without it a
// contradiction costs a sign-in and two requests to discover, which is the one
// thing local validation exists to prevent. The zone the account works in is one
// of those requests, so what is judged here is the form of what was written and
// never the instant it names: the same values are read again in that zone, which
// is where a reading that no clock ever shows is refused.
func (d *details) check(needsStart bool) kit.Step {
	return func(c *kit.Invocation) error {
		if d.end != "" && d.duration != "" {
			return kit.Fail("--end and --duration both say when it ends.").
				Hint("pass one of them.")
		}
		if !needsStart && !c.Changed("start") {
			for _, flag := range []string{"duration", "end"} {
				if c.Changed(flag) {
					return kit.Fail("--%s needs --start, since a length has to hang off a beginning.", flag)
				}
			}
			if c.Changed("all-day") && !d.allDay {
				return wantsATimeOfDay("2026-04-16")
			}
			return nil
		}
		if d.start == "" {
			return nil
		}
		start, err := ical.ParseTime(d.start, time.UTC)
		if err != nil {
			return kit.Fail("--start: %v", err)
		}
		day := start.Time.Format("2006-01-02")
		switch {
		case d.allDay && !start.AllDay:
			return kit.Fail("--all-day and a time of day in --start contradict each other.").
				Hint("--start " + day + " for the day alone.")
		case c.Changed("all-day") && !d.allDay && start.AllDay:
			return wantsATimeOfDay(day)
		case needsStart && start.AllDay && !d.allDay:
			return kit.Fail("--start names a day but not a time of day.").
				Hint("--start " + day + " --all-day for a whole day, or --start " + day + "T09:00.")
		}
		if d.end != "" {
			end, err := ical.ParseTime(d.end, time.UTC)
			if err != nil {
				return kit.Fail("--end: %v", err)
			}
			if end.AllDay != start.AllDay {
				return kit.Fail("--start and --end disagree on whether the event has a time of day.").
					Hint("write both as days, or both with a time.")
			}
		}
		if d.duration != "" && start.AllDay {
			dur, err := units.ParseDuration(d.duration)
			if err != nil {
				return kit.Fail("--duration: %v", err)
			}
			if dur%(24*time.Hour) != 0 {
				return kit.Fail("--duration for an all-day event is measured in days.").
					Hint("--duration 3d")
			}
		}
		return nil
	}
}

// wantsATimeOfDay is what --all-day=false asks for and cannot supply itself.
func wantsATimeOfDay(day string) error {
	return kit.Fail("--all-day=false gives the event a time of day, so --start has to say which.").
		Hint("--all-day=false --start " + day + "T13:00")
}

// length is how long an event lasts, said either way, and settled in one place
// for both commands.
//
// Proton's own composer takes an end time, and a duration is what a terminal
// reaches for, so both are accepted and neither is derived from the other's
// spelling. Which of them was given is settled by check, before this runs.
//
// An end that names a day is the last day the event is on, because that is what
// an end date means on a calendar - iCalendar's exclusive end is a storage
// convention and not something to make anybody count around.
//
// Absent either, an event with no time of day lasts a day, and one with a time of
// day lasts whatever the calendar it is in says a new event lasts, which is the
// answer Proton's own clients take. That the length came from there rather than
// from the command line is the second answer, for a result to say.
func (d *details) length(c *kit.Invocation, calendarID string, start ical.DateTime,
	loc *time.Location) (dur time.Duration, fromCalendar bool, err error) {
	const day = 24 * time.Hour
	switch {
	case d.end != "":
		end, err := ical.ParseTime(d.end, loc)
		if err != nil {
			return 0, false, kit.Fail("--end: %v", err)
		}
		if start.AllDay {
			days := end.Time.Sub(start.Time)/day + 1
			if days < 1 {
				return 0, false, kit.Fail("--end is before --start.")
			}
			return days * day, false, nil
		}
		if !end.Time.After(start.Time) {
			return 0, false, kit.Fail("--end is not after --start.")
		}
		return end.Time.Sub(start.Time), false, nil
	case d.duration != "":
		dur, err := units.ParseDuration(d.duration)
		if err != nil {
			return 0, false, kit.Fail("--duration: %v", err)
		}
		return dur, false, nil
	case start.AllDay:
		return day, false, nil
	}
	defaults, err := c.App.Calendar.CalendarDefaults(c.Ctx, calendarID)
	if err != nil {
		return 0, false, err
	}
	minutes := defaults.Duration
	if minutes <= 0 {
		minutes = composerDefaultMinutes
	}
	return time.Duration(minutes) * time.Minute, true, nil
}

// composerDefaultMinutes is how long a new event lasts when its calendar has
// never been asked, which is the length Proton's own composer opens with.
const composerDefaultMinutes = 30

func eventsRespondCmd() *cobra.Command {
	// The word is "answer", not "status", because an event has a status of its
	// own - whether it is going ahead - and this is the reply one participant
	// sends. Two subjects sharing a flag name is how a name comes to mean two
	// things.
	answer := &kit.Enum{
		Name: "answer", Usage: "Your reply to the invitation",
		Values: []string{"accept", "tentative", "decline"},
	}
	c := &cobra.Command{
		Use:   "respond REF",
		Short: "Answer an invitation, telling the organizer",
		Args:  cobra.ExactArgs(1),
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			reply, err := answer.Value()
			if err != nil {
				return err
			}
			code, err := calsvc.StatusFromFlag(reply)
			if err != nil {
				return err
			}
			calID, eventID, occurrence, err := resolveEvent(c, c.Args[0])
			if err != nil {
				return err
			}
			if occurrence != "" {
				return kit.Fail("An answer applies to the whole series, not to one occurrence.").
					Hint(fmt.Sprintf("drop the @%s from the reference", occurrence))
			}
			var res *calsvc.RespondResult
			if err := kit.Mutate(c, ui.ResultSpec{
				Action: ui.Responded, Kind: "events", Count: 1,
				Detail: reply, IDs: []string{kit.JoinPair(calID, eventID)},
			}, func() error {
				var err error
				res, err = c.App.Calendar.EventRespond(c.Ctx, calID, eventID, code)
				return err
			}); err != nil {
				return err
			}
			if res != nil && res.Reply != nil {
				if err := sendICS(c, res.Reply.Recipients, res.Reply.Subject, res.Reply.Body, res.Reply.ICS, "REPLY"); err != nil {
					c.Warn("You responded, but telling the organizer by email failed: %v", err)
				}
			}
			return nil
		}),
	}
	answer.Register(c)
	_ = c.MarkFlagRequired("answer")
	return c
}

func eventsDeleteCmd() *cobra.Command {
	var onwards bool
	c := &cobra.Command{
		Use:   "delete REF...",
		Short: "Delete events",
		Long: "Delete events.\n\n" +
			"A reference that names one occurrence of a recurring event deletes only that\n" +
			"occurrence. Add --onwards to delete it and every later one, or drop the @ part\n" +
			"of the reference to delete the whole series.",
		Args: cobra.MinimumNArgs(1),
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			type target struct {
				ref                        string
				calID, eventID, occurrence string
			}
			targets := make([]target, 0, len(c.Args))
			var rows []calsvc.Event
			var reached *reach
			counted, endless := 0, false
			for _, ref := range c.Args {
				calID, eventID, occurrence, err := resolveEvent(c, ref)
				if err != nil {
					return err
				}
				if onwards && occurrence == "" {
					return kit.Fail("--onwards needs a reference that names an occurrence.").
						Hint("`events list` prints one, as CALENDAR/EVENT@2026-04-16T09:00")
				}
				reached, err = reachOf(c, calID, eventID, occurrence)
				if err != nil {
					return err
				}
				targets = append(targets, target{ref: ref, calID: calID, eventID: eventID, occurrence: occurrence})
				rows = append(rows, reached.rows...)
				if reached.total == nil {
					endless = true
					continue
				}
				counted += *reached.total
			}

			refs := make([]string, 0, len(targets))
			for _, t := range targets {
				refs = append(refs, t.ref)
			}
			// One reference names one stored event however many meetings it holds,
			// so the count says how many were addressed and the clause beside it
			// says how far each one goes. Several references have nothing that
			// could be said of all of them at once, so they say nothing.
			whole := &reach{rows: rows, total: &counted, series: reached.series}
			if endless {
				whole.total = nil
			}
			detail, name := "", ""
			if len(targets) == 1 {
				detail = reached.clause(targets[0].occurrence, onwards)
				name = reached.title
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Deleted, Kind: "events", Count: len(targets), IDs: refs,
				Name: name, Detail: detail, Preview: whole.preview(), Extra: whole.extra(),
			}, func() error {
				for _, t := range targets {
					var err error
					switch {
					case t.occurrence == "":
						err = c.App.Calendar.EventDelete(c.Ctx, t.calID, t.eventID)
					case onwards:
						err = c.App.Calendar.SeriesTruncate(c.Ctx, t.calID, t.eventID, t.occurrence)
					default:
						err = c.App.Calendar.OccurrenceDelete(c.Ctx, t.calID, t.eventID, t.occurrence)
					}
					if err != nil {
						return err
					}
				}
				return nil
			})
		}),
	}
	c.Flags().BoolVar(&onwards, "onwards", false, "Also delete every later occurrence of the series")
	return c
}

// ── helpers ──

// tellAttendees emails the participants Proton cannot reach through their own
// calendar. A failure here does not undo the change that has already been made.
func tellAttendees(c *kit.Invocation, res *calsvc.EventResult) error {
	if res == nil || res.Mail == nil {
		return nil
	}
	m := res.Mail
	if err := sendICS(c, m.Recipients, m.Subject, m.Body, m.ICS, m.Method); err != nil {
		c.Warn("The change was saved, but the email to %s failed: %v",
			ui.Quantity(len(m.Recipients), "attendees"), err)
	}
	return nil
}

// listedCalendars is the set a listing covers: the one asked for, or all of them.
func listedCalendars(c *kit.Invocation, ref string) ([]string, error) {
	if ref != "" {
		id, err := resolveCalendar(c, ref)
		if err != nil {
			return nil, err
		}
		return []string{id}, nil
	}
	cals, err := c.App.Calendar.CalendarsList(c.Ctx)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(cals))
	for _, cal := range cals {
		ids = append(ids, cal.ID)
	}
	return ids, nil
}

func resolveCalendar(c *kit.Invocation, ref string) (string, error) {
	expanded, err := kit.Expand(c.App, ref)
	if err != nil {
		return "", err
	}
	return c.App.Calendar.ResolveCalendarID(c.Ctx, expanded)
}

// ── export ──

// Export writes what a calendar holds as an .ics file, which is the format every
// other calendar reads and the one Proton's own settings page offers.
//
// A series goes out once, carrying its rule, rather than expanded into the
// occurrences a listing shows: that is what a calendar file is, and expanding it
// would turn one weekly standup into fifty-two unrelated events.
func eventsExportCmd() *cobra.Command {
	var calendar string
	var days kit.DayRange
	var dest kit.Destination
	c := &cobra.Command{
		Use:   "export",
		Short: "Write events out as an .ics file",
		Long: "Write events out as an .ics file.\n\n" +
			"--start and --end are whole days in your own zone, both included.\n\n" +
			"A recurring series is written once, with its rule, so another client reads\n" +
			"it back as the same series.",
		Args: cobra.NoArgs,
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			if err := dest.Validate(true); err != nil {
				return err
			}
			calIDs, err := listedCalendars(c, calendar)
			if err != nil {
				return err
			}
			first, last := days.Or(calsvc.DefaultDays())
			events, err := c.App.Calendar.EventsExport(c.Ctx, calIDs, ical.Days(first, last))
			if err != nil {
				return err
			}
			doc := []byte(ical.Calendar(events))
			name := fmt.Sprintf("%s to %s.ics", first.Format("2006-01-02"), last.Format("2006-01-02"))
			var written string
			if err := kit.Mutate(c, ui.ResultSpec{
				Action: ui.Exported, Kind: "events", Count: len(events),
				Detail: "to " + dest.Describe(), AnswerFollows: dest.Stdout(),
			}, func() error {
				written, err = dest.Write(c, name, doc)
				return err
			}); err != nil {
				return err
			}
			if written != "" {
				c.Note("Wrote %s.", written)
			}
			return nil
		}),
	}
	c.Flags().StringVar(&calendar, "calendar", "", "Which calendar, by name or ID (default: all of them)")
	days.Register(c)
	dest.Register(c)
	return c
}

// ── import ──

// Import reads an .ics file into a calendar, which is the other half of export
// and the thing Proton's own settings page offers beside it.
func eventsImportCmd() *cobra.Command {
	var calendar string
	c := &cobra.Command{
		Use:   "import PATH",
		Short: "Read events in from an .ics file",
		Long: "Read events in from an .ics file, or from stdin with -.\n\n" +
			"An event is addressed by its UID, so reading a file back changes that event\n" +
			"rather than making a second one.\n\n" +
			"Participants are left out. An imported event is a record; no invitations\n" +
			"are sent.\n\n" +
			"A color in the file becomes the nearest of Proton's twenty accent colors,\n" +
			"since those are the only ones it stores.",
		Args: cobra.ExactArgs(1),
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			text, err := kit.ReadTextArg(c, c.Args[0], "PATH")
			if err != nil {
				return err
			}
			if c.Args[0] != "-" {
				b, err := os.ReadFile(c.Args[0])
				if err != nil {
					return kit.Fail("could not read %s: %v", c.Args[0], err)
				}
				text = string(b)
			}
			events, err := ical.ParseCalendar(text)
			if err != nil {
				return kit.Fail("%s is not a calendar file: %v", c.Args[0], err)
			}
			if len(events) == 0 {
				return kit.Fail("%s holds no events.", c.Args[0])
			}
			calID, err := resolveCalendar(c, calendar)
			if err != nil {
				return err
			}
			return kit.Attempt(c, ui.ResultSpec{
				Action: ui.Imported, Kind: "events", Count: len(events),
				Detail:  "from " + c.Args[0],
				Preview: kit.Preview("events", importColumns(), events),
			}, func() ([]calsvc.SkippedEvent, error) {
				res, err := c.App.Calendar.EventsImport(c.Ctx, calID, events)
				if err != nil {
					return nil, err
				}
				return res.Skipped, nil
			})
		}),
	}
	c.Flags().StringVar(&calendar, "calendar", "", "Which calendar to import into, by name or ID (default: your first)")
	return c
}

// importColumns previews a file's events the way a listing shows them, so a dry
// run answers "what is in this file" as well as "what would happen".
func importColumns() []ui.Column[ical.VEvent] {
	return []ui.Column[ical.VEvent]{
		{Header: "WHEN", Cell: func(v ical.VEvent) string {
			if v.Start.IsZero() {
				return ""
			}
			if v.Start.AllDay {
				return v.Start.Time.Format("2006-01-02")
			}
			return v.Start.Time.Local().Format("2006-01-02 15:04")
		}},
		{Header: "TITLE", Flex: true, Cell: func(v ical.VEvent) string { return v.Summary }},
		{Header: "REPEATS", Cell: func(v ical.VEvent) string { return v.RRule }},
	}
}
