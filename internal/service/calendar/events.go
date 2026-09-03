package calendar

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	pgp "github.com/ProtonMail/gopenpgp/v2/crypto"
	pgphelper "github.com/roman-16/proton-cli/internal/crypto/pgp"
	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/roman-16/proton-cli/internal/fetch"
	"github.com/roman-16/proton-cli/internal/ical"
	"github.com/roman-16/proton-cli/internal/proton"
	"github.com/roman-16/proton-cli/internal/ref"
	"github.com/roman-16/proton-cli/internal/units"
)

// Event is one thing on a calendar: a one-off event, or one occurrence of a
// recurring series.
//
// A series is stored once and shown many times, so ID names the event a
// reference addresses - the series itself for every occurrence of it - while
// StoredID names the record actually holding this row's content. They differ
// exactly when an occurrence has been edited on its own.
type Event struct {
	ID          string    `json:"id"`
	StoredID    string    `json:"stored_id,omitempty"`
	CalendarID  string    `json:"calendar_id"`
	Title       string    `json:"title"`
	Location    string    `json:"location,omitempty"`
	Description string    `json:"description,omitempty"`
	Start       time.Time `json:"start"`
	End         time.Time `json:"end"`
	AllDay      bool      `json:"all_day"`
	Zone        string    `json:"zone,omitempty"`
	UID         string    `json:"uid,omitempty"`
	RRule       string    `json:"rrule,omitempty"`
	// Occurrence is this instance's original start in the series' own frame, and
	// the half of a reference that names it. It is empty for a one-off event and
	// for a series addressed as a whole.
	Occurrence string `json:"occurrence,omitempty"`
	// Number is this instance's position in the series.
	Number int `json:"occurrence_number,omitempty"`
	// Count is how many occurrences a series has, set only when the series itself
	// is being reported rather than one of its instances. It is null for a series
	// that never ends, because such a series has no number of occurrences to
	// report rather than a very large one.
	Count *int `json:"occurrence_count,omitempty"`
	// Reminders are the triggers before the start, e.g. "-PT15M". Absent means the
	// event takes the calendar's defaults.
	Reminders []string `json:"reminders"`
	// Attendees are the participants, spelled the way --attendee accepts them, so
	// an event that has been read can be described back.
	Attendees []string `json:"attendees"`
	// Status is whether the event is going ahead. It is always reported, since
	// an event nobody said anything about is confirmed and that is worth stating.
	Status string `json:"status"`
	// Organizer is whoever called the meeting. It is empty for an event with no
	// participants, which has nobody to organise.
	Organizer string                 `json:"organizer,omitempty"`
	Signature pgphelper.VerifyResult `json:"signature,omitempty"`
}

// Recurring reports whether this row belongs to a series.
func (e Event) Recurring() bool { return e.RRule != "" || e.Occurrence != "" }

// ── reading ──

// eventsPageSize is the largest page the events endpoint serves.
const eventsPageSize = 100

// queryTypes are the four windows the events endpoint can be asked for.
//
// Type is not a kind of event, it is a two-by-two selector: part-day or full-day,
// crossed with starting inside the window or having started before it and
// reaching in. Asking only for the first hides every all-day event, and hides
// every recurring series whose first occurrence is in the past - which is how a
// series reaches a later window at all.
var queryTypes = []string{"0", "1", "2", "3"}

type rawNotification struct {
	Type    int
	Trigger string
}

type rawEvent struct {
	ID         string
	CalendarID string
	// StartTime and EndTime are the cleartext times Proton keeps beside the
	// encrypted content. For a full-day event they are the dates it names, held as
	// UTC midnights; for any other event they are instants anchored to the zones
	// below. They are what places an event nobody can decrypt.
	StartTime            int64
	EndTime              int64
	StartTimezone        string
	EndTimezone          string
	FullDay              int
	UID                  string
	IsOrganizer          int
	IsProtonProtonInvite int
	AddressID            string
	Color                *string
	Notifications        []rawNotification
	AttendeesInfo        struct {
		Attendees     []rawAttendee
		MoreAttendees int
	}
	SharedKeyPacket  string
	AddressKeyPacket string
	SharedEvents     []map[string]any
	AttendeesEvents  []map[string]any
}

// rawAttendee is the cleartext per-attendee record from AttendeesInfo: it maps
// the deterministic X-PM token to the server attendee ID and current status.
type rawAttendee struct {
	ID     string
	Token  string
	Status int
}

// notifications re-renders the reminder list for a write, preserving the
// difference between "none" and "the calendar's defaults".
func (e rawEvent) notifications() []map[string]any {
	if e.Notifications == nil {
		return nil
	}
	out := make([]map[string]any, 0, len(e.Notifications))
	for _, n := range e.Notifications {
		out = append(out, map[string]any{"Type": n.Type, "Trigger": n.Trigger})
	}
	return out
}

