package calendar

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/roman-16/proton-cli/internal/ical"
	"github.com/roman-16/proton-cli/internal/proton"
)

// A recurring event is one record standing for many occurrences, plus one extra
// record for every occurrence somebody has edited on its own. Changing "this
// one", "this one and the rest" or "all of them" therefore means writing a
// different combination of those records, and the combinations are Proton's, not
// this CLI's invention: they are what its own clients do.

// EventPatch is what an update changes. A nil field is a field the caller did not
// mention, which is left exactly as it was - including the reminder list and the
// colour, which are not in the event's content but are just as much part of it.
type EventPatch struct {
	Title       *string
	Location    *string
	Description *string
	// Start is where the event is put: a day, or a moment anchored to the zone the
	// change is written against.
	Start *ical.DateTime
	// Duration is how long it lasts. Absent keeps the length it had, which is what
	// makes moving an event a move rather than a rewrite.
	Duration *time.Duration
	// AllDay takes an event's time of day away. Giving one back is a Start that
	// names one, since a time of day is a time somebody has to choose.
	AllDay    *bool
	RRule     *string
	Reminders *[]string
	// Status is whether the event is going ahead: CONFIRMED, TENTATIVE or
	// CANCELLED. Cancelling this way keeps the event and its history, which is
	// what the web client does and what `delete` does not.
	Status *string
	// Color is the event's own accent colour, as the hex Proton stores.
	//
	// There is no value here that takes a colour away: Proton offers no colour
	// meaning "none", so an event that has been given one can only be given
	// another. Its own clients cannot do it either.
	Color *string
}

// TouchesTimes reports whether the patch puts the event somewhere else or gives
// it another length, which is what decides both what a result says about it and
// whether the exclusions and the separately edited occurrences still mean
// anything.
func (p EventPatch) TouchesTimes() bool {
	return p.Start != nil || p.Duration != nil || p.AllDay != nil
}

// breaks reports whether the patch changes something participants have to be told
// about, and something that invalidates instants recorded against the series.
func (p EventPatch) breaks() bool { return p.TouchesTimes() || p.RRule != nil }

// Lands is where the patch puts an event that begins at start, in the form it
// then has: a day for one with no time of day, a moment for one with.
//
// A start that names a time of day gives an all-day event one, because naming a
// time is what asking for a time of day looks like. A start that names only a
// day moves an event and leaves the time of day it had, because that is what
// moving a meeting to Monday means.
//
// A result and a dry run say where a change lands before it is made, so the rule
// is here rather than beside either of them.
func (p EventPatch) Lands(start ical.DateTime) ical.DateTime {
	allDay := start.AllDay
	if p.Start != nil {
		allDay = allDay && p.Start.AllDay
	}
	if p.AllDay != nil {
		allDay = *p.AllDay
	}
	switch {
	case p.Start == nil && !allDay:
		return start
	case p.Start == nil:
		return ical.Day(start.Wall())
	case allDay:
		return ical.Day(p.Start.Wall())
	case p.Start.AllDay:
		return onDay(start, *p.Start)
	default:
		return *p.Start
	}
}

// onDay is an event's own time of day, on another day, in the zone it is
// anchored to.
func onDay(current, day ical.DateTime) ical.DateTime {
	wall := current.Wall()
	y, m, d := day.Time.Date()
	moved := time.Date(y, m, d, wall.Hour(), wall.Minute(), wall.Second(), 0, current.Location())
	return ical.Timed(moved, current.TZID)
}

// apply merges the patch into an event.
func (p EventPatch) apply(v ical.VEvent) ical.VEvent {
	if p.Title != nil {
		v.Summary = *p.Title
	}
	if p.Location != nil {
		v.Location = *p.Location
	}
	if p.Status != nil {
		v.Status = *p.Status
	}
	if p.Description != nil {
		v.Description = *p.Description
	}
	if p.RRule != nil {
		v.RRule = *p.RRule
	}
	if !p.TouchesTimes() {
		return v
	}
	start, end := v.Span()
	dur := end.Time.Sub(start.Time)
	if p.Duration != nil {
		dur = *p.Duration
	}
	return withTimes(v, p.Lands(start), dur)
}

