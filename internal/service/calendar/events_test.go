package calendar

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/roman-16/proton-cli/internal/ical"
	"github.com/roman-16/proton-cli/internal/proton"
	"github.com/roman-16/proton-cli/internal/skip"
)

type windowDoer struct {
	mu      sync.Mutex
	asked   []string
	spans   []string
	byType  map[string][][]map[string]any
	failing map[string]error
}

func (d *windowDoer) Do(context.Context, proton.Request) (*proton.Response, error) { return nil, nil }

func (d *windowDoer) Decode(_ context.Context, r proton.Request, out any) error {
	if err := d.failing[calendarOf(r)]; err != nil {
		return err
	}
	typ := r.Query.Get("Type")
	page := r.Query.Get("Page")
	d.mu.Lock()
	d.asked = append(d.asked, typ+"/"+page)
	d.spans = append(d.spans, r.Query.Get("Start")+"-"+r.Query.Get("End"))
	pages := d.byType[typ]
	d.mu.Unlock()

	body := map[string]any{"Events": []map[string]any{}, "More": 0}
	idx := 0
	if page != "" {
		for _, c := range page {
			idx = idx*10 + int(c-'0')
		}
	}
	if idx < len(pages) {
		body["Events"] = pages[idx]
		if idx+1 < len(pages) {
			body["More"] = 1
		}
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, out)
}

func someWindow() ical.Window {
	day := time.Date(2026, 8, 14, 0, 0, 0, 0, time.UTC)
	return ical.Days(day, day)
}

func (d *windowDoer) askedFor() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := append([]string{}, d.asked...)
	sort.Strings(out)
	return out
}

func (d *windowDoer) spansAsked() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	distinct := map[string]bool{}
	for _, s := range d.spans {
		distinct[s] = true
	}
	out := make([]string, 0, len(distinct))
	for s := range distinct {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

func calendarOf(r proton.Request) string {
	return strings.TrimSuffix(strings.TrimPrefix(r.Path, "/calendar/v1/"), "/events")
}

func rawJSON(id string) map[string]any {
	return map[string]any{"ID": id, "CalendarID": "cal1", "UID": "uid-" + id, "SharedEvents": []any{}}
}

func opened(d proton.Doer, calendars map[string]error) *Service {
	s := New(d, testKeys(nil))
	for id, err := range calendars {
		_, _ = s.unlocked.Do(id, func() (*calKeys, error) {
			if err != nil {
				return nil, err
			}
			return &calKeys{}, nil
		})
	}
	return s
}

func TestRawEventsBetweenAsksForAllFourWindows(t *testing.T) {
	d := &windowDoer{byType: map[string][][]map[string]any{
		"0": {{rawJSON("a")}},
		"1": {{rawJSON("b")}},
		"2": {{rawJSON("c")}},
		"3": {{rawJSON("d")}},
	}}
	got, err := New(d, testKeys(nil)).rawEventsBetween(context.Background(), "cal1", someWindow())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 {
		t.Fatalf("got %d events, want one from each window", len(got))
	}
	for _, want := range []string{"0/0", "1/0", "2/0", "3/0"} {
		if !contains(d.askedFor(), want) {
			t.Errorf("never asked for window %s; asked %v", want, d.askedFor())
		}
	}
}

func TestRawEventsBetweenDeduplicatesAcrossWindows(t *testing.T) {
	d := &windowDoer{byType: map[string][][]map[string]any{
		"0": {{rawJSON("a")}},
		"1": {{rawJSON("a")}},
	}}
	got, err := New(d, testKeys(nil)).rawEventsBetween(context.Background(), "cal1", someWindow())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Errorf("got %d events, want the union deduplicated by ID", len(got))
	}
}

func TestRawEventsBetweenWalksEveryPage(t *testing.T) {
	d := &windowDoer{byType: map[string][][]map[string]any{
		"0": {{rawJSON("a")}, {rawJSON("b")}, {rawJSON("c")}},
	}}
	got, err := New(d, testKeys(nil)).rawEventsBetween(context.Background(), "cal1", someWindow())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d events, want all three pages", len(got))
	}
	for _, want := range []string{"0/0", "0/1", "0/2"} {
		if !contains(d.askedFor(), want) {
			t.Errorf("never asked for page %s; asked %v", want, d.askedFor())
		}
	}
}

