package calendar

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sort"
	"strings"

	pgphelper "github.com/roman-16/proton-cli/internal/crypto/pgp"
	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/roman-16/proton-cli/internal/fetch"
	"github.com/roman-16/proton-cli/internal/proton"
	"github.com/roman-16/proton-cli/internal/ref"
)

// The kinds of calendar Proton's own clients sort an account's calendars into:
// your own, one somebody else owns and shared with you, one filled from an
// address Proton fetches, and one of the public holidays Proton keeps.
const (
	KindPersonal   = "personal"
	KindShared     = "shared"
	KindSubscribed = "subscribed"
	KindHolidays   = "holidays"
)

type Calendar struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Color       string `json:"color"`
	Description string `json:"description,omitempty"`
	Kind        string `json:"kind"`
	// Owner and Access say, for a calendar somebody shared with you, whose it is
	// and whether you may change what is on it.
	Owner  string `json:"owner,omitempty"`
	Access string `json:"access,omitempty"`

	memberID string
	priority int
	active   bool
}

// Proton's numbering of a calendar's type.
const (
	typePersonal   = 0
	typeSubscribed = 1
	typeHolidays   = 2
)

// The bits of a membership's flags that say whether the calendar is in use:
// Proton marks one active, and marks one its owner or an administrator switched
// off.
const (
	flagActive             = 1
	flagSelfDisabled       = 32
	flagSuperOwnerDisabled = 64
)

// kindOf names a calendar's kind, falling back to the number for a type this
// version has not been told about rather than calling it personal.
func kindOf(typ int, owned bool) string {
	switch typ {
	case typePersonal:
		if owned {
			return KindPersonal
		}
		return KindShared
	case typeSubscribed:
		return KindSubscribed
	case typeHolidays:
		return KindHolidays
	}
	return fmt.Sprintf("type %d", typ)
}

// CalendarsList reads the calendars on the account.
func (s *Service) CalendarsList(ctx context.Context) ([]Calendar, error) {
	return s.calendars.Do("", func() ([]Calendar, error) {
		var r struct {
			Calendars []struct {
				ID      string
				Type    int
				Owner   struct{ Email string }
				Members []member
			}
		}
		if err := s.C.Decode(ctx, proton.Request{Method: "GET", Path: "/calendar/v1"}, &r); err != nil {
			return nil, err
		}
		out := make([]Calendar, 0, len(r.Calendars))
		for _, c := range r.Calendars {
			out = append(out, calendarFromListing(c.ID, c.Type, c.Owner.Email, c.Members))
		}
		return out, nil
	})
}

// calendarFromListing reads one calendar out of the account's list of them.
//
// Proton lists only the account's own memberships of each calendar, and its
// clients read everything personal from the first: the name, the colour, what it
// may do. The calendar is yours when the address that owns it is that
// membership's.
func calendarFromListing(id string, typ int, owner string, members []member) Calendar {
	var me member
	if len(members) > 0 {
		me = members[0]
	}
	cal := Calendar{
		ID: id, Name: me.Name, Color: me.Color, Description: me.Description,
		Kind:     kindOf(typ, len(members) > 0 && owner == me.Email),
		memberID: me.ID, priority: me.Priority,
		active: me.Flags&flagActive != 0 && me.Flags&(flagSelfDisabled|flagSuperOwnerDisabled) == 0,
	}
	if cal.Kind == KindShared {
		cal.Owner, cal.Access = owner, accessWord(me.Permissions)
	}
	return cal
}

// Writable reports whether events can be made and changed in the calendar: one
// of your own, or one shared with you to edit.
func (c Calendar) Writable() bool {
	return c.Kind == KindPersonal || (c.Kind == KindShared && c.Access == accessEditor)
}

// Active reports whether the calendar is in use rather than switched off.
func (c Calendar) Active() bool { return c.active }

