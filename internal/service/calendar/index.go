package calendar

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"strconv"
	"time"

	"github.com/roman-16/proton-cli/internal/ical"
	"github.com/roman-16/proton-cli/internal/progress"
	"github.com/roman-16/proton-cli/internal/proton"
	"github.com/roman-16/proton-cli/internal/search"
	"github.com/roman-16/proton-cli/internal/skip"
)

type indexed struct {
	ID            string     `json:"id"`
	CalendarID    string     `json:"calendar_id"`
	UID           string     `json:"uid,omitempty"`
	StartTime     int64      `json:"start_time"`
	EndTime       int64      `json:"end_time"`
	StartTimezone string     `json:"start_zone,omitempty"`
	EndTimezone   string     `json:"end_zone,omitempty"`
	FullDay       int        `json:"full_day,omitempty"`
	Color         string     `json:"color,omitempty"`
	Reminders     []reminder `json:"reminders"`
	ICS           string     `json:"ics,omitempty"`
}

type reminder struct {
	Kind    int    `json:"kind"`
	Trigger string `json:"trigger"`
}

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

var errUnreadable = errIndexRecord{}

type errIndexRecord struct{}

func (errIndexRecord) Error() string { return "calendar: the indexed event could not be read" }

func (s *Service) SetIndex(store *search.Store) { s.index = store }

func (s *Service) IndexPart() search.Part { return indexPart{s: s} }

type indexPart struct{ s *Service }

func (indexPart) App() search.App { return search.AppCalendar }
func (indexPart) Noun() string    { return "events" }

func (p indexPart) Open(ctx context.Context) (search.Session, error) { return p.s.openIndex(ctx) }

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

type indexSession struct {
	s   *Service
	log *search.Log
}

func (s *Service) openIndex(ctx context.Context) (*indexSession, error) {
	log, err := s.index.Load(ctx, search.AppCalendar)
	if err != nil {
		return nil, err
	}
	if log.State.Cursors == nil {
		log.State.Cursors = map[string]string{}
	}
	return &indexSession{s: s, log: log}, nil
}

func (x *indexSession) Status() search.Status { return x.log.Status() }

func (x *indexSession) save(ctx context.Context) error {
	x.log.State.Unreadable = x.unreadable(ctx)
	return x.log.Save(time.Now().Unix())
}

func (x *indexSession) unreadable(ctx context.Context) int {
	count := 0
	for _, rec := range x.log.Records() {
		var in indexed
		if err := json.Unmarshal(rec.Data, &in); err != nil {
			// Recorded and not counted: nothing is hidden, because the record is
			// counted here as one that went in unread, which is what `index list`
			// reports and what a reading of it says too.
			slog.DebugContext(ctx, "calendar: an index record would not read back",
				"kind", string(skip.KindEvent), "reason", string(skip.Malformed), "ref", rec.ID)
			count++
			continue
		}
		if in.ICS == "" {
			count++
		}
	}
	return count
}

func (x *indexSession) Build(ctx context.Context, sink progress.Sink) (search.Result, error) {
	log := x.log
	if log.State.Complete && !log.State.Stale {
		return search.Result{}, nil
	}
	calendars, err := x.s.CalendarsList(ctx)
	if err != nil {
		return search.Result{}, err
	}

	var done search.Result
	progress.Counting(sink, "events")
	sink.Start(0, "Indexing calendar")
	whole := true
	listed := make(map[string]bool, len(calendars))
	for _, cal := range calendars {
		listed[cal.ID] = true
		if log.State.Cursors[cal.ID] == "" {
			cursor, err := x.s.latestCalendarEvent(ctx, cal.ID)
			if err != nil {
				skip.Record(ctx, skip.KindCalendar, cal.ID, skip.Unreadable, err)
				whole = false
				continue
			}
			log.State.Cursors[cal.ID] = cursor
		}
		indexed, err := x.indexCalendar(ctx, cal.ID, sink)
		done = search.Total(done, indexed)
		if err != nil {
			if ctx.Err() != nil {
				return done, err
			}
			skip.Record(ctx, skip.KindCalendar, cal.ID, skip.Unreadable, err)
			delete(log.State.Cursors, cal.ID)
			whole = false
			continue
		}
	}
	left, err := x.forgetCalendars(ctx, listed)
	done = search.Total(done, left)
	if err != nil {
		return done, err
	}
	sink.Done()
	log.State.Total = log.State.Indexed
	log.State.Complete = whole
	log.State.Stale = !whole
	return done, x.save(ctx)
}

