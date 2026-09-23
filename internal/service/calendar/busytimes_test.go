package calendar

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/roman-16/proton-cli/internal/account/plan"
	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/roman-16/proton-cli/internal/ical"
	"github.com/roman-16/proton-cli/internal/proton"
)

// busyDoer answers the plan and the busy-times endpoint the way Proton was seen
// to answer them, and keeps what it was asked. The questions arrive together,
// so it locks.
type busyDoer struct {
	mu sync.Mutex
	// plan is the organization's plan name; empty is an account in none.
	plan string
	// answer is what the busy-times endpoint says about one person, as JSON.
	answer func(email string, q url.Values) (string, error)
	asked  []proton.Request
}

func (d *busyDoer) Do(context.Context, proton.Request) (*proton.Response, error) {
	return nil, errors.New("busyDoer only decodes")
}

func (d *busyDoer) Decode(_ context.Context, r proton.Request, out any) error {
	d.mu.Lock()
	d.asked = append(d.asked, r)
	d.mu.Unlock()
	switch {
	case r.Path == "/core/v4/organizations":
		if d.plan == "" {
			return &proton.APIError{HTTPStatus: 422, Code: plan.NoOrganization, Message: "not a member"}
		}
		return json.Unmarshal(fmt.Appendf(nil,
			`{"Code":1000,"Organization":{"PlanName":%q,"MaxDomains":3}}`, d.plan), out)
	case strings.HasPrefix(r.Path, "/calendar/v1/") && strings.HasSuffix(r.Path, "/busy-schedule"):
		email, err := url.PathUnescape(strings.TrimSuffix(strings.TrimPrefix(r.Path, "/calendar/v1/"), "/busy-schedule"))
		if err != nil {
			return err
		}
		body, err := d.answer(email, r.Query)
		if err != nil {
			return err
		}
		return json.Unmarshal([]byte(body), out)
	}
	return fmt.Errorf("unexpected request %s %s", r.Method, r.Path)
}

// busyAsked are the questions put to the busy-times endpoint.
func (d *busyDoer) busyAsked() []proton.Request {
	d.mu.Lock()
	defer d.mu.Unlock()
	var out []proton.Request
	for _, r := range d.asked {
		if strings.HasSuffix(r.Path, "/busy-schedule") {
			out = append(out, r)
		}
	}
	return out
}

// shownAnswer is Proton saying when somebody is busy: the slots given, as Unix
// seconds, with more to come when more is set.
func shownAnswer(more bool, slots ...[2]int64) string {
	parts := make([]string, 0, len(slots))
	for _, s := range slots {
		parts = append(parts, fmt.Sprintf(`{"Start":%d,"End":%d}`, s[0], s[1]))
	}
	return fmt.Sprintf(`{"Code":1000,"BusySchedule":{"IsDataAccessible":true,"BusyTimeSlots":[%s],"More":%t}}`,
		strings.Join(parts, ","), more)
}

// hiddenAnswer is Proton declining to say when somebody is busy, as it was seen
// to decline for an address outside Proton: no list at all, rather than an
// empty one.
const hiddenAnswer = `{"Code":1000,"BusySchedule":{"IsDataAccessible":false,"BusyTimeSlots":null,"More":false}}`

// inZone makes loc this process's zone for the test, as the CLI makes the zone
// it works in the process's own.
func inZone(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Skipf("%s is not available: %v", name, err)
	}
	saved := time.Local
	time.Local = loc
	t.Cleanup(func() { time.Local = saved })
	return loc
}

func busyWindow(loc *time.Location, first string, days int) ical.Window {
	day, err := time.ParseInLocation("2006-01-02", first, loc)
	if err != nil {
		panic(err)
	}
	return ical.DaysFrom(day, days)
}

func at(loc *time.Location, value string) int64 {
	t, err := time.ParseInLocation("2006-01-02 15:04", value, loc)
	if err != nil {
		panic(err)
	}
	return t.Unix()
}

