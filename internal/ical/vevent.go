// Package ical models the iCalendar events Proton Calendar stores, and the
// recurrence arithmetic that turns one stored series into the occurrences a
// person sees.
//
// The model exists because a calendar event is not a bag of strings. A series
// carries a rule, a zone to advance that rule in, a list of dates removed from
// it, and possibly a reference to the one occurrence it overrides. Code that
// reads those with a line search will drop whichever of them it forgot to look
// for, which is how an edit ends up resurrecting occurrences somebody deleted.
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

// Attendee is one participant. Token is the deterministic Proton attendee token
// (hex SHA-1 of UID + canonical email) that addresses the participant in the
// encrypted attendees card.
type Attendee struct {
	Email    string
	Token    string
	PartStat string
	Role     string
	RSVP     bool
}

// VEvent is a Proton calendar event, assembled from every card that makes it up.
//
// Proton splits an event across cards by property: some are signed in the clear,
// some are encrypted to the calendar key, the attendee list is encrypted
// separately. The split is a storage decision, so it lives in the serializers
// rather than in the shape of this struct.
type VEvent struct {
	UID     string
	DTStamp time.Time

	Start DateTime
	End   DateTime

	// RecurrenceID is set on an override: the original start of the one
	// occurrence of UID that this event replaces.
	RecurrenceID *DateTime
	// RRule is the recurrence rule value, verbatim as stored ("FREQ=WEEKLY;COUNT=10").
	//
	// It is kept as written rather than parsed and re-emitted so that reading and
	// re-saving an event cannot silently rewrite a rule the user or another client
	// authored.
	RRule string
	// ExDates are the occurrences removed from the series, sorted.
	ExDates []DateTime

	Sequence  int
	Organizer string
	Attendees []Attendee

	Summary     string
	Location    string
	Description string
	// Status is the event's own state - CONFIRMED, TENTATIVE or CANCELLED - which
	// says whether it is going ahead. It is distinct from an attendee's PARTSTAT,
	// which says whether one person is coming. Empty means the event never said,
	// which every client reads as confirmed.
	Status string
	// Alarms are the reminders. Proton stores them beside the event rather than
	// inside its cards, so they are only set where they matter: a file somebody
	// else's client will open.
	Alarms []Alarm
	// Color is the event's own colour, for the same reason and in the same place
	// as the reminders: Proton keeps it beside the cards, and a file is where it
	// has to be written down for another client to see it.
	Color string
}

// Alarm is a reminder, as iCalendar writes one.
//
// Action is DISPLAY or EMAIL, and Trigger is the offset from the start, negative
// for a reminder before it ("-PT15M").
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
	// An emailed reminder has to say something, and every client requires the
	// two properties even when it shows neither.
	if action == "EMAIL" {
		lines = append(lines,
			contentline.Line{Name: "SUMMARY", Value: contentline.EscapeText("Reminder")},
			contentline.Line{Name: "DESCRIPTION", Value: contentline.EscapeText("Reminder")},
		)
	}
	return append(lines, contentline.Line{Name: "END", Value: "VALARM"})
}

// Recurring reports whether the event carries a rule, and so stands for more
// than one occurrence.
func (v VEvent) Recurring() bool { return v.RRule != "" }

// IsOverride reports whether the event replaces a single occurrence of a series.
func (v VEvent) IsOverride() bool { return v.RecurrenceID != nil }

// Span is the pair of values an event occupies: its start, and the end that is not
// part of it.
//
// An all-day event's end is exclusive in iCalendar - a single day runs to the next
// one - and an event may be stored with no end at all, or with one that does not
// exceed its start. All of those name the same days, so the reading is settled here
// rather than at each place that measures an event. The stored values are left as
// they were written, so re-saving an event another client authored cannot rewrite
// what it said.
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

// Span is the pair of values the event occupies.
func (v VEvent) Span() (DateTime, DateTime) { return Span(v.Start, v.End) }

// Duration is how long the event lasts. An all-day event's end is exclusive in
// iCalendar, which is why a one-day event reads as 24 hours here.
func (v VEvent) Duration() time.Duration {
	start, end := v.Span()
	return end.Time.Sub(start.Time)
}

