package calendar

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/roman-16/proton-cli/internal/ical"
	"github.com/roman-16/proton-cli/internal/progress"
	"github.com/roman-16/proton-cli/internal/proton"
	"github.com/roman-16/proton-cli/internal/search"
	"github.com/roman-16/proton-cli/internal/skip"
)

// Indexing a calendar is holding every event, so a question about what one says
// stops being a question about a date range.
//
// An event's title, its location, its description and who was invited are sealed
// to the calendar's key, so Proton cannot search any of them. What it can answer
// is "which events touch these dates", which is why a listing is a window and why
// searching without one would mean fetching every event before answering.
//
// So the index holds them: the cleartext frame Proton keeps beside each event -
// which is what places one nobody can read - and the decrypted event itself, as
// the iCalendar text an export would write.

// indexed is one event as the index holds it.
type indexed struct {
	ID         string `json:"id"`
	CalendarID string `json:"calendar_id"`
	UID        string `json:"uid,omitempty"`
	// The cleartext frame, as Proton keeps it beside the event.
	StartTime     int64      `json:"start_time"`
	EndTime       int64      `json:"end_time"`
	StartTimezone string     `json:"start_zone,omitempty"`
	EndTimezone   string     `json:"end_zone,omitempty"`
	FullDay       int        `json:"full_day,omitempty"`
	Color         string     `json:"color,omitempty"`
	Reminders     []reminder `json:"reminders"`
	// ICS is the decrypted event, as an export would write it. It is the whole of
	// what was read, so what the index can answer and what a file would say are
	// the same thing.
	ICS string `json:"ics,omitempty"`
}

// reminder is one notification as Proton holds it, which is what the index holds
// rather than the words a listing renders it as: what goes in is what came out.
type reminder struct {
	Kind    int    `json:"kind"`
	Trigger string `json:"trigger"`
}

// event rebuilds what a listing is made from.
func (in indexed) event() stored {
	raw := rawEvent{
		ID: in.ID, CalendarID: in.CalendarID, UID: in.UID,
		StartTime: in.StartTime, EndTime: in.EndTime,
		StartTimezone: in.StartTimezone, EndTimezone: in.EndTimezone,
		FullDay: in.FullDay,
	}
	if in.Color != "" {
		color := in.Color
		raw.Color = &color
	}
	for _, r := range in.Reminders {
		raw.Notifications = append(raw.Notifications, rawNotification{Type: r.Kind, Trigger: r.Trigger})
	}
	events, err := ical.ParseCalendar(in.ICS)
	if err != nil || len(events) == 0 {
		return stored{raw: raw, readErr: errUnreadable}
	}
	return stored{raw: raw, model: events[0]}
}

// errUnreadable is what an index record that will not parse back reports, which
// is the state an event that would not decrypt is in anyway: it is placed by its
// cleartext times and says nothing else.
var errUnreadable = errIndexRecord{}

type errIndexRecord struct{}

func (errIndexRecord) Error() string { return "calendar: the indexed event could not be read" }

// SetIndex hands the service the profile's index directory.
func (s *Service) SetIndex(store *search.Store) { s.index = store }

// IndexPart is what `index` drives to keep the calendar index current.
func (s *Service) IndexPart() search.Part { return indexPart{s: s} }

type indexPart struct{ s *Service }

func (indexPart) App() search.App { return search.AppCalendar }
func (indexPart) Noun() string    { return "events" }

func (p indexPart) Status() (search.Status, error) { return p.s.index.Status(search.AppCalendar) }

// Count is how many events a build would index, which is every event of every
// calendar. Reading them is the build's own work, so a preview reads them and
// writes nothing.
func (p indexPart) Count(ctx context.Context) (int, error) {
	calendars, err := p.s.CalendarsList(ctx)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, cal := range calendars {
		raws, err := p.s.everyEvent(ctx, cal.ID)
		if err != nil {
			skip.Record(ctx, skip.KindCalendar, cal.ID, skip.Unreadable, err)
			continue
		}
		count += len(raws)
	}
	return count, nil
}

func (p indexPart) Build(ctx context.Context, sink progress.Sink) (search.Result, error) {
	return p.s.buildIndex(ctx, progress.Of(sink))
}

func (p indexPart) Sync(ctx context.Context) (search.Result, error) { return p.s.syncIndex(ctx) }

