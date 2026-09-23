package calendar

import (
	"context"
	"crypto/sha1" //nolint:gosec // matching Proton's attendee-token algorithm
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/roman-16/proton-cli/internal/account/keys"
	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/roman-16/proton-cli/internal/ical"
	"github.com/roman-16/proton-cli/internal/proton"
)

func TestAttendeeTokenMatchesSHA1OfUIDAndCanonicalAddress(t *testing.T) {
	got := attendeeToken("event-uid-42", "alice@proton.me")

	sum := sha1.Sum([]byte("event-uid-42" + "alice@proton.me")) //nolint:gosec
	want := hex.EncodeToString(sum[:])

	if got != want {
		t.Errorf("attendeeToken = %q, want %q", got, want)
	}
	if len(got) != 40 {
		t.Errorf("expected a 40-char hex SHA-1, got %d chars", len(got))
	}
}

// The address is hashed as Proton reduces it, not as it was written. This pair
// is a real event: protonalt.sessions986@proton.me carries the dot, and the
// token Proton resolves an attendee by is the one without it. Hashing the
// written form yields 5adccfab…, which no endpoint accepts.
func TestAttendeeTokenHashesWhatProtonReducedTheAddressTo(t *testing.T) {
	const uid = "1786119460522844246@proton-cli"

	if got, want := attendeeToken(uid, "protonaltsessions986@proton.me"), "150cedec796a1a9fbd35097bc6a63154ad59541e"; got != want {
		t.Errorf("attendeeToken(canonical) = %q, want %q", got, want)
	}
	if got := attendeeToken(uid, "protonalt.sessions986@proton.me"); got == "150cedec796a1a9fbd35097bc6a63154ad59541e" {
		t.Error("the written and canonical forms must not hash alike, or this test proves nothing")
	}
}