// withTimes puts an event at a moment, for a length.
//
// An all-day event's end is exclusive in iCalendar, so a single day runs to the
// next one, and a length too short to reach the next day still has to end there:
// an event that lasts no time at all is not what a whole day means.
func withTimes(v ical.VEvent, start ical.DateTime, dur time.Duration) ical.VEvent {
	if start.AllDay {
		last := ical.Day(start.Time.Add(dur))
		if !last.Time.After(start.Time) {
			last = ical.Day(start.Time.AddDate(0, 0, 1))
		}
		v.Start, v.End = start, last
		return v
	}
	v.Start, v.End = start, ical.Timed(start.Time.Add(dur), start.TZID)
	return v
}

// reminders resolves the notification list a write sends: the patched one, or the
// one the event already had, preserving the difference between having none and
// taking the calendar's defaults.
func (p EventPatch) reminders(raw rawEvent) ([]map[string]any, error) {
	if p.Reminders == nil {
		return raw.notifications(), nil
	}
	return buildReminders(*p.Reminders)
}

// color resolves the colour a write sends: the patched one, or the one the event
// already had.
func (p EventPatch) color(raw rawEvent) *string {
	if p.Color == nil {
		return raw.Color
	}
	return optionalColor(*p.Color)
}

// ── the chain ──

// series is every record that makes up one recurring event.
type series struct {
	master    stored
	overrides []stored
}

// overrideAt returns the record that replaces one occurrence, or nil.
func (c series) overrideAt(at ical.DateTime) *stored {
	for i := range c.overrides {
		if c.overrides[i].model.RecurrenceID.Equal(at) {
			return &c.overrides[i]
		}
	}
	return nil
}

// idsFrom lists the overrides at or after an instant, which is the set a change
// to "this one and the rest" invalidates.
func (c series) idsFrom(at ical.DateTime) []string {
	var out []string
	for _, o := range c.overrides {
		if !o.model.RecurrenceID.Time.Before(at.Time) {
			out = append(out, o.raw.ID)
		}
	}
	return out
}

func (c series) allOverrideIDs() []string {
	out := make([]string, 0, len(c.overrides))
	for _, o := range c.overrides {
		out = append(out, o.raw.ID)
	}
	return out
}

// loadSeries fetches every record sharing a UID.
//
// Proton has an endpoint for exactly this - the chain is what its own clients
// need in order to know which occurrences have been edited - so the separately
// edited ones are looked up rather than guessed at from a date window.
func (s *Service) loadSeries(ctx context.Context, ck *calKeys, calendarID string, master stored) (series, error) {
	q := url.Values{}
	q.Set("UID", master.raw.UID)
	q.Set("PageSize", fmt.Sprintf("%d", eventsPageSize))
	raws, err := proton.All(ctx, func(ctx context.Context, page int) ([]rawEvent, bool, error) {
		q.Set("Page", fmt.Sprintf("%d", page))
		var r struct{ Events []rawEvent }
		if err := s.C.Decode(ctx, proton.Request{Method: "GET", Path: "/calendar/v1/events", Query: q}, &r); err != nil {
			return nil, false, err
		}
		return r.Events, proton.Full(r.Events, eventsPageSize), nil
	})
	if err != nil {
		return series{}, err
	}

	out := series{master: master}
	for _, raw := range raws {
		if raw.ID == master.raw.ID || raw.CalendarID != calendarID {
			continue
		}
		e := s.decrypt(ctx, ck, raw)
		if e.readErr != nil || !e.model.IsOverride() {
			continue
		}
		out.overrides = append(out.overrides, e)
	}
	return out, nil
}

// ── resolving an occurrence ──

