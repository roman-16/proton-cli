package meet

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	pgp "github.com/ProtonMail/gopenpgp/v3/crypto"
	"github.com/roman-16/proton-cli/internal/account/keys"
	pgphelper "github.com/roman-16/proton-cli/internal/crypto/pgp"
	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/roman-16/proton-cli/internal/proton"
)

// The fixture is the personal room Proton's Meet web app made for a test
// account, read back from the API with the password from its link. The room is
// gone, so the password opens nothing any more.
type webMeeting struct {
	MeetingLinkName string
	Password        string
	Salt            string
	SessionKey      string
	MeetingName     string
	Title           string
}

func TestAMeetingTheWebAppMadeOpens(t *testing.T) {
	raw, err := os.ReadFile("testdata/web_personal_room.json")
	if err != nil {
		t.Fatal(err)
	}
	var room webMeeting
	if err := json.Unmarshal(raw, &room); err != nil {
		t.Fatal(err)
	}
	salt, err := base64.StdEncoding.DecodeString(room.Salt)
	if err != nil {
		t.Fatal(err)
	}
	locked, err := base64.StdEncoding.DecodeString(room.SessionKey)
	if err != nil {
		t.Fatal(err)
	}
	sk, err := unlockSessionKey(locked, room.Password, salt)
	if err != nil {
		t.Fatalf("unlock: %v", err)
	}
	if title, err := openTitle(sk, room.MeetingName); err != nil || title != room.Title {
		t.Errorf("title %q, %v; want %q", title, err, room.Title)
	}
	if _, err := unlockSessionKey(locked, "AAAAAAAAAAAA", salt); err == nil {
		t.Error("a wrong password unlocked the session key")
	}
}

func TestASecretRoundTrips(t *testing.T) {
	sec, err := newSecret()
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`^[A-Za-z0-9]{12}$`).MatchString(sec.password) || len(sec.salt) != 16 {
		t.Fatalf("secret %q with a salt of %d bytes", sec.password, len(sec.salt))
	}
	locked, err := sec.lockedSessionKey()
	if err != nil {
		t.Fatal(err)
	}
	sk, err := unlockSessionKey(locked, sec.password, sec.salt)
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := sealTitle(sk, "Weekly sync")
	if err != nil {
		t.Fatal(err)
	}
	if title, err := openTitle(sec.sessionKey, sealed); err != nil || title != "Weekly sync" {
		t.Errorf("open: %q, %v", title, err)
	}
}

func TestAPasswordCopyNeedsItsContext(t *testing.T) {
	kr := userKeyRing(t)
	armored, err := sealPassword(kr, "Synth3tic4Pw")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := openPassword(kr, armored); err != nil || got != "Synth3tic4Pw" {
		t.Errorf("open: %q, %v", got, err)
	}
	msg, err := pgphelper.EncryptText(kr, kr, "Synth3tic4Pw")
	if err != nil {
		t.Fatal(err)
	}
	plain, err := msg.Armor()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := openPassword(kr, plain); err == nil {
		t.Error("a copy signed without the context opened")
	}
}

func TestParseLink(t *testing.T) {
	for _, tc := range []struct {
		in, name, password string
		ok                 bool
	}{
		{"https://meet.proton.me/join/id-6MCF0BSHBR#pwd-k3Xq9vLm2PzT", "6MCF0BSHBR", "k3Xq9vLm2PzT", true},
		{"meet.proton.me/join/id-6MCF0BSHBR#pwd-k3Xq9vLm2PzT", "6MCF0BSHBR", "k3Xq9vLm2PzT", true},
		{"https://meet.proton.me/join/id-6MCF0BSHBR", "6MCF0BSHBR", "", true},
		{"https://example.com/join/id-6MCF0BSHBR", "", "", false},
		{"6MCF0BSHBR", "", "", false},
	} {
		name, password, ok := ParseLink(tc.in)
		if name != tc.name || password != tc.password || ok != tc.ok {
			t.Errorf("ParseLink(%q) = %q, %q, %v", tc.in, name, password, ok)
		}
	}
}

