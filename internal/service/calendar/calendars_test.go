package calendar

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/roman-16/proton-cli/internal/proton"
)

// A calendar is one of the kinds Proton's clients sort calendars into, and
// which it is follows from its type and from whether the address that owns it is
// the account's own membership.
func TestACalendarsKindIsTheOneProtonsClientsSortItInto(t *testing.T) {
	for _, tc := range []struct {
		name                string
		listed              listed
		kind, owner, access string
	}{
		{"a personal calendar of your own", listed{id: "a"}, KindPersonal, "", ""},
		{"a personal calendar shared with you to read",
			listed{id: "b", owner: "jane@proton.me", perms: permViewer}, KindShared, "jane@proton.me", accessViewer},
		{"a personal calendar shared with you to edit",
			listed{id: "c", owner: "jane@proton.me", perms: permEditor}, KindShared, "jane@proton.me", accessEditor},
		{"a subscribed calendar", listed{id: "d", typ: typeSubscribed}, KindSubscribed, "", ""},
		{"a holidays calendar", listed{id: "e", typ: typeHolidays, owner: "holidays@proton.me"}, KindHolidays, "", ""},
		{"a type this version has not been told about", listed{id: "f", typ: 7}, "type 7", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cals := listCalendars(t, "", tc.listed)
			if len(cals) != 1 {
				t.Fatalf("listed %d calendars, want 1", len(cals))
			}
			got := cals[0]
			if got.Kind != tc.kind || got.Owner != tc.owner || got.Access != tc.access {
				t.Errorf("kind %q, owner %q, access %q; want %q, %q, %q",
					got.Kind, got.Owner, got.Access, tc.kind, tc.owner, tc.access)
			}
			raw, err := json.Marshal(got)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(raw), "member") {
				t.Errorf("a calendar reports a count of its members, which the listing cannot know: %s", raw)
			}
		})
	}
}

// Events go into your own calendars and into ones shared with you to edit, and
// only a personal calendar of your own is shared or published. Every refusal
// names the calendar beside the error, so the log can leave the name out.
func TestWhatACalendarTakesFollowsFromItsKind(t *testing.T) {
	cals := listCalendars(t, "",
		listed{id: "mine", name: "Personal"},
		listed{id: "view", name: "Team", owner: "jane@proton.me", perms: permViewer},
		listed{id: "edit", name: "Shared work", owner: "jane@proton.me", perms: permEditor},
		listed{id: "sub", name: "Timetable", typ: typeSubscribed},
		listed{id: "hol", name: "Holidays in Austria", typ: typeHolidays, owner: "holidays@proton.me"},
	)
	takes := map[string]string{
		"mine": "", "edit": "",
		"view": "You can only view Team, which jane@proton.me shared with you.",
		"sub":  "Timetable is read-only: its events come from the address it follows.",
		"hol":  "Holidays in Austria is read-only: its events are Proton's.",
	}
	shares := map[string]string{
		"mine": "",
		"view": "Team is jane@proton.me's, and only they can share or publish it.",
		"edit": "Shared work is jane@proton.me's, and only they can share or publish it.",
		"sub":  "Timetable is a subscribed calendar, which cannot be shared or published.",
		"hol":  "Holidays in Austria is a holidays calendar, which cannot be shared or published.",
	}
	for _, cal := range cals {
		assertRefusal(t, cal, "take events", cal.TakesEvents(), takes[cal.ID])
		assertRefusal(t, cal, "be shared", cal.Shareable(), shares[cal.ID])
		if cal.Writable() != (takes[cal.ID] == "") {
			t.Errorf("%s writable = %v", cal.ID, cal.Writable())
		}
	}
}

func assertRefusal(t *testing.T, cal Calendar, what string, err error, want string) {
	t.Helper()
	if want == "" {
		if err != nil {
			t.Errorf("%s may not %s: %v", cal.ID, what, err)
		}
		return
	}
	if err == nil || err.Error() != want {
		t.Errorf("%s asked to %s: %v, want %q", cal.ID, what, err, want)
		return
	}
	var private *errs.Private
	if !errors.As(err, &private) || private.Name != cal.Name {
		t.Errorf("%s's refusal to %s does not carry the calendar's name beside it", cal.ID, what)
	}
}

// A new event goes where Proton's clients put it: the default calendar when that
// one is in use and takes events, otherwise the first that does - your own before
// any shared with you, each by the priority it was given.
func TestANewEventGoesToTheDefaultCalendarWhenThatOneTakesEvents(t *testing.T) {
	all := []listed{
		{id: "hol", typ: typeHolidays, owner: "holidays@proton.me"},
		{id: "team", owner: "jane@proton.me", perms: permEditor, priority: 1},
		{id: "off", priority: 0, disabled: true},
		{id: "a", priority: 3},
		{id: "b", priority: 2},
	}
	for _, tc := range []struct {
		name, defaultID string
		cals            []listed
		want            string
	}{
		{"the default takes events", "a", all, "a"},
		{"no default is set", "", all, "b"},
		{"the default is a calendar nothing is written to", "hol", all, "b"},
		{"the default is switched off", "off", all, "b"},
		{"the default is shared with you to edit", "team", all, "team"},
		{"nothing of your own takes events", "", all[:3], "team"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := servingCalendars(tc.defaultID, tc.cals...).DefaultCalendar(context.Background())
			if err != nil {
				t.Fatalf("DefaultCalendar: %v", err)
			}
			if got.ID != tc.want {
				t.Errorf("chose %q, want %q", got.ID, tc.want)
			}
		})
	}

	_, err := servingCalendars("", all[0], all[2]).DefaultCalendar(context.Background())
	if !errors.Is(err, ErrNoCalendarTakesEvents) {
		t.Errorf("an account where nothing takes events answered %v", err)
	}
}

