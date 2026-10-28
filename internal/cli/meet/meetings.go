package meet

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/roman-16/proton-cli/internal/cli/kit"
	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/roman-16/proton-cli/internal/ical"
	meetsvc "github.com/roman-16/proton-cli/internal/service/meet"
	"github.com/roman-16/proton-cli/internal/ui"
	"github.com/roman-16/proton-cli/internal/units"
)

// defaultLength is DEFAULT_MEETING_DURATION in Proton's Meet web app.
const defaultLength = 30 * time.Minute

func meetingsCmd() *cobra.Command {
	c := &cobra.Command{Use: "meetings", Short: "Meetings at a set time, once or repeating"}
	c.AddCommand(meetingsCreateCmd(), meetingsDeleteCmd(), meetingsGetCmd(), meetingsListCmd(), meetingsUpdateCmd())
	return c
}

func meetingsListCmd() *cobra.Command {
	var held kit.Held[row]
	c := &cobra.Command{
		Use:   "list",
		Short: "List your meetings",
		Long: "List your meetings, the next one first.\n\n" +
			"A meeting that repeats is listed at its next occurrence. The links are not\n" +
			"shown: anyone holding one can join. `meetings get` shows a link.",
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			all, err := meetings.list(c)
			if err != nil {
				return err
			}
			return held.Answer(c, ui.TableSpec[row]{
				Noun: "meetings",
				Columns: []ui.Column[row]{
					{Header: "ID", ID: true, Cell: func(r row) string { return r.ID }},
					{Header: "TITLE", Flex: true, Handle: true, Cell: func(r row) string { return r.Title }},
					{Header: "NEXT", Cell: func(r row) string { return clock(r.Next) }},
					{Header: "REPEATS", Cell: func(r row) string { return r.RRule }},
				},
			}, rowsOf(all))
		}),
	}
	held.Register(c, "meetings",
		kit.Key[row]{Name: "next", Less: func(a, b row) int { return kit.Ints(unix(a.Next), unix(b.Next)) }},
		kit.Key[row]{Name: "title", Less: func(a, b row) int { return kit.Fold(a.Title, b.Title) }},
	)
	return c
}

func unix(t *time.Time) int64 {
	if t == nil {
		return 1<<63 - 1
	}
	return t.Unix()
}

func meetingsGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get REF",
		Short: "Show one meeting, link and all",
		Long: "Show one meeting, link and all.\n\n" +
			"REF is the ID its link shows after id-, such as 6MCF0BSHBR, the whole link, or\n" +
			"its title. A meeting made for a calendar event opens by its whole link.",
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			m, err := meetings.resolve(c)
			if err != nil {
				return err
			}
			return show(c, m)
		}),
	}
}

type schedule struct {
	title, start, end, duration, rrule string
}

func (f *schedule) register(c *cobra.Command, verb string) {
	fl := c.Flags()
	fl.StringVar(&f.title, "title", "", verb+" the title")
	fl.StringVar(&f.start, "start", "", verb+" the start: a day and a time (2026-11-04T09:00, or full RFC 3339)")
	fl.StringVar(&f.end, "end", "", verb+" the end: a day and a time")
	fl.StringVar(&f.duration, "duration", "", verb+" how long it lasts (e.g. 15m, 1h, 1h30m)")
	fl.StringVar(&f.rrule, "rrule", "", verb+" the recurrence rule, e.g. FREQ=WEEKLY;COUNT=10")
	kit.Exclusive(c, "end", "duration")
}

func (f *schedule) moves(c *kit.Invocation) bool {
	return c.Changed("start") || c.Changed("end") || c.Changed("duration") || c.Changed("rrule")
}

// check judges what the command line says before anything is asked of Proton.
// A meeting runs at a time of day, so a bare day is refused.
func (f *schedule) check(c *kit.Invocation) error {
	for flag, value := range map[string]string{"start": f.start, "end": f.end} {
		if !c.Changed(flag) {
			continue
		}
		when, err := ical.ParseTime(value, time.UTC)
		if err != nil {
			return kit.Fail("--%s: %v", flag, err)
		}
		if when.AllDay {
			return kit.Fail("--%s names a day but not a time of day.", flag).
				Hint("--" + flag + " " + when.Time.Format("2006-01-02") + "T09:00")
		}
	}
	if c.Changed("duration") {
		dur, err := units.ParseDuration(f.duration)
		if err != nil {
			return kit.Fail("--duration: %v", err)
		}
		if dur <= 0 {
			return kit.Fail("--duration has to be longer than nothing.").Hint("--duration 30m")
		}
	}
	return nil
}

func (f *schedule) times(loc *time.Location, m *meetsvc.Meeting) (start, end time.Time, err error) {
	switch {
	case f.start != "":
		written, err := ical.ParseTime(f.start, loc)
		if err != nil {
			return start, end, kit.Fail("--start: %v", err)
		}
		start = written.Time
	case m != nil && m.Start != nil:
		start = *m.Start
	}
	length := defaultLength
	if m != nil && m.Start != nil && m.End != nil && m.End.After(*m.Start) {
		length = m.End.Sub(*m.Start)
	}
	switch {
	case f.end != "":
		written, err := ical.ParseTime(f.end, loc)
		if err != nil {
			return start, end, kit.Fail("--end: %v", err)
		}
		end = written.Time
		if !end.After(start) {
			return start, end, kit.Fail("--end is not after the start.")
		}
	case f.duration != "":
		dur, err := units.ParseDuration(f.duration)
		if err != nil {
			return start, end, kit.Fail("--duration: %v", err)
		}
		end = start.Add(dur)
	default:
		end = start.Add(length)
	}
	return start, end, nil
}