// triggers renders an event's reminders the way --remind spells them, so what a
// listing shows is what would recreate it.
func (e rawEvent) triggers() []string {
	out := make([]string, 0, len(e.Notifications))
	for _, n := range e.Notifications {
		out = append(out, ReminderText(n.Type, n.Trigger))
	}
	return out
}

// stored is an event as Proton holds it, together with what its cards said.
type stored struct {
	raw   rawEvent
	model ical.VEvent
	sig   pgphelper.VerifyResult
	// readErr records that the content could not be read. Such an event is still
	// reported, from its cleartext times, because a row you cannot read is worth
	// seeing; it is never expanded or written back.
	readErr error
}

// decrypt resolves which key the event's content is wrapped to and reads it.
//
// The attendee list is read separately and best-effort. It lives in its own card,
// it is not part of what the event says, and an event that arrived without one this
// key can open is still an event: folding it into the same read would make a
// missing participant list look like an unreadable event.
func (s *Service) decrypt(ctx context.Context, ck *calKeys, raw rawEvent) stored {
	// An invitation you received wraps its content to the invited address rather
	// than to the calendar key.
	packet, decKR := raw.SharedKeyPacket, ck.calKR
	if packet == "" && raw.AddressKeyPacket != "" {
		if kr, ok := s.addressKeyRing(ctx, raw.AddressID); ok {
			packet, decKR = raw.AddressKeyPacket, kr
		}
	}
	model, sig, err := decryptEvent(raw.SharedEvents, packet, decKR, ck.addrKR)
	if err == nil && len(raw.AttendeesEvents) > 0 {
		if attendees, _, aerr := decryptEvent(raw.AttendeesEvents, packet, decKR, ck.addrKR); aerr == nil {
			model.Attendees = attendees.Attendees
		}
	}
	return stored{raw: raw, model: model, sig: sig, readErr: err}
}

// EventsList returns everything the window covers on the given calendars,
// expanding each series into the occurrences that fall in it.
//
// A calendar that cannot be read is left out rather than allowed to empty the
// answer: the list of calendars is eventually consistent, so one that was deleted
// a moment ago can still be named, and "what is on my calendars" is worth
// answering from the ones that are there. Only when nothing could be read at all
// is that reported - which is also what makes a single named calendar strict,
// since then the one failure is the only one.
func (s *Service) EventsList(ctx context.Context, calendarIDs []string, w ical.Window) ([]Event, error) {
	var (
		mu    sync.Mutex
		wg    sync.WaitGroup
		out   []Event
		first error
		read  int
	)
	for _, calID := range calendarIDs {
		wg.Add(1)
		go func(calID string) {
			defer wg.Done()
			events, err := s.calendarEvents(ctx, calID, w)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if first == nil {
					first = err
				}
				slog.Debug("calendar: skipped a calendar that could not be read",
					"calendar", calID, "error", err)
				return
			}
			read++
			out = append(out, events...)
		}(calID)
	}
	wg.Wait()
	if read == 0 && first != nil {
		return nil, first
	}
	slices.SortStableFunc(out, func(a, b Event) int {
		if c := a.Start.Compare(b.Start); c != 0 {
			return c
		}
		return strings.Compare(a.Title, b.Title)
	})
	return out, nil
}

func (s *Service) calendarEvents(ctx context.Context, calendarID string, w ical.Window) ([]Event, error) {
	ck, err := s.unlockCalendar(ctx, calendarID)
	if err != nil {
		return nil, err
	}
	raws, err := s.rawEventsBetween(ctx, calendarID, w)
	if err != nil {
		return nil, err
	}
	events := make([]stored, 0, len(raws))
	for _, raw := range raws {
		events = append(events, s.decrypt(ctx, ck, raw))
	}
	return expand(events, w), nil
}

// rawEventsBetween asks for all four windows and pages each one.
//
// The four are independent queries over the same range, so they run together:
// serialising them would quadruple the wall-clock of a call that is already
// waiting on the network. An event can legitimately answer more than one of them,
// so the union is deduplicated.
func (s *Service) rawEventsBetween(ctx context.Context, calendarID string, w ical.Window) ([]rawEvent, error) {
	from, to := fetchBounds(w)
	byType := make([][]rawEvent, len(queryTypes))
	queries := make([]func(context.Context) error, len(queryTypes))
	for i, typ := range queryTypes {
		queries[i] = func(ctx context.Context) error {
			page, err := s.rawEventsOfType(ctx, calendarID, from, to, typ)
			byType[i] = page
			return err
		}
	}
	if err := fetch.Together(ctx, queries...); err != nil {
		return nil, err
	}

	var out []rawEvent
	seen := map[string]bool{}
	for _, page := range byType {
		for _, e := range page {
			if seen[e.ID] {
				continue
			}
			seen[e.ID] = true
			out = append(out, e)
		}
	}
	return out, nil
}

