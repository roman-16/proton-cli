package calendar

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/roman-16/proton-cli/internal/account/keys"
	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/roman-16/proton-cli/internal/ical"
	"github.com/roman-16/proton-cli/internal/proton"
	"github.com/roman-16/proton-cli/internal/service/meet"
)

const meetURL = "https://meet.proton.me/join/id-6MCF0BSHBR#pwd-k3Xq9vLm2PzT"

type fakeMeetings struct {
	created   []meet.NewMeeting
	deleted   []string
	createErr error
	forbidden bool
}

func (f *fakeMeetings) Allowed(context.Context) bool { return !f.forbidden }

func (f *fakeMeetings) Create(_ context.Context, nm meet.NewMeeting) (*meet.Meeting, error) {
	if f.createErr != nil {
		return nil, f.createErr
	}
	f.created = append(f.created, nm)
	return &meet.Meeting{ID: "meeting-id", Name: "6MCF0BSHBR", URL: meetURL, Title: nm.Title}, nil
}

func (f *fakeMeetings) Delete(_ context.Context, m *meet.Meeting) error {
	f.deleted = append(f.deleted, m.Name)
	return nil
}

func (f *fakeMeetings) ByLink(_ context.Context, name, _ string) (*meet.Meeting, error) {
	if name != "6MCF0BSHBR" {
		return nil, &errs.NotFound{Kind: "meeting"}
	}
	return &meet.Meeting{ID: "meeting-id", Name: name, URL: meetURL}, nil
}

type syncDoer struct {
	t        *testing.T
	stored   rawEvent
	settings map[string]any
	synced   []map[string]any
	refuse   error
}

func (d *syncDoer) Do(context.Context, proton.Request) (*proton.Response, error) {
	return nil, errors.New("syncDoer only decodes")
}

func (d *syncDoer) Decode(_ context.Context, r proton.Request, out any) error {
	answer := func(v any) error {
		raw, err := json.Marshal(v)
		if err != nil {
			return err
		}
		return json.Unmarshal(raw, out)
	}
	switch {
	case r.Method == "GET" && r.Path == "/calendar/v1/cal1/events/ev1":
		return answer(map[string]any{"Event": d.stored})
	case r.Method == "GET" && r.Path == "/settings/calendar":
		return answer(map[string]any{"CalendarUserSettings": d.settings})
	case r.Method == "PUT" && r.Path == "/calendar/v1/cal1/events/sync":
		if d.refuse != nil {
			return d.refuse
		}
		ops := r.Body.(map[string]any)["Events"].([]syncOp)
		var responses []map[string]any
		for i, op := range ops {
			d.synced = append(d.synced, map[string]any(op))
			responses = append(responses, map[string]any{"Index": i, "Response": map[string]any{"Code": 1000, "Event": map[string]any{"ID": "ev1"}}})
		}
		return answer(map[string]any{"Responses": responses})
	}
	return errors.New("unexpected request " + r.Method + " " + r.Path)
}

func meetService(t *testing.T, d *syncDoer, m Meetings) (*Service, *calKeys) {
	t.Helper()
	s := New(d, testKeys(&keys.Unlocked{}))
	s.SetMeetings(m)
	ck := bookingKeys(t)
	ck.memberID = "member1"
	if _, err := s.unlocked.Do("cal1", func() (*calKeys, error) { return ck, nil }); err != nil {
		t.Fatal(err)
	}
	return s, ck
}

func storedAs(t *testing.T, ck *calKeys, v ical.VEvent) rawEvent {
	t.Helper()
	op, _, err := ck.createOp(eventBody{model: v, isOrganizer: 1})
	if err != nil {
		t.Fatal(err)
	}
	return asRaw(t, op["Event"].(map[string]any), v.UID)
}