func zoneOf(c *kit.Invocation) (string, *time.Location, error) {
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

func at(t time.Time, loc *time.Location, zone string) string {
	return t.In(loc).Format("2006-01-02 15:04") + " " + zone
}

func meetingsCreateCmd() *cobra.Command {
	var f schedule
	c := &cobra.Command{
		Use:   "create",
		Short: "Schedule a meeting and print its link",
		Long: "Schedule a meeting and print its link.\n\n" +
			"--start takes a day and a time, as 2026-11-04T09:00, read in your zone. Without\n" +
			"--end or --duration a meeting lasts 30 minutes, and --rrule makes it repeat.\n\n" +
			"A meeting made here is in Proton Meet alone. `calendar events create --meet`\n" +
			"makes one on an event, and invites the people on it.",
		RunE: kit.Run([]kit.Step{f.check, title(&f.title)}, func(c *kit.Invocation) error {
			if strings.TrimSpace(f.title) == "" || f.start == "" {
				return kit.Fail("A meeting needs a title and a start.").
					Hint("--title 'Weekly sync' --start 2026-11-04T09:00")
			}
			zone, loc, err := zoneOf(c)
			if err != nil {
				return err
			}
			start, end, err := f.times(loc, nil)
			if err != nil {
				return err
			}
			nm := meetsvc.NewMeeting{
				Title: f.title, Type: meetsvc.Scheduled, Start: &start, End: &end, Timezone: zone, RRule: f.rrule,
			}
			if f.rrule != "" {
				nm.Type = meetsvc.Recurring
			}
			detail := "for " + at(start, loc, zone)
			if !c.Changed("end") && !c.Changed("duration") {
				detail += ", " + units.Duration(end.Sub(start)) + " long"
			}
			var made *meetsvc.Meeting
			if err := kit.Mutate(c, ui.ResultSpec{
				Action: ui.Created, Kind: "meetings", Count: 1, Name: f.title, Detail: detail, AnswerFollows: true,
			}, func() error {
				var err error
				made, err = c.App.Meet.Create(c.Ctx, nm)
				return err
			}); err != nil {
				return err
			}
			if made == nil {
				return nil
			}
			return show(c, made)
		}),
	}
	f.register(c, "Set")
	return c
}

func meetingsUpdateCmd() *cobra.Command {
	var f schedule
	c := &cobra.Command{
		Use:   "update REF",
		Short: "Rename a meeting, or move it to another time",
		Long: "Rename a meeting, or move it to another time.\n\n" +
			"Anything you do not mention is left alone. A meeting you move keeps its length\n" +
			"unless --end or --duration says otherwise, and --rrule '' makes it happen once.\n\n" +
			"A meeting that belongs to a calendar event changes with the event, through\n" +
			"`calendar events update`.",
		RunE: kit.Run([]kit.Step{f.check, title(&f.title), kit.StepExpand}, func(c *kit.Invocation) error {
			renaming, moving := c.Changed("title"), f.moves(c)
			if !renaming && !moving {
				return kit.Fail("Nothing to change.").Hint("--title 'Weekly sync'", "--start 2026-11-04T09:00")
			}
			m, err := meetings.resolve(c)
			if err != nil {
				return err
			}
			if m.FromCalendar() {
				return errs.Naming(m.Title, kit.Fail("\"%s\" belongs to a calendar event and changes with it.", m.Title).
					Hint(kit.Program+" calendar events update "+eventRef(m)))
			}
			var start, end time.Time
			var zone, rrule, detail string
			if moving {
				var loc *time.Location
				if zone, loc, err = zoneOf(c); err != nil {
					return err
				}
				if start, end, err = f.times(loc, m); err != nil {
					return err
				}
				rrule = m.RRule
				if c.Changed("rrule") {
					rrule = f.rrule
				}
				detail = "now " + at(start, loc, zone)
			}
			name := m.Title
			if renaming {
				name = f.title
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Updated, Kind: "meetings", Count: 1, Name: name, IDs: []string{m.Name}, Detail: detail,
			}, func() error {
				if moving {
					if err := c.App.Meet.Reschedule(c.Ctx, m, start, end, rrule, zone); err != nil {
						return err
					}
				}
				if !renaming {
					return nil
				}
				if err := c.App.Meet.Rename(c.Ctx, m, f.title); err != nil && moving {
					return fmt.Errorf("the meeting moved, but its new title was refused: %w", err)
				} else if err != nil {
					return err
				}
				return nil
			})
		}),
	}
	f.register(c, "Replace")
	return c
}

func meetingsDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete REF",
		Short: "Delete a meeting",
		Long: "Delete a meeting. Its link stops working for everyone who has it.\n\n" +
			"A meeting that belongs to a calendar event is deleted all the same, and the\n" +
			"event keeps a link that no longer works.",
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			m, err := meetings.resolve(c)
			if err != nil {
				return err
			}
			if m.FromCalendar() {
				c.Warn("This meeting belongs to a calendar event, which keeps a link that stops working.")
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Deleted, Kind: "meetings", Count: 1, Name: m.Title, IDs: []string{m.Name},
			}, func() error {
				return c.App.Meet.Delete(c.Ctx, m)
			})
		}),
	}
}

func eventRef(m *meetsvc.Meeting) string {
	if m.CalendarEventID == "" {
		return "REF"
	}
	return kit.JoinPair(m.CalendarID, m.CalendarEventID)
}
