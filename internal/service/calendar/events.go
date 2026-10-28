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

	pgp "github.com/ProtonMail/gopenpgp/v3/crypto"
	"github.com/roman-16/proton-cli/internal/accent"
	"github.com/roman-16/proton-cli/internal/account/keys"
	pgphelper "github.com/roman-16/proton-cli/internal/crypto/pgp"
	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/roman-16/proton-cli/internal/fetch"
	"github.com/roman-16/proton-cli/internal/ical"
	"github.com/roman-16/proton-cli/internal/proton"
	"github.com/roman-16/proton-cli/internal/ref"
	"github.com/roman-16/proton-cli/internal/service/meet"
	"github.com/roman-16/proton-cli/internal/skip"
	"github.com/roman-16/proton-cli/internal/units"
)

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
	Occurrence  string    `json:"occurrence,omitempty"`
	Number      int       `json:"occurrence_number,omitempty"`
	Count       *int      `json:"occurrence_count,omitempty"`
	Reminders   []string  `json:"reminders"`
	Attendees   []string  `json:"attendees"`
	Status      string    `json:"status"`
	Organizer   string    `json:"organizer,omitempty"`
	// Color is the event's own accent colour, as the hex Proton stores. Empty
	// means it has none of its own and is drawn in its calendar's.
	Color     string                 `json:"color,omitempty"`
	Signature pgphelper.VerifyResult `json:"signature,omitempty"`
}

func (e Event) Recurring() bool { return e.RRule != "" || e.Occurrence != "" }

const eventsPageSize = 100

var queryTypes = []string{"0", "1", "2", "3"}

func fullDay(typ string) bool { return typ == "2" || typ == "3" }

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
	SharedKeyPacket   string
	AddressKeyPacket  string
	CalendarKeyPacket string
	SharedEvents      []map[string]any
	CalendarEvents    []map[string]any
	AttendeesEvents   []map[string]any
}

type rawAttendee struct {
	ID     string
	Token  string
	Status int
}

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

func (e rawEvent) ownColor() string {
	if e.Color == nil {
		return ""
	}
	return *e.Color
}

func (e rawEvent) triggers() []string {
	out := make([]string, 0, len(e.Notifications))
	for _, n := range e.Notifications {
		out = append(out, ReminderText(n.Type, n.Trigger))
	}
	return out
}

// stored is an event as Proton holds it, together with what its cards said.
type stored struct {
	raw     rawEvent
	model   ical.VEvent
	sig     pgphelper.VerifyResult
	readErr error
}

func (s *Service) decrypt(ctx context.Context, ck *calKeys, raw rawEvent) stored {
	packet, decKR := raw.SharedKeyPacket, ck.calKR
	if packet == "" && raw.AddressKeyPacket != "" {
		if rings, ok := s.addressKeyRing(ctx, raw.AddressID); ok {
			packet, decKR = raw.AddressKeyPacket, rings.Read
		}
	}
	model, sig, err := readCards(ck.addr.Read,
		cardSet{raw.SharedEvents, packet, decKR},
		cardSet{raw.CalendarEvents, raw.CalendarKeyPacket, ck.calKR})
	if err == nil && len(raw.AttendeesEvents) > 0 {
		if attendees, _, aerr := decryptEvent(raw.AttendeesEvents, packet, decKR, ck.addr.Read); aerr == nil {
			model.Attendees = attendees.Attendees
		}
	}
	return stored{raw: raw, model: model, sig: sig, readErr: err}
}

type eventFetch func(ctx context.Context, calendarID string) ([]rawEvent, error)

func (s *Service) inWindow(w ical.Window) eventFetch {
	return func(ctx context.Context, calendarID string) ([]rawEvent, error) {
		return s.rawEventsBetween(ctx, calendarID, w)
	}
}