// buildIndex reads every event of every calendar and writes it down.
//
// A calendar is read whole rather than in windows: there is no anchor to carry
// on from, and the point of the index is that no date range is named.
func (s *Service) buildIndex(ctx context.Context, sink progress.Sink) (search.Result, error) {
	log, err := s.index.Load(ctx, search.AppCalendar)
	if err != nil {
		return search.Result{}, err
	}
	if log.State.Complete {
		return search.Result{}, nil
	}
	calendars, err := s.CalendarsList(ctx)
	if err != nil {
		return search.Result{}, err
	}
	if log.State.Cursors == nil {
		log.State.Cursors = map[string]string{}
	}

	var done search.Result
	sink.Start(0, "Indexing calendar")
	for _, cal := range calendars {
		// The cursor is taken before the calendar is read, so an event created
		// while it is being read is caught by the first sync rather than missed.
		if log.State.Cursors[cal.ID] == "" {
			cursor, err := s.latestCalendarEvent(ctx, cal.ID)
			if err != nil {
				skip.Record(ctx, skip.KindCalendar, cal.ID, skip.Unreadable, err)
				continue
			}
			log.State.Cursors[cal.ID] = cursor
		}
		indexed, err := s.indexCalendar(ctx, log, cal.ID, sink)
		done = search.Total(done, indexed)
		if err != nil {
			if ctx.Err() != nil {
				return done, err
			}
			skip.Record(ctx, skip.KindCalendar, cal.ID, skip.Unreadable, err)
			continue
		}
	}
	sink.Done()
	log.State.Total = log.State.Indexed
	log.State.Complete = true
	return done, log.Save(time.Now().Unix())
}

// indexCalendar reads one calendar whole.
func (s *Service) indexCalendar(ctx context.Context, log *search.Log, calendarID string, sink progress.Sink) (search.Result, error) {
	ck, err := s.unlockCalendar(ctx, calendarID)
	if err != nil {
		return search.Result{}, err
	}
	raws, err := s.everyEvent(ctx, calendarID)
	if err != nil {
		return search.Result{}, err
	}
	var done search.Result
	records := make([]search.Record, 0, len(raws))
	for _, raw := range raws {
		rec, readable, err := record(s.decrypt(ctx, ck, raw))
		if err != nil {
			return done, err
		}
		if !readable {
			done.Unreadable++
		}
		records = append(records, rec)
		done.Indexed++
		sink.Add(1)
	}
	log.State.Unreadable += done.Unreadable
	if err := log.Append(records...); err != nil {
		return done, err
	}
	return done, log.Save(time.Now().Unix())
}

// everyEvent is every event a calendar holds, page by page.
//
// A listing names days, and the endpoint refuses a range much wider than a
// month: it is built to answer "what touches these days", so a listing pays a
// request per span of the range it covers. Asked with no range at all the
// endpoint enumerates the calendar instead, which is what an index is and what a
// search costs without one - the whole thing, once, rather than a window at a
// time.
func (s *Service) everyEvent(ctx context.Context, calendarID string) ([]rawEvent, error) {
	return proton.All(ctx, func(ctx context.Context, page int) ([]rawEvent, bool, error) {
		q := url.Values{}
		q.Set("Page", strconv.Itoa(page))
		q.Set("PageSize", strconv.Itoa(eventsPageSize))
		var r struct{ Events []rawEvent }
		if err := s.C.Decode(ctx, proton.Request{
			Method: "GET", Path: "/calendar/v1/" + calendarID + "/events", Query: q,
		}, &r); err != nil {
			return nil, false, err
		}
		return r.Events, proton.Full(r.Events, eventsPageSize), nil
	})
}

// epoch is far enough back for the oldest event anybody holds.
func epoch() time.Time { return time.Date(1970, time.January, 2, 0, 0, 0, 0, time.UTC) }

// yearsExpanded is how far ahead a repeating event is listed when nobody named
// an end, which is what Proton's own clients expand a search to.
const yearsExpanded = 3

// record seals one event, and says whether it could be read.
func record(e stored) (search.Record, bool, error) {
	in := indexed{
		ID: e.raw.ID, CalendarID: e.raw.CalendarID, UID: e.raw.UID,
		StartTime: e.raw.StartTime, EndTime: e.raw.EndTime,
		StartTimezone: e.raw.StartTimezone, EndTimezone: e.raw.EndTimezone,
		FullDay: e.raw.FullDay, Color: e.raw.ownColor(), Reminders: reminders(e.raw),
	}
	readable := e.readErr == nil
	if readable {
		in.ICS = ical.Calendar([]ical.VEvent{e.model})
	}
	data, err := json.Marshal(in)
	if err != nil {
		return search.Record{}, readable, err
	}
	return search.Record{ID: in.ID, Data: data}, readable, nil
}

