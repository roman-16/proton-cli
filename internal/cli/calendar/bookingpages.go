package calendar

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/roman-16/proton-cli/internal/cli/kit"
	"github.com/roman-16/proton-cli/internal/errs"
	calsvc "github.com/roman-16/proton-cli/internal/service/calendar"
	"github.com/roman-16/proton-cli/internal/ui"
	"github.com/roman-16/proton-cli/internal/units"
)

const bookingWarning = "Anyone with this link can see when you are free and book a time with you, " +
	"until the page is deleted."

const weekdayOffer = "mon-fri=09:00-17:00"

func bookingPagesCmd() *cobra.Command {
	c := &cobra.Command{Use: "booking-pages", Short: "Pages where people book a time with you"}
	c.AddCommand(bookingPagesCreateCmd(), bookingPagesDeleteCmd(), bookingPagesGetCmd(),
		bookingPagesListCmd(), bookingPagesUpdateCmd())
	return c
}

func bookingPageList(c *kit.Invocation) *kit.Lookup[calsvc.BookingPage] {
	return &kit.Lookup[calsvc.BookingPage]{
		Kind:   "booking page",
		Load:   func(ctx context.Context) ([]calsvc.BookingPage, error) { return c.App.Calendar.BookingPages(ctx) },
		ID:     func(p calsvc.BookingPage) string { return p.ID },
		Handle: func(p calsvc.BookingPage) string { return p.Title },
	}
}

func bookingPageColumns() []ui.Column[calsvc.BookingPage] {
	return []ui.Column[calsvc.BookingPage]{
		{Header: "ID", ID: true, Cell: func(p calsvc.BookingPage) string { return p.ID }},
		{Header: "TITLE", Flex: true, Handle: true, Cell: func(p calsvc.BookingPage) string { return p.Title }},
		{Header: "CALENDAR", Flex: true, Cell: func(p calsvc.BookingPage) string { return p.Calendar }},
		{Header: "LOCATION", Flex: true, Cell: meetingPlace},
		{Header: "CREATED", Cell: func(p calsvc.BookingPage) string { return units.Time(p.Created) }},
	}
}

func meetingPlace(p calsvc.BookingPage) string {
	if p.Meet {
		return "Proton Meet"
	}
	return p.Location
}

func bookingPageFields(p *calsvc.BookingPageDetail, signed bool) []ui.Field {
	fields := []ui.Field{
		{Label: "URL", Value: p.URL},
		{Label: "Title", Value: p.Title, Handle: true},
		{Label: "Calendar", Value: p.Calendar},
		{Label: "Contact", Value: p.Contact},
		{Label: "Duration", Value: p.Duration},
		{Label: "Available", Value: strings.Join(calsvc.Tokens(p.Available), "\n")},
		{Label: "Zone", Value: p.Zone},
		{Label: "Notice", Value: p.Notice},
		{Label: "Blocked by", Value: strings.Join(p.BlockedBy, ", ")},
		{Label: "Location", Value: meetingPlace(p.BookingPage)},
		{Label: "Description", Value: p.Description},
		{Label: "Created", Value: units.Time(p.Created)},
	}
	if signed {
		fields = append(fields, kit.SignatureField(string(p.Signature)))
	}
	return append(fields, ui.Field{Label: "ID", Value: p.ID, ID: true})
}

type pageFlags struct {
	title, description, location string
	duration                     string
	available, blockedBy         []string
	notice                       *kit.Enum
	meet, noBlockedBy            bool
	windows                      []calsvc.Window
	length                       time.Duration
}

func (f *pageFlags) register(c *cobra.Command, verb string) {
	f.notice = &kit.Enum{
		Name: "notice", Usage: verb + " how soon before an appointment it can still be booked",
		Values: calsvc.NoticeWords(),
	}
	available := "When appointments can be booked, as DAY=START-END (repeatable)"
	blockedBy := "Another calendar whose events keep a time from being booked, by name or ID (repeatable)"
	if verb == "Set" {
		f.notice.Default = calsvc.NoticeNone
	} else {
		available = "Replace when appointments can be booked, as DAY=START-END (repeatable)"
		blockedBy = "Replace the other calendars whose events keep a time from being booked (repeatable)"
	}
	f.notice.Register(c)
	fl := c.Flags()
	fl.StringVar(&f.title, "title", "", verb+" the title")
	fl.StringVar(&f.description, "description", "", verb+" what people read before they book")
	fl.StringVar(&f.duration, "duration", "", verb+" how long an appointment lasts: "+
		strings.Join(calsvc.BookingDurationWords(), ", "))
	fl.StringArrayVar(&f.available, "available", nil, available)
	fl.StringArrayVar(&f.blockedBy, "blocked-by", nil, blockedBy)
}