func (x *indexSession) indexCalendar(ctx context.Context, calendarID string, sink progress.Sink) (search.Result, error) {
	ck, keyErr := x.s.unlockCalendar(ctx, calendarID)
	if keyErr != nil {
		// Recorded and not counted: nothing is missing from the answer that the
		// answer does not show. Every event of the calendar goes in by its frame,
		// and how many are in that state is what `index list` counts as unreadable.
		slog.DebugContext(ctx, "calendar: a calendar's key would not open for the index",
			"kind", string(skip.KindCalendar), "reason", string(skip.Unlockable),
			"calendar", calendarID, "error", keyErr)
	}
	raws, err := x.s.everyEvent(ctx, calendarID)
	if err != nil {
		return search.Result{}, err
	}
	var done search.Result
	seen := make(map[string]bool, len(raws))
	records := make([]search.Record, 0, len(raws))
	for _, raw := range raws {
		seen[raw.ID] = true
		rec, readable, err := record(x.read(ctx, ck, raw))
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
	for _, in := range x.eventsOf(ctx, calendarID) {
		if seen[in.ID] {
			continue
		}
		records = append(records, search.Record{ID: in.ID, Gone: true})
		done.Removed++
	}
	if err := x.log.Append(records...); err != nil {
		return done, err
	}
	return done, x.save(ctx)
}

func (x *indexSession) read(ctx context.Context, ck *calKeys, raw rawEvent) stored {
	if ck == nil {
		return stored{raw: raw, readErr: errUnreadable}
	}
	return x.s.decrypt(ctx, ck, raw)
}

func (x *indexSession) calendarKeys(ctx context.Context, calendarID string) func() *calKeys {
	var ck *calKeys
	asked := false
	return func() *calKeys {
		if asked {
			return ck
		}
		asked = true
		var err error
		if ck, err = x.s.unlockCalendar(ctx, calendarID); err != nil {
			// Recorded and not counted: nothing is missing from the answer that the
			// answer does not show. What changed goes in by its frame, and how many
			// events are in that state is what `index list` counts as unreadable.
			slog.DebugContext(ctx, "calendar: a calendar's key would not open for the catch-up",
				"kind", string(skip.KindCalendar), "reason", string(skip.Unlockable),
				"calendar", calendarID, "error", err)
		}
		return ck
	}
}

func (x *indexSession) eventsOf(ctx context.Context, calendarID string) []indexed {
	var out []indexed
	for _, rec := range x.log.Records() {
		var in indexed
		if err := json.Unmarshal(rec.Data, &in); err != nil {
			skip.Record(ctx, skip.KindEvent, rec.ID, skip.Malformed, err)
			continue
		}
		if in.CalendarID == calendarID {
			out = append(out, in)
		}
	}
	return out
}

func (x *indexSession) forgetCalendars(ctx context.Context, listed map[string]bool) (search.Result, error) {
	var done search.Result
	var records []search.Record
	for calendarID := range x.log.State.Cursors {
		if listed[calendarID] {
			continue
		}
		for _, in := range x.eventsOf(ctx, calendarID) {
			records = append(records, search.Record{ID: in.ID, Gone: true})
			done.Removed++
		}
		delete(x.log.State.Cursors, calendarID)
	}
	return done, x.log.Append(records...)
}

func (s *Service) everyEvent(ctx context.Context, calendarID string) ([]rawEvent, error) {
	raws, err := proton.All(ctx, func(ctx context.Context, page int) ([]rawEvent, bool, error) {
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
	if err != nil {
		return nil, err
	}
	if err := s.withEveryAttendee(ctx, raws); err != nil {
		return nil, err
	}
	return raws, nil
}

func epoch() time.Time { return time.Date(1970, time.January, 2, 0, 0, 0, 0, time.UTC) }

const yearsExpanded = 3

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

func reminders(raw rawEvent) []reminder {
	out := make([]reminder, 0, len(raw.Notifications))
	for _, n := range raw.Notifications {
		out = append(out, reminder{Kind: n.Type, Trigger: n.Trigger})
	}
	return out
}

func (x *indexSession) Sync(ctx context.Context) (search.Result, error) {
	log := x.log
	if len(log.State.Cursors) == 0 {
		return search.Result{}, nil
	}
	calendars, err := x.s.CalendarsList(ctx)
	if err != nil {
		return search.Result{}, err
	}
	var done search.Result
	for _, cal := range calendars {
		cursor, known := log.State.Cursors[cal.ID]
		if !known {
			log.State.Complete = false
			continue
		}
		applied, err := x.syncCalendar(ctx, cal.ID, cursor)
		done = search.Total(done, applied)
		if err != nil {
			skip.Record(ctx, skip.KindCalendar, cal.ID, skip.Unreadable, err)
		}
	}
	return done, x.save(ctx)
}

func (x *indexSession) syncCalendar(ctx context.Context, calendarID, cursor string) (search.Result, error) {
	log := x.log
	var done search.Result
	keys := x.calendarKeys(ctx, calendarID)
	for page := 0; page < maxDrain; page++ {
		var batch calendarEventBatch
		if err := x.s.C.Decode(ctx, proton.Request{
			Method: "GET", Path: "/calendar/v1/" + calendarID + "/modelevents/" + cursor,
		}, &batch); err != nil {
			return done, err
		}
		if batch.Refresh != 0 {
			slog.WarnContext(ctx, "A calendar changed more than its history describes, so it will be read from Proton again.",
				"kind", string(skip.KindCalendar), "reason", string(skip.Unreadable))
			log.State.Stale = true
			fresh, err := x.s.latestCalendarEvent(ctx, calendarID)
			if err != nil {
				return done, err
			}
			log.State.Cursors[calendarID] = fresh
			done.Refreshed = true
			return done, nil
		}
		var records []search.Record
		for _, e := range batch.CalendarEvents {
			raw, held, err := x.current(ctx, calendarID, e.ID, e.Action)
			if err != nil {
				return done, err
			}
			if !held {
				records = append(records, search.Record{ID: e.ID, Gone: true})
				done.Removed++
				continue
			}
			rec, readable, err := record(x.read(ctx, keys(), raw))
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
		cursor = batch.CalendarModelEventID
		log.State.Cursors[calendarID] = cursor
		if batch.More == 0 {
			break
		}
	}
	return done, nil
}

// The history carries an event without its encrypted parts, so a change is read
// again whole.
func (x *indexSession) current(ctx context.Context, calendarID, eventID string, action int) (rawEvent, bool, error) {
	if action == eventDeleted {
		return rawEvent{}, false, nil
	}
	raw, err := x.s.rawEvent(ctx, calendarID, eventID)
	if proton.DoesNotExist(err) {
		return rawEvent{}, false, nil
	}
	return raw, err == nil, err
}

const maxDrain = 50

const eventDeleted = 0

type calendarEventBatch struct {
	CalendarModelEventID string
	More                 int
	Refresh              int
	CalendarEvents       []struct {
		ID     string
		Action int
	}
}

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

func (s *Service) Indexed() bool { return s.index != nil && s.index.Exists(search.AppCalendar) }

func (s *Service) indexedEvents(ctx context.Context, calendarIDs []string) ([]stored, bool) {
	if !s.Indexed() {
		return nil, false
	}
	status, err := s.index.Status(search.AppCalendar)
	if err != nil || !status.Complete || status.Stale {
		if err != nil {
			slog.DebugContext(ctx, "calendar: the index could not be read, so Proton answered",
				"kind", string(skip.KindCalendar), "reason", string(skip.Unreadable), "error", err)
		}
		return nil, false
	}
	x, err := s.openIndex(ctx)
	if err != nil {
		slog.DebugContext(ctx, "calendar: the index could not be opened, so Proton answered",
			"kind", string(skip.KindCalendar), "reason", string(skip.Unreadable), "error", err)
		return nil, false
	}
	x.syncBeforeRead(ctx)
	if !x.log.State.Complete || x.log.State.Stale {
		return nil, false
	}

	wanted := map[string]bool{}
	for _, id := range calendarIDs {
		wanted[id] = true
	}
	out := make([]stored, 0, len(x.log.Records()))
	for _, rec := range x.log.Records() {
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

func (x *indexSession) syncBeforeRead(ctx context.Context) {
	lock, err := x.s.index.Claim()
	if err != nil {
		slog.DebugContext(ctx, "calendar: the index was busy, so it was read as it stands",
			"kind", string(skip.KindCalendar), "reason", string(skip.Unreadable), "error", err)
		return
	}
	defer lock.Release()
	if _, err := x.Sync(ctx); err != nil {
		// Recorded and not counted: what is indexed is still every event the last
		// catch-up saw, and the answer covers what it covered before.
		slog.DebugContext(ctx, "calendar: the index could not be brought up to date",
			"kind", string(skip.KindCalendar), "reason", string(skip.Unreadable), "error", err)
	}
}

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
			row.attendeeEmails()) {
			out = append(out, row)
		}
	}
	return out, nil
}

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

func SearchWindow() ical.Window {
	return ical.Days(epoch(), time.Now().AddDate(yearsExpanded, 0, 0))
}