func (s *Service) readCalendars(ctx context.Context, calendarIDs []string, events eventFetch) ([][]stored, error) {
	answers := make([][]stored, len(calendarIDs))
	failures := make([]error, len(calendarIDs))
	var wg sync.WaitGroup
	for i, calendarID := range calendarIDs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			answers[i], failures[i] = s.readCalendar(ctx, calendarID, events)
		}()
	}
	wg.Wait()

	out := make([][]stored, 0, len(calendarIDs))
	var first error
	for i := range calendarIDs {
		if failures[i] != nil {
			if first == nil {
				first = failures[i]
			}
			continue
		}
		out = append(out, answers[i])
	}
	if len(out) == 0 && first != nil {
		return nil, first
	}
	return out, nil
}

func (s *Service) readCalendar(ctx context.Context, calendarID string, events eventFetch) ([]stored, error) {
	var (
		ck      *calKeys
		raws    []rawEvent
		keysErr error
	)
	if err := fetch.Together(ctx,
		func(ctx context.Context) error {
			ck, keysErr = s.unlockCalendar(ctx, calendarID)
			return keysErr
		},
		func(ctx context.Context) error {
			var err error
			raws, err = events(ctx, calendarID)
			return err
		},
	); err != nil {
		reason := skip.Unreadable
		if keysErr != nil {
			reason = skip.Unlockable
		}
		skip.Record(ctx, skip.KindCalendar, calendarID, reason, err)
		return nil, err
	}
	out := make([]stored, 0, len(raws))
	for _, raw := range raws {
		out = append(out, s.decrypt(ctx, ck, raw))
	}
	return out, nil
}

func (s *Service) EventsList(ctx context.Context, calendarIDs []string, w ical.Window) ([]Event, error) {
	groups, err := s.readCalendars(ctx, calendarIDs, s.inWindow(w))
	if err != nil {
		return nil, err
	}
	var out []Event
	for _, events := range groups {
		out = append(out, expand(events, w)...)
	}
	slices.SortStableFunc(out, func(a, b Event) int {
		if c := a.Start.Compare(b.Start); c != 0 {
			return c
		}
		return strings.Compare(a.Title, b.Title)
	})
	return out, nil
}

func (s *Service) rawEventsBetween(ctx context.Context, calendarID string, w ical.Window) ([]rawEvent, error) {
	spans := fetchSpans(w)
	answers := make([][]rawEvent, len(spans)*len(queryTypes))
	queries := make([]func(context.Context) error, 0, len(answers))
	for _, sp := range spans {
		for _, typ := range queryTypes {
			i := len(queries)
			queries = append(queries, func(ctx context.Context) error {
				var err error
				answers[i], err = s.rawEventsOfType(ctx, calendarID, sp.from, sp.to, typ)
				return err
			})
		}
	}
	if err := fetch.Together(ctx, queries...); err != nil {
		return nil, err
	}

	var out []rawEvent
	seen := map[string]bool{}
	for _, answer := range answers {
		for _, e := range answer {
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
// wider at each end, and nothing before the epoch.
//
// Wider on purpose. An all-day event names a date rather than an instant, so Proton
// holds it at an instant up to a day from the day it belongs to here, and the
// endpoint's own idea of which events touch the edge of a range is not this CLI's.
// What is fetched only has to contain the answer; the window decides it.
//
// Nothing before the epoch is asked for; see sinceEpoch.
func fetchBounds(w ical.Window) (from, to time.Time) {
	first, until := w.Bounds()
	return sinceEpoch(first.AddDate(0, 0, -1), until.AddDate(0, 0, 1))
}

func sinceEpoch(from, to time.Time) (time.Time, time.Time) {
	if from.Unix() < 0 {
		from = time.Unix(0, 0)
	}
	if to.Before(from) {
		to = from
	}
	return from, to
}

// fetchSpan is the widest range the events endpoint is asked for at once.
//
// Proton refuses a range it considers too wide, at a width it does not publish;
// three months is already too many. Six weeks is the widest range Proton's own
// calendar asks for - a month view's grid - so it is one the endpoint has to keep
// answering.
const fetchSpan = 42 * 24 * time.Hour

type span struct{ from, to time.Time }

func fetchSpans(w ical.Window) []span {
	from, to := fetchBounds(w)
	return cutSpans(from, to, fetchSpan)
}

func cutSpans(from, to time.Time, width time.Duration) []span {
	var out []span
	for start := from; start.Before(to); start = start.Add(width) {
		end := start.Add(width)
		if end.After(to) {
			end = to
		}
		out = append(out, span{from: start, to: end})
	}
	return out
}

func (s *Service) rawEventsOfType(ctx context.Context, calendarID string, from, to time.Time, typ string) ([]rawEvent, error) {
	return proton.All(ctx, func(ctx context.Context, page int) ([]rawEvent, bool, error) {
		q := url.Values{}
		q.Set("Start", fmt.Sprintf("%d", from.Unix()))
		q.Set("End", fmt.Sprintf("%d", to.Unix()))
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
			return nil, false, err
		}
		return r.Events, r.More == 1 && len(r.Events) > 0, nil
	})
}

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
		Color:      e.raw.ownColor(),
	}
	if e.readErr != nil {
		return ev
	}
	ev.Title = e.model.Summary
	ev.Location = e.model.Location
	ev.Description = e.model.ShownDescription()
	ev.RRule = e.model.RRule
	ev.Organizer = e.model.Organizer
	ev.Status = StatusText(e.model.Status)
	for _, a := range e.model.Attendees {
		ev.Attendees = append(ev.Attendees, AttendeeText(a.Email, a.Role))
	}
	return ev
}