func (f *pageFlags) judge(c *kit.Invocation) error {
	f.title = strings.TrimSpace(f.title)
	f.description = strings.TrimSpace(f.description)
	f.location = strings.TrimSpace(f.location)
	for _, limit := range []struct {
		what, value string
		most        int
	}{
		{"title", f.title, calsvc.BookingTitleLimit},
		{"location", f.location, calsvc.BookingLocationLimit},
		{"description", f.description, calsvc.BookingDescriptionLimit},
	} {
		if len([]rune(limit.value)) > limit.most {
			return kit.Fail("A booking page's %s may be at most %d characters.", limit.what, limit.most)
		}
	}
	if c.Changed("location") && f.location == "" {
		return kit.Fail("--location needs a place.").Hint("--location 'Hauptplatz 1, Graz'", "--meet")
	}
	if f.duration != "" {
		d, err := units.ParseDuration(f.duration)
		if err != nil || !calsvc.BookingDurationOffered(d) {
			return kit.Fail("--duration accepts: %s.", strings.Join(calsvc.BookingDurationWords(), ", "))
		}
		f.length = d
	}
	if len(f.available) > 0 {
		windows, err := parseAvailable(f.available)
		if err != nil {
			return err
		}
		f.windows = windows
	}
	return nil
}

func availableGrammar() error {
	return kit.Fail("--available takes DAY=START-END, as mon=09:00-17:00 or 2026-10-05=09:00-12:00.")
}

func parseAvailable(tokens []string) ([]calsvc.Window, error) {
	var out []calsvc.Window
	var weekly, dated bool
	for _, raw := range tokens {
		token := strings.TrimSpace(raw)
		days, hours, ok := strings.Cut(token, "=")
		if !ok {
			return nil, availableGrammar()
		}
		from, to, ok := strings.Cut(hours, "-")
		if !ok {
			return nil, availableGrammar()
		}
		start, startOK := clockOf(from, false)
		end, endOK := clockOf(to, true)
		if !startOK || !endOK {
			return nil, availableGrammar()
		}
		if end <= start {
			return nil, kit.Fail("--available %s ends before it starts; a window stays within one day.", token)
		}
		named, isDated, err := daysOf(days)
		if err != nil {
			return nil, kit.Fail("--available %s: %v", token, err)
		}
		weekly, dated = weekly || !isDated, dated || isDated
		if weekly && dated {
			return nil, kit.Fail("A booking page repeats every week or runs on set dates, not both.")
		}
		for _, day := range named {
			out = append(out, calsvc.Window{Day: day, Start: start, End: end})
		}
	}
	slices.SortStableFunc(out, func(a, b calsvc.Window) int {
		if a.Day != b.Day {
			return dayOrder(a.Day) - dayOrder(b.Day)
		}
		return strings.Compare(a.Start, b.Start)
	})
	for i := 1; i < len(out); i++ {
		if out[i].Day == out[i-1].Day && out[i].Start < out[i-1].End {
			return nil, kit.Fail("--available %s and %s overlap.", out[i-1], out[i])
		}
	}
	return out, nil
}

func clockOf(s string, end bool) (string, bool) {
	s = strings.TrimSpace(s)
	if end && (s == "24:00" || s == "00:00") {
		return "24:00", true
	}
	t, err := time.Parse("15:04", s)
	if err != nil {
		return "", false
	}
	return t.Format("15:04"), true
}

func daysOf(s string) ([]string, bool, error) {
	var out []string
	var dated, weekly bool
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if _, err := time.Parse("2006-01-02", part); err == nil {
			out, dated = append(out, part), true
			continue
		}
		weekly = true
		first, last, isRange := strings.Cut(part, "-")
		from, err := units.ParseWeekday(first)
		if err != nil {
			return nil, false, err
		}
		to := from
		if isRange {
			if to, err = units.ParseWeekday(last); err != nil {
				return nil, false, err
			}
		}
		for d := from; ; d = (d + 1) % 7 {
			out = append(out, units.Weekday(d))
			if d == to {
				break
			}
		}
	}
	if dated && weekly {
		return nil, false, errors.New("a booking page repeats every week or runs on set dates, not both")
	}
	return out, dated, nil
}