func asRaw(t *testing.T, event map[string]any, uid string) rawEvent {
	t.Helper()
	served := map[string]any{}
	for k, v := range event {
		served[strings.Replace(k, "EventContent", "Events", 1)] = v
	}
	b, err := json.Marshal(served)
	if err != nil {
		t.Fatal(err)
	}
	var raw rawEvent
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatal(err)
	}
	raw.ID, raw.CalendarID, raw.UID, raw.IsOrganizer = "ev1", "cal1", uid, 1
	return raw
}

func written(t *testing.T, s *Service, ck *calKeys, before rawEvent, op map[string]any) ical.VEvent {
	t.Helper()
	raw := asRaw(t, op["Event"].(map[string]any), before.UID)
	if raw.SharedKeyPacket == "" {
		raw.SharedKeyPacket = before.SharedKeyPacket
	}
	if raw.CalendarKeyPacket == "" {
		raw.CalendarKeyPacket = before.CalendarKeyPacket
	}
	e := s.decrypt(t.Context(), ck, raw)
	if e.readErr != nil {
		t.Fatal(e.readErr)
	}
	return e.model
}

func meetEvent() ical.VEvent {
	start := ical.Timed(time.Date(2026, 11, 3, 10, 0, 0, 0, time.UTC), "Europe/Vienna")
	v := ical.VEvent{
		UID: "uid-1", Start: start, End: ical.Timed(start.Time.Add(30*time.Minute), "Europe/Vienna"),
		Summary: "Design review", Description: "Bring the numbers", Status: "TENTATIVE",
	}
	return v.WithConference(ical.Conference{ID: "6MCF0BSHBR", URL: meetURL, Provider: ical.ProviderMeet, Host: "me@proton.me"})
}

func TestAnEditKeepsTheCallAndWhatTheModelDoesNotName(t *testing.T) {
	d := &syncDoer{t: t}
	s, ck := meetService(t, d, &fakeMeetings{})
	before := meetEvent()
	d.stored = storedAs(t, ck, before)

	location := "Room 3"
	if _, err := s.EventUpdate(t.Context(), "cal1", "ev1", EventPatch{Location: &location}); err != nil {
		t.Fatal(err)
	}
	after := written(t, s, ck, d.stored, d.synced[0])
	if after.Conference == nil || *after.Conference != *before.Conference {
		t.Errorf("conference = %+v, want %+v", after.Conference, before.Conference)
	}
	if after.Description != before.Description || after.Status != "TENTATIVE" || after.Location != location {
		t.Errorf("after = %+v", after)
	}
}

func TestMeetAddsALinkAndNoMeetTakesItAway(t *testing.T) {
	d := &syncDoer{t: t}
	meetings := &fakeMeetings{}
	s, ck := meetService(t, d, meetings)
	plain := meetEvent().WithoutCall()
	d.stored = storedAs(t, ck, plain)

	yes := true
	res, err := s.EventUpdate(t.Context(), "cal1", "ev1", EventPatch{Meet: &yes})
	if err != nil {
		t.Fatal(err)
	}
	if res.Call == nil || res.Call.URL != meetURL || len(meetings.created) != 1 || !meetings.created[0].FromCalendar {
		t.Fatalf("res = %+v, created %+v", res, meetings.created)
	}
	with := written(t, s, ck, d.stored, d.synced[0])
	if call, ok := with.Call(); !ok || call.URL != meetURL || !strings.Contains(with.Description, "Join Proton Meet: "+meetURL) {
		t.Errorf("with = %+v", with)
	}

	d.stored, d.synced = storedAs(t, ck, with), nil
	no := false
	if _, err := s.EventUpdate(t.Context(), "cal1", "ev1", EventPatch{Meet: &no}); err != nil {
		t.Fatal(err)
	}
	without := written(t, s, ck, d.stored, d.synced[0])
	if _, ok := without.Call(); ok || without.Description != "Bring the numbers" {
		t.Errorf("without = %+v", without)
	}
	if len(meetings.deleted) != 1 || meetings.deleted[0] != "6MCF0BSHBR" {
		t.Errorf("deleted %v, want the meeting the link opened", meetings.deleted)
	}

	d.stored = storedAs(t, ck, without)
	res, err = s.EventUpdate(t.Context(), "cal1", "ev1", EventPatch{Meet: &no})
	if err != nil || !res.MeetUnchanged {
		t.Errorf("a second --no-meet: %+v, %v", res, err)
	}
}