// occurrenceTarget is one instance of a series, resolved to the records behind it.
type occurrenceTarget struct {
	chain    series
	at       ical.DateTime
	instance ical.Occurrence
	// override is the record replacing this instance, nil when the rule still
	// generates it.
	override *stored
}

func (s *Service) resolveOccurrence(ctx context.Context, ck *calKeys, calendarID string, master stored, occurrence string) (occurrenceTarget, error) {
	if !master.model.Recurring() {
		return occurrenceTarget{}, errs.Problemf("%s is not a recurring event, so it has no occurrences.", master.raw.ID)
	}
	at, err := master.model.ParseOccurrence(occurrence)
	if err != nil {
		return occurrenceTarget{}, errs.Problemf("%v", err).
			Hint("occurrences are named by their start, as `events list` prints it")
	}
	instance, ok, err := master.model.OccurrenceAt(at)
	if err != nil {
		return occurrenceTarget{}, err
	}
	chain, err := s.loadSeries(ctx, ck, calendarID, master)
	if err != nil {
		return occurrenceTarget{}, err
	}
	override := chain.overrideAt(at)
	if !ok && override == nil {
		return occurrenceTarget{}, &errs.NotFound{Kind: "occurrence", Ref: occurrence}
	}
	return occurrenceTarget{chain: chain, at: at, instance: instance, override: override}, nil
}

// row reports the instance as a person sees it.
func (t occurrenceTarget) row() Event {
	master := t.chain.master
	if t.override != nil {
		ev := t.override.row()
		ev.ID = master.raw.ID
		ev.StoredID = t.override.raw.ID
		ev.Occurrence = t.at.String()
		ev.RRule = master.model.RRule
		ev.Number = t.instance.Number
		return ev
	}
	return master.occurrenceRow(t.instance)
}

// currentVEvent is what the series says this instance looks like today: the
// series' own content, placed at the instance, with nothing recurring left on it.
func (t occurrenceTarget) currentVEvent() ical.VEvent {
	if t.override != nil {
		return t.override.model
	}
	v := t.chain.master.model
	v.RRule = ""
	v.ExDates = nil
	v.RecurrenceID = nil
	v.Start = t.instance.Start
	v.End = t.instance.End
	return v
}

// ── writing ──

// EventUpdate changes a stored event, which for a recurring event is the whole
// series.
//
// Everything the caller did not mention is carried over: not only the title and
// the times but the recurrence rule, the exclusions, the reminders, the colour and
// whether the account is the organizer. An update that rebuilt those from
// defaults would quietly resurrect deleted occurrences and reset reminders.
func (s *Service) EventUpdate(ctx context.Context, calendarID, eventID string, p EventPatch) (*EventResult, error) {
	ck, err := s.unlockCalendar(ctx, calendarID)
	if err != nil {
		return nil, err
	}
	old, err := s.storedEvent(ctx, ck, calendarID, eventID)
	if err != nil {
		return nil, err
	}
	if old.readErr != nil {
		return nil, fmt.Errorf("read the event before updating it: %w", old.readErr)
	}
	updated := p.apply(old.model)

	var ops []syncOp
	if old.model.Recurring() && p.breaks() {
		// The exclusions and the separately edited occurrences name instants this
		// series no longer has, so they go with the pattern that created them.
		updated.ExDates = nil
		chain, err := s.loadSeries(ctx, ck, calendarID, old)
		if err != nil {
			return nil, err
		}
		for _, id := range chain.allOverrideIDs() {
			ops = append(ops, deleteOp(id))
		}
	}
	updated.Sequence = ical.NextSequence(old.model, updated)

	notifs, err := p.reminders(old.raw)
	if err != nil {
		return nil, err
	}
	op, err := ck.updateOp(eventID, eventBody{
		model:         updated,
		notifications: notifs,
		color:         p.color(old.raw),
		isOrganizer:   old.raw.IsOrganizer,
		keyPacket:     old.raw.SharedKeyPacket,
		attendeeList:  attendeeList(old.raw),
	})
	if err != nil {
		return nil, err
	}
	if _, err := s.sync(ctx, calendarID, ck.memberID, append(ops, op)); err != nil {
		return nil, err
	}
	mail, err := s.changeMail(ctx, updated, old.model, p, nil)
	if err != nil {
		return nil, err
	}
	return &EventResult{ID: eventID, Mail: mail}, nil
}