// Deleting the default calendar hands the role to the first calendar of your own
// that is left, as Proton's clients do, or to nothing when none is.
func TestDeletingTheDefaultCalendarHandsTheDefaultOn(t *testing.T) {
	own := []listed{
		{id: "a", priority: 1},
		{id: "b", priority: 2},
		{id: "hol", typ: typeHolidays, owner: "holidays@proton.me"},
	}
	for _, tc := range []struct {
		name, defaultID string
		deleting        []string
		moves           bool
		next            string
	}{
		{"the default goes", "a", []string{"a"}, true, "b"},
		{"another calendar goes", "a", []string{"b"}, false, ""},
		{"the calendar standing in for an unset default goes", "", []string{"a"}, true, "b"},
		{"every calendar of your own goes", "a", []string{"a", "b"}, true, ""},
		{"only a holidays calendar goes", "b", []string{"hol"}, false, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			next, moves, err := servingCalendars(tc.defaultID, own...).DefaultAfter(context.Background(), tc.deleting)
			if err != nil {
				t.Fatalf("DefaultAfter: %v", err)
			}
			if moves != tc.moves {
				t.Fatalf("moves = %v, want %v", moves, tc.moves)
			}
			got := ""
			if next != nil {
				got = next.ID
			}
			if got != tc.next {
				t.Errorf("the default goes to %q, want %q", got, tc.next)
			}
		})
	}
}

// A holidays calendar is removed through Proton's own route for one, a shared
// one is left by ending the membership, and the default is set or cleared in the
// account's calendar settings.
func TestEachKindOfCalendarIsRemovedByItsOwnRoute(t *testing.T) {
	s := servingCalendars("",
		listed{id: "mine"},
		listed{id: "team", owner: "jane@proton.me", perms: permViewer},
		listed{id: "hol", typ: typeHolidays, owner: "holidays@proton.me"},
	)
	cals, err := s.CalendarsList(context.Background())
	if err != nil {
		t.Fatalf("CalendarsList: %v", err)
	}
	byID := map[string]Calendar{}
	for _, c := range cals {
		byID[c.ID] = c
	}
	ctx := context.Background()
	if err := s.CalendarDelete(ctx, byID["hol"]); err != nil {
		t.Fatal(err)
	}
	if err := s.CalendarDelete(ctx, byID["mine"]); err != nil {
		t.Fatal(err)
	}
	if err := s.CalendarLeave(ctx, byID["team"]); err != nil {
		t.Fatal(err)
	}
	if err := s.CalendarSetDefault(ctx, ""); err != nil {
		t.Fatal(err)
	}
	var sent []string
	var cleared bool
	for _, r := range s.C.(*routeDoer).reqs {
		if r.Method == "GET" {
			continue
		}
		sent = append(sent, r.Method+" "+r.Path)
		if body, ok := r.Body.(map[string]any); ok && r.Path == "/settings/calendar" {
			v, present := body["DefaultCalendarID"]
			cleared = present && v == nil
		}
	}
	want := []string{
		"DELETE /calendar/v1/hol/managed",
		"DELETE /calendar/v1/mine",
		"DELETE /calendar/v1/team/members/m-team",
		"PUT /settings/calendar",
	}
	if strings.Join(sent, "\n") != strings.Join(want, "\n") {
		t.Errorf("sent:\n%s\nwant:\n%s", strings.Join(sent, "\n"), strings.Join(want, "\n"))
	}
	if !cleared {
		t.Error("clearing the default did not send DefaultCalendarID as null")
	}
}

// listed is one calendar as the account's list of calendars carries it, with
// the account's own membership of it as that membership's only one.
type listed struct {
	id, name, owner string
	typ, perms      int
	priority        int
	disabled        bool
}

func calendarListing(cals ...listed) []byte {
	const me = "me@proton.me"
	out := make([]map[string]any, 0, len(cals))
	for _, c := range cals {
		owner, name, flags := c.owner, c.name, flagActive
		if owner == "" {
			owner = me
		}
		if name == "" {
			name = c.id
		}
		if c.disabled {
			flags |= flagSelfDisabled
		}
		out = append(out, map[string]any{
			"ID": c.id, "Type": c.typ, "Owner": map[string]any{"Email": owner},
			"Members": []map[string]any{{
				"ID": "m-" + c.id, "Email": me, "Name": name, "Color": "#8080FF",
				"Permissions": c.perms, "Flags": flags, "Priority": c.priority,
			}},
		})
	}
	raw, _ := json.Marshal(map[string]any{"Calendars": out})
	return raw
}

// servingCalendars is a service whose account holds these calendars and has
// defaultID as its default calendar.
func servingCalendars(defaultID string, cals ...listed) *Service {
	return New(&routeDoer{handler: func(r proton.Request) ([]byte, error) {
		switch r.Path {
		case "/calendar/v1":
			return calendarListing(cals...), nil
		case "/settings/calendar":
			var id any
			if defaultID != "" {
				id = defaultID
			}
			return json.Marshal(map[string]any{"CalendarUserSettings": map[string]any{"DefaultCalendarID": id}})
		}
		return []byte(`{}`), nil
	}}, testKeys(nil))
}

func listCalendars(t *testing.T, defaultID string, cals ...listed) []Calendar {
	t.Helper()
	out, err := servingCalendars(defaultID, cals...).CalendarsList(context.Background())
	if err != nil {
		t.Fatalf("CalendarsList: %v", err)
	}
	return out
}