func dayOrder(day string) int {
	if d, err := units.ParseWeekday(day); err == nil {
		return (int(d) + 6) % 7
	}
	t, _ := time.Parse("2006-01-02", day)
	return 7 + int(t.Unix()/86400)
}

func blockingCalendars(c *kit.Invocation, refs []string, own string) ([]string, error) {
	out := []string{}
	for _, ref := range refs {
		expanded, err := kit.Expand(c.App, strings.TrimSpace(ref))
		if err != nil {
			return nil, err
		}
		cal, err := calendarList(c).Find(c.Ctx, expanded)
		if err != nil {
			return nil, err
		}
		if err := cal.BlocksBookings(); err != nil {
			return nil, err
		}
		if cal.ID != own && !slices.Contains(out, cal.ID) {
			out = append(out, cal.ID)
		}
	}
	return out, nil
}

func bookingCalendar(c *kit.Invocation, ref string) (calsvc.Calendar, error) {
	if ref == "" {
		cal, err := c.App.Calendar.DefaultBookingCalendar(c.Ctx)
		if errors.Is(err, calsvc.ErrNoCalendarTakesBookings) {
			return cal, kit.Fail("You have no calendar of your own to take bookings.").
				Hint(kit.Program + " calendar settings calendars create --name Personal")
		}
		return cal, err
	}
	expanded, err := kit.Expand(c.App, ref)
	if err != nil {
		return calsvc.Calendar{}, err
	}
	cal, err := calendarList(c).Find(c.Ctx, expanded)
	if err != nil {
		return cal, err
	}
	return cal, cal.TakesBookings()
}

func calendarLength(c *kit.Invocation, calendarID string) (time.Duration, error) {
	defaults, err := c.App.Calendar.CalendarDefaults(c.Ctx, calendarID)
	if err != nil {
		return 0, err
	}
	d := time.Duration(defaults.Duration) * time.Minute
	if !calsvc.BookingDurationOffered(d) {
		d = composerDefaultMinutes * time.Minute
	}
	return d, nil
}

func bookingPagesCreateCmd() *cobra.Command {
	var f pageFlags
	var calendar string
	c := &cobra.Command{
		Use:   "create",
		Short: "Make a booking page and print its link",
		Long: "Make a booking page and print its link.\n\n" +
			"Anyone with the link can see when you are free and book an appointment, which\n" +
			"lands in --calendar as an event.\n\n" +
			"--available is when appointments can be booked, as DAY=START-END, repeatable.\n" +
			"A weekday repeats every week; a date is that day alone, and a page is one or\n" +
			"the other:\n\n" +
			"  mon=09:00-17:00         every Monday\n" +
			"  mon-fri=09:00-12:00     every weekday morning\n" +
			"  mon,wed=14:00-18:00     every Monday and Wednesday afternoon\n" +
			"  2026-10-05=09:00-12:00  that morning only\n\n" +
			"Without it, a page offers Monday to Friday, 09:00-17:00, every week. Times are\n" +
			"read in your zone, and 24:00 ends a window at midnight.\n\n" +
			"Without --duration, an appointment lasts what a new event lasts in --calendar.\n" +
			"Without --location, each appointment gets a Proton Meet link. A page offers at\n" +
			"most 200 appointments. How many pages you may have depends on your plan; a free\n" +
			"plan allows none.",
		RunE: kit.Run([]kit.Step{f.judge}, func(c *kit.Invocation) error {
			if f.title == "" {
				return kit.Fail("A booking page needs a title.").Hint("--title 'Intro call'")
			}
			if len(f.windows) == 0 {
				windows, err := parseAvailable([]string{weekdayOffer})
				if err != nil {
					return err
				}
				f.windows = windows
			}
			loc, err := c.App.Location(c.Ctx)
			if err != nil {
				return err
			}
			if f.length != 0 {
				if _, err := calsvc.Offer(f.windows, f.length, loc, time.Now()); err != nil {
					return err
				}
			}
			notice, err := f.notice.Value()
			if err != nil {
				return err
			}
			if err := c.App.Calendar.BookingRoom(c.Ctx); err != nil {
				return err
			}
			cal, err := bookingCalendar(c, calendar)
			if err != nil {
				return err
			}
			length := f.length
			if length == 0 {
				if length, err = calendarLength(c, cal.ID); err != nil {
					return err
				}
			}
			slots, err := calsvc.Offer(f.windows, length, loc, time.Now())
			if err != nil {
				return err
			}
			blocked, err := blockingCalendars(c, f.blockedBy, cal.ID)
			if err != nil {
				return err
			}
			var made *calsvc.BookingPageDetail
			if err := kit.Mutate(c, ui.ResultSpec{
				Action: ui.Created, Kind: "booking pages", Count: 1, Name: f.title,
				Detail: "in " + cal.Name, AnswerFollows: true,
			}, func() error {
				made, err = c.App.Calendar.BookingPageCreate(c.Ctx, cal, calsvc.NewBookingPage{
					Title: f.title, Description: f.description, Location: f.location,
					Slots: slots, Notice: notice, BlockedBy: blocked,
				})
				return err
			}); err != nil {
				return err
			}
			if made == nil || c.App.DryRun {
				return nil
			}
			c.Warn("%s", bookingWarning)
			return kit.Show(c, ui.RecordSpec{Object: made, Fields: bookingPageFields(made, false)})
		}),
	}
	f.register(c, "Set")
	c.Flags().StringVar(&f.location, "location", "", "Set where appointments take place (default: a Proton Meet link)")
	c.Flags().StringVar(&calendar, "calendar", "",
		"Which calendar bookings go into, by name or ID (default: your default calendar)")
	return c
}

func bookingPagesGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get REF",
		Short: "Show one booking page, link and all",
		Long: "Show one booking page, link and all.\n\n" +
			"The link appears here and when the page is made, never in a listing.",
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			found, err := bookingPageList(c).Find(c.Ctx, c.Args[0])
			if err != nil {
				return err
			}
			page, err := c.App.Calendar.BookingPageOpen(c.Ctx, found.ID)
			if err != nil {
				return err
			}
			c.Warn("%s", bookingWarning)
			return kit.Show(c, ui.RecordSpec{Object: page, Fields: bookingPageFields(page, true)})
		}),
	}
}

func bookingPagesListCmd() *cobra.Command {
	var held kit.Held[calsvc.BookingPage]
	c := &cobra.Command{
		Use:   "list",
		Short: "List your booking pages",
		Long: "List your booking pages.\n\n" +
			"The links are not shown; `booking-pages get` shows one.",
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			rows, err := bookingPageList(c).Rows(c.Ctx)
			if err != nil {
				return err
			}
			return held.Answer(c, ui.TableSpec[calsvc.BookingPage]{
				Noun: "booking pages", Columns: bookingPageColumns(),
			}, rows)
		}),
	}
	held.Register(c, "booking pages",
		kit.Key[calsvc.BookingPage]{Name: "title", Less: func(a, b calsvc.BookingPage) int {
			return kit.Fold(a.Title, b.Title)
		}},
		kit.Key[calsvc.BookingPage]{Name: "calendar", Less: func(a, b calsvc.BookingPage) int {
			return kit.Fold(a.Calendar, b.Calendar)
		}},
		kit.Key[calsvc.BookingPage]{Name: "created", Less: func(a, b calsvc.BookingPage) int {
			return kit.Ints(a.Created, b.Created)
		}},
	)
	return c
}

