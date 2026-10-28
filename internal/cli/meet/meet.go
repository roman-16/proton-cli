package meet

import (
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/roman-16/proton-cli/internal/cli/kit"
	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/roman-16/proton-cli/internal/ref"
	meetsvc "github.com/roman-16/proton-cli/internal/service/meet"
	"github.com/roman-16/proton-cli/internal/ui"
	"github.com/roman-16/proton-cli/internal/units"
)

func New() *cobra.Command {
	c := &cobra.Command{Use: "meet", Short: "Meetings and rooms people join by a link"}
	c.AddCommand(meetingsCmd(), roomsCmd())
	return c
}

const personal = "personal"

type collection struct {
	name  string
	noun  string
	holds func(meetsvc.Type) bool
}

var (
	meetings = collection{name: "meetings", noun: "meeting", holds: meetsvc.Type.Timed}
	rooms    = collection{name: "rooms", noun: "room", holds: meetsvc.Type.Room}
)

func (k collection) other() collection {
	if k.name == meetings.name {
		return rooms
	}
	return meetings
}

func (k collection) list(c *kit.Invocation) ([]meetsvc.Meeting, error) {
	all, err := c.App.Meet.List(c.Ctx)
	if err != nil {
		return nil, err
	}
	out := make([]meetsvc.Meeting, 0, len(all))
	for _, m := range all {
		if k.holds(m.Type) {
			out = append(out, m)
		}
	}
	return out, nil
}

func (k collection) resolve(c *kit.Invocation) (*meetsvc.Meeting, error) {
	needle := strings.TrimSpace(c.Args[0])
	if k.name == rooms.name && needle == personal {
		return c.App.Meet.Personal(c.Ctx)
	}
	all, err := c.App.Meet.List(c.Ctx)
	if err != nil {
		return nil, err
	}
	name, password, link := meetsvc.ParseLink(needle)
	if link {
		needle = name
	}
	matches := matching(all, needle)
	if len(matches) == 0 && link {
		m, err := c.App.Meet.ByLink(c.Ctx, name, password)
		if err != nil {
			return nil, err
		}
		matches = []meetsvc.Meeting{*m}
	}
	var mine []meetsvc.Meeting
	for _, m := range matches {
		if k.holds(m.Type) {
			mine = append(mine, m)
		}
	}
	if len(mine) == 0 && len(matches) == 1 {
		m := matches[0]
		return nil, errs.Naming(m.Title, kit.Fail("\"%s\" is a %s, not a %s.", m.Title, k.other().noun, k.noun).
			Hint(kit.Program+" meet "+k.other().name+" "+c.Cmd.Name()+" "+m.Name))
	}
	m, err := ref.Pick(k.noun, c.Args[0], mine,
		func(m meetsvc.Meeting) string { return m.Name },
		func(m meetsvc.Meeting) string { return m.Title })
	if err != nil {
		return nil, err
	}
	return &m, nil
}

func matching(all []meetsvc.Meeting, needle string) []meetsvc.Meeting {
	var byID, byTitle, inTitle []meetsvc.Meeting
	lower := strings.ToLower(needle)
	for _, m := range all {
		switch {
		case m.Name == needle || (len(needle) >= ref.ShortLen && strings.HasPrefix(m.Name, needle)):
			byID = append(byID, m)
		case strings.EqualFold(m.Title, needle):
			byTitle = append(byTitle, m)
		case needle != "" && strings.Contains(strings.ToLower(m.Title), lower):
			inTitle = append(inTitle, m)
		}
	}
	switch {
	case len(byID) > 0:
		return byID
	case len(byTitle) > 0:
		return byTitle
	}
	return inTitle
}

type row struct {
	ID              string       `json:"id"`
	Title           string       `json:"title"`
	Type            meetsvc.Type `json:"type"`
	Start           *time.Time   `json:"start,omitempty"`
	End             *time.Time   `json:"end,omitempty"`
	Next            *time.Time   `json:"next,omitempty"`
	Timezone        string       `json:"zone,omitempty"`
	RRule           string       `json:"rrule,omitempty"`
	CalendarID      string       `json:"calendar_id,omitempty"`
	CalendarEventID string       `json:"calendar_event_id,omitempty"`
}

func rowOf(m meetsvc.Meeting) row {
	return row{
		ID: m.Name, Title: m.Title, Type: m.Type, Start: m.Start, End: m.End, Next: m.Next,
		Timezone: m.Timezone, RRule: m.RRule, CalendarID: m.CalendarID, CalendarEventID: m.CalendarEventID,
	}
}

func rowsOf(ms []meetsvc.Meeting) []row {
	out := make([]row, 0, len(ms))
	for _, m := range ms {
		out = append(out, rowOf(m))
	}
	return out
}

func clock(t *time.Time) string {
	if t == nil {
		return ""
	}
	return units.Time(t.Unix())
}

func show(c *kit.Invocation, m *meetsvc.Meeting) error {
	c.Warn("%s", warning(m))
	fields := []ui.Field{
		{Label: "URL", Value: m.URL},
		{Label: "Title", Value: m.Title, Handle: true},
	}
	if m.Type.Room() {
		fields = append(fields, ui.Field{Label: "Type", Value: m.Type.String()})
	} else {
		fields = append(fields,
			ui.Field{Label: "Start", Value: clock(m.Start)},
			ui.Field{Label: "End", Value: clock(m.End)},
		)
		if m.Type == meetsvc.Recurring {
			fields = append(fields, ui.Field{Label: "Next", Value: clock(m.Next)})
		}
		fields = append(fields,
			ui.Field{Label: "Recurrence", Value: m.RRule},
			ui.Field{Label: "Zone", Value: m.Timezone},
		)
	}
	if m.FromCalendar() {
		fields = append(fields, ui.Field{
			Label: "Event", Value: kit.JoinPair(m.CalendarID, m.CalendarEventID), Ref: "calendar events",
		})
	}
	fields = append(fields, ui.Field{Label: "ID", Value: m.Name, ID: true})
	return kit.Show(c, ui.RecordSpec{Object: m, Fields: fields})
}

func warning(m *meetsvc.Meeting) string {
	switch m.Type {
	case meetsvc.Personal:
		return "Anyone with this link can join your personal room until you rotate its link."
	case meetsvc.Room:
		return "Anyone with this link can join the room until it is deleted."
	}
	return "Anyone with this link can join the meeting until it is deleted."
}

func title(value *string) kit.Step {
	return func(c *kit.Invocation) error {
		if c.Changed("title") && strings.TrimSpace(*value) == "" {
			return kit.Fail("--title needs a title.").Hint("--title 'Team sync'")
		}
		return nil
	}
}