// fetchBounds are the instants the events endpoint is asked for: the window, a day
// wider at each end.
//
// Wider on purpose. An all-day event names a date rather than an instant, so Proton
// holds it at an instant up to a day from the day it belongs to here, and the
// endpoint's own idea of which events touch the edge of a range is not this CLI's.
// What is fetched only has to contain the answer; the window decides it.
func fetchBounds(w ical.Window) (from, to time.Time) {
	first, until := w.Bounds()
	return first.AddDate(0, 0, -1), until.AddDate(0, 0, 1)
}

func (s *Service) rawEventsOfType(ctx context.Context, calendarID string, from, to time.Time, typ string) ([]rawEvent, error) {
	var out []rawEvent
	for page := 0; ; page++ {
		q := url.Values{}
		q.Set("Start", fmt.Sprintf("%d", max(from.Unix(), 0)))
		q.Set("End", fmt.Sprintf("%d", max(to.Unix(), 0)))
		// UTC, because that is the frame in which a full-day event's cleartext times
		// are the dates it names.
		q.Set("Timezone", "UTC")
		q.Set("Type", typ)
		q.Set("Page", fmt.Sprintf("%d", page))
		q.Set("PageSize", fmt.Sprintf("%d", eventsPageSize))

		var r struct {
			Events []rawEvent
			More   int
		}
		if err := s.C.Decode(ctx, proton.Request{
			Method: "GET", Path: "/calendar/v1/" + calendarID + "/events", Query: q,
		}, &r); err != nil {
			return nil, err
		}
		out = append(out, r.Events...)
		if r.More != 1 || len(r.Events) == 0 {
			return out, nil
		}
	}
}

// expand turns stored events into the rows a person sees: a one-off is itself, a
// series becomes its occurrences in the window, and an occurrence that has been
// edited on its own replaces the one the rule would have generated.
//
// Every row is put to the window by the same rule, whether it came from a stored
// event or from a rule this CLI expanded. The endpoint is asked for more than the
// window holds, so the window is what makes the answer the one that was asked for
// rather than the one the server happened to return.
func expand(events []stored, w ical.Window) []Event {
	masters := map[string]stored{}
	overrides := map[string][]stored{}
	var plain []stored
	for _, e := range events {
		switch {
		case e.readErr != nil:
			plain = append(plain, e)
		case e.model.IsOverride():
			overrides[e.raw.UID] = append(overrides[e.raw.UID], e)
		case e.model.Recurring():
			masters[e.raw.UID] = e
		default:
			plain = append(plain, e)
		}
	}

	var out []Event
	for _, e := range plain {
		if w.Covers(e.when()) {
			out = append(out, e.row())
		}
	}

	for uid, master := range masters {
		replaced := make([]ical.DateTime, 0, len(overrides[uid]))
		for _, o := range overrides[uid] {
			replaced = append(replaced, *o.model.RecurrenceID)
		}
		occurrences, err := master.model.Occurrences(w)
		if err != nil {
			// A rule this build cannot read still describes a real event, so the
			// series is reported at its own start rather than dropped.
			out = append(out, master.row())
			continue
		}
		for _, occ := range occurrences {
			if slices.ContainsFunc(replaced, occ.Start.Equal) {
				continue
			}
			out = append(out, master.occurrenceRow(occ))
		}
	}

	for uid, list := range overrides {
		master, hasMaster := masters[uid]
		for _, o := range list {
			if !w.Covers(o.when()) {
				continue
			}
			row := o.row()
			if hasMaster {
				// An occurrence is addressed by where it sits in its series, not by
				// which record happens to hold it, so that the reference does not
				// change the first time somebody edits it.
				row.ID = master.raw.ID
				row.StoredID = o.raw.ID
				row.Occurrence = master.model.Start.At(o.model.RecurrenceID.Time).String()
				row.RRule = master.model.RRule
			} else {
				row.Occurrence = o.model.RecurrenceID.String()
			}
			out = append(out, row)
		}
	}
	return out
}

// when is the pair of values the event occupies, whether or not its content could
// be read.
//
// An event nobody can decrypt is still placed on the right day, because the times
// Proton keeps in the clear beside it are the same values its content carries.
func (e stored) when() (start, end ical.DateTime) {
	if e.readErr == nil {
		return e.model.Span()
	}
	if e.raw.FullDay == 1 {
		return ical.Span(
			ical.Day(time.Unix(e.raw.StartTime, 0).UTC()),
			ical.Day(time.Unix(e.raw.EndTime, 0).UTC()))
	}
	return ical.Span(
		ical.Timed(time.Unix(e.raw.StartTime, 0), e.raw.StartTimezone),
		ical.Timed(time.Unix(e.raw.EndTime, 0), e.raw.EndTimezone))
}