func TestOnlyTheOrganizerChangesTheCall(t *testing.T) {
	d := &syncDoer{t: t}
	s, ck := meetService(t, d, &fakeMeetings{})
	d.stored = storedAs(t, ck, meetEvent())
	d.stored.IsOrganizer = 0
	no := false
	_, err := s.EventUpdate(t.Context(), "cal1", "ev1", EventPatch{Meet: &no})
	var p *errs.Problem
	if !errors.As(err, &p) || len(d.synced) != 0 {
		t.Errorf("err = %v, synced %d", err, len(d.synced))
	}
}

func TestAnotherServicesCallIsNotReplaced(t *testing.T) {
	d := &syncDoer{t: t}
	s, ck := meetService(t, d, &fakeMeetings{})
	d.stored = storedAs(t, ck, meetEvent().WithoutCall().WithConference(ical.Conference{
		ID: "123", URL: "https://us02web.zoom.us/j/123", Provider: ical.ProviderZoom,
	}))
	yes := true
	if _, err := s.EventUpdate(t.Context(), "cal1", "ev1", EventPatch{Meet: &yes}); err == nil {
		t.Error("a Zoom call was replaced")
	}
}

func TestAMeetingIsGivenBackWhenTheEventIsRefused(t *testing.T) {
	d := &syncDoer{t: t, refuse: errors.New("refused")}
	meetings := &fakeMeetings{}
	s, _ := meetService(t, d, meetings)
	start := ical.Timed(time.Date(2026, 11, 3, 10, 0, 0, 0, time.UTC), "Europe/Vienna")
	if _, err := s.EventCreate(t.Context(), "cal1", EventInput{Title: "Review", Start: start, Duration: time.Hour, Meet: true}); err == nil {
		t.Fatal("the refusal was lost")
	}
	if len(meetings.deleted) != 1 {
		t.Errorf("deleted %v, want the meeting made for the event", meetings.deleted)
	}
}

func TestAutoAddFollowsTheSettingAndTheOrganization(t *testing.T) {
	for _, tc := range []struct {
		setting   any
		forbidden bool
		want      bool
	}{
		{setting: map[string]any{"Provider": 2, "DisplayNotification": 1}, want: true},
		{setting: 2, want: true},
		{setting: map[string]any{"Provider": 1}, want: false},
		{setting: 0, want: false},
		{setting: 2, forbidden: true, want: false},
	} {
		d := &syncDoer{t: t, settings: map[string]any{"AutoAddConferenceLink": tc.setting}}
		s, _ := meetService(t, d, &fakeMeetings{forbidden: tc.forbidden})
		if got := s.AutoAddsMeet(t.Context()); got != tc.want {
			t.Errorf("setting %v, forbidden %v: %v, want %v", tc.setting, tc.forbidden, got, tc.want)
		}
	}
}

func TestAWholeSeriesCarriesItsCallToEditedOccurrences(t *testing.T) {
	series := meetEvent()
	occurrence := series.WithoutCall()
	carried, changed := carryCall(occurrence, series)
	if !changed || carried.Conference == nil || carried.Conference.URL != meetURL {
		t.Errorf("carried = %+v", carried)
	}
	if _, changed := carryCall(series, series); changed {
		t.Error("an occurrence that already has the call was rewritten")
	}
	removed, changed := carryCall(series, series.WithoutCall())
	if !changed || removed.Conference != nil {
		t.Errorf("removed = %+v", removed)
	}
}