func TestRawEventsBetweenAsksEverySpanForAllFourWindows(t *testing.T) {
	first := time.Date(2026, 9, 16, 0, 0, 0, 0, time.UTC)
	w := ical.Days(first, first.AddDate(0, 0, 90))
	d := &windowDoer{byType: map[string][][]map[string]any{
		"0": {{rawJSON("a")}},
		"1": {{rawJSON("b")}},
	}}
	got, err := New(d, testKeys(nil)).rawEventsBetween(context.Background(), "cal1", w)
	if err != nil {
		t.Fatal(err)
	}
	spans := fetchSpans(w)
	if len(spans) < 2 {
		t.Fatalf("a 91-day window was cut into %d span(s); the test needs more than one", len(spans))
	}
	if asked := d.spansAsked(); len(asked) != len(spans) {
		t.Errorf("asked for %d distinct spans, want %d: %v", len(asked), len(spans), asked)
	}
	for _, sp := range spans {
		want := fmt.Sprintf("%d-%d", sp.from.Unix(), sp.to.Unix())
		if !contains(d.spansAsked(), want) {
			t.Errorf("never asked for span %s; asked %v", want, d.spansAsked())
		}
	}
	if asked := d.askedFor(); len(asked) != len(spans)*len(queryTypes) {
		t.Errorf("made %d requests, want each span asked for all %d windows", len(asked), len(queryTypes))
	}
	if len(got) != 2 {
		t.Errorf("got %d events, want an event answering every span reported once", len(got))
	}
}

func TestFetchSpansCoverTheBoundsInRangesTheEndpointAccepts(t *testing.T) {
	first := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	w := ical.Days(first, first.AddDate(1, 0, 0))
	from, to := fetchBounds(w)
	spans := fetchSpans(w)
	if len(spans) == 0 {
		t.Fatal("a year was cut into no spans at all")
	}
	if !spans[0].from.Equal(from) {
		t.Errorf("the first span starts %s, want the bounds' start %s", spans[0].from, from)
	}
	if last := spans[len(spans)-1]; !last.to.Equal(to) {
		t.Errorf("the last span ends %s, want the bounds' end %s", last.to, to)
	}
	for i, sp := range spans {
		if width := sp.to.Sub(sp.from); width > fetchSpan || width <= 0 {
			t.Errorf("span %d is %s wide, want within (0, %s]", i, width, fetchSpan)
		}
		if i > 0 && !sp.from.Equal(spans[i-1].to) {
			t.Errorf("span %d starts %s, want where span %d ended, %s", i, sp.from, i-1, spans[i-1].to)
		}
	}
}

func TestFetchSpansAskForADefaultListingAtOnce(t *testing.T) {
	first, last := DefaultDays()
	if spans := fetchSpans(ical.Days(first, last)); len(spans) != 1 {
		t.Errorf("the default window was cut into %d spans, want 1", len(spans))
	}
}

func TestFetchBoundsStartNoEarlierThanTheEpoch(t *testing.T) {
	last := time.Date(1970, 1, 10, 0, 0, 0, 0, time.UTC)
	from, to := fetchBounds(ical.Days(time.Date(1969, 12, 20, 0, 0, 0, 0, time.UTC), last))
	if from.Unix() != 0 {
		t.Errorf("asked from %d, want the epoch", from.Unix())
	}
	if want := last.AddDate(0, 0, 2); !to.Equal(want) {
		t.Errorf("asked to %s, want %s", to, want)
	}
	if spans := fetchSpans(ical.Days(time.Time{}, time.Time{})); len(spans) != 0 {
		t.Errorf("a window entirely before the epoch was cut into %d spans, want none to ask for", len(spans))
	}
}