// row reports the stored event as itself.
//
// The times are read in the zone the reader is in, which for an all-day event is
// the only way to name the day it is on: it carries a date and no instant, so
// placing it anywhere else moves it to the day before or after.
func (e stored) row() Event {
	start, end := e.when()
	ev := Event{
		ID:         e.raw.ID,
		CalendarID: e.raw.CalendarID,
		UID:        e.raw.UID,
		Start:      start.In(time.Local),
		End:        end.In(time.Local),
		AllDay:     start.AllDay,
		Zone:       start.TZID,
		Signature:  e.sig,
		Reminders:  e.raw.triggers(),
	}
	if e.readErr != nil {
		return ev
	}
	ev.Title = e.model.Summary
	ev.Location = e.model.Location
	ev.Description = e.model.Description
	ev.RRule = e.model.RRule
	ev.Organizer = e.model.Organizer
	ev.Status = StatusText(e.model.Status)
	for _, a := range e.model.Attendees {
		ev.Attendees = append(ev.Attendees, AttendeeText(a.Email, a.Role))
	}
	return ev
}

// occurrenceRow reports one instance of a series.
func (e stored) occurrenceRow(occ ical.Occurrence) Event {
	ev := e.row()
	ev.Start = occ.Start.In(time.Local)
	ev.End = occ.End.In(time.Local)
	ev.Occurrence = occ.Start.String()
	ev.Number = occ.Number
	return ev
}

// EventGet reports one event. An empty occurrence names the stored event, which
// for a series is the series itself; otherwise it names one instance.
//
// The event and the keys that read it are asked for at the same time. The
// reference names the event, so neither request needs the other's answer - only
// the decryption between them does.
func (s *Service) EventGet(ctx context.Context, calendarID, eventID, occurrence string) (*Event, error) {
	var (
		ck  *calKeys
		raw rawEvent
	)
	if err := fetch.Together(ctx,
		func(ctx context.Context) error {
			var err error
			ck, err = s.unlockCalendar(ctx, calendarID)
			return err
		},
		func(ctx context.Context) error {
			var err error
			raw, err = s.rawEvent(ctx, calendarID, eventID)
			return err
		},
	); err != nil {
		return nil, err
	}
	e := s.decrypt(ctx, ck, raw)
	if occurrence == "" {
		ev := e.row()
		if e.readErr == nil && e.model.Recurring() {
			ev.Count, _ = e.model.CountOccurrences()
		}
		return &ev, nil
	}
	if e.readErr != nil {
		return nil, e.readErr
	}
	occ, err := s.resolveOccurrence(ctx, ck, calendarID, e, occurrence)
	if err != nil {
		return nil, err
	}
	ev := occ.row()
	return &ev, nil
}

// Reach is what a change to a stored event actually touches.
//
// A series is one event to address and many meetings to lose, so the two counts
// are kept apart: Rows is the sample a person reads to recognise what they are
// about to change, and Total is how many there are - nil when the series never
// ends, which is a thing to be told rather than a number to be given.
type Reach struct {
	Rows  []Event
	Total *int
}

// Series reports what a stored event reaches, drawing at most sample of its
// occurrences.
//
// It is what a preview and a confirmation both rest on: a count alone cannot be
// checked, and the occurrences are the things that would change.
func (s *Service) Series(ctx context.Context, calendarID, eventID string, sample int) (*Reach, error) {
	ck, err := s.unlockCalendar(ctx, calendarID)
	if err != nil {
		return nil, err
	}
	master, err := s.readMaster(ctx, ck, calendarID, eventID)
	if err != nil {
		return nil, err
	}
	if !master.model.Recurring() {
		one := 1
		return &Reach{Rows: []Event{master.row()}, Total: &one}, nil
	}
	chain, err := s.loadSeries(ctx, ck, calendarID, master)
	if err != nil {
		return nil, err
	}
	total, err := master.model.CountOccurrences()
	if err != nil {
		return nil, err
	}
	var out []Event
	if err := master.model.Walk(func(occ ical.Occurrence) bool {
		if override := chain.overrideAt(occ.Start); override != nil {
			row := override.row()
			row.ID = master.raw.ID
			row.StoredID = override.raw.ID
			row.Occurrence = occ.Start.String()
			row.Number = occ.Number
			row.RRule = master.model.RRule
			out = append(out, row)
		} else {
			out = append(out, master.occurrenceRow(occ))
		}
		return len(out) < sample
	}); err != nil {
		return nil, err
	}
	return &Reach{Rows: out, Total: total}, nil
}

func (s *Service) rawEvent(ctx context.Context, calendarID, eventID string) (rawEvent, error) {
	var r struct{ Event rawEvent }
	if err := s.C.Decode(ctx, proton.Request{
		Method: "GET", Path: "/calendar/v1/" + calendarID + "/events/" + eventID,
	}, &r); err != nil {
		return rawEvent{}, err
	}
	return r.Event, nil
}

func (s *Service) storedEvent(ctx context.Context, ck *calKeys, calendarID, eventID string) (stored, error) {
	raw, err := s.rawEvent(ctx, calendarID, eventID)
	if err != nil {
		return stored{}, err
	}
	return s.decrypt(ctx, ck, raw), nil
}

