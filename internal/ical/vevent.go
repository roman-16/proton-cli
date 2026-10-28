package ical

import (
	"crypto/rand"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/roman-16/proton-cli/internal/contentline"
)

const prodID = "-//proton-cli//EN"

type Attendee struct {
	Email    string
	Token    string
	PartStat string
	Role     string
	RSVP     bool
}

type VEvent struct {
	UID     string
	DTStamp time.Time

	Start DateTime
	End   DateTime

	RecurrenceID *DateTime
	RRule        string
	ExDates      []DateTime

	Sequence  int
	Organizer string
	Attendees []Attendee

	Summary     string
	Location    string
	Description string
	Status      string
	Alarms      []Alarm
	Color       string

	Conference *Conference
	Rest       []contentline.Line
}

type Alarm struct {
	Action  string
	Trigger string
}

func (a Alarm) lines() []contentline.Line {
	action := a.Action
	if action == "" {
		action = "DISPLAY"
	}
	lines := []contentline.Line{
		{Name: "BEGIN", Value: "VALARM"},
		{Name: "ACTION", Value: action},
		{Name: "TRIGGER", Value: a.Trigger},
	}
	// RFC 5545 requires both on an emailed alarm, and clients refuse one without them.
	if action == "EMAIL" {
		lines = append(lines,
			contentline.Line{Name: "SUMMARY", Value: contentline.EscapeText("Reminder")},
			contentline.Line{Name: "DESCRIPTION", Value: contentline.EscapeText("Reminder")},
		)
	}
	return append(lines, contentline.Line{Name: "END", Value: "VALARM"})
}

func (v VEvent) Recurring() bool { return v.RRule != "" }

func (v VEvent) IsOverride() bool { return v.RecurrenceID != nil }

// An all-day DTEND is exclusive in RFC 5545, and other clients store no end or one
// that does not exceed the start.
func Span(start, end DateTime) (DateTime, DateTime) {
	if start.AllDay {
		if end.IsZero() || !end.Time.After(start.Time) {
			return start, start.At(start.Time.AddDate(0, 0, 1))
		}
		return start, end
	}
	if end.IsZero() {
		return start, start
	}
	return start, end
}

func (v VEvent) Span() (DateTime, DateTime) { return Span(v.Start, v.End) }

func (v VEvent) Duration() time.Duration {
	start, end := v.Span()
	return end.Time.Sub(start.Time)
}

var calendarLevel = map[string]bool{"VERSION": true, "PRODID": true, "CALSCALE": true, "METHOD": true}

// Parse reads an event from its cards, joined in the order Proton's web client
// reads them: a property in a later card replaces the same property in an
// earlier one.
func Parse(text string) (VEvent, error) {
	var v VEvent
	var card []contentline.Line
	nested := 0
	flush := func() {
		v.Rest = replaced(v.Rest, card)
		card = nil
	}
	for _, raw := range contentline.Unfold(text) {
		l, ok := contentline.Parse(raw)
		if !ok {
			continue
		}
		switch {
		case l.Name == "BEGIN" && (strings.EqualFold(l.Value, "VCALENDAR") || strings.EqualFold(l.Value, "VEVENT")):
			if nested == 0 {
				flush()
			}
			continue
		case l.Name == "BEGIN":
			nested++
			continue
		case l.Name == "END" && nested > 0:
			nested--
			continue
		case l.Name == "END":
			flush()
			continue
		case nested > 0 || calendarLevel[l.Name]:
			continue
		}
		known, err := v.set(l)
		if err != nil {
			return VEvent{}, err
		}
		if !known {
			card = append(card, l)
		}
	}
	flush()
	if v.UID == "" {
		return VEvent{}, fmt.Errorf("event has no UID")
	}
	v.sortExDates()
	return v, nil
}

func replaced(rest, card []contentline.Line) []contentline.Line {
	if len(card) == 0 {
		return rest
	}
	names := map[string]bool{}
	for _, l := range card {
		names[l.Name] = true
	}
	out := slices.DeleteFunc(slices.Clone(rest), func(l contentline.Line) bool { return names[l.Name] })
	return append(out, card...)
}