// TakesEvents refuses a calendar nothing can be written to, saying why.
func (c Calendar) TakesEvents() error {
	switch {
	case c.Writable():
		return nil
	case c.Kind == KindHolidays:
		return errs.Naming(c.Name, errs.Problemf("%s is read-only: its events are Proton's.", c.Name))
	case c.Kind == KindSubscribed:
		return errs.Naming(c.Name, errs.Problemf(
			"%s is read-only: its events come from the address it follows.", c.Name))
	case c.Kind == KindShared:
		return errs.Naming(c.Name, errs.Problemf(
			"You can only view %s, which %s shared with you.", c.Name, c.Owner))
	}
	return errs.Naming(c.Name, errs.Problemf("%s is a kind of calendar nothing is written to.", c.Name))
}

// Shareable refuses a calendar that cannot be given to anybody or published as a
// link, saying why. Only a personal calendar of your own can be.
func (c Calendar) Shareable() error {
	switch c.Kind {
	case KindPersonal:
		return nil
	case KindShared:
		return errs.Naming(c.Name, errs.Problemf(
			"%s is %s's, and only they can share or publish it.", c.Name, c.Owner))
	case KindSubscribed:
		return errs.Naming(c.Name, errs.Problemf(
			"%s is a subscribed calendar, which cannot be shared or published.", c.Name))
	case KindHolidays:
		return errs.Naming(c.Name, errs.Problemf(
			"%s is a holidays calendar, which cannot be shared or published.", c.Name))
	}
	return errs.Naming(c.Name, errs.Problemf("%s cannot be shared or published.", c.Name))
}

// ErrNoCalendarTakesEvents is DefaultCalendar's answer for an account none of
// whose calendars can take a new event.
var ErrNoCalendarTakesEvents = errors.New("no calendar takes events")

// DefaultCalendar is the calendar a new event goes into when nothing names one.
//
// It is the one Proton's clients choose: the default calendar the account has
// set, when that one is in use and can take events, and otherwise the first that
// can.
func (s *Service) DefaultCalendar(ctx context.Context) (Calendar, error) {
	cals, settings, err := s.calendarsAndSettings(ctx)
	if err != nil {
		return Calendar{}, err
	}
	takers := inUse(cals, Calendar.Writable)
	if len(takers) == 0 {
		return Calendar{}, ErrNoCalendarTakesEvents
	}
	if i := slices.IndexFunc(takers, func(c Calendar) bool { return c.ID == settings.DefaultCalendarID }); i >= 0 {
		return takers[i], nil
	}
	if settings.DefaultCalendarID != "" {
		// Recorded and not counted: nothing is missing from an answer, and the
		// result names the calendar that was used instead.
		slog.DebugContext(ctx, "the default calendar takes no events, so the first that does was used",
			"calendar", settings.DefaultCalendarID)
	}
	return takers[0], nil
}

// DefaultAfter is what becomes of the default calendar once the named calendars
// are deleted.
//
// moves is false while the default survives. When it goes, Proton's clients
// hand the default to the first remaining calendar of your own, and next is that
// calendar - or nil when none remains, which leaves the account with no default.
func (s *Service) DefaultAfter(ctx context.Context, deleting []string) (next *Calendar, moves bool, err error) {
	cals, settings, err := s.calendarsAndSettings(ctx)
	if err != nil {
		return nil, false, err
	}
	own := inUse(cals, func(c Calendar) bool { return c.Kind == KindPersonal })
	if len(own) == 0 {
		return nil, false, nil
	}
	current := own[0]
	if i := slices.IndexFunc(own, func(c Calendar) bool { return c.ID == settings.DefaultCalendarID }); i >= 0 {
		current = own[i]
	}
	if !slices.Contains(deleting, current.ID) {
		return nil, false, nil
	}
	for _, c := range own {
		if !slices.Contains(deleting, c.ID) {
			return &c, true, nil
		}
	}
	return nil, true, nil
}

// CalendarSetDefault makes a calendar the one new events go into, or leaves the
// account with none when calendarID is empty.
func (s *Service) CalendarSetDefault(ctx context.Context, calendarID string) error {
	var id any
	if calendarID != "" {
		id = calendarID
	}
	return s.C.Decode(ctx, proton.Request{
		Method: "PUT", Path: "/settings/calendar", Body: map[string]any{"DefaultCalendarID": id},
	}, nil)
}