// Parse reads an event from the concatenation of its decrypted cards.
//
// Merging rather than parsing each card apart is deliberate: every card repeats
// UID and DTSTAMP and carries a disjoint set of the rest, so the union is the
// event. Properties that legitimately appear in two cards - EXDATE is in both
// the shared and the calendar part - are deduplicated.
func Parse(text string) (VEvent, error) {
	var v VEvent
	for _, l := range contentline.ParseAll(text) {
		if err := v.set(l); err != nil {
			return VEvent{}, err
		}
	}
	if v.UID == "" {
		return VEvent{}, fmt.Errorf("event has no UID")
	}
	v.sortExDates()
	return v, nil
}

func (v *VEvent) set(l contentline.Line) error {
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
			return err
		}
		if len(ds) > 0 {
			v.Start = ds[0]
		}
	case "DTEND":
		ds, err := parseValues(l)
		if err != nil {
			return err
		}
		if len(ds) > 0 {
			v.End = ds[0]
		}
	case "RECURRENCE-ID":
		ds, err := parseValues(l)
		if err != nil {
			return err
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
			return err
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
	}
	return nil
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

// ── serializing ──

// SharedSigned renders the card Proton signs but does not encrypt.
//
// The property set is Proton's SHARED_SIGNED_FIELDS, in its order. Keeping the
// membership stated in one place is what stops a property being dropped: the
// recurrence rule, the exclusions and the override reference all live here, and
// an event rebuilt without them is an event silently turned back into a one-off.
func (v VEvent) SharedSigned() string {
	lines := []contentline.Line{
		{Name: "UID", Value: v.UID},
		{Name: "DTSTAMP", Value: stamp(v.DTStamp)},
		v.Start.line("DTSTART"),
	}
	if !v.End.IsZero() {
		lines = append(lines, v.End.line("DTEND"))
	}
	if v.RecurrenceID != nil {
		lines = append(lines, v.RecurrenceID.line("RECURRENCE-ID"))
	}
	if v.RRule != "" {
		lines = append(lines, contentline.Line{Name: "RRULE", Value: v.RRule})
	}
	for _, d := range v.ExDates {
		lines = append(lines, d.line("EXDATE"))
	}
	if v.Organizer != "" {
		lines = append(lines, contentline.Line{
			Name:   "ORGANIZER",
			Params: contentline.Params{{Name: "CN", Value: v.Organizer}},
			Value:  "mailto:" + v.Organizer,
		})
	}
	lines = append(lines, contentline.Line{Name: "SEQUENCE", Value: strconv.Itoa(v.Sequence)})
	return component(lines)
}

// SharedEncrypted renders the card Proton encrypts to the calendar key and
// signs. The property set is Proton's SHARED_ENCRYPTED_FIELDS.
func (v VEvent) SharedEncrypted() string {
	lines := []contentline.Line{
		{Name: "UID", Value: v.UID},
		{Name: "DTSTAMP", Value: stamp(v.DTStamp)},
		{Name: "SUMMARY", Value: contentline.EscapeText(v.Summary)},
	}
	if v.Location != "" {
		lines = append(lines, contentline.Line{Name: "LOCATION", Value: contentline.EscapeText(v.Location)})
	}
	if v.Description != "" {
		lines = append(lines, contentline.Line{Name: "DESCRIPTION", Value: contentline.EscapeText(v.Description)})
	}
	if v.Status != "" {
		lines = append(lines, contentline.Line{Name: "STATUS", Value: v.Status})
	}
	return component(lines)
}

// AttendeesEncrypted renders the attendee card, whose property set is Proton's
// ATTENDEES_ENCRYPTED_FIELDS. It is empty when there are no attendees.
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

// component wraps property lines in the VCALENDAR and VEVENT envelope every card
// carries.
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

// EventUID mints an identifier for a new event.
//
// It comes from a cryptographic source rather than the clock because two events
// created in the same moment must not share a UID, and a nanosecond clock can
// return the same reading twice on a supported system.
func EventUID() string { return rand.Text() + "@proton-cli" }