// Proton answers anybody who asks, and its calendar asks only from a plan with
// room for several people. So does this: every other account is told which
// plans it would need, and nothing is asked about anybody.
func TestBusyTimesAreAskedOnlyOnAPlanForSeveralPeople(t *testing.T) {
	loc := inZone(t, "Europe/Vienna")
	for _, name := range []string{"", "bundle2022", "mail2022", "passfamily2024"} {
		d := &busyDoer{plan: name, answer: func(string, url.Values) (string, error) { return shownAnswer(false), nil }}
		_, _, err := New(d, testKeys(nil)).BusyTimes(context.Background(),
			[]string{"jane.roe@example.com"}, "Europe/Vienna", busyWindow(loc, "2026-05-04", 7))
		var problem *errs.Problem
		if !errors.As(err, &problem) || !strings.Contains(problem.Error(), "Duo, Family, Visionary or business plan") {
			t.Errorf("plan %q: BusyTimes = %v, want the plans it would need", name, err)
		}
		if asked := d.busyAsked(); len(asked) != 0 {
			t.Errorf("plan %q: asked %d busy-time questions before refusing", name, len(asked))
		}
	}
	for name := range busyPlans {
		d := &busyDoer{plan: name, answer: func(string, url.Values) (string, error) { return shownAnswer(false), nil }}
		if _, _, err := New(d, testKeys(nil)).BusyTimes(context.Background(),
			[]string{"jane.roe@example.com"}, "Europe/Vienna", busyWindow(loc, "2026-05-04", 7)); err != nil {
			t.Errorf("plan %q was refused: %v", name, err)
		}
	}
}

// Each person is asked all four windows over every week of the range, in the
// zone the days are read in, and from the first day's midnight to the last's -
// which is how Proton's calendar asks for the week it shows.
func TestBusyTimesAskAllFourWindowsOfEveryWeekInTheReadersZone(t *testing.T) {
	loc := inZone(t, "Europe/Vienna")
	w := busyWindow(loc, "2026-05-04", 9)
	d := &busyDoer{plan: "visionary2022", answer: func(string, url.Values) (string, error) { return shownAnswer(false), nil }}
	if _, _, err := New(d, testKeys(nil)).BusyTimes(context.Background(),
		[]string{"jane.roe+team@example.com"}, "Europe/Vienna", w); err != nil {
		t.Fatalf("BusyTimes = %v", err)
	}

	asked := d.busyAsked()
	if len(asked) != 2*len(queryTypes) {
		t.Fatalf("asked %d questions about nine days, want two weeks of four windows", len(asked))
	}
	from, until := w.Bounds()
	starts, ends := map[int64]bool{}, map[int64]bool{}
	types := map[string]int{}
	for _, r := range asked {
		if r.Path != "/calendar/v1/jane.roe+team@example.com/busy-schedule" {
			t.Errorf("asked %s, want the address in the path as it was written", r.Path)
		}
		if zone := r.Query.Get("Timezone"); zone != "Europe/Vienna" {
			t.Errorf("asked in %q, want the reader's zone", zone)
		}
		if r.Query.Get("Page") != "0" || r.Query.Get("PageSize") != "100" {
			t.Errorf("asked page %s of %s, want the first page at the widest", r.Query.Get("Page"), r.Query.Get("PageSize"))
		}
		start, _ := strconv.ParseInt(r.Query.Get("Start"), 10, 64)
		end, _ := strconv.ParseInt(r.Query.Get("End"), 10, 64)
		if width := time.Duration(end-start) * time.Second; width <= 0 || width > busySpan {
			t.Errorf("asked a span %s wide, want within (0, %s]", width, busySpan)
		}
		starts[start], ends[end] = true, true
		types[r.Query.Get("Type")]++
	}
	if !starts[from.Unix()] || !ends[until.Unix()] {
		t.Errorf("the spans asked for do not run from the first day's midnight to the last day's end")
	}
	for _, typ := range queryTypes {
		if types[typ] != 2 {
			t.Errorf("window %s asked %d times, want once a week", typ, types[typ])
		}
	}
}

