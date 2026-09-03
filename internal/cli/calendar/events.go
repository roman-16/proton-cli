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

// details are the fields an event carries. create and update share them, so the
// two commands cannot disagree about what an event is.
type details struct {
	title, location, description string
	start, duration, rrule       string
	end                          string
	status                       *kit.Enum
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
	f := c.Flags()
	f.StringVar(&d.title, "title", "", verb+" the title")
	f.StringVar(&d.start, "start", "", verb+" the start (RFC 3339, or YYYY-MM-DDTHH:MM)")
	f.StringVar(&d.end, "end", "", verb+" the end (RFC 3339, or YYYY-MM-DDTHH:MM)")
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
		Args:  cobra.NoArgs,
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
			start, err := ical.ParseTime(d.start, loc)
			if err != nil {
				return kit.Fail("--start: %v", err)
			}
			dur, err := d.span(start, loc)
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
				Detail: "for " + timeLabel(start, d.allDay, zone),
			}, func() (string, error) {
				var err error
				res, err = c.App.Calendar.EventCreate(c.Ctx, calID, calsvc.EventInput{
					Title: d.title, Location: d.location, Description: d.description,
					Start: start, End: start.Add(dur), AllDay: d.allDay, Zone: zone,
					RRule: d.rrule, Reminders: d.reminders, Attendees: attendees,
					Status: calsvc.ICalStatus(status),
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
			"A reference that names one occurrence of a recurring event changes only that\n" +
			"occurrence. Add --onwards to change it and every later one, or drop the @ part\n" +
			"of the reference to change the whole series, which --dry-run will show you\n" +
			"before you do.",
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
			patch, err := d.patch(c, reached)
			if err != nil {
				return err
			}
			var res *calsvc.EventResult
			if err := kit.Mutate(c, ui.ResultSpec{
				Action: ui.Updated, Kind: "events", Count: 1, Name: reached.name(d.title),
				IDs: []string{c.Args[0]}, Preview: reached.preview(), Extra: reached.extra(),
				Detail: clauses(reached.clause(occurrence, onwards), reached.moved(patch)),
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

// patch turns the flags the user actually set into the change to make. A flag left
// alone is absent rather than empty, which is what lets --description "" clear a
// description instead of meaning "leave it".
func (d *details) patch(c *kit.Invocation, reached *reach) (calsvc.EventPatch, error) {
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
		return p, kit.Fail("--remind and --no-remind contradict each other.")
	}
	if c.Changed("status") {
		word, err := d.status.Value()
		if err != nil {
			return p, err
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

	if !c.Changed("start") {
		return p, nil
	}
	// A time is written against the zone the invocation works in, so re-timing an
	// event re-anchors it. That is only true when a time is being written: an
	// event whose location changed keeps the zone it was made in, wherever the
	// person changing it happens to be.
	zone, loc, err := workingZone(c)
	if err != nil {
		return p, err
	}
	start, err := ical.ParseTime(d.start, loc)
	if err != nil {
		return p, kit.Fail("--start: %v", err)
	}
	p.Start, p.Zone = &start, &zone
	if c.Changed("duration") || c.Changed("end") {
		dur, err := d.span(start, loc)
		if err != nil {
			return p, err
		}
		end := start.Add(dur)
		p.End = &end
	}
	return p, nil
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

// timeLabel is when an event sits, said in the zone it is anchored to.
//
// The zone is on screen because it is the one part of a written time that the
// command line does not state and that nothing else would reveal until the event
// turned up an hour out in somebody else's calendar.
func timeLabel(t time.Time, allDay bool, zone string) string {
	if allDay {
		return t.Format("2006-01-02") + " (all day)"
	}
	return t.Format("2006-01-02 15:04") + " " + zone
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

// moved says where a change puts the event, when it moves it at all.
func (r *reach) moved(p calsvc.EventPatch) string {
	if p.Start == nil {
		return ""
	}
	allDay := r.allDay
	if p.AllDay != nil {
		allDay = *p.AllDay
	}
	zone := ""
	if p.Zone != nil {
		zone = *p.Zone
	}
	return "now " + timeLabel(*p.Start, allDay, zone)
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
// Proton: whether an event's length was stated twice, and whether a length was
// stated with no beginning to hang off.
//
// It is a step so that it runs before the calendar is resolved. Without it a
// contradiction costs a sign-in and two requests to discover, which is the one
// thing local validation exists to prevent.
func (d *details) check(needsStart bool) kit.Step {
	return func(c *kit.Invocation) error {
		if d.end != "" && d.duration != "" {
			return kit.Fail("--end and --duration both say when it ends.").
				Hint("pass one of them.")
		}
		if needsStart || c.Changed("start") {
			return nil
		}
		for _, flag := range []string{"duration", "end"} {
			if c.Changed(flag) {
				return kit.Fail("--%s needs --start, since a length has to hang off a beginning.", flag)
			}
		}
		return nil
	}
}

// span is how long an event lasts, said either way.
//
// Proton's own composer takes an end time, and a duration is what a terminal
// reaches for, so both are accepted and neither is derived from the other's
// spelling. Which of them was given is settled by check, before this runs.
//
// Absent either, an event lasts an hour - a whole day for one with no time of
// day, which is measured in days.
func (d *details) span(start time.Time, loc *time.Location) (time.Duration, error) {
	switch {
	case d.end != "":
		end, err := ical.ParseTime(d.end, loc)
		if err != nil {
			return 0, kit.Fail("--end: %v", err)
		}
		if !end.After(start) {
			return 0, kit.Fail("--end is not after --start.")
		}
		return end.Sub(start), nil
	case d.duration != "":
		dur, err := units.ParseDuration(d.duration)
		if err != nil {
			return 0, kit.Fail("--duration: %v", err)
		}
		return dur, nil
	case d.allDay:
		return 24 * time.Hour, nil
	}
	return time.Hour, nil
}

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
			"are sent.",
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
