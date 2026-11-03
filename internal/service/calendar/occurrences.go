package calendar

import (
	"context"
	"fmt"
	"time"

	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/roman-16/proton-cli/internal/ical"
	"github.com/roman-16/proton-cli/internal/proton"
)

type EventPatch struct {
	Title       *string
	Location    *string
	Description *string
	Start       *ical.DateTime
	Duration    *time.Duration
	AllDay      *bool
	RRule       *string
	Reminders   *[]string
	Status      *string
	Color       *string
	Meet        *bool
}

func (p EventPatch) TouchesTimes() bool {
	return p.Start != nil || p.Duration != nil || p.AllDay != nil
}

func (p EventPatch) breaks() bool { return p.TouchesTimes() || p.RRule != nil }

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

func onDay(current, day ical.DateTime) ical.DateTime {
	wall := current.Wall()
	y, m, d := day.Time.Date()
	moved := time.Date(y, m, d, wall.Hour(), wall.Minute(), wall.Second(), 0, current.Location())
	return ical.Timed(moved, current.TZID)
}

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
		v = v.WithDescription(*p.Description)
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

func (p EventPatch) reminders(raw rawEvent) ([]map[string]any, error) {
	if p.Reminders == nil {
		return raw.notifications(), nil
	}
	return buildReminders(*p.Reminders)
}

func (p EventPatch) color(raw rawEvent) *string {
	if p.Color == nil {
		return raw.Color
	}
	return optionalColor(*p.Color)
}

type series struct {
	master    stored
	overrides []stored
}

func (c series) overrideAt(at ical.DateTime) *stored {
	for i := range c.overrides {
		if c.overrides[i].model.RecurrenceID.Equal(at) {
			return &c.overrides[i]
		}
	}
	return nil
}

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