// reminders is an event's notifications as the index holds them.
func reminders(raw rawEvent) []reminder {
	out := make([]reminder, 0, len(raw.Notifications))
	for _, n := range raw.Notifications {
		out = append(out, reminder{Kind: n.Type, Trigger: n.Trigger})
	}
	return out
}

// syncIndex applies each calendar's change feed.
func (s *Service) syncIndex(ctx context.Context) (search.Result, error) {
	log, err := s.index.Load(ctx, search.AppCalendar)
	if err != nil {
		return search.Result{}, err
	}
	if len(log.State.Cursors) == 0 {
		return search.Result{}, nil
	}
	calendars, err := s.CalendarsList(ctx)
	if err != nil {
		return search.Result{}, err
	}
	var done search.Result
	for _, cal := range calendars {
		cursor, known := log.State.Cursors[cal.ID]
		if !known {
			// A calendar made since the build has nothing indexed, so there is
			// nothing to catch up on: it goes in whole the next time one is built.
			log.State.Complete = false
			continue
		}
		applied, err := s.syncCalendar(ctx, log, cal.ID, cursor)
		done = search.Total(done, applied)
		if err != nil {
			// Recorded and counted: a calendar that did not catch up is one whose
			// events the index may answer with as they were, so the answer is
			// short of whatever changed in it.
			skip.Record(ctx, skip.KindCalendar, cal.ID, skip.Unreadable, err)
		}
	}
	return done, log.Save(time.Now().Unix())
}

// syncCalendar follows one calendar's feed from where the index left off.
func (s *Service) syncCalendar(ctx context.Context, log *search.Log, calendarID, cursor string) (search.Result, error) {
	var done search.Result
	var ck *calKeys
	for page := 0; page < maxDrain; page++ {
		var batch calendarEventBatch
		if err := s.C.Decode(ctx, proton.Request{
			Method: "GET", Path: "/calendar/v1/" + calendarID + "/modelevents/" + cursor,
		}, &batch); err != nil {
			return done, err
		}
		if batch.Refresh != 0 {
			slog.WarnContext(ctx, "A calendar changed more than its history describes, so the index will be built again.",
				"kind", string(skip.KindCalendar), "reason", string(skip.Unreadable))
			log.State.Complete = false
			fresh, err := s.latestCalendarEvent(ctx, calendarID)
			if err != nil {
				return done, err
			}
			log.State.Cursors[calendarID] = fresh
			return done, nil
		}
		cursor = batch.CalendarModelEventID
		log.State.Cursors[calendarID] = cursor

		var records []search.Record
		for _, e := range batch.CalendarEvents {
			if e.Action == eventDeleted || e.Event == nil {
				records = append(records, search.Record{ID: e.ID, Gone: true})
				done.Removed++
				continue
			}
			if ck == nil {
				var err error
				if ck, err = s.unlockCalendar(ctx, calendarID); err != nil {
					return done, err
				}
			}
			rec, readable, err := record(s.decrypt(ctx, ck, *e.Event))
			if err != nil {
				return done, err
			}
			if !readable {
				done.Unreadable++
			}
			records = append(records, rec)
			done.Indexed++
		}
		if err := log.Append(records...); err != nil {
			return done, err
		}
		if batch.More == 0 {
			break
		}
	}
	return done, nil
}

// maxDrain caps how many pages one catch-up follows.
const maxDrain = 50

// eventDeleted is the action Proton reports for an event that is gone.
const eventDeleted = 0

// calendarEventBatch is one page of a calendar's history. The cursor it carries
// is named the way the endpoint that hands out the first one names it.
type calendarEventBatch struct {
	CalendarModelEventID string
	More                 int
	Refresh              int
	CalendarEvents       []struct {
		ID     string
		Action int
		Event  *rawEvent
	}
}

// latestCalendarEvent is the cursor for "from now on" in one calendar's own
// history, which Proton names after the calendar rather than after the feed.
func (s *Service) latestCalendarEvent(ctx context.Context, calendarID string) (string, error) {
	var r struct{ CalendarModelEventID string }
	if err := s.C.Decode(ctx, proton.Request{
		Method: "GET", Path: "/calendar/v1/" + calendarID + "/modelevents/latest",
	}, &r); err != nil {
		return "", err
	}
	if r.CalendarModelEventID == "" {
		return "", fmt.Errorf("calendar: no cursor for calendar history")
	}
	return r.CalendarModelEventID, nil
}

