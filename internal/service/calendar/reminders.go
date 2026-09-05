package calendar

import (
	"context"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/roman-16/proton-cli/internal/ical"
	"github.com/roman-16/proton-cli/internal/proton"
	"github.com/roman-16/proton-cli/internal/skip"
	"github.com/roman-16/proton-cli/internal/units"
)

// A reminder is an event's notification, at the moment it goes off.
//
// When a reminder fires is Proton's to say, not ours to derive. The alarms
// endpoint answers it exactly - an all-day event's reminder goes off at the
// calendar's chosen hour rather than midnight minus a trigger, a recurring one
// answers per occurrence, and an event that leans on its calendar's default
// notifications still gets its alarm. This CLI's own events list expands
// recurrences to list them, but the moment an interruption happens is served
// up already resolved. So it is fetched, and the events they name are read for
// their titles, the same events list already reads.

const (
	// remindHorizon is how far ahead a watch looks for alarms. A reminder set
	// for an event further out than the horizon would never be seen in time,
	// and one that falls inside it arrives the moment it is due.
	remindHorizon = 30 * 24 * time.Hour
	// remindRefresh is how often a watch re-reads the calendars, so an event
	// created or moved after it started still reminds on time.
	remindRefresh = 30 * time.Second
	// remindLate is how far past its moment a reminder is still worth raising.
	// Beyond that the machine was asleep or the network was gone, and what
	// arrives is not a reminder but a history lesson. It is the web clients'
	// own cutoff.
	remindLate = time.Minute
	// alarmPageSize is how many alarms a page of the endpoint holds.
	alarmPageSize = 100
	// alarmMaxPages caps how many pages a wide window may walk before giving
	// up, so a year-long listing cannot hold the loop forever.
	alarmMaxPages = 100
)

// Reminder is one notification of one occurrence: the event it warns about,
// reported as `events get` reports it, and the three facts that belong to the
// warning rather than to the event.
type Reminder struct {
	Event
	// Fires is when it goes off, which the alarms endpoint says exactly.
	Fires time.Time `json:"fires"`
	// Remind is the trigger, spelled as --remind takes it.
	Remind string `json:"remind"`
	// Says is the sentence a notification would carry, which is Proton's own
	// wording. It is here because working it out means knowing how far off the
	// start is and whether the event has a time of day at all - a judgement,
	// where the sender and subject of a message are just two fields.
	Says string `json:"says"`
}

// calendarAlarm is one of Proton's served alarms, reduced to the facts a
// reminder needs: which event, on which calendar, when it rings, and when the
// occurrence it belongs to starts.
type calendarAlarm struct {
	CalendarID      string
	EventID         string
	Occurrence      int64
	EventOccurrence int64
	Trigger         string
}

// RemindersList reports every reminder due on the window's days.
//
// The window is the one `events list` takes, so the two commands ask the same
// question of the same days - what differs is only which side of a reminder they
// report, the event or the interruption.
func (s *Service) RemindersList(ctx context.Context, calendarIDs []string, w ical.Window) ([]Reminder, error) {
	from, until := w.Bounds()
	return s.remindersBetween(ctx, calendarIDs, from, until, nil)
}

// reminderEvents caches the events a reminder names, so a watch that re-reads
// the calendars alongside its short refresh does not re-fetch every event each
// time - only the ones whose alarm has just appeared.
type reminderEvents map[string]*Event