// OccurrenceUpdate changes one instance of a series, writing the extra record
// that replaces it. The series itself keeps its rule and its other occurrences.
func (s *Service) OccurrenceUpdate(ctx context.Context, calendarID, eventID, occurrence string, p EventPatch) (*EventResult, error) {
	ck, err := s.unlockCalendar(ctx, calendarID)
	if err != nil {
		return nil, err
	}
	master, err := s.readMaster(ctx, ck, calendarID, eventID)
	if err != nil {
		return nil, err
	}
	target, err := s.resolveOccurrence(ctx, ck, calendarID, master, occurrence)
	if err != nil {
		return nil, err
	}

	current := target.currentVEvent()
	updated := p.apply(current).AsOverride(master.model, target.at)
	// A replacement may never claim to be older than the series it belongs to, and
	// Proton refuses a sequence that goes backwards.
	updated.Sequence = max(ical.NextSequence(current, updated), master.model.Sequence)

	notifs, err := p.reminders(overrideRaw(target, master))
	if err != nil {
		return nil, err
	}
	body := eventBody{
		model:         updated,
		notifications: notifs,
		color:         p.color(overrideRaw(target, master)),
		isOrganizer:   master.raw.IsOrganizer,
	}

	var ops []syncOp
	var external []string
	if target.override != nil {
		body.keyPacket = target.override.raw.SharedKeyPacket
		body.attendeeList = attendeeList(target.override.raw)
		op, err := ck.updateOp(target.override.raw.ID, body)
		if err != nil {
			return nil, err
		}
		ops = append(ops, op)
	} else {
		// Proton refuses a replacement whose series carries no sequence at all, so
		// the series is written back alongside it. This CLI always writes one, but
		// an event another client created may not have.
		parent, err := ck.updateOp(master.raw.ID, eventBody{
			model:         master.model,
			notifications: master.raw.notifications(),
			color:         master.raw.Color,
			isOrganizer:   master.raw.IsOrganizer,
			keyPacket:     master.raw.SharedKeyPacket,
			attendeeList:  attendeeList(master.raw),
		})
		if err != nil {
			return nil, err
		}
		if external, err = s.attachAttendees(ctx, &body, emailsOf(updated)); err != nil {
			return nil, err
		}
		op, _, err := ck.createOp(body)
		if err != nil {
			return nil, err
		}
		ops = append(ops, parent, op)
	}

	if _, err := s.sync(ctx, calendarID, ck.memberID, ops); err != nil {
		return nil, err
	}
	mail, err := s.changeMail(ctx, updated, current, p, external)
	if err != nil {
		return nil, err
	}
	return &EventResult{ID: eventID, Mail: mail}, nil
}

