package ical

import (
	"strconv"
	"strings"

	"github.com/roman-16/proton-cli/internal/contentline"
)

// A self-contained iCalendar document is what goes out by email: an invitation,
// an update to one, or a reply. Unlike a stored card it carries a METHOD and every
// property in one component, because the recipient's client has nothing else to
// join it to.

// Document renders the event as a complete VCALENDAR with the given METHOD,
// suitable for attaching as text/calendar.
//
// REQUEST covers both a first invitation and an update to one; which it is, the
// recipient works out from the sequence.
func (v VEvent) Document(method string) string {
	lines := []contentline.Line{
		{Name: "BEGIN", Value: "VCALENDAR"},
		{Name: "VERSION", Value: "2.0"},
		{Name: "PRODID", Value: prodID},
		{Name: "METHOD", Value: method},
		{Name: "CALSCALE", Value: "GREGORIAN"},
	}
	lines = append(lines, v.component()...)
	lines = append(lines, contentline.Line{Name: "END", Value: "VCALENDAR"})
	return contentline.Render(lines)
}

// Calendar renders events as one VCALENDAR with no METHOD: a file, rather than a
// message about an event.
//
// The absence of METHOD is the whole difference. A document with one says what to
// do about the event it carries - invite, update, reply - and a client that
// opens it acts. A calendar file just holds events, which is what an export is
// and what every other client writes.
func Calendar(events []VEvent) string {
	lines := []contentline.Line{
		{Name: "BEGIN", Value: "VCALENDAR"},
		{Name: "VERSION", Value: "2.0"},
		{Name: "PRODID", Value: prodID},
		{Name: "CALSCALE", Value: "GREGORIAN"},
	}
	for _, v := range events {
		lines = append(lines, v.component()...)
	}
	lines = append(lines, contentline.Line{Name: "END", Value: "VCALENDAR"})
	return contentline.Render(lines)
}

// component is the VEVENT itself, which a message and a file both wrap.
func (v VEvent) component() []contentline.Line {
	lines := []contentline.Line{
		{Name: "BEGIN", Value: "VEVENT"},
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
	if v.Summary != "" {
		lines = append(lines, contentline.Line{Name: "SUMMARY", Value: contentline.EscapeText(v.Summary)})
	}
	if v.Location != "" {
		lines = append(lines, contentline.Line{Name: "LOCATION", Value: contentline.EscapeText(v.Location)})
	}
	if v.Description != "" {
		lines = append(lines, contentline.Line{Name: "DESCRIPTION", Value: contentline.EscapeText(v.Description)})
	}
	if v.Organizer != "" {
		lines = append(lines, contentline.Line{
			Name:   "ORGANIZER",
			Params: contentline.Params{{Name: "CN", Value: v.Organizer}},
			Value:  "mailto:" + v.Organizer,
		})
	}
	for _, a := range v.Attendees {
		lines = append(lines, attendeeLine(a))
	}
	if v.Status != "" {
		lines = append(lines, contentline.Line{Name: "STATUS", Value: v.Status})
	}
	if v.Color != "" {
		lines = append(lines, contentline.Line{Name: "COLOR", Value: v.Color})
	}
	lines = append(lines, contentline.Line{Name: "SEQUENCE", Value: strconv.Itoa(v.Sequence)})
	for _, a := range v.Alarms {
		lines = append(lines, a.lines()...)
	}
	return append(lines, contentline.Line{Name: "END", Value: "VEVENT"})
}

// ReplyDocument renders the METHOD:REPLY document that answers an invitation. It
// carries one ATTENDEE line, the responder's, tagged with the answer.
//
// protonReply adds the marker Proton sets on Proton-to-Proton replies.
func (v VEvent) ReplyDocument(attendeeEmail, partstat string, protonReply bool) string {
	reply := v
	reply.Description = ""
	reply.Attendees = []Attendee{{Email: attendeeEmail, PartStat: partstat}}
	doc := reply.Document("REPLY")
	if !protonReply {
		return doc
	}
	// The marker sits with the other event properties, so it goes in before the
	// attendee line that closes the component's meaningful content.
	return insertBefore(doc, "ATTENDEE;", "X-PM-PROTON-REPLY;TYPE=boolean:true")
}

// insertBefore puts a line ahead of the first line starting with prefix.
func insertBefore(doc, prefix, line string) string {
	lines := contentline.Unfold(doc)
	out := make([]string, 0, len(lines)+1)
	inserted := false
	for _, l := range lines {
		if !inserted && len(l) >= len(prefix) && l[:len(prefix)] == prefix {
			out = append(out, line)
			inserted = true
		}
		out = append(out, l)
	}
	if !inserted {
		out = append(out, line)
	}
	return joinCRLF(out)
}

func joinCRLF(lines []string) string {
	out := ""
	for i, l := range lines {
		if i > 0 {
			out += "\r\n"
		}
		out += l
	}
	return out
}

// ParseCalendar reads a whole .ics file into the events it holds.
//
// This is not what Parse does, and the difference matters. Parse merges every
// line it is given into one event, which is right for Proton's cards - each
// carries a disjoint slice of one event - and wrong for a file, where a second
// VEVENT is a second event and a nested VALARM is not an event property at all.
//
// The nesting is the trap. A VALARM carries its own SUMMARY and DESCRIPTION, so
// feeding a component's lines in flat would let a reminder's wording overwrite
// the event's. Sub-components are therefore read as themselves.
//
// Anything outside a VEVENT - the calendar's own properties, a VTIMEZONE - is
// skipped: this reads events, and a file may legitimately hold more than events.
func ParseCalendar(text string) ([]VEvent, error) {
	var (
		out     []VEvent
		event   *VEvent
		alarm   *Alarm
		invalid error
	)
	for _, raw := range contentline.Unfold(text) {
		l, ok := contentline.Parse(raw)
		if !ok {
			continue
		}
		switch {
		case l.Name == "BEGIN" && strings.EqualFold(l.Value, "VEVENT"):
			event, alarm = &VEvent{}, nil
			continue
		case l.Name == "BEGIN" && strings.EqualFold(l.Value, "VALARM") && event != nil:
			alarm = &Alarm{}
			continue
		case l.Name == "END" && strings.EqualFold(l.Value, "VALARM") && event != nil:
			if alarm != nil && alarm.Trigger != "" {
				event.Alarms = append(event.Alarms, *alarm)
			}
			alarm = nil
			continue
		case l.Name == "END" && strings.EqualFold(l.Value, "VEVENT"):
			if event != nil && event.UID != "" {
				event.sortExDates()
				out = append(out, *event)
			}
			event, alarm = nil, nil
			continue
		case l.Name == "BEGIN" || l.Name == "END":
			continue
		}
		switch {
		case alarm != nil:
			switch l.Name {
			case "ACTION":
				alarm.Action = strings.ToUpper(l.Value)
			case "TRIGGER":
				alarm.Trigger = l.Value
			}
		case event != nil:
			// A property this reader does not understand is skipped rather than
			// failing the file: another client's extension is not a reason to
			// refuse the events around it.
			if err := event.set(l); err != nil && invalid == nil {
				invalid = err
			}
		}
	}
	if len(out) == 0 && invalid != nil {
		return nil, invalid
	}
	return out, nil
}