// The endpoint caps a page and says when more follow, so a busy week does not
// lose its tail.
func TestBusyTimesWalkEveryPage(t *testing.T) {
	loc := inZone(t, "Europe/Vienna")
	d := &busyDoer{plan: "duo2024", answer: func(_ string, q url.Values) (string, error) {
		if q.Get("Type") != "0" {
			return shownAnswer(false), nil
		}
		if q.Get("Page") == "0" {
			return shownAnswer(true, [2]int64{at(loc, "2026-05-04 09:00"), at(loc, "2026-05-04 10:00")}), nil
		}
		return shownAnswer(false, [2]int64{at(loc, "2026-05-05 09:00"), at(loc, "2026-05-05 10:00")}), nil
	}}
	busy, _, err := New(d, testKeys(nil)).BusyTimes(context.Background(),
		[]string{"jane.roe@example.com"}, "Europe/Vienna", busyWindow(loc, "2026-05-04", 7))
	if err != nil {
		t.Fatalf("BusyTimes = %v", err)
	}
	if len(busy) != 2 {
		t.Errorf("listed %d busy times, want one from each page", len(busy))
	}
}

// A full-day busy time names days rather than instants, so it lands on the day
// it names wherever the reader is - a day early west of UTC and a day late east
// of it would be the mistake.
func TestAFullDayBusyTimeIsOnTheDayItNames(t *testing.T) {
	for _, zone := range []string{"Europe/Vienna", "America/New_York", "Pacific/Auckland"} {
		t.Run(zone, func(t *testing.T) {
			loc := inZone(t, zone)
			fifth := time.Date(2026, 5, 5, 0, 0, 0, 0, time.UTC).Unix()
			sixth := time.Date(2026, 5, 6, 0, 0, 0, 0, time.UTC).Unix()
			d := &busyDoer{plan: "family2022", answer: func(_ string, q url.Values) (string, error) {
				if q.Get("Type") == "2" {
					return shownAnswer(false, [2]int64{fifth, sixth}), nil
				}
				return shownAnswer(false), nil
			}}
			busy, _, err := New(d, testKeys(nil)).BusyTimes(context.Background(),
				[]string{"jane.roe@example.com"}, zone, busyWindow(loc, "2026-05-04", 3))
			if err != nil {
				t.Fatalf("BusyTimes = %v", err)
			}
			if len(busy) != 1 {
				t.Fatalf("listed %d busy times, want the one day", len(busy))
			}
			got := busy[0]
			if !got.AllDay || got.Start.Format("2006-01-02 15:04") != "2026-05-05 00:00" ||
				got.End.Format("2006-01-02 15:04") != "2026-05-06 00:00" {
				t.Errorf("the day came back as %s to %s, all day %t", got.Start, got.End, got.AllDay)
			}
			if got.Start.Location() != loc {
				t.Errorf("the day is read in %s, want the reader's zone", got.Start.Location())
			}
		})
	}
}

// A busy time that reaches from one week into the next answers the question for
// both, and is still one busy time.
func TestABusyTimeReachingIntoTheNextWeekIsListedOnce(t *testing.T) {
	loc := inZone(t, "Europe/Vienna")
	night := [2]int64{at(loc, "2026-05-10 22:00"), at(loc, "2026-05-11 02:00")}
	d := &busyDoer{plan: "bundlepro2024", answer: func(_ string, q url.Values) (string, error) {
		if q.Get("Type") == "0" || q.Get("Type") == "1" {
			return shownAnswer(false, night), nil
		}
		return shownAnswer(false), nil
	}}
	busy, _, err := New(d, testKeys(nil)).BusyTimes(context.Background(),
		[]string{"jane.roe@example.com"}, "Europe/Vienna", busyWindow(loc, "2026-05-04", 14))
	if err != nil {
		t.Fatalf("BusyTimes = %v", err)
	}
	if len(busy) != 1 {
		t.Errorf("listed the night %d times, want once", len(busy))
	}
}