func (v *VEvent) set(l contentline.Line) (bool, error) {
	switch l.Name {
	case "UID":
		v.UID = l.Value
	case "DTSTAMP":
		if ds, err := parseValues(l); err == nil && len(ds) > 0 {
			v.DTStamp = ds[0].Time
		}
	case "DTSTART":
		ds, err := parseValues(l)
		if err != nil {
			return true, err
		}
		if len(ds) > 0 {
			v.Start = ds[0]
		}
	case "DTEND":
		ds, err := parseValues(l)
		if err != nil {
			return true, err
		}
		if len(ds) > 0 {
			v.End = ds[0]
		}
	case "RECURRENCE-ID":
		ds, err := parseValues(l)
		if err != nil {
			return true, err
		}
		if len(ds) > 0 {
			id := ds[0]
			v.RecurrenceID = &id
		}
	case "RRULE":
		v.RRule = strings.TrimSpace(l.Value)
	case "EXDATE":
		ds, err := parseValues(l)
		if err != nil {
			return true, err
		}
		for _, d := range ds {
			if !slices.ContainsFunc(v.ExDates, d.Equal) {
				v.ExDates = append(v.ExDates, d)
			}
		}
	case "SEQUENCE":
		n, err := strconv.Atoi(strings.TrimSpace(l.Value))
		if err == nil && n > 0 {
			v.Sequence = n
		}
	case "ORGANIZER":
		v.Organizer = mailAddress(l)
	case "ATTENDEE":
		v.Attendees = append(v.Attendees, attendeeFrom(l))
	case "SUMMARY":
		v.Summary = contentline.UnescapeText(l.Value)
	case "LOCATION":
		v.Location = contentline.UnescapeText(l.Value)
	case "DESCRIPTION":
		v.Description = contentline.UnescapeText(l.Value)
	case "STATUS":
		v.Status = strings.ToUpper(l.Value)
	case "COLOR":
		v.Color = strings.TrimSpace(l.Value)
	case conferenceIDName:
		v.conference().readID(l)
	case conferenceURLName:
		v.conference().readURL(l)
	default:
		return false, nil
	}
	return true, nil
}

func mailAddress(l contentline.Line) string {
	if addr := strings.TrimPrefix(l.Value, "mailto:"); addr != l.Value || addr != "" {
		return addr
	}
	return l.Params.Get("CN")
}

func attendeeFrom(l contentline.Line) Attendee {
	return Attendee{
		Email:    mailAddress(l),
		Token:    l.Params.Get("X-PM-TOKEN"),
		PartStat: l.Params.Get("PARTSTAT"),
		Role:     l.Params.Get("ROLE"),
		RSVP:     strings.EqualFold(l.Params.Get("RSVP"), "TRUE"),
	}
}

func (v *VEvent) sortExDates() {
	slices.SortStableFunc(v.ExDates, func(a, b DateTime) int { return a.Time.Compare(b.Time) })
}

// Where Proton keeps the properties it has no field list for, in its own
// getVeventParts: everything else goes into the shared encrypted card.
var restCard = map[string]card{"TRANSP": calendarSigned, "COMMENT": calendarEncrypted}

type card int

const (
	sharedEncrypted card = iota
	calendarSigned
	calendarEncrypted
)

func (v VEvent) rest(c card) []contentline.Line {
	var out []contentline.Line
	for _, l := range v.Rest {
		if restCard[l.Name] == c {
			out = append(out, l)
		}
	}
	return out
}

func (v VEvent) identity() []contentline.Line {
	return []contentline.Line{
		{Name: "UID", Value: v.UID},
		{Name: "DTSTAMP", Value: stamp(v.DTStamp)},
	}
}

// SharedSigned is Proton's SHARED_SIGNED_FIELDS.
func (v VEvent) SharedSigned() string {
	lines := append(v.identity(), v.Start.line("DTSTART"))
	if !v.End.IsZero() {
		lines = append(lines, v.End.line("DTEND"))
	}
	if v.RecurrenceID != nil {
		lines = append(lines, v.RecurrenceID.line("RECURRENCE-ID"))
	}
	if v.RRule != "" {
		lines = append(lines, contentline.Line{Name: "RRULE", Value: v.RRule})
	}
	lines = append(lines, v.exdateLines()...)
	if v.Organizer != "" {
		lines = append(lines, contentline.Line{
			Name:   "ORGANIZER",
			Params: contentline.Params{{Name: "CN", Value: v.Organizer}},
			Value:  "mailto:" + v.Organizer,
		})
	}
	lines = append(lines, contentline.Line{Name: "SEQUENCE", Value: strconv.Itoa(v.Sequence)})
	if v.Conference != nil && v.Conference.ID != "" {
		lines = append(lines, v.Conference.idLine())
	}
	return component(lines)
}