func (e stored) occurrenceRow(occ ical.Occurrence) Event {
	ev := e.row()
	ev.Start = occ.Start.In(time.Local)
	ev.End = occ.End.In(time.Local)
	ev.Occurrence = occ.Start.String()
	ev.Number = occ.Number
	return ev
}

type EventDetail struct {
	Event
	VideoCall *VideoCall `json:"video_call,omitempty"`
}

type VideoCall struct {
	Provider string `json:"provider"`
	URL      string `json:"url"`
}

func videoCall(v ical.VEvent) *VideoCall {
	call, ok := v.Call()
	if !ok {
		return nil
	}
	return &VideoCall{Provider: ProviderName(call.Provider), URL: call.URL}
}

func ProviderName(provider int) string {
	switch provider {
	case ical.ProviderMeet:
		return "proton-meet"
	case ical.ProviderZoom:
		return "zoom"
	}
	return "other"
}

func (s *Service) EventGet(ctx context.Context, calendarID, eventID, occurrence string) (*EventDetail, error) {
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
		ev := EventDetail{Event: e.row()}
		if e.readErr == nil {
			ev.VideoCall = videoCall(e.model)
			if e.model.Recurring() {
				ev.Count, _ = e.model.CountOccurrences()
			}
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
	return &EventDetail{Event: occ.row(), VideoCall: videoCall(occ.currentVEvent())}, nil
}

type Reach struct {
	Rows  []Event
	Total *int
}

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

type EventInput struct {
	Title       string
	Location    string
	Description string
	Start       ical.DateTime
	Duration    time.Duration
	RRule       string
	Reminders   []string
	Attendees   []string
	Status      string
	// Color is the event's own accent colour, as the hex Proton stores. Empty
	// gives it none, which is how an event comes to be drawn in its calendar's.
	Color string
	Meet  bool
}

// EventResult is the outcome of a write. Mail is non-nil when participants have
// to be told by email, which is the case for attendees Proton cannot reach
// through their own calendar.
type EventResult struct {
	ID   string
	Ref  string
	Mail *Mail
	// Call is the Proton Meet call this write gave the event.
	Call          *ical.Call
	MeetUnchanged bool
}

func gaveCall(v ical.VEvent, made *meet.Meeting) *ical.Call {
	if made == nil {
		return nil
	}
	call, _ := v.Call()
	return &call
}

// Mail is a ready-to-send iCalendar message.
type Mail struct {
	ICS        string
	Recipients []string
	Subject    string
	Body       string
	Method     string
}

func (s *Service) EventCreate(ctx context.Context, calendarID string, in EventInput) (res *EventResult, err error) {
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
	v = withTimes(v, in.Start, in.Duration)
	if len(in.Attendees) > 0 {
		v.Organizer = ck.email
	}

	body := eventBody{
		model: v, notifications: notifs, color: optionalColor(in.Color), isOrganizer: 1,
	}
	external, err := s.attachAttendees(ctx, &body, in.Attendees)
	if err != nil {
		return nil, err
	}
	res = &EventResult{}
	if in.Meet {
		made, conference, err := s.meetingFor(ctx, ck, in.Title)
		if err != nil {
			return nil, err
		}
		body.model = body.model.WithConference(conference)
		res.Call = gaveCall(body.model, made)
		defer func() {
			if res == nil {
				s.giveBack(ctx, made)
			}
		}()
	}
	op, _, err := ck.createOp(body)
	if err != nil {
		return nil, err
	}
	created, err := s.sync(ctx, calendarID, ck.memberID, []syncOp{op})
	if err != nil {
		return nil, err
	}
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
		e, role, err := ParseAttendee(raw)
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
	return keys.Published(ctx, s.C, email)
}

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

func ParseAttendee(raw string) (email, role string, err error) {
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
	return email, "", fmt.Errorf("attendee %q: after the colon write required or optional", raw)
}

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

// EventStatuses are what Proton stores an event's status as: it refuses
// TENTATIVE, which only an invitation from another service carries.
func EventStatuses() []string { return []string{"confirmed", "cancelled"} }

// ICalStatus turns the word a person writes into the one iCalendar stores.
func ICalStatus(word string) string { return eventStatuses[strings.ToLower(word)] }

func StatusText(status string) string {
	for word, ical := range eventStatuses {
		if ical == strings.ToUpper(status) {
			return word
		}
	}
	return "confirmed"
}

func ReminderText(kind int, trigger string) string {
	spec := units.Duration(triggerDuration(trigger))
	if kind == remindByEmail {
		return spec + ":email"
	}
	return spec
}

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

// EventsExport returns what a calendar holds as iCalendar models, ready to be
// written as a file.
//
// A series is exported **once**, carrying its rule, rather than expanded into the
// occurrences a listing shows. That is what a calendar file is: expanding it
// would turn one weekly standup into fifty-two unrelated events, and no client
// could put it back.
//
// An event that cannot be decrypted is left out rather than written as a stub,
// because a file is something another client will trust - and recorded, because a
// file that is short says nothing about what is missing from it.
func (s *Service) EventsExport(ctx context.Context, calendarIDs []string, w ical.Window) ([]ical.VEvent, error) {
	groups, err := s.readCalendars(ctx, calendarIDs, s.inWindow(w))
	if err != nil {
		return nil, err
	}
	var out []ical.VEvent
	for _, events := range groups {
		for _, e := range events {
			if e.readErr != nil {
				skip.Record(ctx, skip.KindEvent, e.raw.ID, skip.Undecryptable, e.readErr)
				continue
			}
			v := e.model
			v.Alarms = alarmsOf(e.raw)
			v.Color = e.raw.ownColor()
			out = append(out, v)
		}
	}
	slices.SortStableFunc(out, func(a, b ical.VEvent) int {
		return a.Start.Time.Compare(b.Start.Time)
	})
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

type ImportResult struct {
	Imported []string       `json:"imported"`
	Skipped  []SkippedEvent `json:"skipped"`
}

type SkippedEvent struct {
	Summary string `json:"summary,omitempty"`
	UID     string `json:"uid,omitempty"`
	Reason  string `json:"reason"`
}

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
// A colour is snapped onto Proton's palette, which is the only set of colours it
// stores: a file may name any colour CSS has, and the event is worth more than
// the exactness of its decoration.
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
		color := accent.Nearest(v.Color)
		if v.Color != "" && color == "" {
			// The event still lands and nothing is missing from the import, so this
			// is recorded and not counted.
			slog.DebugContext(ctx, "calendar: an imported event named a colour that is not one",
				"reason", "unreadable color")
		}
		body := eventBody{
			model:         importable(v),
			notifications: notificationsOf(v.Alarms),
			color:         optionalColor(color),
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

func importable(v ical.VEvent) ical.VEvent {
	v.Attendees = nil
	v.Organizer = ""
	v.Alarms = nil
	v.Conference = nil
	v.Rest = nil
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