// ── creating ──

// EventInput is the full set of fields for a new event.
type EventInput struct {
	Title       string
	Location    string
	Description string
	Start, End  time.Time
	AllDay      bool
	// Zone anchors the event, so a recurring series keeps its wall-clock time
	// across a daylight-saving change instead of drifting by an hour.
	Zone      string
	RRule     string
	Reminders []string
	Attendees []string
	// Status is whether the event is going ahead: CONFIRMED, TENTATIVE or
	// CANCELLED. Empty means it never said, which every client reads as
	// confirmed.
	Status string
}

// EventResult is the outcome of a write. Mail is non-nil when participants have
// to be told by email, which is the case for attendees Proton cannot reach
// through their own calendar.
type EventResult struct {
	ID   string
	Ref  string
	Mail *Mail
}

// Mail is a ready-to-send iCalendar message.
type Mail struct {
	ICS        string
	Recipients []string
	Subject    string
	Body       string
	Method     string
}

func (s *Service) EventCreate(ctx context.Context, calendarID string, in EventInput) (*EventResult, error) {
	ck, err := s.unlockCalendar(ctx, calendarID)
	if err != nil {
		return nil, err
	}
	notifs, err := buildReminders(in.Reminders)
	if err != nil {
		return nil, err
	}
	v := ical.VEvent{
		UID:         ical.EventUID(),
		Summary:     in.Title,
		Location:    in.Location,
		Description: in.Description,
		Status:      in.Status,
		RRule:       in.RRule,
	}
	loc, err := zoneOf(in.Zone)
	if err != nil {
		return nil, err
	}
	v = withTimes(v, in.Start, in.End, in.AllDay, loc.String())
	if len(in.Attendees) > 0 {
		v.Organizer = ck.email
	}

	body := eventBody{model: v, notifications: notifs, isOrganizer: 1}
	external, err := s.attachAttendees(ctx, &body, in.Attendees)
	if err != nil {
		return nil, err
	}
	op, _, err := ck.createOp(body)
	if err != nil {
		return nil, err
	}
	created, err := s.sync(ctx, calendarID, ck.memberID, []syncOp{op})
	if err != nil {
		return nil, err
	}
	res := &EventResult{}
	if len(created) > 0 {
		res.ID = created[0]
	}
	if len(external) > 0 {
		res.Mail = inviteMail(body.model, external)
	}
	return res, nil
}

// attachAttendees resolves the participants onto an event: their tokens, the
// cleartext record, the keys their copies have to be readable with, and the
// addresses Proton cannot deliver to, which have to be emailed instead.
func (s *Service) attachAttendees(ctx context.Context, body *eventBody, emails []string) (external []string, err error) {
	if len(emails) == 0 {
		return nil, nil
	}
	atts, clear, keys, external, err := s.resolveAttendees(ctx, body.model.UID, emails)
	if err != nil {
		return nil, err
	}
	body.model.Attendees = atts
	body.attendeeList = clear
	body.attendeeKeys = keys
	return external, nil
}

func inviteMail(v ical.VEvent, recipients []string) *Mail {
	return &Mail{
		ICS:        v.Document("REQUEST"),
		Recipients: recipients,
		Subject:    "Invitation: " + v.Summary,
		Body: fmt.Sprintf("You have been invited to %q.\n\nThe calendar invitation is attached.",
			v.Summary),
		Method: "REQUEST",
	}
}

func updateMail(v ical.VEvent, recipients []string) *Mail {
	return &Mail{
		ICS:        v.Document("REQUEST"),
		Recipients: recipients,
		Subject:    "Updated invitation: " + v.Summary,
		Body: fmt.Sprintf("%q has been updated.\n\nThe updated calendar invitation is attached.",
			v.Summary),
		Method: "REQUEST",
	}
}

// resolveAttendees computes per-attendee tokens, the cleartext Attendees list, the
// keys of the participants with Proton accounts, and the addresses without one,
// which have to be emailed.
//
// None of it needs the event's session key, which is what lets the key be made
// once, when the cards are built.
func (s *Service) resolveAttendees(ctx context.Context, uid string, emails []string) (atts []ical.Attendee, clear []map[string]any, keys []protonAttendee, external []string, err error) {
	written := make([]string, 0, len(emails))
	roles := make(map[string]string, len(emails))
	for _, raw := range emails {
		e, role, err := attendeeRole(raw)
		if err != nil {
			return nil, nil, nil, nil, err
		}
		if e == "" {
			continue
		}
		written = append(written, e)
		roles[e] = role
	}
	canonical, err := s.canonicalEmails(ctx, written)
	if err != nil {
		return nil, nil, nil, nil, err
	}

	seen := map[canonicalAddr]bool{}
	for _, email := range written {
		// Two addresses Proton reduces to the same one are the same person, so
		// the canonical form is what decides a duplicate.
		if seen[canonical[email]] {
			continue
		}
		seen[canonical[email]] = true
		token := attendeeToken(uid, canonical[email])
		atts = append(atts, ical.Attendee{Email: email, Token: token, Role: roles[email]})
		clear = append(clear, map[string]any{"Token": token, "Status": 0})

		kr, err := s.attendeeKeyRing(ctx, email)
		if err != nil {
			return nil, nil, nil, nil, err
		}
		if kr == nil {
			external = append(external, email)
			continue
		}
		keys = append(keys, protonAttendee{email: email, kr: kr})
	}
	return atts, clear, keys, external, nil
}