// SeriesSplit changes one instance and every later one: the series is ended just
// before it, and a new series takes over from there.
func (s *Service) SeriesSplit(ctx context.Context, calendarID, eventID, occurrence string, p EventPatch) (*EventResult, error) {
	ck, err := s.unlockCalendar(ctx, calendarID)
	if err != nil {
		return nil, err
	}
	master, err := s.readMaster(ctx, ck, calendarID, eventID)
	if err != nil {
		return nil, err
	}
	target, err := s.resolveOccurrence(ctx, ck, calendarID, master, occurrence)
	if err != nil {
		return nil, err
	}
	truncated, ok := master.model.TruncateBefore(target.at, target.instance.Number)
	if !ok {
		return nil, errs.Problemf("This is the first occurrence, so there is nothing before it to keep.").
			Hint(fmt.Sprintf("update the whole series instead: drop the @%s from the reference", occurrence))
	}
	truncated.Sequence = master.model.Sequence + 1

	current := target.currentVEvent()
	remainder := p.apply(current)
	remainder.RRule = master.model.RRule
	if p.RRule != nil {
		remainder.RRule = *p.RRule
	}
	remainder = remainder.AsFutureSeries(master.model, target.at, target.instance.Number)

	notifs, err := p.reminders(master.raw)
	if err != nil {
		return nil, err
	}
	head, err := ck.updateOp(master.raw.ID, eventBody{
		model:         truncated,
		notifications: master.raw.notifications(),
		color:         master.raw.Color,
		isOrganizer:   master.raw.IsOrganizer,
		keyPacket:     master.raw.SharedKeyPacket,
		attendeeList:  attendeeList(master.raw),
	})
	if err != nil {
		return nil, err
	}
	tailBody := eventBody{
		model:         remainder,
		notifications: notifs,
		color:         p.color(master.raw),
		isOrganizer:   master.raw.IsOrganizer,
	}
	external, err := s.attachAttendees(ctx, &tailBody, emailsOf(remainder))
	if err != nil {
		return nil, err
	}
	tail, _, err := ck.createOp(tailBody)
	if err != nil {
		return nil, err
	}

	ops := make([]syncOp, 0, len(target.chain.overrides)+2)
	// The remainder is a new series with a new identity, so occurrences edited on
	// their own past the split no longer belong to anything.
	for _, id := range target.chain.idsFrom(target.at) {
		ops = append(ops, deleteOp(id))
	}
	ops = append(ops, head, tail)

	created, err := s.sync(ctx, calendarID, ck.memberID, ops)
	if err != nil {
		return nil, err
	}
	mail, err := s.changeMail(ctx, remainder, current, p, external)
	if err != nil {
		return nil, err
	}
	res := &EventResult{ID: eventID, Mail: mail}
	if len(created) > 0 {
		res.ID = created[len(created)-1]
	}
	return res, nil
}

// EventDelete removes a stored event, and with it every occurrence of a series
// that was edited on its own.
func (s *Service) EventDelete(ctx context.Context, calendarID, eventID string) error {
	ck, err := s.unlockCalendar(ctx, calendarID)
	if err != nil {
		return err
	}
	ops := []syncOp{deleteOp(eventID)}
	if e, err := s.storedEvent(ctx, ck, calendarID, eventID); err == nil && e.readErr == nil && e.model.Recurring() {
		chain, err := s.loadSeries(ctx, ck, calendarID, e)
		if err != nil {
			return err
		}
		for _, id := range chain.allOverrideIDs() {
			ops = append(ops, deleteOp(id))
		}
	}
	_, err = s.sync(ctx, calendarID, ck.memberID, ops)
	return err
}

// OccurrenceDelete cancels one instance of a series, leaving the rule and every
// other occurrence intact.
func (s *Service) OccurrenceDelete(ctx context.Context, calendarID, eventID, occurrence string) error {
	ck, err := s.unlockCalendar(ctx, calendarID)
	if err != nil {
		return err
	}
	master, err := s.readMaster(ctx, ck, calendarID, eventID)
	if err != nil {
		return err
	}
	target, err := s.resolveOccurrence(ctx, ck, calendarID, master, occurrence)
	if err != nil {
		return err
	}

	var ops []syncOp
	// An instance edited on its own has a record of its own, which the exclusion
	// does not remove: without this the "deleted" occurrence keeps rendering.
	if target.override != nil {
		ops = append(ops, deleteOp(target.override.raw.ID))
	}
	if excluded, changed := master.model.ExcludeOccurrence(target.at); changed {
		op, err := ck.updateOp(master.raw.ID, eventBody{
			model:         excluded,
			notifications: master.raw.notifications(),
			color:         master.raw.Color,
			isOrganizer:   master.raw.IsOrganizer,
			keyPacket:     master.raw.SharedKeyPacket,
			attendeeList:  attendeeList(master.raw),
		})
		if err != nil {
			return err
		}
		ops = append(ops, op)
	}
	_, err = s.sync(ctx, calendarID, ck.memberID, ops)
	return err
}