// SharedEncrypted is Proton's SHARED_ENCRYPTED_FIELDS, and every property no
// field list names.
func (v VEvent) SharedEncrypted() string {
	lines := append(v.identity(), contentline.Line{Name: "SUMMARY", Value: contentline.EscapeText(v.Summary)})
	if v.Location != "" {
		lines = append(lines, contentline.Line{Name: "LOCATION", Value: contentline.EscapeText(v.Location)})
	}
	if v.Description != "" {
		lines = append(lines, contentline.Line{Name: "DESCRIPTION", Value: contentline.EscapeText(v.Description)})
	}
	if v.Conference != nil && v.Conference.URL != "" {
		lines = append(lines, v.Conference.urlLine())
	}
	lines = append(lines, v.rest(sharedEncrypted)...)
	return component(lines)
}

// CalendarSigned is Proton's CALENDAR_SIGNED_FIELDS. Proton sends no card that
// holds nothing but the event's identity, so neither does this.
func (v VEvent) CalendarSigned() string {
	lines := v.exdateLines()
	if v.Status != "" {
		lines = append(lines, contentline.Line{Name: "STATUS", Value: v.Status})
	}
	lines = append(lines, v.rest(calendarSigned)...)
	if len(lines) == 0 {
		return ""
	}
	return component(append(v.identity(), lines...))
}

// CalendarEncrypted is Proton's CALENDAR_ENCRYPTED_FIELDS.
func (v VEvent) CalendarEncrypted() string {
	lines := v.rest(calendarEncrypted)
	if len(lines) == 0 {
		return ""
	}
	return component(append(v.identity(), lines...))
}

func (v VEvent) exdateLines() []contentline.Line {
	out := make([]contentline.Line, 0, len(v.ExDates))
	for _, d := range v.ExDates {
		out = append(out, d.line("EXDATE"))
	}
	return out
}

func (v VEvent) AttendeesEncrypted() string {
	if len(v.Attendees) == 0 {
		return ""
	}
	lines := []contentline.Line{{Name: "UID", Value: v.UID}}
	for _, a := range v.Attendees {
		lines = append(lines, attendeeLine(a))
	}
	return component(lines)
}

func attendeeLine(a Attendee) contentline.Line {
	role := a.Role
	if role == "" {
		role = "REQ-PARTICIPANT"
	}
	partstat := a.PartStat
	if partstat == "" {
		partstat = "NEEDS-ACTION"
	}
	params := contentline.Params{
		{Name: "CN", Value: a.Email},
		{Name: "ROLE", Value: role},
		{Name: "RSVP", Value: "TRUE"},
		{Name: "PARTSTAT", Value: partstat},
	}
	if a.Token != "" {
		params = append(params, contentline.Param{Name: "X-PM-TOKEN", Value: a.Token})
	}
	return contentline.Line{Name: "ATTENDEE", Params: params, Value: "mailto:" + a.Email}
}

func component(body []contentline.Line) string {
	lines := make([]contentline.Line, 0, len(body)+6)
	lines = append(lines,
		contentline.Line{Name: "BEGIN", Value: "VCALENDAR"},
		contentline.Line{Name: "VERSION", Value: "2.0"},
		contentline.Line{Name: "PRODID", Value: prodID},
		contentline.Line{Name: "BEGIN", Value: "VEVENT"},
	)
	lines = append(lines, body...)
	lines = append(lines,
		contentline.Line{Name: "END", Value: "VEVENT"},
		contentline.Line{Name: "END", Value: "VCALENDAR"},
	)
	return contentline.Render(lines)
}

func EventUID() string { return rand.Text() + "@proton-cli" }