func TestFetchBoundsReachADayPastTheWindow(t *testing.T) {
	first := time.Date(2026, 8, 14, 0, 0, 0, 0, time.UTC)
	from, to := fetchBounds(ical.Days(first, first))
	if want := first.AddDate(0, 0, -1); !from.Equal(want) {
		t.Errorf("asked from %s, want %s", from, want)
	}
	if want := first.AddDate(0, 0, 2); !to.Equal(want) {
		t.Errorf("asked to %s, want %s", to, want)
	}
}

func TestReadCalendarsLeavesOutTheOneItCannotRead(t *testing.T) {
	d := &windowDoer{
		byType:  map[string][][]map[string]any{"0": {{rawJSON("a")}}},
		failing: map[string]error{"cal2": errors.New("no such calendar")},
	}
	s := opened(d, map[string]error{"cal1": nil, "cal2": nil})
	ctx, tally := skip.With(context.Background())

	groups, err := s.readCalendars(ctx, []string{"cal1", "cal2"}, s.inWindow(someWindow()))
	if err != nil {
		t.Fatalf("one unreadable calendar emptied the answer: %v", err)
	}
	if len(groups) != 1 || len(groups[0]) != 1 {
		t.Fatalf("read %d calendars, want the one that answered", len(groups))
	}
	if tally.Count() != 1 {
		t.Errorf("counted %d skips, want the calendar that was left out", tally.Count())
	}
	if tally.Kind() != skip.KindCalendar {
		t.Errorf("recorded a missing %q, want a calendar", tally.Kind())
	}
	if !tally.Hides() {
		t.Error("a calendar went missing and nothing says its events went with it")
	}
}

func TestReadCalendarsIsStrictWhenNothingCouldBeRead(t *testing.T) {
	first, second := errors.New("first is gone"), errors.New("second is gone")
	d := &windowDoer{
		byType:  map[string][][]map[string]any{"0": {{rawJSON("a")}}},
		failing: map[string]error{"cal1": first, "cal2": second},
	}
	s := opened(d, map[string]error{"cal1": nil, "cal2": nil})
	ctx, tally := skip.With(context.Background())

	if _, err := s.readCalendars(ctx, []string{"cal1"}, s.inWindow(someWindow())); !errors.Is(err, first) {
		t.Errorf("the one calendar named failed and the answer was %v, want the failure", err)
	}
	_, err := s.readCalendars(ctx, []string{"cal2", "cal1"}, s.inWindow(someWindow()))
	if !errors.Is(err, second) {
		t.Errorf("reported %v, want the failure of the first calendar named", err)
	}
	if tally.Count() != 3 {
		t.Errorf("counted %d skips, want every calendar that failed recorded", tally.Count())
	}
}

func TestReadCalendarsRecordsACalendarItCannotUnlock(t *testing.T) {
	d := &windowDoer{byType: map[string][][]map[string]any{"0": {{rawJSON("a")}}}}
	s := opened(d, map[string]error{"cal1": nil, "cal2": errors.New("the passphrase will not open")})
	ctx, tally := skip.With(context.Background())

	groups, err := s.readCalendars(ctx, []string{"cal1", "cal2"}, s.inWindow(someWindow()))
	if err != nil {
		t.Fatalf("one calendar that would not unlock emptied the answer: %v", err)
	}
	if len(groups) != 1 {
		t.Fatalf("read %d calendars, want the one that opened", len(groups))
	}
	if tally.Count() != 1 || tally.Kind() != skip.KindCalendar {
		t.Errorf("counted %d missing %q, want one calendar", tally.Count(), tally.Kind())
	}
}

func TestReadCalendarsKeepsEachCalendarsEventsApart(t *testing.T) {
	d := &windowDoer{byType: map[string][][]map[string]any{"0": {{rawJSON("a")}}}}
	s := opened(d, map[string]error{"cal1": nil, "cal2": nil})

	groups, err := s.readCalendars(context.Background(), []string{"cal1", "cal2"}, s.inWindow(someWindow()))
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 2 {
		t.Fatalf("two calendars answered and came back as %d group(s)", len(groups))
	}
}