// SeriesTruncate removes one instance and every later one by ending the series
// just before it.
func (s *Service) SeriesTruncate(ctx context.Context, calendarID, eventID, occurrence string) error {
	ck, err := s.unlockCalendar(ctx, calendarID)
	if err != nil {
		return err
	}
	master, err := s.readMaster(ctx, ck, calendarID, eventID)
	if err != nil {
		return err
	}
	target, err := s.resolveOccurrence(ctx, ck, calendarID, master, occurrence)
	if err != nil {
		return err
	}
	truncated, ok := master.model.TruncateBefore(target.at, target.instance.Number)
	if !ok {
		return errs.Problemf("This is the first occurrence, so ending the series here removes all of it.").
			Hint(fmt.Sprintf("delete the whole series instead: drop the @%s from the reference", occurrence))
	}

	ops := make([]syncOp, 0, len(target.chain.overrides)+1)
	for _, id := range target.chain.idsFrom(target.at) {
		ops = append(ops, deleteOp(id))
	}
	op, err := ck.updateOp(master.raw.ID, eventBody{
		model:         truncated,
		notifications: master.raw.notifications(),
		color:         master.raw.Color,
		isOrganizer:   master.raw.IsOrganizer,
		keyPacket:     master.raw.SharedKeyPacket,
		attendeeList:  attendeeList(master.raw),
	})
	if err != nil {
		return err
	}
	_, err = s.sync(ctx, calendarID, ck.memberID, append(ops, op))
	return err
}

// ── helpers ──

// readMaster fetches the event a reference addresses and insists it can be read,
// which every scoped change needs before it can decide what to write.
func (s *Service) readMaster(ctx context.Context, ck *calKeys, calendarID, eventID string) (stored, error) {
	e, err := s.storedEvent(ctx, ck, calendarID, eventID)
	if err != nil {
		return stored{}, err
	}
	if e.readErr != nil {
		return stored{}, fmt.Errorf("read the event before changing it: %w", e.readErr)
	}
	return e, nil
}

// overrideRaw is the record whose non-content fields a replacement inherits: its
// own if it exists, otherwise the series'.
func overrideRaw(t occurrenceTarget, master stored) rawEvent {
	if t.override != nil {
		return t.override.raw
	}
	return master.raw
}

// attendeeList re-sends the cleartext per-attendee record, so an update does not
// drop the participants' server-side status.
func attendeeList(raw rawEvent) []map[string]any {
	if len(raw.AttendeesInfo.Attendees) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(raw.AttendeesInfo.Attendees))
	for _, a := range raw.AttendeesInfo.Attendees {
		out = append(out, map[string]any{"Token": a.Token, "Status": a.Status})
	}
	return out
}

func emailsOf(v ical.VEvent) []string {
	out := make([]string, 0, len(v.Attendees))
	for _, a := range v.Attendees {
		out = append(out, a.Email)
	}
	return out
}

// changeMail is the message participants need when a change is one they have to
// know about.
//
// Only attendees outside Proton are emailed: Proton writes a change straight into
// a Proton attendee's own calendar, so mailing them as well would tell them twice.
func (s *Service) changeMail(ctx context.Context, updated, old ical.VEvent, p EventPatch, external []string) (*Mail, error) {
	if !p.breaks() || len(updated.Attendees) == 0 || updated.Sequence <= old.Sequence {
		return nil, nil
	}
	if external == nil {
		var err error
		if external, err = s.externalAttendees(ctx, emailsOf(updated)); err != nil {
			return nil, err
		}
	}
	if len(external) == 0 {
		return nil, nil
	}
	return updateMail(updated, external), nil
}

// externalAttendees returns the addresses with no Proton account.
func (s *Service) externalAttendees(ctx context.Context, emails []string) ([]string, error) {
	var out []string
	for _, email := range emails {
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
			out = append(out, email)
		}
	}
	return out, nil
}