func TestICalTriggerConvertsDurationsToNegativeTriggers(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"15m", "-PT15M"},
		{"1h", "-PT60M"},
		{"90m", "-PT90M"},
		{"1d", "-P1D"},
		{"2d", "-P2D"},
	}
	for _, c := range cases {
		got, err := icalTrigger(c.in)
		if err != nil {
			t.Errorf("icalTrigger(%q) returned error: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("icalTrigger(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	if _, err := icalTrigger("0m"); err == nil {
		t.Error("expected an error for a zero-length reminder")
	}
	if _, err := icalTrigger("not-a-duration"); err == nil {
		t.Error("expected an error for an unparseable reminder")
	}
}

func TestBuildRemindersMapsToDeviceNotifications(t *testing.T) {
	out, err := buildReminders([]string{"15m", "1d"})
	if err != nil {
		t.Fatalf("buildReminders error: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("expected 2 notifications, got %d", len(out))
	}
	if out[0]["Type"] != 1 || out[0]["Trigger"] != "-PT15M" {
		t.Errorf("first reminder = %v, want Type 1 / -PT15M", out[0])
	}
	if out[1]["Trigger"] != "-P1D" {
		t.Errorf("second reminder trigger = %v, want -P1D", out[1]["Trigger"])
	}

	if out, _ := buildReminders(nil); out != nil {
		t.Errorf("expected nil for no reminders, got %v", out)
	}
}

// ── RSVP: pure seams ──

func TestStatusFromFlag(t *testing.T) {
	cases := map[string]int{"accept": partstatAccepted, "tentative": partstatTentative, "decline": partstatDeclined}
	for in, want := range cases {
		got, err := StatusFromFlag(in)
		if err != nil || got != want {
			t.Errorf("StatusFromFlag(%q) = %d,%v; want %d,nil", in, got, err, want)
		}
	}
	if _, err := StatusFromFlag("maybe"); err == nil {
		t.Error("StatusFromFlag(\"maybe\") should error")
	}
}

func TestPartstatICS(t *testing.T) {
	cases := map[int]string{
		partstatAccepted:    "ACCEPTED",
		partstatTentative:   "TENTATIVE",
		partstatDeclined:    "DECLINED",
		partstatNeedsAction: "NEEDS-ACTION",
	}
	for status, want := range cases {
		if got := partstatICS(status); got != want {
			t.Errorf("partstatICS(%d) = %q, want %q", status, got, want)
		}
	}
}

func TestReplyBodyMatchesWebClientsWording(t *testing.T) {
	cases := map[int]string{
		partstatAccepted:  "me@proton.me accepted your invitation to Sync",
		partstatTentative: "me@proton.me tentatively accepted your invitation to Sync",
		partstatDeclined:  "me@proton.me declined your invitation to Sync",
	}
	for status, want := range cases {
		if got := replyBody("me@proton.me", status, "Sync"); got != want {
			t.Errorf("replyBody(%d) = %q, want %q", status, got, want)
		}
	}
}

// The organizer is told which event was answered, so the day named has to be the
// day the event is on. A whole-day event carries a date and no instant, so reading
// it as one names the day before in every zone behind UTC.
func TestReplySubjectNamesTheDayTheEventIsOn(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skipf("America/New_York is not available: %v", err)
	}
	saved := time.Local
	time.Local = loc
	t.Cleanup(func() { time.Local = saved })

	day := ical.Day(time.Date(2026, 8, 14, 0, 0, 0, 0, time.UTC))
	if got := replySubject(day); got != "Re: Invitation for an event on 2026-08-14" {
		t.Errorf("replySubject of a whole-day event = %q", got)
	}

	at := time.Date(2026, 8, 14, 9, 0, 0, 0, time.UTC)
	if got := replySubject(ical.Timed(at, "UTC")); got != "Re: Invitation for an event starting on 2026-08-14 05:00" {
		t.Errorf("replySubject of a timed event = %q, want it read in the reader's zone", got)
	}
}

func TestFindSelfAttendee(t *testing.T) {
	uid := "uid-9"
	mine := attendeeToken(uid, "me@proton.me")
	attendees := []rawAttendee{{ID: "other", Token: "deadbeef"}, {ID: "mine", Token: mine, Status: 0}}

	t.Run("matches my token", func(t *testing.T) {
		id, email, ok := findSelfAttendee(map[string]string{mine: "me@proton.me"}, attendees)
		if !ok || id != "mine" || email != "me@proton.me" {
			t.Errorf("got (%q,%q,%v), want (mine,me@proton.me,true)", id, email, ok)
		}
	})
	t.Run("no match", func(t *testing.T) {
		stranger := attendeeToken(uid, "someone@else.com")
		if _, _, ok := findSelfAttendee(map[string]string{stranger: "someone@else.com"}, attendees); ok {
			t.Error("expected no match for an unrelated address")
		}
	})
}

// selfTokens asks the server what each of the account's addresses reduces to,
// so an account whose address carries a dot is named by the same token Proton
// would compute for it.
func TestSelfTokensNamesAddressesByTheirCanonicalForm(t *testing.T) {
	d := &routeDoer{handler: func(r proton.Request) ([]byte, error) {
		return canonicalJSON(map[string]string{"my.self@proton.me": "myself@proton.me"}), nil
	}}

	got, err := New(d, testKeys(nil)).selfTokens(context.Background(), "uid-9", []keys.Address{{Email: "my.self@proton.me"}})
	if err != nil {
		t.Fatalf("selfTokens: %v", err)
	}
	want := attendeeToken("uid-9", "myself@proton.me")
	if email, ok := got[want]; !ok || email != "my.self@proton.me" {
		t.Errorf("expected the canonical token %q to name my.self@proton.me, got %v", want, got)
	}
	if _, ok := got[attendeeToken("uid-9", "my.self@proton.me")]; ok {
		t.Error("the address as written must not produce a token")
	}
}

func TestCanonicalEmailsAsksOnceAndRefusesAnUnanswerableAddress(t *testing.T) {
	t.Run("batches and caches", func(t *testing.T) {
		calls := 0
		d := &routeDoer{handler: func(r proton.Request) ([]byte, error) {
			calls++
			return canonicalJSON(map[string]string{"a.b@proton.me": "ab@proton.me", "c@x.test": "c@x.test"}), nil
		}}
		s := New(d, testKeys(nil))
		if _, err := s.canonicalEmails(context.Background(), []string{"a.b@proton.me", "c@x.test", "a.b@proton.me"}); err != nil {
			t.Fatalf("canonicalEmails: %v", err)
		}
		if _, err := s.canonicalEmails(context.Background(), []string{"a.b@proton.me"}); err != nil {
			t.Fatalf("canonicalEmails again: %v", err)
		}
		if calls != 1 {
			t.Errorf("asked the server %d times, want 1", calls)
		}
		if q := d.reqs[0].Query["Emails[]"]; len(q) != 2 {
			t.Errorf("asked for %v, want both addresses once each", q)
		}
	})

	t.Run("an unanswered address is an error, not the address as written", func(t *testing.T) {
		d := &routeDoer{handler: func(r proton.Request) ([]byte, error) {
			return canonicalJSON(nil), nil
		}}
		if _, err := New(d, testKeys(nil)).canonicalEmails(context.Background(), []string{"who@x.test"}); err == nil {
			t.Fatal("expected an error when the server does not answer for an address")
		}
	})
}

// ── RSVP: EventRespond wiring (router fake Doer) ──

// routeDoer routes requests by a handler and records every request, so tests
// can assert the exact partstat PUT without a live API or PGP fixtures.
type routeDoer struct {
	reqs    []proton.Request
	handler func(proton.Request) ([]byte, error)
}

func (d *routeDoer) Do(_ context.Context, r proton.Request) (*proton.Response, error) {
	d.reqs = append(d.reqs, r)
	body, err := d.handler(r)
	if err != nil {
		return nil, err
	}
	return &proton.Response{Status: 200, Body: body}, nil
}

func (d *routeDoer) Decode(_ context.Context, r proton.Request, out any) error {
	d.reqs = append(d.reqs, r)
	body, err := d.handler(r)
	if err != nil {
		return err
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(body, out)
}

func (d *routeDoer) putTo(substr string) (proton.Request, bool) {
	for _, r := range d.reqs {
		if r.Method == "PUT" && strings.Contains(r.Path, substr) {
			return r, true
		}
	}
	return proton.Request{}, false
}

// canonicalJSON builds the canonical-address endpoint's answer.
func canonicalJSON(m map[string]string) []byte {
	var responses []map[string]any
	for email, canonical := range m {
		responses = append(responses, map[string]any{
			"Email":    email,
			"Response": map[string]any{"CanonicalEmail": canonical, "Code": 1000},
		})
	}
	b, _ := json.Marshal(map[string]any{"Code": 1001, "Responses": responses})
	return b
}

// eventJSON builds a canned single-event GET body with one attendee.
func eventJSON(t *testing.T, uid string, isOrganizer int, attendees []map[string]any) []byte {
	t.Helper()
	b, err := json.Marshal(map[string]any{"Event": map[string]any{
		"ID": "ev1", "CalendarID": "cal1", "UID": uid,
		"IsOrganizer": isOrganizer, "IsProtonProtonInvite": 0,
		"StartTime": 1000, "EndTime": 2000, "FullDay": 0,
		"AttendeesInfo": map[string]any{"Attendees": attendees, "MoreAttendees": 0},
		"SharedEvents":  []any{},
	}})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestEventRespondUpdatesPartstat(t *testing.T) {
	uid := "uid-1"
	event := eventJSON(t, uid, 0, []map[string]any{{"ID": "att1", "Token": attendeeToken(uid, "me@proton.me"), "Status": 0}})
	d := &routeDoer{handler: func(r proton.Request) ([]byte, error) {
		switch {
		case r.Method == "GET" && strings.HasSuffix(r.Path, "/events/ev1"):
			return event, nil
		case r.Method == "PUT" && strings.Contains(r.Path, "/attendees/att1"):
			return []byte(`{"Code":1000}`), nil
		case strings.Contains(r.Path, "/addresses/canonical"):
			return canonicalJSON(map[string]string{"me@proton.me": "me@proton.me"}), nil
		default:
			// Members lookup etc.: empty so the best-effort reply build bails.
			return []byte(`{}`), nil
		}
	}}
	u := &keys.Unlocked{Addresses: []keys.Address{{ID: "a1", Email: "me@proton.me"}}}

	res, err := New(d, testKeys(u)).EventRespond(context.Background(), "cal1", "ev1", partstatAccepted)
	if err != nil {
		t.Fatalf("EventRespond: %v", err)
	}
	if res.Status != "accepted" {
		t.Errorf("Status = %q, want accepted", res.Status)
	}
	put, ok := d.putTo("/calendar/v1/cal1/events/ev1/attendees/att1")
	if !ok {
		t.Fatalf("expected a PUT to the attendee endpoint; got %d requests", len(d.reqs))
	}
	body, ok := put.Body.(map[string]any)
	if !ok {
		t.Fatalf("PUT body is not map[string]any: %T", put.Body)
	}
	if body["Status"] != partstatAccepted {
		t.Errorf("PUT Status = %v, want %d", body["Status"], partstatAccepted)
	}
	if ut, _ := body["UpdateTime"].(int64); ut <= 0 {
		t.Errorf("PUT UpdateTime = %v, want a positive unix time", body["UpdateTime"])
	}
}

func TestEventRespondRejectsOrganizer(t *testing.T) {
	event := eventJSON(t, "uid-1", 1, nil)
	d := &routeDoer{handler: func(r proton.Request) ([]byte, error) { return event, nil }}
	u := &keys.Unlocked{Addresses: []keys.Address{{Email: "me@proton.me"}}}

	_, err := New(d, testKeys(u)).EventRespond(context.Background(), "cal1", "ev1", partstatAccepted)
	if err == nil {
		t.Fatal("expected an error when responding as the organizer")
	}
	var coder errs.ExitCoder
	if !errors.As(err, &coder) || coder.ExitCode() != 1 {
		t.Errorf("expected exit 1 (user error), got %v", err)
	}
	if _, ok := d.putTo("/attendees/"); ok {
		t.Error("must not update partstat when the caller is the organizer")
	}
}

func TestEventRespondNotAttendee(t *testing.T) {
	uid := "uid-1"
	event := eventJSON(t, uid, 0, []map[string]any{{"ID": "someoneelse", "Token": "deadbeef", "Status": 0}})
	d := &routeDoer{handler: func(r proton.Request) ([]byte, error) {
		if strings.Contains(r.Path, "/addresses/canonical") {
			return canonicalJSON(map[string]string{"me@proton.me": "me@proton.me"}), nil
		}
		return event, nil
	}}
	u := &keys.Unlocked{Addresses: []keys.Address{{Email: "me@proton.me"}}}

	_, err := New(d, testKeys(u)).EventRespond(context.Background(), "cal1", "ev1", partstatAccepted)
	var nf *errs.NotFound
	if !errors.As(err, &nf) {
		t.Fatalf("expected *errs.NotFound (exit 3), got %v", err)
	}
	if _, ok := d.putTo("/attendees/"); ok {
		t.Error("must not update partstat when the caller is not an attendee")
	}
}

// testKeys hands a service the key hierarchy a test wants it to decrypt with.
// A test that decrypts nothing passes nil, which is never asked for.
func testKeys(u *keys.Unlocked) keys.Get {
	return func(context.Context) (*keys.Unlocked, error) {
		if u == nil {
			return nil, errors.New("this test has no keys")
		}
		return u, nil
	}
}

// A reminder says how it is delivered by what follows the colon, and a bare
// duration is the notification the composer defaults to.
func TestBuildRemindersCarriesTheDeliveryKind(t *testing.T) {
	out, err := buildReminders([]string{"15m", "1d:email", "2h:notification"})
	if err != nil {
		t.Fatalf("buildReminders: %v", err)
	}
	want := []struct {
		kind    int
		trigger string
	}{{1, "-PT15M"}, {0, "-P1D"}, {1, "-PT120M"}}
	for i, w := range want {
		if out[i]["Type"] != w.kind || out[i]["Trigger"] != w.trigger {
			t.Errorf("reminder %d = %v, want Type %d Trigger %s", i, out[i], w.kind, w.trigger)
		}
	}
	if _, err := buildReminders([]string{"15m:carrier-pigeon"}); err == nil {
		t.Error("an unknown delivery should be refused")
	}
}

// What a listing prints is what --remind would accept, so an event can be read
// and recreated without translating anything by hand.
func TestReminderTextRoundTripsThroughTheTrigger(t *testing.T) {
	for _, spec := range []string{"15m", "1h", "1d", "2d:email", "90m:email"} {
		built, err := buildReminders([]string{spec})
		if err != nil {
			t.Fatalf("%q: %v", spec, err)
		}
		got := ReminderText(built[0]["Type"].(int), built[0]["Trigger"].(string))
		again, err := buildReminders([]string{got})
		if err != nil {
			t.Fatalf("%q rendered as %q, which is not accepted: %v", spec, got, err)
		}
		if again[0]["Trigger"] != built[0]["Trigger"] || again[0]["Type"] != built[0]["Type"] {
			t.Errorf("%q rendered as %q, which means something else: %v", spec, got, again[0])
		}
	}
}

// Proton's other clients write triggers this one does not, so reading has to
// cover more shapes than writing.
func TestTriggerDurationReadsWhatOtherClientsWrite(t *testing.T) {
	for _, c := range []struct {
		trigger string
		want    time.Duration
	}{
		{"-PT15M", 15 * time.Minute},
		{"-P1D", 24 * time.Hour},
		{"-P1W", 7 * 24 * time.Hour},
		{"-PT1H30M", 90 * time.Minute},
		{"-P1DT2H", 26 * time.Hour},
		{"-PT30S", 30 * time.Second},
	} {
		if got := triggerDuration(c.trigger); got != c.want {
			t.Errorf("triggerDuration(%q) = %v, want %v", c.trigger, got, c.want)
		}
	}
}

// An attendee says how much their presence matters by what follows the colon,
// and a bare address is required - which is what inviting someone ordinarily
// means.
func TestParseAttendeeReadsTheSuffix(t *testing.T) {
	for _, c := range []struct{ in, email, role string }{
		{"jane@example.com", "jane@example.com", roleRequired},
		{"jane@example.com:optional", "jane@example.com", roleOptional},
		{"jane@example.com:required", "jane@example.com", roleRequired},
		{"  jane@example.com : optional ", "jane@example.com", roleOptional},
	} {
		email, role, err := ParseAttendee(c.in)
		if err != nil {
			t.Errorf("ParseAttendee(%q): %v", c.in, err)
			continue
		}
		if email != c.email || role != c.role {
			t.Errorf("ParseAttendee(%q) = %q/%q, want %q/%q", c.in, email, role, c.email, c.role)
		}
	}
	if _, _, err := ParseAttendee("jane@example.com:maybe"); err == nil {
		t.Error("an unknown role should be refused rather than silently ignored")
	}
}

// What a listing prints is what --attendee accepts, so a participant can be read
// back into a command.
func TestAttendeeTextRoundTrips(t *testing.T) {
	for _, in := range []string{"jane@example.com", "jane@example.com:optional"} {
		email, role, err := ParseAttendee(in)
		if err != nil {
			t.Fatalf("%q: %v", in, err)
		}
		if got := AttendeeText(email, role); got != in {
			t.Errorf("AttendeeText round-tripped %q as %q", in, got)
		}
	}
}
