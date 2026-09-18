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

func (p indexPart) Open(ctx context.Context) (search.Session, error) { return p.s.openIndex(ctx) }

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

// indexSession is the calendar index, open.
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

// save writes the state file, with how much of the index went in unread counted
// off the records it holds.
func (x *indexSession) save(ctx context.Context) error {
	x.log.State.Unreadable = x.unreadable(ctx)
	return x.log.Save(time.Now().Unix())
}

// unreadable is how many of the events the index holds went in without their
// contents.
//
// It is counted off the records rather than added up as they are written: an
// event written twice is one event, one that has been read since is no longer
// among them, and one the calendar no longer has is gone.
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

// Build reads every event of every calendar and writes it down.
//
// A calendar is read whole rather than in windows: there is no anchor to carry
// on from, and the point of the index is that no date range is named. Reading it
// whole is also what settles what is no longer in it, which is why the same pass
// answers a feed that could not say what changed.
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
		// The cursor is taken before the calendar is read, so an event created
		// while it is being read is caught by the first sync rather than missed.
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
			// A calendar that was not read is not one this index can answer for, and
			// the cursor goes with it: keeping it would have the next catch-up carry
			// on from a moment nothing was ever read at.
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

// indexCalendar reads one calendar whole.
//
// A calendar whose key will not open is read anyway. Proton keeps the times of
// an event beside it in the clear, so what goes in is an event on the right day
// that says nothing it cannot read - which is what a listing shows for one too,
// and is the difference between a calendar that is missing from a search and one
// that answers as much of itself as this account can see.
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
	// The calendar was read whole, so an indexed event it did not carry is one
	// the calendar no longer has.
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

// read is one event as the index will hold it, decrypted when there is a key to
// decrypt it with.
func (x *indexSession) read(ctx context.Context, ck *calKeys, raw rawEvent) stored {
	if ck == nil {
		return stored{raw: raw, readErr: errUnreadable}
	}
	return x.s.decrypt(ctx, ck, raw)
}

// calendarKeys reaches a calendar's keys the first time an event needs them,
// and is nil when they will not open for this account.
//
// A catch-up that finds nothing changed asks for no keys at all, which is what
// most of them find.
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

// eventsOf is what the index holds for one calendar.
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

// forgetCalendars takes out what belonged to a calendar the account no longer
// has. Nothing in the feed says a calendar was removed - the calendar's feed is
// what would have said it.
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

// Sync applies each calendar's change feed.
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
			// A calendar made since the build has nothing indexed, so there is
			// nothing to catch up on: it goes in whole the next time one is built.
			log.State.Complete = false
			continue
		}
		applied, err := x.syncCalendar(ctx, cal.ID, cursor)
		done = search.Total(done, applied)
		if err != nil {
			// Recorded and counted: a calendar that did not catch up is one whose
			// events the index may answer with as they were, so the answer is
			// short of whatever changed in it.
			skip.Record(ctx, skip.KindCalendar, cal.ID, skip.Unreadable, err)
		}
	}
	return done, x.save(ctx)
}

// syncCalendar follows one calendar's feed from where the index left off.
//
// A calendar whose key will not open is followed anyway, by the frame Proton
// keeps in the clear beside each event: it is what reading the calendar whole
// writes down, and the difference between a calendar that stops at the moment
// it was sealed and one that stays current.
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
			if e.Action == eventDeleted || e.Event == nil {
				records = append(records, search.Record{ID: e.ID, Gone: true})
				done.Removed++
				continue
			}
			rec, readable, err := record(x.read(ctx, keys(), *e.Event))
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
		// The cursor moves once the page is in the index, so a page that could not
		// be applied is asked for again rather than skipped - by the next run, or by
		// the next poll of a watch that keeps this session open.
		cursor = batch.CalendarModelEventID
		log.State.Cursors[calendarID] = cursor
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
	// What the catch-up found out about the index counts as much as what the file
	// said before it: one that has just been told it owes a reading of the
	// calendars answers for nothing.
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

// syncBeforeRead brings the index up to date before it is read. A directory
// another run is writing to is left alone.
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