// remindersBetween is the same question asked to the second rather than to the
// day, which is what a watch needs.
//
// Alarms are fetched over the window, and the events they name are read for
// their titles. Events are read as far ahead as the window reaches, because an
// alarm is inside the window even when the event it warns about is not.
func (s *Service) remindersBetween(ctx context.Context, calendarIDs []string, from, until time.Time, events reminderEvents) ([]Reminder, error) {
	if events == nil {
		events = reminderEvents{}
	}
	alarms, err := s.alarms(ctx, calendarIDs, from, until)
	if err != nil {
		return nil, err
	}
	if len(alarms) == 0 {
		return nil, nil
	}

	load := func(calendarID, eventID string) (*Event, error) {
		key := calendarID + "/" + eventID
		if e, ok := events[key]; ok {
			return e, nil
		}
		e, err := s.EventGet(ctx, calendarID, eventID, "")
		if err != nil {
			return nil, err
		}
		events[key] = e
		return e, nil
	}

	now := time.Now()
	var out []Reminder
	for _, al := range alarms {
		event, err := load(al.CalendarID, al.EventID)
		if err != nil {
			skip.Record(ctx, skip.KindReminder, al.EventID, skip.Unreadable, err)
			continue
		}
		r := reminderFromAlarm(al, event, now)
		if r != nil {
			out = append(out, *r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Fires.Before(out[j].Fires) })
	return out, nil
}

// alarms fetches the device alarms on each calendar between the two instants,
// de-duplicated.
func (s *Service) alarms(ctx context.Context, calendarIDs []string, from, until time.Time) ([]calendarAlarm, error) {
	seen := map[string]bool{}
	var out []calendarAlarm
	for _, calendarID := range calendarIDs {
		// The endpoint takes the moment to resume from rather than a page
		// number, so each answer says where the next request starts.
		start := from.Unix()
		err := proton.Pages(ctx, func(ctx context.Context, page int) (bool, error) {
			var r struct {
				Alarms []struct {
					ID              string
					EventID         string
					Occurrence      int64
					EventOccurrence int64
					Trigger         string
				}
			}
			if err := s.C.Decode(ctx, proton.Request{
				Method: "GET", Path: "/calendar/v1/" + calendarID + "/alarms",
				Query: proton.Query(
					"Start", strconv.FormatInt(start, 10),
					"End", strconv.FormatInt(until.Unix(), 10),
					"PageSize", strconv.Itoa(alarmPageSize),
				),
			}, &r); err != nil {
				return false, err
			}
			for _, a := range r.Alarms {
				fire := time.Unix(a.Occurrence, 0)
				if fire.Before(from) || !fire.Before(until) {
					continue
				}
				key := strconv.FormatInt(a.Occurrence, 10) + "/" + a.EventID
				if seen[key] {
					continue
				}
				seen[key] = true
				out = append(out, calendarAlarm{
					CalendarID: calendarID, EventID: a.EventID,
					Occurrence: a.Occurrence, EventOccurrence: a.EventOccurrence,
					Trigger: a.Trigger,
				})
			}
			if !proton.Full(r.Alarms, alarmPageSize) {
				return false, nil
			}
			start = r.Alarms[len(r.Alarms)-1].Occurrence + 1
			if page+1 >= alarmMaxPages {
				// Recorded and not counted: what is missing is the rest of a
				// walk this CLI chose to stop, not a thing that could not be
				// read, so there is no ref to name and no number to add to a
				// warning. The log says which calendar and how far it got.
				slog.DebugContext(ctx, "calendar: stopped walking alarms at the page cap",
					"calendar", calendarID, "count", page+1, "reason", "page cap")
				return false, nil
			}
			return true, nil
		})
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

// reminderFromAlarm joins one served alarm to its event.
//
// The row is the event itself, moved to the occurrence being warned about: what
// a reminder adds is when it rings and what it says, not a shorter description
// of the appointment. Both moments come from Proton - when the alarm rings, and
// when the occurrence starts, which for an all-day event is the calendar's
// chosen morning hour rather than anything derivable from the trigger alone. A
// cancelled event keeps no appointment and raises nothing.
func reminderFromAlarm(al calendarAlarm, event *Event, now time.Time) *Reminder {
	if event == nil || event.Status == "cancelled" {
		return nil
	}
	start := occurrenceStart(al)
	// The span moves with the occurrence: how long it runs is the event's, when it
	// ends is this occurrence's. An event whose end is not after its start has
	// none to move.
	var end time.Time
	if event.End.After(event.Start) {
		end = start.Add(event.End.Sub(event.Start))
	}
	row := *event
	row.Start, row.End = start, end
	row.Occurrence = start.Format("2006-01-02T15:04")
	return &Reminder{
		Event:  row,
		Fires:  time.Unix(al.Occurrence, 0),
		Remind: units.Duration(triggerDuration(al.Trigger)),
		Says:   says(row, now),
	}
}

// occurrenceStart is when the alarmed occurrence begins: the server's own word
// for it, or - should an answer leave that field out - the trigger read back
// from the fire time, honouring its sign, since a trigger before the start puts
// the start after the alarm and one after the start puts it before.
func occurrenceStart(al calendarAlarm) time.Time {
	fire := time.Unix(al.Occurrence, 0)
	if al.EventOccurrence != 0 {
		return time.Unix(al.EventOccurrence, 0)
	}
	before := strings.HasPrefix(al.Trigger, "-")
	if before {
		return fire.Add(triggerDuration(al.Trigger))
	}
	return fire.Add(-triggerDuration(al.Trigger))
}

// RemindersWatch raises each reminder as it comes due, until the context ends.
//
// It sleeps to the moment rather than polling for it: the times are known in
// advance, so the only thing worth asking Proton about periodically is whether
// the calendars have changed underneath.
func (s *Service) RemindersWatch(ctx context.Context, calendarIDs []string, emit func(Reminder) error) error {
	var (
		due         []Reminder
		raised      = map[string]bool{}
		events      = reminderEvents{}
		nextRefresh time.Time
	)
	for {
		now := time.Now()
		if !now.Before(nextRefresh) {
			// A moment before now, so a reminder that came due while the
			// calendars were being read is still found rather than skipped.
			found, err := s.remindersBetween(ctx, calendarIDs, now.Add(-remindLate), now.Add(remindHorizon), events)
			if err != nil {
				return err
			}
			due, raised = found, stillRaised(found, raised)
			nextRefresh = now.Add(remindRefresh)
			continue
		}

		next := nextUnraised(due, raised)
		wait := nextRefresh.Sub(now)
		if next != nil {
			wait = min(wait, next.Fires.Sub(now))
		}
		if wait > 0 {
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(wait):
			}
			continue
		}

		// The refresh is not due, so the wait ran out on the reminder itself.
		raised[remindKey(*next)] = true
		if time.Since(next.Fires) > remindLate {
			continue
		}
		fired := *next
		fired.Says = says(fired.Event, time.Now())
		if err := emit(fired); err != nil {
			return err
		}
	}
}

// nextUnraised is the soonest reminder that has not gone off yet.
func nextUnraised(due []Reminder, raised map[string]bool) *Reminder {
	for i := range due {
		if !raised[remindKey(due[i])] {
			return &due[i]
		}
	}
	return nil
}

// stillRaised forgets the reminders a refresh no longer knows about, so what a
// watch remembers is bounded by what is ahead of it rather than by how long it
// has been running.
func stillRaised(due []Reminder, raised map[string]bool) map[string]bool {
	kept := make(map[string]bool, len(raised))
	for _, r := range due {
		if key := remindKey(r); raised[key] {
			kept[key] = true
		}
	}
	return kept
}

// remindKey identifies one firing, so a refresh that turns up the same reminder
// again does not raise it twice.
func remindKey(r Reminder) string {
	return r.CalendarID + "/" + r.ID + "/" + r.Fires.UTC().Format(time.RFC3339) + "/" + r.Remind
}

// says is what a notification would read, in Proton's own words: its calendar
// tells you an event "starts at 10:00", "starts tomorrow", or - for one already
// under way - "started at 09:00".
//
// The dates and times are this CLI's, not the browser's locale formats, because
// a date is written one way here and a reminder is not the place to write it
// another.
func says(e Event, now time.Time) string {
	start := e.Start
	switch {
	case start.Sub(now).Abs() <= 30*time.Second:
		return e.Title + " starts now"
	case start.Before(now):
		switch {
		case sameDay(start, now):
			if e.AllDay {
				return e.Title + " starts today"
			}
			return e.Title + " started at " + start.Format("15:04")
		case sameDay(start, now.AddDate(0, 0, -1)):
			if e.AllDay {
				return e.Title + " started yesterday"
			}
			return e.Title + " started yesterday at " + start.Format("15:04")
		case e.AllDay:
			return e.Title + " started on " + start.Format("2006-01-02")
		default:
			return e.Title + " started on " + start.Format("2006-01-02") + " at " + start.Format("15:04")
		}
	case sameDay(start, now):
		if e.AllDay {
			return e.Title + " starts today"
		}
		return e.Title + " starts at " + start.Format("15:04")
	case sameDay(start, now.AddDate(0, 0, 1)):
		if e.AllDay {
			return e.Title + " starts tomorrow"
		}
		return e.Title + " starts tomorrow at " + start.Format("15:04")
	case e.AllDay:
		return e.Title + " starts on " + start.Format("2006-01-02")
	default:
		return e.Title + " starts on " + start.Format("2006-01-02") + " at " + start.Format("15:04")
	}
}

func sameDay(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.In(a.Location()).Date()
	return ay == by && am == bm && ad == bd
}
