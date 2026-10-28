package ical

import (
	"strings"
	"testing"
	"time"

	"github.com/roman-16/proton-cli/internal/contentline"
)

const meetURL = "https://meet.proton.me/join/id-6MCF0BSHBR#pwd-k3Xq9vLm2PzT"

func props(pairs ...string) []contentline.Line {
	out := make([]contentline.Line, 0, len(pairs)/2)
	for i := 0; i+1 < len(pairs); i += 2 {
		out = append(out, contentline.Line{Name: pairs[i], Value: pairs[i+1]})
	}
	return out
}

func unfolded(card string) string { return strings.Join(contentline.Unfold(card), "\n") }

func cards(v VEvent) string {
	return strings.Join([]string{v.SharedSigned(), v.SharedEncrypted(), v.CalendarSigned(), v.CalendarEncrypted()}, "\r\n")
}

func TestLaterCardWins(t *testing.T) {
	shared := component(props("UID", "u", "STATUS", "CONFIRMED", "X-CUSTOM", "first"))
	calendar := component(props("UID", "u", "STATUS", "CANCELLED", "X-CUSTOM", "second"))
	v, err := Parse(shared + "\r\n" + calendar)
	if err != nil {
		t.Fatal(err)
	}
	if v.Status != "CANCELLED" {
		t.Errorf("status = %q, want the calendar card's", v.Status)
	}
	if len(v.Rest) != 1 || v.Rest[0].Value != "second" {
		t.Errorf("rest = %+v, want the calendar card's value alone", v.Rest)
	}
}

func TestPropertiesTheModelDoesNotNameGoWhereProtonKeepsThem(t *testing.T) {
	in := component(props(
		"UID", "u", "DTSTART", "20261103T090000Z", "SUMMARY", "Review",
		"CREATED", "20261001T090000Z", "X-CUSTOM", "kept", "TRANSP", "TRANSPARENT", "COMMENT", "a note",
	))
	v, err := Parse(in)
	if err != nil {
		t.Fatal(err)
	}
	for card, want := range map[string][]string{
		v.SharedEncrypted():   {"CREATED:20261001T090000Z", "X-CUSTOM:kept"},
		v.CalendarSigned():    {"TRANSP:TRANSPARENT"},
		v.CalendarEncrypted(): {"COMMENT:a note"},
	} {
		for _, w := range want {
			if !strings.Contains(unfolded(card), w) {
				t.Errorf("missing %q in\n%s", w, card)
			}
		}
	}
	back, err := Parse(cards(v))
	if err != nil {
		t.Fatal(err)
	}
	if len(back.Rest) != 4 {
		t.Errorf("round trip carried %d unnamed properties, want 4: %+v", len(back.Rest), back.Rest)
	}
}

func TestCalendarCardsAreLeftOutWhenEmpty(t *testing.T) {
	v := VEvent{UID: "u", Summary: "Plain"}
	if v.CalendarSigned() != "" || v.CalendarEncrypted() != "" {
		t.Errorf("an event with nothing for the calendar cards wrote one")
	}
	v.ExDates = []DateTime{Day(time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC))}
	if !strings.Contains(v.CalendarSigned(), "EXDATE") || !strings.Contains(v.SharedSigned(), "EXDATE") {
		t.Errorf("exclusions belong in both signed cards")
	}
}

func TestConferenceRoundTrips(t *testing.T) {
	v := VEvent{UID: "u", Summary: "Review"}.WithConference(Conference{
		ID: "6MCF0BSHBR", URL: meetURL, Provider: ProviderMeet, Host: "alex@proton.me",
	})
	if !strings.Contains(unfolded(v.SharedSigned()), "X-PM-CONFERENCE-ID;X-PM-PROVIDER=2:6MCF0BSHBR") {
		t.Errorf("signed card:\n%s", v.SharedSigned())
	}
	if !strings.Contains(unfolded(v.SharedEncrypted()), "X-PM-CONFERENCE-URL;X-PM-HOST=alex@proton.me:"+meetURL) {
		t.Errorf("encrypted card:\n%s", v.SharedEncrypted())
	}
	back, err := Parse(cards(v))
	if err != nil {
		t.Fatal(err)
	}
	if back.Conference == nil || *back.Conference != *v.Conference {
		t.Errorf("conference = %+v, want %+v", back.Conference, v.Conference)
	}
	if len(back.Rest) != 0 {
		t.Errorf("the conference leaked into the rest: %+v", back.Rest)
	}
}

