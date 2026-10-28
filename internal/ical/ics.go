package ical

import (
	"strconv"
	"strings"

	"github.com/roman-16/proton-cli/internal/contentline"
)

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
	lines = append(lines, v.exdateLines()...)
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
	if c := v.Conference; c != nil {
		if c.ID != "" {
			lines = append(lines, c.idLine())
		}
		if c.URL != "" {
			lines = append(lines, c.urlLine())
		}
	}
	lines = append(lines, v.Rest...)
	lines = append(lines, contentline.Line{Name: "SEQUENCE", Value: strconv.Itoa(v.Sequence)})
	for _, a := range v.Alarms {
		lines = append(lines, a.lines()...)
	}
	return append(lines, contentline.Line{Name: "END", Value: "VEVENT"})
}

// ReplyDocument keeps what Proton's web client keeps in a reply: the event's
// identity and times, and the one attendee answering.
func (v VEvent) ReplyDocument(attendeeEmail, partstat string, protonReply bool) string {
	reply := v
	reply.Description = ""
	reply.Conference = nil
	reply.Rest = nil
	reply.Attendees = []Attendee{{Email: attendeeEmail, PartStat: partstat}}
	doc := reply.Document("REPLY")
	if !protonReply {
		return doc
	}
	return insertBefore(doc, "ATTENDEE;", "X-PM-PROTON-REPLY;TYPE=boolean:true")
}

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
			known, err := event.set(l)
			if err != nil && invalid == nil {
				invalid = err
			}
			if !known {
				event.Rest = append(event.Rest, l)
			}
		}
	}
	if len(out) == 0 && invalid != nil {
		return nil, invalid
	}
	return out, nil
}