// What comes back is put to the days that were asked about, not to wherever the
// endpoint's own edges fell.
func TestABusyTimeOffTheDaysAskedIsLeftOut(t *testing.T) {
	loc := inZone(t, "Europe/Vienna")
	d := &busyDoer{plan: "mailpro2022", answer: func(_ string, q url.Values) (string, error) {
		if q.Get("Type") != "0" {
			return shownAnswer(false), nil
		}
		return shownAnswer(false,
			[2]int64{at(loc, "2026-05-03 09:00"), at(loc, "2026-05-03 10:00")},
			[2]int64{at(loc, "2026-05-04 09:00"), at(loc, "2026-05-04 10:00")},
			[2]int64{at(loc, "2026-05-05 00:00"), at(loc, "2026-05-05 01:00")}), nil
	}}
	busy, _, err := New(d, testKeys(nil)).BusyTimes(context.Background(),
		[]string{"jane.roe@example.com"}, "Europe/Vienna", busyWindow(loc, "2026-05-04", 1))
	if err != nil {
		t.Fatalf("BusyTimes = %v", err)
	}
	if len(busy) != 1 || busy[0].Start.Format("2006-01-02 15:04") != "2026-05-04 09:00" {
		t.Errorf("listed %v, want only the busy time on the day asked about", busy)
	}
}

// Somebody Proton will not say for is named, in the order they were asked about,
// rather than left out to read as free. One address in two spellings is asked
// about once.
func TestSomebodyProtonWillNotShowIsUnknownRatherThanFree(t *testing.T) {
	loc := inZone(t, "Europe/Vienna")
	d := &busyDoer{plan: "mailbiz2024", answer: func(email string, q url.Values) (string, error) {
		if email == "max@example.org" {
			return hiddenAnswer, nil
		}
		if q.Get("Type") != "0" {
			return shownAnswer(false), nil
		}
		return shownAnswer(false, [2]int64{at(loc, "2026-05-04 09:00"), at(loc, "2026-05-04 10:00")}), nil
	}}
	busy, unknown, err := New(d, testKeys(nil)).BusyTimes(context.Background(),
		[]string{"max@example.org", "Jane.Roe@example.com", "jane.roe@example.com"}, "Europe/Vienna",
		busyWindow(loc, "2026-05-04", 7))
	if err != nil {
		t.Fatalf("BusyTimes = %v", err)
	}
	if !slices.Equal(unknown, []string{"max@example.org"}) {
		t.Errorf("unknown = %v, want the one Proton would not say for", unknown)
	}
	if len(busy) != 1 || busy[0].Email != "Jane.Roe@example.com" {
		t.Errorf("listed %v, want Jane's one busy time under the spelling first written", busy)
	}
	if asked := d.busyAsked(); len(asked) != 2*len(queryTypes) {
		t.Errorf("asked %d questions, want four for each of two people", len(asked))
	}

	everyone := &busyDoer{plan: "mailbiz2024", answer: func(string, url.Values) (string, error) { return shownAnswer(false), nil }}
	_, unknown, err = New(everyone, testKeys(nil)).BusyTimes(context.Background(),
		[]string{"jane.roe@example.com"}, "Europe/Vienna", busyWindow(loc, "2026-05-04", 7))
	if err != nil {
		t.Fatalf("BusyTimes = %v", err)
	}
	if unknown == nil || len(unknown) != 0 {
		t.Errorf("unknown = %#v with everybody shown, want an empty list rather than none", unknown)
	}
}

// A question Proton refused leaves the answer without part of somebody's week,
// so the whole answer fails rather than listing them as free.
func TestARefusedQuestionFailsTheAnswer(t *testing.T) {
	loc := inZone(t, "Europe/Vienna")
	refusal := &proton.APIError{HTTPStatus: 500, Message: "Internal server error"}
	d := &busyDoer{plan: "visionary2022", answer: func(_ string, q url.Values) (string, error) {
		if q.Get("Type") == "3" {
			return "", refusal
		}
		return shownAnswer(false), nil
	}}
	_, _, err := New(d, testKeys(nil)).BusyTimes(context.Background(),
		[]string{"jane.roe@example.com"}, "Europe/Vienna", busyWindow(loc, "2026-05-04", 7))
	if !errors.Is(err, error(refusal)) {
		t.Errorf("BusyTimes = %v, want Proton's refusal", err)
	}
}