func (s *Service) loadSeries(ctx context.Context, ck *calKeys, calendarID string, master stored) (series, error) {
	raws, err := s.eventsByUID(ctx, master.raw.UID)
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

type occurrenceTarget struct {
	chain    series
	at       ical.DateTime
	instance ical.Occurrence
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
	updated, made, unchanged, err := s.withMeet(ctx, ck, p, p.apply(old.model), old.raw.IsOrganizer)
	if err != nil {
		return nil, err
	}
	saved := false
	defer func() {
		if !saved {
			s.giveBack(ctx, made)
		}
	}()

	var ops []syncOp
	if old.model.Recurring() && (p.breaks() || (p.Meet != nil && !unchanged)) {
		chain, err := s.loadSeries(ctx, ck, calendarID, old)
		if err != nil {
			return nil, err
		}
		if p.breaks() {
			updated.ExDates = nil
			for _, id := range chain.allOverrideIDs() {
				ops = append(ops, deleteOp(id))
			}
		} else {
			for _, o := range chain.overrides {
				if carried, changed := carryCall(o.model, updated); changed {
					op, err := ck.updateOp(o.raw.ID, o.rewrite(carried))
					if err != nil {
						return nil, err
					}
					ops = append(ops, op)
				}
			}
		}
	}
	updated.Sequence = ical.NextSequence(old.model, updated)

	notifs, err := p.reminders(old.raw)
	if err != nil {
		return nil, err
	}
	body := old.rewrite(updated)
	body.notifications = notifs
	body.color = p.color(old.raw)
	if made == nil && p.Title != nil && *p.Title != old.model.Summary {
		body.meetTitle = s.retitle(ctx, ck, updated)
	}
	op, err := ck.updateOp(eventID, body)
	if err != nil {
		return nil, err
	}
	if _, err := s.sync(ctx, calendarID, ck.memberID, append(ops, op)); err != nil {
		return nil, err
	}
	saved = true
	if p.Meet != nil && !*p.Meet && !unchanged {
		s.endMeeting(ctx, ck, old.model)
	}
	res := &EventResult{ID: eventID, Call: gaveCall(updated, made), MeetUnchanged: unchanged}
	if res.Mail, err = s.changeMail(ctx, updated, old.model, p, nil); err != nil {
		return nil, err
	}
	return res, nil
}

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
	updated, made, unchanged, err := s.withMeet(ctx, ck, p, p.apply(current), master.raw.IsOrganizer)
	if err != nil {
		return nil, err
	}
	saved := false
	defer func() {
		if !saved {
			s.giveBack(ctx, made)
		}
	}()
	updated = updated.AsOverride(master.model, target.at)
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
		body.calendarKeyPacket = target.override.raw.CalendarKeyPacket
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
		parent, err := ck.updateOp(master.raw.ID, master.rewrite(master.model))
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
	saved = true
	res := &EventResult{ID: eventID, Call: gaveCall(updated, made), MeetUnchanged: unchanged}
	if res.Mail, err = s.changeMail(ctx, updated, current, p, external); err != nil {
		return nil, err
	}
	return res, nil
}

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
	remainder, made, unchanged, err := s.withMeet(ctx, ck, p, p.apply(current), master.raw.IsOrganizer)
	if err != nil {
		return nil, err
	}
	saved := false
	defer func() {
		if !saved {
			s.giveBack(ctx, made)
		}
	}()
	remainder.RRule = master.model.RRule
	if p.RRule != nil {
		remainder.RRule = *p.RRule
	}
	remainder = remainder.AsFutureSeries(master.model, target.at, target.instance.Number)

	notifs, err := p.reminders(master.raw)
	if err != nil {
		return nil, err
	}
	head, err := ck.updateOp(master.raw.ID, master.rewrite(truncated))
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
	for _, id := range target.chain.idsFrom(target.at) {
		ops = append(ops, deleteOp(id))
	}
	ops = append(ops, head, tail)

	created, err := s.sync(ctx, calendarID, ck.memberID, ops)
	if err != nil {
		return nil, err
	}
	saved = true
	res := &EventResult{ID: eventID, Call: gaveCall(remainder, made), MeetUnchanged: unchanged}
	if res.Mail, err = s.changeMail(ctx, remainder, current, p, external); err != nil {
		return nil, err
	}
	if len(created) > 0 {
		res.ID = created[len(created)-1]
	}
	return res, nil
}

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
	if target.override != nil {
		ops = append(ops, deleteOp(target.override.raw.ID))
	}
	if excluded, changed := master.model.ExcludeOccurrence(target.at); changed {
		op, err := ck.updateOp(master.raw.ID, master.rewrite(excluded))
		if err != nil {
			return err
		}
		ops = append(ops, op)
	}
	_, err = s.sync(ctx, calendarID, ck.memberID, ops)
	return err
}

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
	op, err := ck.updateOp(master.raw.ID, master.rewrite(truncated))
	if err != nil {
		return err
	}
	_, err = s.sync(ctx, calendarID, ck.memberID, append(ops, op))
	return err
}

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

func overrideRaw(t occurrenceTarget, master stored) rawEvent {
	if t.override != nil {
		return t.override.raw
	}
	return master.raw
}

func (e stored) rewrite(model ical.VEvent) eventBody {
	return eventBody{
		model:             model,
		notifications:     e.raw.notifications(),
		color:             e.raw.Color,
		isOrganizer:       e.raw.IsOrganizer,
		keyPacket:         e.raw.SharedKeyPacket,
		calendarKeyPacket: e.raw.CalendarKeyPacket,
		attendeeList:      attendeeList(e.raw),
	}
}

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

func (s *Service) changeMail(ctx context.Context, updated, old ical.VEvent, p EventPatch, external []string) (*Mail, error) {
	rescheduled := p.breaks() && updated.Sequence > old.Sequence
	if len(updated.Attendees) == 0 || (!rescheduled && callOf(updated) == callOf(old)) {
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

func callOf(v ical.VEvent) ical.Call {
	call, _ := v.Call()
	return call
}