// Indexed reports whether this machine holds a copy of the calendars.
func (s *Service) Indexed() bool { return s.index != nil && s.index.Exists(search.AppCalendar) }

// indexedEvents is every event the index holds, brought up to date first.
func (s *Service) indexedEvents(ctx context.Context, calendarIDs []string) ([]stored, bool) {
	if !s.Indexed() {
		return nil, false
	}
	status, err := s.index.Status(search.AppCalendar)
	if err != nil || !status.Complete {
		if err != nil {
			slog.DebugContext(ctx, "calendar: the index could not be read, so Proton answered",
				"kind", string(skip.KindCalendar), "reason", string(skip.Unreadable), "error", err)
		}
		return nil, false
	}
	s.syncBeforeRead(ctx)

	log, err := s.index.Load(ctx, search.AppCalendar)
	if err != nil {
		slog.DebugContext(ctx, "calendar: the index could not be opened, so Proton answered",
			"kind", string(skip.KindCalendar), "reason", string(skip.Unreadable), "error", err)
		return nil, false
	}
	wanted := map[string]bool{}
	for _, id := range calendarIDs {
		wanted[id] = true
	}
	out := make([]stored, 0, len(log.Records()))
	for _, rec := range log.Records() {
		var in indexed
		if err := json.Unmarshal(rec.Data, &in); err != nil {
			skip.Record(ctx, skip.KindEvent, rec.ID, skip.Malformed, err)
			continue
		}
		if len(wanted) > 0 && !wanted[in.CalendarID] {
			continue
		}
		out = append(out, in.event())
	}
	return out, true
}

// syncBeforeRead brings the index up to date before it is read. A directory
// another run is writing to is left alone.
func (s *Service) syncBeforeRead(ctx context.Context) {
	lock, err := s.index.Claim()
	if err != nil {
		slog.DebugContext(ctx, "calendar: the index was busy, so it was read as it stands",
			"kind", string(skip.KindCalendar), "reason", string(skip.Unreadable), "error", err)
		return
	}
	defer lock.Release()
	if _, err := s.syncIndex(ctx); err != nil {
		// Recorded and not counted: what is indexed is still every event the last
		// catch-up saw, and the answer covers what it covered before.
		slog.DebugContext(ctx, "calendar: the index could not be brought up to date",
			"kind", string(skip.KindCalendar), "reason", string(skip.Unreadable), "error", err)
	}
}

// EventsSearch answers a question about what events say.
//
// Without a window it covers everything indexed, which is the whole point of
// asking about words rather than about days: "when was the dentist" has no range
// to name. With one it covers that range, so a keyword narrows a listing rather
// than replacing it.
//
// Without an index every event is fetched first, which is seconds for a calendar
// of thousands and is what keeps the search a thing that works before anybody
// has built anything.
func (s *Service) EventsSearch(ctx context.Context, calendarIDs []string, w ical.Window, keyword string) ([]Event, error) {
	events, ok := s.indexedEvents(ctx, calendarIDs)
	if !ok {
		var err error
		if events, err = s.everyStoredEvent(ctx, calendarIDs); err != nil {
			return nil, err
		}
	}
	terms := search.Terms(keyword)
	rows := expand(events, w)
	out := make([]Event, 0, len(rows))
	for _, row := range rows {
		if search.Matches(terms, row.Title, row.Location, row.Description, row.Organizer,
			strings.Join(row.Attendees, " ")) {
			out = append(out, row)
		}
	}
	return out, nil
}

// everyStoredEvent reads every event of the named calendars from Proton, which
// is what a search costs where nothing is indexed.
func (s *Service) everyStoredEvent(ctx context.Context, calendarIDs []string) ([]stored, error) {
	groups, err := s.readCalendars(ctx, calendarIDs, s.everyEvent)
	if err != nil {
		return nil, err
	}
	var out []stored
	for _, events := range groups {
		out = append(out, events...)
	}
	return out, nil
}

// SearchWindow is the range a keyword search covers when nobody named one:
// everything that has happened, and the next three years of everything that
// repeats.
func SearchWindow() ical.Window {
	return ical.Days(epoch(), time.Now().AddDate(yearsExpanded, 0, 0))
}
