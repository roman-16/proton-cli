package calendar

import (
	"context"
	"fmt"
	"strings"

	pgphelper "github.com/roman-16/proton-cli/internal/crypto/pgp"
	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/roman-16/proton-cli/internal/proton"
	"github.com/roman-16/proton-cli/internal/ref"
)

type Calendar struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Color       string `json:"color"`
	Description string `json:"description,omitempty"`
	MemberCount int    `json:"member_count"`
	// Kind is what fills the calendar: your own events, an address Proton
	// fetches, or a holiday feed Proton maintains. The last two are read-only
	// here, because what they hold belongs to whoever publishes it.
	Kind string `json:"kind"`
}

// calendarKinds are Proton's own numbering.
var calendarKinds = map[int]string{0: "personal", 1: "subscribed", 2: "holidays"}

// kindOf names a calendar's type, falling back to the number for one this
// version has not been told about rather than calling it personal.
func kindOf(t int) string {
	if name, ok := calendarKinds[t]; ok {
		return name
	}
	return fmt.Sprintf("type %d", t)
}

// CalendarsList reads per-user prefs (Name/Color/Description) from Members[0].
func (s *Service) CalendarsList(ctx context.Context) ([]Calendar, error) {
	return s.calendars.Do("", func() ([]Calendar, error) {
		var r struct {
			Calendars []struct {
				ID      string
				Type    int
				Members []member
			}
		}
		if err := s.C.Decode(ctx, proton.Request{Method: "GET", Path: "/calendar/v1"}, &r); err != nil {
			return nil, err
		}
		out := make([]Calendar, 0, len(r.Calendars))
		for _, c := range r.Calendars {
			var name, color, desc string
			if len(c.Members) > 0 {
				name = c.Members[0].Name
				color = c.Members[0].Color
				desc = c.Members[0].Description
			}
			out = append(out, Calendar{
				ID: c.ID, Name: name, Color: color, Description: desc,
				MemberCount: len(c.Members), Kind: kindOf(c.Type),
			})
		}
		return out, nil
	})
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

// CalendarDelete requires the caller to have unlocked the password scope first.
func (s *Service) CalendarDelete(ctx context.Context, id string) error {
	return s.C.Decode(ctx, proton.Request{Method: "DELETE", Path: "/calendar/v1/" + id}, nil)
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
// to be looked up, and nothing named at all means the first one.
func (s *Service) ResolveCalendarID(ctx context.Context, nameOrID string) (string, error) {
	if ref.Full(nameOrID) {
		return nameOrID, nil
	}
	cals, err := s.CalendarsList(ctx)
	if err != nil {
		return "", err
	}
	if nameOrID == "" {
		if len(cals) == 0 {
			return "", &errs.NotFound{Kind: "calendar"}
		}
		return cals[0].ID, nil
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
