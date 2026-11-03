package ical

import "testing"

func TestMethodReadsTheCalendarLevelProperty(t *testing.T) {
	reply := VEvent{UID: "u", Attendees: []Attendee{{Email: "jane@example.com", PartStat: "ACCEPTED"}}}
	if got := Method(reply.Document("REPLY")); got != "REPLY" {
		t.Errorf("Method = %q, want REPLY", got)
	}
	if got := Method(Calendar([]VEvent{reply})); got != "" {
		t.Errorf("Method of a file with none = %q", got)
	}
	nested := "BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\nUID:u\r\nMETHOD:CANCEL\r\nEND:VEVENT\r\nMETHOD:reply\r\nEND:VCALENDAR"
	if got := Method(nested); got != "REPLY" {
		t.Errorf("Method read %q, want the calendar's own REPLY", got)
	}
}

func TestProtonReplyIsReadBackFromAReplyDocument(t *testing.T) {
	v := VEvent{UID: "u", Summary: "Sync"}
	for _, c := range []struct {
		proton bool
	}{{true}, {false}} {
		events, err := ParseCalendar(v.ReplyDocument("jane@example.com", "ACCEPTED", c.proton))
		if err != nil || len(events) != 1 {
			t.Fatalf("ParseCalendar: %v, %d events", err, len(events))
		}
		if got := events[0].ProtonReply(); got != c.proton {
			t.Errorf("ProtonReply = %v, want %v", got, c.proton)
		}
		if a := events[0].Attendees; len(a) != 1 || a[0].PartStat != "ACCEPTED" {
			t.Errorf("attendees = %+v", a)
		}
	}
}