func TestJoinInstructionsAreProtonsWording(t *testing.T) {
	v := VEvent{Description: "Bring the numbers"}.WithConference(Conference{
		ID: "6MCF0BSHBR", URL: meetURL, Provider: ProviderMeet,
	})
	want := "Bring the numbers\n" + callSeparator + "\nJoin Proton Meet: " + meetURL + "\n" + callSeparator
	if v.Description != want {
		t.Errorf("description =\n%q\nwant\n%q", v.Description, want)
	}
	if got := v.ShownDescription(); got != "Bring the numbers" {
		t.Errorf("shown = %q", got)
	}
	if got := v.WithDescription("New text").Description; got != "New text\n"+callSeparator+"\nJoin Proton Meet: "+meetURL+"\n"+callSeparator {
		t.Errorf("a new description lost the instructions: %q", got)
	}
	gone := v.WithoutCall()
	if gone.Conference != nil || gone.Description != "Bring the numbers" {
		t.Errorf("without the call: %+v", gone)
	}
}

func TestZoomInstructions(t *testing.T) {
	v := VEvent{}.WithConference(Conference{
		ID: "123", URL: "https://us02web.zoom.us/j/123", Provider: ProviderZoom, Password: "abc", Host: "h@x.org",
	})
	want := "\n" + callSeparator + "\nJoin Zoom Meeting: https://us02web.zoom.us/j/123 (ID: 123, passcode: abc)\n\nMeeting host: h@x.org\n" + callSeparator
	if v.Description != want {
		t.Errorf("description =\n%q\nwant\n%q", v.Description, want)
	}
	call, ok := v.Call()
	if !ok || call.Provider != ProviderZoom {
		t.Errorf("call = %+v, %v", call, ok)
	}
}

func TestAMeetLinkInTheDescriptionIsTheCall(t *testing.T) {
	v := VEvent{Description: "Notes\n" + callSeparator + "\nJoin Proton Meet: " + meetURL + "\n" + callSeparator}
	call, ok := v.Call()
	if !ok || call.URL != meetURL || call.Provider != ProviderMeet {
		t.Errorf("call = %+v, %v", call, ok)
	}
	if v.ShownDescription() != "Notes" {
		t.Errorf("shown = %q", v.ShownDescription())
	}
	plain := VEvent{Description: "Notes\n" + callSeparator + "\nsomething\n" + callSeparator}
	if plain.ShownDescription() != plain.Description {
		t.Errorf("a block with no call behind it was hidden")
	}
}

func TestAReplyCarriesNoConference(t *testing.T) {
	v := VEvent{UID: "u", Summary: "Review", Rest: props("X-CUSTOM", "x")}.
		WithConference(Conference{ID: "6MCF0BSHBR", URL: meetURL, Provider: ProviderMeet})
	if doc := unfolded(v.Document("REQUEST")); !strings.Contains(doc, "X-PM-CONFERENCE-URL") || !strings.Contains(doc, "X-CUSTOM:x") {
		t.Errorf("an invitation lost the call:\n%s", doc)
	}
	if doc := v.ReplyDocument("jane@example.com", "ACCEPTED", false); strings.Contains(doc, "X-PM-CONFERENCE") || strings.Contains(doc, "X-CUSTOM") {
		t.Errorf("a reply carried the call:\n%s", doc)
	}
}

func TestAFileCarriesWhatItDoesNotKnow(t *testing.T) {
	events, err := ParseCalendar("BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\nUID:u\r\nDTSTART:20261103T090000Z\r\nX-CUSTOM:kept\r\nEND:VEVENT\r\nEND:VCALENDAR")
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || len(events[0].Rest) != 1 || events[0].Rest[0].Value != "kept" {
		t.Errorf("events = %+v", events)
	}
}