type meetDoer struct {
	t       *testing.T
	sent    []proton.Request
	answers map[string]any
}

func (d *meetDoer) Do(context.Context, proton.Request) (*proton.Response, error) {
	return nil, errors.New("meetDoer only decodes")
}

func (d *meetDoer) Decode(_ context.Context, r proton.Request, out any) error {
	d.sent = append(d.sent, r)
	key := r.Method + " " + r.Path
	if key == "GET /core/v4/auth/modulus" {
		modulus, err := os.ReadFile("testdata/modulus.asc")
		if err != nil {
			d.t.Fatalf("modulus fixture: %v", err)
		}
		return answer(map[string]any{"Modulus": string(modulus), "ModulusID": "modulus-id"}, out)
	}
	if strings.HasPrefix(r.Path, "/meet/") && r.Door != proton.MeetDoor {
		d.t.Errorf("%s went through door %d, want Meet's", key, r.Door)
	}
	a, ok := d.answers[key]
	if !ok {
		return errors.New("unexpected request " + key)
	}
	if err, ok := a.(error); ok {
		return err
	}
	return answer(a, out)
}

func answer(v, out any) error {
	if out == nil {
		return nil
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, out)
}

func userKeyRing(t *testing.T) *pgp.KeyRing {
	t.Helper()
	key, err := pgphelper.GenerateKey("Test", "test@example.com")
	if err != nil {
		t.Fatal(err)
	}
	kr, err := pgp.NewKeyRing(key)
	if err != nil {
		t.Fatal(err)
	}
	return kr
}

func testAccount(t *testing.T) *keys.Unlocked {
	t.Helper()
	return &keys.Unlocked{
		UserKR: userKeyRing(t),
		Addresses: []keys.Address{
			{ID: "addr-off", Email: "old@proton.me", Order: 1, Status: 0, Send: 1, Receive: 1},
			{ID: "addr-main", Email: "me@proton.me", Order: 2, Status: 1, Send: 1, Receive: 1},
		},
	}
}

func service(d *meetDoer, u *keys.Unlocked) *Service {
	return New(d, func(context.Context) (*keys.Unlocked, error) { return u, nil })
}

func TestCreateSealsWhatTheWebAppSeals(t *testing.T) {
	u := testAccount(t)
	d := &meetDoer{t: t, answers: map[string]any{"POST /meet/v1/meetings": map[string]any{"Meeting": map[string]any{
		"ID": "meeting-id", "MeetingLinkName": "6MCF0BSHBR", "Type": 2,
	}}}}
	m, err := service(d, u).Create(t.Context(), NewMeeting{Title: "Design review", Type: Scheduled, FromCalendar: true})
	if err != nil {
		t.Fatal(err)
	}
	body := d.sent[len(d.sent)-1].Body.(createBody)
	if body.AddressID != "addr-main" || body.ProtonCalendar != 1 || body.Type != 2 || body.StartTime != nil {
		t.Errorf("body = %+v", body)
	}
	name, password, ok := ParseLink(m.URL)
	if !ok || name != "6MCF0BSHBR" || password == "" {
		t.Fatalf("link = %q", m.URL)
	}
	copied, err := openPassword(u.UserKR, body.Password)
	if err != nil || copied != password {
		t.Errorf("the account's copy holds %q, %v; the link %q", copied, err, password)
	}
	salt, _ := base64.StdEncoding.DecodeString(body.Salt)
	locked, _ := base64.StdEncoding.DecodeString(body.SessionKey)
	sk, err := unlockSessionKey(locked, password, salt)
	if err != nil {
		t.Fatal(err)
	}
	if title, err := openTitle(sk, body.Name); err != nil || title != "Design review" {
		t.Errorf("sealed title %q, %v", title, err)
	}
}