// attendeeKeyRing is an address's public key, or nil when the address has no
// Proton account and therefore no calendar to deliver to.
func (s *Service) attendeeKeyRing(ctx context.Context, email string) (*pgp.KeyRing, error) {
	var r struct {
		Address struct {
			Keys []struct{ PublicKey string }
		}
	}
	if err := s.C.Decode(ctx, proton.Request{
		Method: "GET", Path: "/core/v4/keys/all", Query: proton.Query("Email", email),
	}, &r); err != nil {
		return nil, err
	}
	if len(r.Address.Keys) == 0 {
		return nil, nil
	}
	key, err := pgp.NewKeyFromArmored(r.Address.Keys[0].PublicKey)
	if err != nil {
		if pgphelper.PostQuantum(r.Address.Keys[0].PublicKey) {
			return nil, pgphelper.NotSupported(email)
		}
		return nil, fmt.Errorf("parse the key for %s: %w", email, err)
	}
	return pgp.NewKeyRing(key)
}

// ── resolving ──

// ResolveEvent finds an event by title across every calendar over the days the
// default window covers.
func (s *Service) ResolveEvent(ctx context.Context, needle string) (calendarID, eventID, occurrence string, err error) {
	cals, err := s.CalendarsList(ctx)
	if err != nil {
		return "", "", "", err
	}
	ids := make([]string, 0, len(cals))
	for _, c := range cals {
		ids = append(ids, c.ID)
	}
	events, err := s.EventsList(ctx, ids, ical.Days(DefaultDays()))
	if err != nil {
		return "", "", "", err
	}
	var matches []Event
	for _, e := range events {
		if e.Title != "" && strings.Contains(strings.ToLower(e.Title), strings.ToLower(needle)) {
			matches = append(matches, e)
		}
	}
	m, err := ref.Pick("event", needle, matches,
		func(e Event) string { return e.ID },
		func(e Event) string {
			return fmt.Sprintf("%s  %s  (calendar %s)", e.Start.Local().Format("2006-01-02 15:04"), e.Title, e.CalendarID)
		})
	if err != nil {
		return "", "", "", err
	}
	return m.CalendarID, m.ID, m.Occurrence, nil
}

// ── reminders ──

// Proton's two kinds of reminder. The web client offers both wherever it offers
// one, so a reminder here says which it is by suffixing the duration - "1d:email"
// - and a bare duration is the notification, which is what the composer defaults
// to.
const (
	remindByEmail  = 0
	remindOnDevice = 1
)

func buildReminders(reminders []string) ([]map[string]any, error) {
	if reminders == nil {
		return nil, nil
	}
	out := make([]map[string]any, 0, len(reminders))
	for _, r := range reminders {
		spec, kind, err := reminderKind(r)
		if err != nil {
			return nil, err
		}
		trig, err := icalTrigger(spec)
		if err != nil {
			return nil, errs.Problemf("--remind %q is not a warning time: %v.", r, err).
				Hint("a reminder is a duration before the start, like 15m, 1h or 1d")
		}
		out = append(out, map[string]any{"Type": kind, "Trigger": trig})
	}
	return out, nil
}

// reminderKind splits "15m" or "1d:email" into the duration and how to deliver
// it.
func reminderKind(r string) (spec string, kind int, err error) {
	spec, suffix, found := strings.Cut(r, ":")
	if !found {
		return spec, remindOnDevice, nil
	}
	switch strings.ToLower(suffix) {
	case "email":
		return spec, remindByEmail, nil
	case "notification":
		return spec, remindOnDevice, nil
	}
	return "", 0, fmt.Errorf("reminder %q: after the colon write email or notification", r)
}

// iCalendar's two participation roles. Proton's composer lets an organiser mark
// anyone optional, so an attendee says which by suffixing the address -
// "jane@example.com:optional" - and a bare address is required, which is what
// inviting someone ordinarily means.
const (
	roleRequired = "REQ-PARTICIPANT"
	roleOptional = "OPT-PARTICIPANT"
)