func TestEventsExportRecordsAnEventItCannotRead(t *testing.T) {
	d := &windowDoer{byType: map[string][][]map[string]any{"0": {{rawJSON("a"), rawJSON("b")}}}}
	s := opened(d, map[string]error{"cal1": nil})
	ctx, tally := skip.With(context.Background())

	w := someWindow()
	out, err := s.EventsExport(ctx, []string{"cal1"}, &w)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 0 {
		t.Fatalf("exported %d events nobody could read, want a file of what opened", len(out))
	}
	if tally.Count() != 2 {
		t.Errorf("counted %d skips, want the two events left out of the file", tally.Count())
	}
	if tally.Kind() != skip.KindEvent {
		t.Errorf("recorded a missing %q, want an event", tally.Kind())
	}
}

func TestRowReadsAnAllDayEventInTheReadersZone(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skipf("America/New_York is not available: %v", err)
	}
	saved := time.Local
	time.Local = loc
	t.Cleanup(func() { time.Local = saved })

	day := time.Date(2026, 8, 14, 0, 0, 0, 0, time.UTC)
	row := allDayStored("holiday", day).row()
	if got := row.Start.Format("2006-01-02 15:04"); got != "2026-08-14 00:00" {
		t.Errorf("the row starts %s, want the day the event names", got)
	}
	if got := row.End.Format("2006-01-02 15:04"); got != "2026-08-15 00:00" {
		t.Errorf("the row ends %s, want the midnight after its day", got)
	}
	if !row.AllDay {
		t.Error("the row does not report itself as all-day")
	}
}

func TestRowPlacesAnEventItCannotRead(t *testing.T) {
	at := atVienna(t, 4, 16, 9)
	row := unreadableStored(at).row()
	if !row.Start.Equal(at) {
		t.Errorf("the row starts %s, want %s", row.Start, at)
	}
	if row.End.Sub(row.Start) != time.Hour {
		t.Errorf("the row runs %v, want the hour the cleartext times say", row.End.Sub(row.Start))
	}
	if row.Zone != "Europe/Vienna" {
		t.Errorf("the row is anchored to %q, want the zone Proton reports", row.Zone)
	}
	if row.Title != "" {
		t.Errorf("the row claims a title it could not read: %q", row.Title)
	}
}

func TestRowReportsTheColourTheEventCarries(t *testing.T) {
	hex := "#179FD9"
	day := time.Date(2026, 8, 14, 0, 0, 0, 0, time.UTC)

	coloured := allDayStored("holiday", day)
	coloured.raw.Color = &hex
	if got := coloured.row().Color; got != hex {
		t.Errorf("the row reports the colour %q, want %s", got, hex)
	}

	unreadable := unreadableStored(atVienna(t, 4, 16, 9))
	unreadable.raw.Color = &hex
	if got := unreadable.row().Color; got != hex {
		t.Errorf("an unreadable event reports the colour %q, want %s", got, hex)
	}

	if got := allDayStored("plain", day).row().Color; got != "" {
		t.Errorf("an event with no colour of its own reports %q", got)
	}
}

