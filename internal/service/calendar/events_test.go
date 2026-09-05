package calendar

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/roman-16/proton-cli/internal/ical"
	"github.com/roman-16/proton-cli/internal/proton"
)

// windowDoer answers the events endpoint from a per-window fixture and records
// which windows and pages were asked for.
type windowDoer struct {
	mu     sync.Mutex
	asked  []string
	byType map[string][][]map[string]any
}

func (d *windowDoer) Do(context.Context, proton.Request) (*proton.Response, error) { return nil, nil }

func (d *windowDoer) Decode(_ context.Context, r proton.Request, out any) error {
	typ := r.Query.Get("Type")
	page := r.Query.Get("Page")
	d.mu.Lock()
	d.asked = append(d.asked, typ+"/"+page)
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

// someWindow is any window at all, for the tests that care about which queries the
// endpoint is asked rather than about what falls in the range.
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

func rawJSON(id string) map[string]any {
	return map[string]any{"ID": id, "CalendarID": "cal1", "UID": "uid-" + id, "SharedEvents": []any{}}
}

// Type is a two-by-two selector, not a kind of event. Asking only for the first
// window hides every all-day event, and hides every recurring series whose first
// occurrence is in the past - which is the only way a series reaches a later
// window at all.
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

// An event can legitimately answer more than one window: a recurring series both
// starts in the range and reaches into it.
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

// The endpoint caps a page and reports whether more follow. A single request keeps
// whatever came back, so a busy window silently loses its tail.
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

func TestRawEventsBetweenNeverSendsANegativeBound(t *testing.T) {
	// The endpoint refuses a negative timestamp, and a zero time is what an unset
	// range looks like.
	d := &windowDoer{byType: map[string][][]map[string]any{}}
	if _, err := New(d, testKeys(nil)).rawEventsBetween(context.Background(), "cal1", ical.Days(time.Time{}, time.Time{})); err != nil {
		t.Fatal(err)
	}
	for _, a := range d.askedFor() {
		if strings.Contains(a, "-") {
			t.Errorf("asked with a negative bound: %s", a)
		}
	}
}

// The endpoint is asked for a day either side of the window. An all-day event names
// a date rather than an instant, so Proton holds it at an instant up to a day from
// the day it belongs to here, and the endpoint's own idea of which events touch the
// edge of a range is not this CLI's.
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

// The times a row carries are read in the reader's own zone, because that is the
// only frame in which an all-day event is on the day it says: read as an instant it
// slips to the day before in every zone behind UTC, taking the date column, the
// sort order and the machine output with it.
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

// An event nobody can decrypt is still placed and still anchored, from the times
// Proton keeps in the clear beside it.
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

// An event's colour is Proton's own cleartext field, so a row reports it whether
// or not the content behind it could be read.
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
	// Proton reads a null reminder list as "use the calendar's defaults" and an
	// empty one as "no reminders", so a round trip has to keep them apart or an
	// edit silently resets somebody's reminders.
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

// A Proton attendee reads their copy of an event with the event's session key,
// wrapped to their own. So the key that is wrapped has to be the key the event was
// encrypted with, and the only way to guarantee that is to make the key once.
//
// This pins the property that makes the mistake impossible: working out who the
// attendees are needs no session key at all, so nothing has to build the event's
// cards in order to find out.
func TestResolveAttendeesNeedsNoSessionKey(t *testing.T) {
	d := &attendeeDoer{proton: map[string]bool{"alice@proton.me": true}}
	// A service with no calendar keys: anything that tried to build the event's
	// cards here would have nothing to build them with.
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

// attendeeDoer answers the canonical-address and public-key endpoints.
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

// testPublicKey is a throwaway key, so a fake can answer the public-key endpoint
// the way Proton does for an address that has an account.
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