func (s *Service) calendarsAndSettings(ctx context.Context) ([]Calendar, userSettings, error) {
	var cals []Calendar
	var settings userSettings
	err := fetch.Together(ctx,
		func(ctx context.Context) error {
			var err error
			cals, err = s.CalendarsList(ctx)
			return err
		},
		func(ctx context.Context) error {
			var err error
			settings, err = s.userSettings(ctx)
			return err
		},
	)
	return cals, settings, err
}

// inUse is the calendars in use that keep, in the order Proton's clients offer
// them: your own before any shared with you, each by the priority it was given.
func inUse(cals []Calendar, keep func(Calendar) bool) []Calendar {
	var out []Calendar
	for _, c := range cals {
		if c.active && keep(c) {
			out = append(out, c)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if mineI, mineJ := out[i].Kind == KindPersonal, out[j].Kind == KindPersonal; mineI != mineJ {
			return mineI
		}
		return out[i].priority < out[j].priority
	})
	return out
}

// CalendarName is the name this account gave a calendar.
//
// It reads the membership, which is where Proton keeps the name and which every
// command that touches a calendar has already fetched. Asking for the whole list
// of calendars to turn one ID into one string is a request for something already
// in hand. A calendar this account is not a member of is reported by its ID,
// which is the only name it has here.
func (s *Service) CalendarName(ctx context.Context, calendarID string) (string, error) {
	u, err := s.keys(ctx)
	if err != nil {
		return "", err
	}
	b, err := s.calendarBootstrap(ctx, calendarID)
	if err != nil {
		return "", err
	}
	if me, _, ok := ourMember(b.Members, u); ok && me.Name != "" {
		return me.Name, nil
	}
	return calendarID, nil
}

// CalendarCreate makes a calendar.
//
// A url makes it a subscribed one: Proton fetches that address on a schedule and
// fills the calendar from it, and the calendar is read-only here because what it
// holds belongs to whoever publishes it. Everything else about creating one is
// the same, keys included.
func (s *Service) CalendarCreate(ctx context.Context, name, color, url string) (string, error) {
	u, err := s.keys(ctx)
	if err != nil {
		return "", err
	}
	addrRings, addr, err := u.PrimaryAddr()
	if err != nil {
		return "", err
	}
	if url != "" {
		if err := s.validateSubscription(ctx, url); err != nil {
			return "", err
		}
	}
	body := map[string]any{"Name": name, "Color": color, "Display": 1, "AddressID": addr.ID}
	if url != "" {
		body["URL"] = url
	}
	var r struct{ Calendar struct{ ID string } }
	if err := s.C.Decode(ctx, proton.Request{
		Method: "POST", Path: "/calendar/v1", Body: body,
	}, &r); err != nil {
		return "", err
	}

	// A freshly created calendar has no keys; provision them (setupCalendar)
	// so the calendar can hold events and accept member updates.
	payload, err := pgphelper.GenerateCalendarKey(addrRings.Write)
	if err != nil {
		return "", fmt.Errorf("generate calendar key: %w", err)
	}
	if err := s.C.Decode(ctx, proton.Request{
		Method: "POST", Path: "/calendar/v1/" + r.Calendar.ID + "/keys",
		Body: map[string]any{
			"AddressID":  addr.ID,
			"PrivateKey": payload.PrivateKey,
			"Passphrase": map[string]any{"DataPacket": payload.DataPacket, "KeyPacket": payload.KeyPacket},
			"Signature":  payload.Signature,
		},
	}, nil); err != nil {
		return "", fmt.Errorf("set up calendar keys: %w", err)
	}
	return r.Calendar.ID, nil
}

// CalendarDelete removes a calendar from the account.
//
// Proton removes a holidays calendar through a route of its own, which asks for
// no password: the calendar is Proton's, and what goes is only your membership
// of it. Deleting a calendar of your own is guarded by a re-authentication the
// client performs when Proton asks for one.
func (s *Service) CalendarDelete(ctx context.Context, cal Calendar) error {
	if cal.Kind == KindHolidays {
		return s.C.Decode(ctx, proton.Request{Method: "DELETE", Path: "/calendar/v1/" + cal.ID + "/managed"}, nil)
	}
	return s.C.Decode(ctx, proton.Request{Method: "DELETE", Path: "/calendar/v1/" + cal.ID}, nil)
}