func TestListOpensWhatItCanAndCountsTheRest(t *testing.T) {
	u := testAccount(t)
	sec, err := newSecret()
	if err != nil {
		t.Fatal(err)
	}
	locked, _ := sec.lockedSessionKey()
	title, _ := sealTitle(sec.sessionKey, "Team room")
	copied, _ := sealPassword(u.UserKR, sec.password)
	d := &meetDoer{t: t, answers: map[string]any{"GET /meet/v1/meetings/upcoming": map[string]any{"Meetings": []map[string]any{
		{"ID": "a", "MeetingLinkName": "E87W2DAJ60", "MeetingName": title, "Password": copied,
			"Salt": base64.StdEncoding.EncodeToString(sec.salt), "SessionKey": base64.StdEncoding.EncodeToString(locked), "Type": 4},
		{"ID": "b", "MeetingLinkName": "AAAAAAAAAA", "Type": 4},
	}}}}
	got, err := service(d, u).List(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Title != "Team room" || got[0].URL != Link("E87W2DAJ60", sec.password) || got[0].Type != Room {
		t.Errorf("list = %+v", got)
	}
}

func TestRotatingTooSoonIsASentence(t *testing.T) {
	d := &meetDoer{t: t, answers: map[string]any{
		"POST /meet/v1/meetings/personal/rotate": &proton.APIError{HTTPStatus: 422, Code: 2028, Message: "can only be rotated once every 24 hours"},
	}}
	_, err := service(d, testAccount(t)).RotatePersonal(t.Context(), "Personal meeting room")
	var p *errs.Problem
	if !errors.As(err, &p) {
		t.Errorf("err = %v, want a problem phrased for a person", err)
	}
}

func TestNoPersonalRoomSaysHowToGetOne(t *testing.T) {
	d := &meetDoer{t: t, answers: map[string]any{"GET /meet/v1/meetings/upcoming": map[string]any{"Meetings": []any{}}}}
	_, err := service(d, testAccount(t)).Personal(t.Context())
	var nf *errs.NotFound
	if !errors.As(err, &nf) || len(nf.Try) == 0 {
		t.Errorf("err = %v", err)
	}
}

func TestNextIsTheFirstOccurrenceNotYetOver(t *testing.T) {
	start := time.Date(2026, 10, 21, 8, 0, 0, 0, time.UTC)
	end := start.Add(30 * time.Minute)
	now := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	weekly := Meeting{Type: Recurring, Start: &start, End: &end, RRule: "FREQ=WEEKLY", Timezone: "Europe/Vienna"}
	if got := next(weekly, now); got == nil || !got.Equal(time.Date(2026, 10, 28, 9, 0, 0, 0, time.UTC)) {
		t.Errorf("next = %v", got)
	}
	once := Meeting{Type: Scheduled, Start: &start, End: &end}
	if got := next(once, now); got == nil || !got.Equal(start) {
		t.Errorf("a meeting that happens once is next at its own start, got %v", got)
	}
	over := Meeting{Type: Recurring, Start: &start, End: &end, RRule: "FREQ=DAILY;COUNT=2", Timezone: "UTC"}
	if got := next(over, now); got == nil || !got.Equal(start) {
		t.Errorf("a series that is over keeps its start, got %v", got)
	}
}

func TestAnOrganizationMayKeepMeetFromItsMembers(t *testing.T) {
	for _, tc := range []struct {
		answer any
		want   bool
	}{
		{map[string]any{"AllowedProducts": []string{"All"}}, true},
		{map[string]any{"AllowedProducts": []string{"Mail", "Meet"}}, true},
		{map[string]any{"AllowedProducts": []string{"Mail", "Drive"}}, false},
		{map[string]any{}, true},
		{&proton.APIError{HTTPStatus: 422, Code: 2501}, true},
	} {
		d := &meetDoer{t: t, answers: map[string]any{"GET /core/v4/organizations/settings": tc.answer}}
		if got := service(d, testAccount(t)).Allowed(t.Context()); got != tc.want {
			t.Errorf("%v: %v, want %v", tc.answer, got, tc.want)
		}
	}
}