// attendeeRole splits "jane@example.com" or "jane@example.com:optional" into the
// address and how much their presence matters.
//
// The address is split on the last colon, not the first, so that a scheme or a
// port somebody pasted in does not eat the address.
func attendeeRole(raw string) (email, role string, err error) {
	email = strings.TrimSpace(raw)
	i := strings.LastIndex(email, ":")
	if i < 0 {
		return email, roleRequired, nil
	}
	suffix := strings.ToLower(strings.TrimSpace(email[i+1:]))
	email = strings.TrimSpace(email[:i])
	switch suffix {
	case "optional":
		return email, roleOptional, nil
	case "required":
		return email, roleRequired, nil
	}
	return "", "", fmt.Errorf("attendee %q: after the colon write required or optional", raw)
}

// AttendeeText renders a participant the way --attendee spells them, so a
// listing can be read back into a command.
func AttendeeText(email, role string) string {
	if role == roleOptional {
		return email + ":optional"
	}
	return email
}

// Event statuses, as iCalendar spells them and as the CLI does.
var eventStatuses = map[string]string{
	"confirmed": "CONFIRMED", "tentative": "TENTATIVE", "cancelled": "CANCELLED",
}

// EventStatuses lists the words --status accepts, for the flag's declared domain.
func EventStatuses() []string { return []string{"confirmed", "tentative", "cancelled"} }

// ICalStatus turns the word a person writes into the one iCalendar stores.
func ICalStatus(word string) string { return eventStatuses[strings.ToLower(word)] }

// StatusText is the inverse, and answers "confirmed" for an event that never
// said: an absent STATUS is what every client reads as going ahead.
func StatusText(status string) string {
	for word, ical := range eventStatuses {
		if ical == strings.ToUpper(status) {
			return word
		}
	}
	return "confirmed"
}

// ReminderText renders a stored reminder the way it is written on the command
// line, so what a listing shows can be passed straight back to --remind.
func ReminderText(kind int, trigger string) string {
	spec := units.Duration(triggerDuration(trigger))
	if kind == remindByEmail {
		return spec + ":email"
	}
	return spec
}

// icalTrigger converts a duration-before-start ("15m", "1h", "1d") into an iCal
// negative trigger ("-PT15M", "-PT60M", "-P1D").
func icalTrigger(dur string) (string, error) {
	d, err := units.ParseDuration(dur)
	if err != nil {
		return "", err
	}
	if d <= 0 {
		return "", fmt.Errorf("must be positive")
	}
	if d%(24*time.Hour) == 0 {
		return fmt.Sprintf("-P%dD", int(d/(24*time.Hour))), nil
	}
	return fmt.Sprintf("-PT%dM", int(d/time.Minute)), nil
}

// triggerDuration reads an iCalendar trigger back into how long before the start
// it fires. It is icalTrigger's inverse, over the shapes icalTrigger writes and
// the ones Proton's other clients write beside them.
func triggerDuration(trigger string) time.Duration {
	body := strings.TrimPrefix(strings.TrimPrefix(trigger, "-"), "P")
	date, clock, _ := strings.Cut(body, "T")
	var total time.Duration
	for _, part := range []struct {
		in    string
		units map[byte]time.Duration
	}{
		{date, map[byte]time.Duration{'W': 7 * 24 * time.Hour, 'D': 24 * time.Hour}},
		{clock, map[byte]time.Duration{'H': time.Hour, 'M': time.Minute, 'S': time.Second}},
	} {
		n := 0
		for i := 0; i < len(part.in); i++ {
			if ch := part.in[i]; ch >= '0' && ch <= '9' {
				n = n*10 + int(ch-'0')
				continue
			}
			if unit, ok := part.units[part.in[i]]; ok {
				total += time.Duration(n) * unit
			}
			n = 0
		}
	}
	return total
}

// ── exporting ──

// EventsExport returns what a calendar holds as iCalendar models, ready to be
// written as a file.
//
// A series is exported **once**, carrying its rule, rather than expanded into the
// occurrences a listing shows. That is what a calendar file is: expanding it
// would turn one weekly standup into fifty-two unrelated events, and no client
// could put it back.
//
// An event that cannot be decrypted is left out rather than written as a stub,
// because a file is something another client will trust.
func (s *Service) EventsExport(ctx context.Context, calendarIDs []string, w ical.Window) ([]ical.VEvent, error) {
	var out []ical.VEvent
	var first error
	read := 0
	for _, calID := range calendarIDs {
		events, err := s.calendarExport(ctx, calID, w)
		if err != nil {
			if first == nil {
				first = err
			}
			slog.Debug("calendar: skipped a calendar that could not be exported",
				"calendar", calID, "error", err)
			continue
		}
		read++
		out = append(out, events...)
	}
	if read == 0 && first != nil {
		return nil, first
	}
	slices.SortStableFunc(out, func(a, b ical.VEvent) int {
		return a.Start.Time.Compare(b.Start.Time)
	})
	return out, nil
}

