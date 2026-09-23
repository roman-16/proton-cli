package calendar

import (
	"cmp"
	"context"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/roman-16/proton-cli/internal/account/plan"
	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/roman-16/proton-cli/internal/fetch"
	"github.com/roman-16/proton-cli/internal/ical"
	"github.com/roman-16/proton-cli/internal/proton"
)

// When other people are busy.
//
// Proton's own calendar shows this while an event with participants is being
// placed on the grid: each participant's busy times, drawn over the week in
// view. What comes back is a start and an end and nothing else - no title, no
// place, no calendar - so it says when somebody is busy and never what with.
//
// Proton answers anybody who asks. Its calendar asks only from a plan with room
// for several people, so that is checked here first, and nothing is asked for an
// account Proton's own client would never ask for.

// busyPlans are the plans Proton's calendar shows busy times on
// (isUserEligibleForBusySlots, packages/components/helpers/busySlots.ts): the
// business plans, Duo, Family and Visionary.
var busyPlans = map[string]bool{
	"bundlebiz2025": true,
	"bundlepro2022": true,
	"bundlepro2024": true,
	"duo2024":       true,
	"family2022":    true,
	"mailbiz2024":   true,
	"mailpro2022":   true,
	"visionary2022": true,
}

// busySpan is the widest range the busy-times endpoint is asked for at once: a
// week, which is the most Proton's own calendar asks it for.
const busySpan = 7 * 24 * time.Hour

// busyPageSize is the largest page the busy-times endpoint serves.
const busyPageSize = 100

// BusyTime is a stretch of time somebody's calendars show them as busy.
type BusyTime struct {
	Email  string    `json:"email"`
	Start  time.Time `json:"start"`
	End    time.Time `json:"end"`
	AllDay bool      `json:"all_day"`
}

// rawBusySlot is one busy time as Proton sends it. For a full-day event the two
// are the dates it runs through, held as UTC midnights; for any other they are
// instants.
type rawBusySlot struct {
	Start int64
	End   int64
}

// busyQuery is one of the questions asked about one person, and its answer.
type busyQuery struct {
	email string
	span  span
	typ   string
	slots []rawBusySlot
	// hidden is Proton declining to say when this person is busy.
	hidden bool
}

// BusyTimes reports when each person is busy on the days the window covers, and
// which of them Proton would not say for.
//
// Every person is asked all four windows over every week of the range, in the
// zone the days are read in, which is how Proton's calendar asks for the week it
// shows. What comes back is put to the window by the same rule a listing of
// events is, so the answer is the days that were asked about and not whatever
// the endpoint's own edges took in.
func (s *Service) BusyTimes(ctx context.Context, emails []string, zone string, w ical.Window) ([]BusyTime, []string, error) {
	current, err := plan.Read(ctx, s.C)
	if err != nil {
		return nil, nil, err
	}
	if !busyPlans[current.Name] {
		return nil, nil, errs.Problemf("Seeing when others are busy needs a Duo, Family, Visionary or business plan.")
	}

	people := distinctAddresses(emails)
	from, to := sinceEpoch(w.Bounds())
	spans := cutSpans(from, to, busySpan)
	queries := make([]*busyQuery, 0, len(people)*len(spans)*len(queryTypes))
	for _, email := range people {
		for _, sp := range spans {
			for _, typ := range queryTypes {
				queries = append(queries, &busyQuery{email: email, span: sp, typ: typ})
			}
		}
	}
	calls := make([]func(context.Context) error, len(queries))
	for i, q := range queries {
		calls[i] = func(ctx context.Context) error { return s.askBusy(ctx, q, zone) }
	}
	if err := fetch.Together(ctx, calls...); err != nil {
		return nil, nil, err
	}

	hidden := map[string]bool{}
	for _, q := range queries {
		if q.hidden {
			hidden[q.email] = true
		}
	}
	var busy []BusyTime
	seen := map[string]bool{}
	for _, q := range queries {
		if hidden[q.email] {
			continue
		}
		for _, row := range q.rows(w) {
			// A busy time that reaches from one week into the next answers the
			// question for both, so it arrives twice.
			key := strings.Join([]string{strings.ToLower(row.Email),
				strconv.FormatInt(row.Start.Unix(), 10), strconv.FormatInt(row.End.Unix(), 10),
				strconv.FormatBool(row.AllDay)}, "|")
			if seen[key] {
				continue
			}
			seen[key] = true
			busy = append(busy, row)
		}
	}
	slices.SortStableFunc(busy, func(a, b BusyTime) int {
		return cmp.Or(a.Start.Compare(b.Start), strings.Compare(strings.ToLower(a.Email), strings.ToLower(b.Email)))
	})

	unknown := []string{}
	for _, email := range people {
		if hidden[email] {
			unknown = append(unknown, email)
		}
	}
	return busy, unknown, nil
}

// askBusy asks one of the questions about one person, page by page.
func (s *Service) askBusy(ctx context.Context, q *busyQuery, zone string) error {
	slots, err := proton.All(ctx, func(ctx context.Context, page int) ([]rawBusySlot, bool, error) {
		params := url.Values{}
		params.Set("Start", strconv.FormatInt(q.span.from.Unix(), 10))
		params.Set("End", strconv.FormatInt(q.span.to.Unix(), 10))
		params.Set("Type", q.typ)
		params.Set("Timezone", zone)
		params.Set("Page", strconv.Itoa(page))
		params.Set("PageSize", strconv.Itoa(busyPageSize))
		var r struct {
			BusySchedule struct {
				IsDataAccessible bool
				BusyTimeSlots    []rawBusySlot
				More             bool
			}
		}
		if err := s.C.Decode(ctx, proton.Request{
			Method: "GET", Path: "/calendar/v1/" + url.PathEscape(q.email) + "/busy-schedule", Query: params,
		}, &r); err != nil {
			return nil, false, err
		}
		answer := r.BusySchedule
		if !answer.IsDataAccessible {
			q.hidden = true
		}
		return answer.BusyTimeSlots, answer.More && len(answer.BusyTimeSlots) > 0, nil
	})
	q.slots = slots
	return err
}

// rows are the busy times one answer holds that touch a day of the window, in
// the zone the days are read in.
func (q *busyQuery) rows(w ical.Window) []BusyTime {
	allDay := fullDay(q.typ)
	out := make([]BusyTime, 0, len(q.slots))
	for _, slot := range q.slots {
		var start, end ical.DateTime
		if allDay {
			start, end = ical.Span(ical.Day(time.Unix(slot.Start, 0).UTC()), ical.Day(time.Unix(slot.End, 0).UTC()))
		} else {
			start, end = ical.Span(ical.Timed(time.Unix(slot.Start, 0), ""), ical.Timed(time.Unix(slot.End, 0), ""))
		}
		if !w.Covers(start, end) {
			continue
		}
		out = append(out, BusyTime{
			Email: q.email, Start: start.In(time.Local), End: end.In(time.Local), AllDay: allDay,
		})
	}
	return out
}

// distinctAddresses are the addresses asked about, each once, in the order and
// the spelling they were first written in. An address is the same address in
// any case.
func distinctAddresses(emails []string) []string {
	out := make([]string, 0, len(emails))
	seen := map[string]bool{}
	for _, email := range emails {
		key := strings.ToLower(email)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, email)
	}
	return out
}