func contains(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

func TestNotificationsPreserveTheDifferenceBetweenNoneAndDefaults(t *testing.T) {
	var absent rawEvent
	if got := absent.notifications(); got != nil {
		t.Errorf("an event with no reminder list produced %v, want nil", got)
	}
	empty := rawEvent{Notifications: []rawNotification{}}
	if got := empty.notifications(); got == nil || len(got) != 0 {
		t.Errorf("an event with an empty reminder list produced %v, want an empty list", got)
	}
	set := rawEvent{Notifications: []rawNotification{{Type: 1, Trigger: "-PT42M"}}}
	if got := set.notifications(); len(got) != 1 || got[0]["Trigger"] != "-PT42M" {
		t.Errorf("reminders did not survive: %v", got)
	}
}

func TestBuildRemindersKeepsAnEmptyRequestDistinctFromNoRequest(t *testing.T) {
	got, err := buildReminders(nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Errorf("buildReminders(nil) = %v, want nil", got)
	}
	got, err = buildReminders([]string{})
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || len(got) != 0 {
		t.Errorf("buildReminders([]) = %v, want an empty list", got)
	}
}

func TestResolveAttendeesNeedsNoSessionKey(t *testing.T) {
	d := &attendeeDoer{proton: map[string]bool{"alice@proton.me": true}}
	s := New(d, testKeys(nil))

	atts, clear, keys, external, err := s.resolveAttendees(context.Background(), "uid-1",
		[]string{"alice@proton.me", "bob@example.test", " ", "alice@proton.me"})
	if err != nil {
		t.Fatal(err)
	}
	if len(atts) != 2 || len(clear) != 2 {
		t.Fatalf("resolved %d attendees from two distinct addresses", len(atts))
	}
	for _, a := range atts {
		if a.Token == "" {
			t.Errorf("%s has no token, so the encrypted part cannot address them", a.Email)
		}
	}
	if len(keys) != 1 || keys[0].email != "alice@proton.me" {
		t.Errorf("Proton attendees = %+v, want alice alone", keys)
	}
	if len(external) != 1 || external[0] != "bob@example.test" {
		t.Errorf("addresses to email = %v, want bob alone", external)
	}
}

type attendeeDoer struct{ proton map[string]bool }

func (d *attendeeDoer) Do(context.Context, proton.Request) (*proton.Response, error) {
	return nil, nil
}

func (d *attendeeDoer) Decode(_ context.Context, r proton.Request, out any) error {
	var body []byte
	switch {
	case strings.Contains(r.Path, "/keys/all"):
		keys := []map[string]any{}
		if d.proton[r.Query.Get("Email")] {
			keys = append(keys, map[string]any{"PublicKey": testPublicKey, "Primary": 1})
		}
		body, _ = json.Marshal(map[string]any{"Address": map[string]any{"Keys": keys}})
	default:
		var responses []map[string]any
		for _, email := range r.Query["Emails[]"] {
			responses = append(responses, map[string]any{
				"Email":    email,
				"Response": map[string]any{"CanonicalEmail": email, "Code": 1000},
			})
		}
		body, _ = json.Marshal(map[string]any{"Code": 1001, "Responses": responses})
	}
	return json.Unmarshal(body, out)
}

const testPublicKey = `-----BEGIN PGP PUBLIC KEY BLOCK-----
Comment: https://gopenpgp.org
Version: GopenPGP 2.10.0

xjMEanrRaBYJKwYBBAHaRw8BAQdACd3+qIrIQSG1BAIGCPhcHAK9ZFIy5uLYHMXw
ASIB0ovNGHRlc3QgPHRlc3RAZXhhbXBsZS50ZXN0PsK/BBMWCABxBYJqetFoAwsJ
BwkQNW6wIneRB3I1FAAAAAAAHAAQc2FsdEBub3RhdGlvbnMub3BlbnBncGpzLm9y
Z7RzFMVqdrzrhZrsmVkgBFkCFQgDFgACAhkBApsDAh4BFiEEaiNpE6gAeUfdgRwc
NW6wIneRB3IAAAmgAQCtFTfVjdVp/HJv3pF5rz0R2K/+9rYy+lqeSYsq2pdeTwD/
Qf0H3pe7+/UIivsrVJlyaTQZ+mRYOO703+8lilkZlQTOOARqetFoEgorBgEEAZdV
AQUBAQdAfeMJes3qT4zHKU1+XYHmPfIO7Z7A2zxdN/ek6BmER14DAQoJwq4EGBYI
AGAFgmp60WgJEDVusCJ3kQdyNRQAAAAAABwAEHNhbHRAbm90YXRpb25zLm9wZW5w
Z3Bqcy5vcmf0bHd8xMlCKVp5U+bPEFC+ApsMFiEEaiNpE6gAeUfdgRwcNW6wIneR
B3IAALwCAP44Jm/M79n2m7svoV9utRucsvD6pAXhrn9Z4vGWGaCZ2gD7B1ZJ/xyC
RNaFQnmm/GZKYXJ2+371ALYPsqZCwuDS0Q0=
=rm4w
-----END PGP PUBLIC KEY BLOCK-----`