func (s *Service) calendarExport(ctx context.Context, calendarID string, w ical.Window) ([]ical.VEvent, error) {
	ck, err := s.unlockCalendar(ctx, calendarID)
	if err != nil {
		return nil, err
	}
	raws, err := s.rawEventsBetween(ctx, calendarID, w)
	if err != nil {
		return nil, err
	}
	out := make([]ical.VEvent, 0, len(raws))
	for _, raw := range raws {
		e := s.decrypt(ctx, ck, raw)
		if e.readErr != nil {
			continue
		}
		v := e.model
		v.Alarms = alarmsOf(raw)
		out = append(out, v)
	}
	return out, nil
}

// alarmsOf turns Proton's notifications into the VALARM components every other
// client reads.
func alarmsOf(raw rawEvent) []ical.Alarm {
	alarms := make([]ical.Alarm, 0, len(raw.Notifications))
	for _, n := range raw.Notifications {
		action := "DISPLAY"
		if n.Type == remindByEmail {
			action = "EMAIL"
		}
		alarms = append(alarms, ical.Alarm{Action: action, Trigger: n.Trigger})
	}
	return alarms
}

// ── importing ──

// ImportResult says what an import did, per event, so a partial success reports
// which events landed rather than one number that hides the rest.
type ImportResult struct {
	Imported []string       `json:"imported"`
	Skipped  []SkippedEvent `json:"skipped"`
}

// SkippedEvent is one event an import could not take, and why.
type SkippedEvent struct {
	Summary string `json:"summary,omitempty"`
	UID     string `json:"uid,omitempty"`
	Reason  string `json:"reason"`
}

// String names the event, falling back to the UID for one with no summary, since
// an untitled event still has to be findable in the file.
func (s SkippedEvent) String() string {
	switch {
	case s.Summary != "":
		return fmt.Sprintf("Skipped %q: %s.", s.Summary, s.Reason)
	case s.UID != "":
		return fmt.Sprintf("Skipped the event with UID %s: %s.", s.UID, s.Reason)
	default:
		return fmt.Sprintf("Skipped an event: %s.", s.Reason)
	}
}

// EventsImport writes the events of a parsed calendar file into a calendar.
//
// Each event keeps its own UID, so importing the same file twice into the same
// calendar produces duplicates rather than merging - which is what Proton's own
// import does, and the honest behaviour: this cannot know whether two events
// sharing a UID are the same event or a file somebody edited.
//
// Attendees are dropped. An imported event is a record of something, not an
// invitation being reissued, and writing the participants back would make this
// account the organiser of a meeting it did not call - which, for an event with
// external addresses, means email going out.
//
// An event that cannot be written is reported and the rest continue: a file of a
// thousand events should not be lost to one of them.
func (s *Service) EventsImport(ctx context.Context, calendarID string, events []ical.VEvent) (*ImportResult, error) {
	ck, err := s.unlockCalendar(ctx, calendarID)
	if err != nil {
		return nil, err
	}
	res := &ImportResult{}
	for _, v := range events {
		if v.Start.IsZero() {
			res.Skipped = append(res.Skipped, SkippedEvent{
				Summary: v.Summary, UID: v.UID, Reason: "no start time",
			})
			continue
		}
		body := eventBody{
			model:         importable(v),
			notifications: notificationsOf(v.Alarms),
			// The participants and the organiser are stripped on the way in, so
			// what is written is this account's own event rather than a meeting
			// somebody else called - and Proton refuses to store an event in your
			// calendar that claims neither.
			isOrganizer: 1,
		}
		op, err := ck.importOp(body)
		if err != nil {
			res.Skipped = append(res.Skipped, SkippedEvent{
				Summary: v.Summary, UID: v.UID, Reason: err.Error(),
			})
			continue
		}
		created, err := s.syncImported(ctx, calendarID, ck.memberID, []syncOp{op})
		if err != nil {
			res.Skipped = append(res.Skipped, SkippedEvent{
				Summary: v.Summary, UID: v.UID, Reason: err.Error(),
			})
			continue
		}
		if len(created) > 0 {
			res.Imported = append(res.Imported, created[0])
		}
	}
	return res, nil
}

// importable strips what an imported event must not carry: the participants and
// the organiser, since this account did not call the meeting, and a stamp, since
// the event is being written now.
func importable(v ical.VEvent) ical.VEvent {
	v.Attendees = nil
	v.Organizer = ""
	v.Alarms = nil
	v.DTStamp = time.Now().UTC()
	if v.UID == "" {
		v.UID = ical.EventUID()
	}
	return v
}

// notificationsOf turns VALARM components back into Proton's notifications, so a
// file's reminders survive the import.
func notificationsOf(alarms []ical.Alarm) []map[string]any {
	if len(alarms) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(alarms))
	for _, a := range alarms {
		kind := remindOnDevice
		if strings.EqualFold(a.Action, "EMAIL") {
			kind = remindByEmail
		}
		out = append(out, map[string]any{"Type": kind, "Trigger": a.Trigger})
	}
	return out
}