func bookingPagesUpdateCmd() *cobra.Command {
	var f pageFlags
	c := &cobra.Command{
		Use:   "update REF",
		Short: "Change what a booking page offers",
		Long: "Change what a booking page offers.\n\n" +
			"Anything you do not mention is left alone. --available replaces every window,\n" +
			"read in your zone; --duration alone re-cuts the windows the page offers now.\n" +
			"--blocked-by replaces the list and --no-blocked-by empties it. The calendar a\n" +
			"page books into, and its link, cannot be changed.",
		RunE: kit.Run([]kit.Step{kit.StepExpand, f.judge}, func(c *kit.Invocation) error {
			changed := slices.ContainsFunc([]string{"title", "description", "location", "meet", "duration",
				"available", "notice", "blocked-by", "no-blocked-by"}, c.Changed)
			if !changed {
				return kit.Fail("Nothing to change.").Hint("--title 'Intro call'", "--available mon=09:00-12:00")
			}
			if c.Changed("title") && f.title == "" {
				return kit.Fail("A booking page needs a title.").Hint("--title 'Intro call'")
			}
			loc, err := c.App.Location(c.Ctx)
			if err != nil {
				return err
			}
			if f.length != 0 && len(f.windows) > 0 {
				if _, err := calsvc.Offer(f.windows, f.length, loc, time.Now()); err != nil {
					return err
				}
			}
			found, err := bookingPageList(c).Find(c.Ctx, c.Args[0])
			if err != nil {
				return err
			}
			cal, err := calendarList(c).Find(c.Ctx, found.CalendarID)
			if err != nil {
				return err
			}
			if !cal.Active() {
				return errs.Naming(found.Title, errs.Naming(cal.Name, kit.Fail(
					"\"%s\" books into %s, which is disabled, so the page cannot be changed.", found.Title, cal.Name)))
			}
			ch, err := c.App.Calendar.BookingPageChange(c.Ctx, found.ID)
			if err != nil {
				return err
			}
			patch, err := f.patch(c, ch, loc)
			if err != nil {
				return err
			}
			if err := ch.Apply(patch); err != nil {
				return err
			}
			name := found.Title
			if patch.Title != nil {
				name = *patch.Title
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Updated, Kind: "booking pages", Count: 1, Name: name, IDs: []string{found.ID},
			}, func() error {
				return c.App.Calendar.BookingPageSave(c.Ctx, ch)
			})
		}),
	}
	f.register(c, "Replace")
	c.Flags().StringVar(&f.location, "location", "", "Replace where appointments take place")
	c.Flags().BoolVar(&f.meet, "meet", false, "Give each appointment a Proton Meet link instead of a place")
	c.Flags().BoolVar(&f.noBlockedBy, "no-blocked-by", false,
		"Let only the page's own calendar keep times from being booked")
	kit.Exclusive(c, "location", "meet")
	kit.Exclusive(c, "blocked-by", "no-blocked-by")
	return c
}

func (f *pageFlags) patch(c *kit.Invocation, ch *calsvc.BookingChange, loc *time.Location) (calsvc.BookingPatch, error) {
	var p calsvc.BookingPatch
	if c.Changed("title") {
		p.Title = &f.title
	}
	if c.Changed("description") {
		p.Description = &f.description
	}
	if c.Changed("location") {
		p.Location = &f.location
	}
	p.Meet = f.meet
	if c.Changed("notice") {
		notice, err := f.notice.Value()
		if err != nil {
			return p, err
		}
		p.Notice = &notice
	}
	switch {
	case len(f.windows) > 0:
		length := f.length
		if length == 0 {
			length = ch.Duration()
		}
		if length == 0 {
			var err error
			if length, err = calendarLength(c, ch.CalendarID()); err != nil {
				return p, err
			}
		}
		slots, err := calsvc.Offer(f.windows, length, loc, time.Now())
		if err != nil {
			return p, err
		}
		p.Slots = slots
	case f.length != 0:
		p.Duration = f.length
	}
	switch {
	case f.noBlockedBy:
		p.BlockedBy = &[]string{}
	case len(f.blockedBy) > 0:
		blocked, err := blockingCalendars(c, f.blockedBy, ch.CalendarID())
		if err != nil {
			return p, err
		}
		p.BlockedBy = &blocked
	}
	return p, nil
}

func bookingPagesDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete REF...",
		Short: "Delete booking pages",
		Long: "Delete booking pages.\n\n" +
			"The link stops working. Appointments already booked stay in your calendar.",
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			sel, err := kit.SelectFrom(c, "booking pages", bookingPageColumns(), bookingPageList(c))
			if err != nil {
				return err
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Deleted, Kind: "booking pages", Count: sel.Len(), IDs: sel.IDs,
				Name:    kit.Sole(sel.Rows, func(p calsvc.BookingPage) string { return p.Title }),
				Preview: sel.Preview(),
			}, func() error {
				for _, p := range sel.Rows {
					if err := c.App.Calendar.BookingPageDelete(c.Ctx, p.ID); err != nil {
						return err
					}
				}
				return nil
			})
		}),
	}
}