// CalendarLeave gives up a calendar somebody shared with you, which ends your
// membership of it.
func (s *Service) CalendarLeave(ctx context.Context, cal Calendar) error {
	return s.C.Decode(ctx, proton.Request{
		Method: "DELETE", Path: "/calendar/v1/" + cal.ID + "/members/" + cal.memberID,
	}, nil)
}

func (s *Service) calendarMemberID(ctx context.Context, calendarID string) (string, error) {
	u, err := s.keys(ctx)
	if err != nil {
		return "", err
	}
	b, err := s.calendarBootstrap(ctx, calendarID)
	if err != nil {
		return "", err
	}
	if me, _, ok := ourMember(b.Members, u); ok {
		return me.ID, nil
	}
	return "", fmt.Errorf("no matching member for calendar %s", calendarID)
}

// CalendarRename updates a calendar's display name and/or color (stored as
// per-member settings). Empty fields are left unchanged.
func (s *Service) CalendarRename(ctx context.Context, calendarID, name, color string) error {
	memberID, err := s.calendarMemberID(ctx, calendarID)
	if err != nil {
		return err
	}
	body := map[string]any{}
	if name != "" {
		body["Name"] = name
	}
	if color != "" {
		body["Color"] = color
	}
	return s.C.Decode(ctx, proton.Request{
		Method: "PUT", Path: fmt.Sprintf("/calendar/v1/%s/members/%s", calendarID, memberID), Body: body,
	}, nil)
}

// ResolveCalendarID turns a name or an ID into an ID.
//
// An ID is already the answer, so it is returned without asking: a reference that
// names the calendar outright should not cost the list of all of them. A name has
// to be looked up.
func (s *Service) ResolveCalendarID(ctx context.Context, nameOrID string) (string, error) {
	if ref.Full(nameOrID) {
		return nameOrID, nil
	}
	cals, err := s.CalendarsList(ctx)
	if err != nil {
		return "", err
	}
	for _, c := range cals {
		if c.ID == nameOrID {
			return c.ID, nil
		}
	}
	for _, c := range cals {
		if strings.EqualFold(c.Name, nameOrID) {
			return c.ID, nil
		}
	}
	return "", &errs.NotFound{Kind: "calendar", Ref: nameOrID}
}

// subscriptionStatuses are what Proton says about an address it was asked to
// subscribe to, spelled as the reason somebody can act on.
//
// The numbers are Proton's own (CALENDAR_SUBSCRIPTION_STATUS): a first block
// about the calendar it found, and a second, from twenty up, about the request
// it made to fetch it.
var subscriptionStatuses = map[int]string{
	1:  "Proton could not read a calendar there",
	2:  "that address does not hold a calendar file Proton can read",
	3:  "that calendar has been deleted",
	4:  "there is no calendar at that address",
	5:  "the account that published it no longer exists",
	6:  "the calendar there is larger than Proton will take",
	7:  "that calendar is still being fetched, so try again shortly",
	8:  "that calendar has no key to read it with",
	20: "the address could not be fetched",
	21: "the server there refused the request",
	22: "that address needs credentials Proton does not have",
	23: "the server there refused access",
	24: "there is nothing at that address",
	25: "the server there failed",
	26: "the address took too long to answer",
	27: "that Proton calendar link no longer works",
	28: "that Proton calendar cannot be read",
	30: "that is not a valid address",
}

