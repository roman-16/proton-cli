package calendar

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/roman-16/proton-cli/internal/ical"
	"github.com/roman-16/proton-cli/internal/proton"
	"github.com/roman-16/proton-cli/internal/service/meet"
)

// autoAddLink is AutoAddConferenceLink in the calendar settings, which Proton
// answers as an object naming a provider and takes back as the bare number.
type autoAddLink struct{ provider int }

func (a *autoAddLink) UnmarshalJSON(b []byte) error {
	var provider int
	if err := json.Unmarshal(b, &provider); err == nil {
		a.provider = provider
		return nil
	}
	var object struct{ Provider int }
	if err := json.Unmarshal(b, &object); err != nil {
		return err
	}
	a.provider = object.Provider
	return nil
}

// AutoAddsMeet says whether Proton's web client would give a new event with a
// guest a Proton Meet link: the account's Auto-add Meet links setting, for an
// account its organization lets use Meet.
func (s *Service) AutoAddsMeet(ctx context.Context) bool {
	settings, err := s.userSettings(ctx)
	if err != nil {
		// Recorded and not counted: the event is made as asked, without the link
		// the setting might have added, and nothing it holds is missing.
		slog.DebugContext(ctx, "calendar: the calendar settings could not be read for Auto-add Meet links",
			"reason", "unreadable", "error", err)
		return false
	}
	return settings.AutoAddConferenceLink.provider == ical.ProviderMeet && s.meetings != nil && s.meetings.Allowed(ctx)
}

func (s *Service) meetingFor(ctx context.Context, ck *calKeys, title string) (*meet.Meeting, ical.Conference, error) {
	if s.meetings == nil {
		return nil, ical.Conference{}, errors.New("calendar: no Meet service to make a meeting with")
	}
	m, err := s.meetings.Create(ctx, meet.NewMeeting{
		Title: title, Type: meet.Scheduled, AddressID: ck.addressID, FromCalendar: true,
	})
	if err != nil {
		return nil, ical.Conference{}, err
	}
	return m, ical.Conference{ID: m.Name, URL: m.URL, Provider: ical.ProviderMeet, Host: ck.email}, nil
}

// withMeet gives an event a Proton Meet link or takes it away, as the patch
// says. unchanged reports a patch that asked for what the event already is.
func (s *Service) withMeet(ctx context.Context, ck *calKeys, p EventPatch, v ical.VEvent, isOrganizer int) (out ical.VEvent, made *meet.Meeting, unchanged bool, err error) {
	if p.Meet == nil {
		return v, nil, false, nil
	}
	if isOrganizer != 1 {
		return v, nil, false, errs.Problemf("Only the organizer can change this event's Proton Meet link.")
	}
	call, has := v.Call()
	switch {
	case has && call.Provider != ical.ProviderMeet:
		return v, nil, false, errs.Problemf("This event's video call is not a Proton Meet one, and an event holds one call.").
			Hint("change it in the Proton Calendar web app")
	case *p.Meet == has:
		return v, nil, true, nil
	case !*p.Meet:
		return v.WithoutCall(), nil, false, nil
	}
	m, conference, err := s.meetingFor(ctx, ck, v.Summary)
	if err != nil {
		return v, nil, false, err
	}
	return v.WithConference(conference), m, false, nil
}

func (s *Service) giveBack(ctx context.Context, m *meet.Meeting) {
	if m == nil {
		return
	}
	if err := s.meetings.Delete(context.WithoutCancel(ctx), m); err != nil {
		slog.WarnContext(ctx, fmt.Sprintf(
			"The event was not saved, and the Proton Meet meeting %s made for it could not be deleted.", m.Name),
			"error", err)
	}
}

// carryCall gives a separately edited occurrence the series' call, or takes
// the series' Proton Meet call away from it, as Proton's web client does when
// a whole series is saved. changed is false for an occurrence already so.
func carryCall(occurrence, series ical.VEvent) (ical.VEvent, bool) {
	before, had := occurrence.Call()
	after, has := series.Call()
	switch {
	case has && (!had || before != after) && series.Conference != nil:
		return occurrence.WithConference(*series.Conference), true
	case !has && had && before.Provider == ical.ProviderMeet:
		return occurrence.WithoutCall(), true
	}
	return occurrence, false
}

// retitle is the event's new title sealed for its Proton Meet meeting, which
// Proton's web client sends with the event so the meeting is renamed with it.
// It does that only for a meeting this account hosts, and a meeting that cannot
// be opened keeps its old title rather than stopping the rename.
// endMeeting deletes the meeting behind the call a whole event has lost, once
// the event without it is stored. Nothing lists a meeting made for an event, so
// nothing would find it afterwards. It runs even when the command was cancelled,
// and a meeting it cannot delete leaves the event's change standing.
func (s *Service) endMeeting(ctx context.Context, ck *calKeys, before ical.VEvent) {
	c := before.Conference
	call, ok := before.Call()
	if !ok || call.Provider != ical.ProviderMeet || c == nil || s.meetings == nil || !strings.EqualFold(c.Host, ck.email) {
		return
	}
	name, password, ok := meet.ParseLink(c.URL)
	if !ok {
		return
	}
	ctx = context.WithoutCancel(ctx)
	m, err := s.meetings.ByLink(ctx, name, password)
	if err == nil {
		err = s.meetings.Delete(ctx, m)
	}
	var notFound *errs.NotFound
	if err == nil || errors.As(err, &notFound) {
		return
	}
	slog.WarnContext(ctx, fmt.Sprintf(
		"The event no longer has its Proton Meet link, but the meeting %s could not be deleted.", name),
		"error", err)
}

func (s *Service) retitle(ctx context.Context, ck *calKeys, v ical.VEvent) string {
	c := v.Conference
	call, ok := v.Call()
	if !ok || call.Provider != ical.ProviderMeet || c == nil || !strings.EqualFold(c.Host, ck.email) {
		return ""
	}
	sealed, reason, err := s.sealedTitle(ctx, c.URL, v.Summary)
	if reason != "" {
		code := 0
		var apiErr *proton.APIError
		if errors.As(err, &apiErr) {
			code = apiErr.Code
		}
		slog.WarnContext(ctx, "The event was renamed, but its Proton Meet meeting kept its old title.",
			"reason", reason, "code", code)
	}
	return sealed
}

func (s *Service) sealedTitle(ctx context.Context, url, title string) (sealed, reason string, err error) {
	if s.meetings == nil {
		return "", "no meet service", nil
	}
	name, password, ok := meet.ParseLink(url)
	if !ok || password == "" {
		return "", "link without a password", nil
	}
	m, err := s.meetings.ByLink(ctx, name, password)
	var notFound *errs.NotFound
	switch {
	case errors.As(err, &notFound):
		return "", "meeting gone", err
	case err != nil:
		return "", "meeting unreadable", err
	}
	if sealed, err = m.SealTitle(title); err != nil {
		return "", "meeting unreadable", err
	}
	return sealed, "", nil
}