// validateSubscription asks Proton whether it can read a calendar at that
// address.
func (s *Service) validateSubscription(ctx context.Context, url string) error {
	var r struct {
		ValidationResult struct {
			Result int
			// Reason is Proton's own account of what went wrong, and it is more
			// specific than any of the words below - "Received HTTP 404 from
			// accessing URL" says which of the many ways an address can fail.
			Reason string
		}
	}
	if err := s.C.Decode(ctx, proton.Request{
		Method: "POST", Path: "/calendar/v1/subscription/validate",
		// Mode 0 is download and parse, which is the only answer worth having:
		// an address that resolves but holds nothing is the common mistake.
		Body: map[string]any{"URL": url, "Mode": 0},
	}, &r); err != nil {
		return err
	}
	if r.ValidationResult.Result == 0 {
		return nil
	}
	hint := "the address has to serve an .ics file Proton can fetch"
	if r.ValidationResult.Reason != "" {
		hint = r.ValidationResult.Reason
	}
	if why, ok := subscriptionStatuses[r.ValidationResult.Result]; ok {
		return errs.Problemf("%s.", why).Hint(hint)
	}
	// A number this version has not been told about is still a refusal, and
	// saying which one is more use than a sentence that hides it.
	return errs.Problemf("Proton would not subscribe to that address (reason %d).",
		r.ValidationResult.Result).Hint(hint)
}

// ── per-calendar defaults ──

// CalendarDefaults are the settings a calendar applies to events made in it, and
// what it tells other people about your availability.
//
// They are per-calendar rather than per-account because that is where Proton
// keeps them: a work calendar can default to half-hour meetings with a
// fifteen-minute reminder while a personal one does not.
type CalendarDefaults struct {
	// Duration is how long a new event lasts by default, in minutes.
	Duration int `json:"default_duration_minutes"`
	// Reminders and AllDayReminders are the defaults for an event with a time of
	// day and one without, spelled the way --remind accepts them.
	Reminders       []string `json:"default_reminders"`
	AllDayReminders []string `json:"default_all_day_reminders"`
	// Busy says whether events in this calendar make you look busy to people who
	// check your availability.
	Busy bool `json:"shows_as_busy"`
}

type rawCalendarSettings struct {
	DefaultEventDuration        int
	DefaultPartDayNotifications []rawNotification
	DefaultFullDayNotifications []rawNotification
	MakesUserBusy               int
}

func (s *Service) CalendarDefaults(ctx context.Context, calendarID string) (*CalendarDefaults, error) {
	var r struct{ CalendarSettings rawCalendarSettings }
	if err := s.C.Decode(ctx, proton.Request{
		Method: "GET", Path: "/calendar/v1/" + calendarID + "/settings",
	}, &r); err != nil {
		return nil, err
	}
	cs := r.CalendarSettings
	return &CalendarDefaults{
		Duration:        cs.DefaultEventDuration,
		Reminders:       reminderTexts(cs.DefaultPartDayNotifications),
		AllDayReminders: reminderTexts(cs.DefaultFullDayNotifications),
		Busy:            cs.MakesUserBusy == 1,
	}, nil
}

func reminderTexts(notifs []rawNotification) []string {
	out := make([]string, 0, len(notifs))
	for _, n := range notifs {
		out = append(out, ReminderText(n.Type, n.Trigger))
	}
	return out
}

// DefaultsPatch changes only what it names, so setting a duration does not clear
// the reminders somebody configured in the web client.
type DefaultsPatch struct {
	Duration        *int
	Reminders       *[]string
	AllDayReminders *[]string
	Busy            *bool
}

func (s *Service) CalendarDefaultsUpdate(ctx context.Context, calendarID string, p DefaultsPatch) error {
	body := map[string]any{}
	if p.Duration != nil {
		body["DefaultEventDuration"] = *p.Duration
	}
	if p.Busy != nil {
		body["MakesUserBusy"] = boolBit(*p.Busy)
	}
	for _, part := range []struct {
		field string
		spec  *[]string
	}{
		{"DefaultPartDayNotifications", p.Reminders},
		{"DefaultFullDayNotifications", p.AllDayReminders},
	} {
		if part.spec == nil {
			continue
		}
		notifs, err := buildReminders(*part.spec)
		if err != nil {
			return err
		}
		// An empty list is meaningful - it is how the defaults are turned off -
		// so it is sent as one rather than dropped.
		if notifs == nil {
			notifs = []map[string]any{}
		}
		body[part.field] = notifs
	}
	if len(body) == 0 {
		return nil
	}
	return s.C.Decode(ctx, proton.Request{
		Method: "PUT", Path: "/calendar/v1/" + calendarID + "/settings", Body: body,
	}, nil)
}

func boolBit(b bool) int {
	if b {
		return 1
	}
	return 0
}
