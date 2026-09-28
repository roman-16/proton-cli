package calendar

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	pgp "github.com/ProtonMail/gopenpgp/v3/crypto"

	"github.com/roman-16/proton-cli/internal/account/keys"
	"github.com/roman-16/proton-cli/internal/account/plan"
	pgphelper "github.com/roman-16/proton-cli/internal/crypto/pgp"
	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/roman-16/proton-cli/internal/proton"
)

func counting(n int) []byte {
	out := make([]byte, 32)
	for i := range out {
		out[i] = byte(n + i)
	}
	return out
}

func TestTheBookingIDAndKeyAreDerivedTheWayProtonsBookingPageDerivesThem(t *testing.T) {
	uid, err := bookingUID(counting(0))
	if err != nil {
		t.Fatal(err)
	}
	if uid != "W7GF1VC486F+IwymQJsfmiVg2aT2HSL8JFSQTeihvXs=" {
		t.Errorf("booking UID = %s", uid)
	}
	password, err := bookingKeyPassword("CAL", counting(0), counting(100))
	if err != nil {
		t.Fatal(err)
	}
	if string(password) != "+sNiLZfwGbHNqZx4xvwR7smr0xIp2MY7jE3a1j86sRY=" {
		t.Errorf("booking key password = %s", password)
	}
	if got := bookingLink(counting(0)); got != "https://calendar.proton.me/bookings#AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8=" {
		t.Errorf("link = %s", got)
	}
}

func TestASlotIsSignedAsTheJSONProtonsBookingPageRebuilds(t *testing.T) {
	weekly, err := slotText(1790595000, 1790596800, "Europe/Vienna", rruleOf(Slot{Weekly: true}))
	if err != nil {
		t.Fatal(err)
	}
	if weekly != `{"EndTime":1790596800,"RRule":"FREQ=WEEKLY","StartTime":1790595000,"Timezone":"Europe/Vienna"}` {
		t.Errorf("weekly slot text = %s", weekly)
	}
	once, err := slotText(1790595000, 1790596800, "America/Argentina/Buenos_Aires", nil)
	if err != nil {
		t.Fatal(err)
	}
	if once != `{"EndTime":1790596800,"RRule":null,"StartTime":1790595000,"Timezone":"America/Argentina/Buenos_Aires"}` {
		t.Errorf("dated slot text = %s", once)
	}
}

func bookingKeys(t *testing.T) *calKeys {
	t.Helper()
	generate := func(name string) *pgp.Key {
		key, err := pgphelper.GenerateKey(name, name+"@proton.me")
		if err != nil {
			t.Fatalf("generate: %v", err)
		}
		return key
	}
	addrKR, err := pgp.NewKeyRing(generate("me"))
	if err != nil {
		t.Fatal(err)
	}
	calKey := generate("calendar")
	calKR, err := pgp.NewKeyRing(calKey)
	if err != nil {
		t.Fatal(err)
	}
	return &calKeys{
		calKR: calKR, addr: keys.Rings{Read: addrKR, Write: addrKR},
		email: "me@proton.me", primary: calKey,
	}
}

func TestAPagesSecretOpensOnlyUnderTheCalendarItWasSealedFor(t *testing.T) {
	ck := bookingKeys(t)
	sealed, err := sealBookingSecret(ck, "cal1", counting(7))
	if err != nil {
		t.Fatal(err)
	}
	secret, verdict, err := openBookingSecret(ck, "cal1", sealed)
	if err != nil || verdict != pgphelper.Verified || string(secret) != string(counting(7)) {
		t.Errorf("open = %x, %s, %v", secret, verdict, err)
	}
	if _, verdict, _ := openBookingSecret(ck, "cal2", sealed); verdict == pgphelper.Verified {
		t.Error("a secret sealed for one calendar verified as another's")
	}
}

func TestAPagesContentOpensWithTheKeyTheLinkDerives(t *testing.T) {
	ck := bookingKeys(t)
	password, err := bookingKeyPassword("cal1", counting(1), counting(2))
	if err != nil {
		t.Fatal(err)
	}
	want := bookingContent{Summary: "Intro <call>", Description: "Line one\nline two", WithProtonMeetLink: true}
	sealed, err := sealBookingContent(ck, password, "uid", want)
	if err != nil {
		t.Fatal(err)
	}
	got, verdict, err := openBookingContent(ck, password, "uid", sealed)
	if err != nil || got != want || verdict != pgphelper.Verified {
		t.Errorf("open = %+v, %s, %v", got, verdict, err)
	}
	if _, verdict, _ := openBookingContent(ck, password, "another uid", sealed); verdict == pgphelper.Verified {
		t.Error("content signed for one page verified as another's")
	}
	if _, _, err := openBookingContent(ck, []byte("wrong"), "uid", sealed); err == nil {
		t.Error("the content opened with the wrong key")
	}
}

func TestEverySignatureOnAPageVerifiesUnderItsOwnContext(t *testing.T) {
	ck := bookingKeys(t)
	slots := []Slot{{Start: 1790595000, End: 1790596800, Zone: "Europe/Vienna", Weekly: true}}
	signed, err := signBookingPage(ck, "cal1", "uid", counting(1), counting(2), bookingContent{Summary: "x"}, slots)
	if err != nil {
		t.Fatal(err)
	}
	if len(signed.slots) != 1 || signed.slots[0]["RRule"].(*string) == nil {
		t.Fatalf("slots = %+v", signed.slots)
	}
	raw := rawBookingSlot{
		StartTime: 1790595000, EndTime: 1790596800, Timezone: "Europe/Vienna",
		RRule: signed.slots[0]["RRule"].(*string), DetachedSignature: signed.slots[0]["DetachedSignature"].(string),
	}
	if got := verifySlot(ck, "uid", raw); got != pgphelper.Verified {
		t.Errorf("slot = %s", got)
	}
	raw.EndTime += 60
	if got := verifySlot(ck, "uid", raw); got == pgphelper.Verified {
		t.Error("a slot whose time was changed still verified")
	}
	sig, err := base64.StdEncoding.DecodeString(signed.calendarKey)
	if err != nil {
		t.Fatal(err)
	}
	fingerprints := strings.Join(ck.primary.GetSHA256Fingerprints(), ";")
	if err := pgphelper.VerifyTextInContext(ck.addr.Read, fingerprints, sig, pgp.Bytes,
		pgp.NewVerificationContext("bookings.calendarKey.uid", true, 0)); err != nil {
		t.Errorf("the calendar key's signature: %v", err)
	}
	if _, err := signBookingPage(&calKeys{addr: ck.addr}, "cal1", "uid", counting(1), counting(2),
		bookingContent{}, slots); err == nil {
		t.Error("a page was signed for a calendar with no primary key")
	}
}

func vienna(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Europe/Vienna")
	if err != nil {
		t.Skipf("Europe/Vienna is not available: %v", err)
	}
	return loc
}

func TestAWeeklyWindowIsAnchoredInTheWeekThatHoldsToday(t *testing.T) {
	loc := vienna(t)
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, loc)
	slots, err := Offer([]Window{
		{Day: "mon", Start: "09:00", End: "10:00"},
		{Day: "sun", Start: "22:00", End: "24:00"},
	}, 30*time.Minute, loc, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(slots) != 6 {
		t.Fatalf("slots = %d, want 6", len(slots))
	}
	first := time.Unix(slots[0].Start, 0).In(loc)
	if first.Format("2006-01-02 15:04") != "2026-09-28 09:00" || !slots[0].Weekly || slots[0].Zone != "Europe/Vienna" {
		t.Errorf("first slot = %s %+v", first, slots[0])
	}
	last := time.Unix(slots[5].End, 0).In(loc)
	if last.Format("2006-01-02 15:04") != "2026-10-05 00:00" {
		t.Errorf("the Sunday window ends %s", last)
	}
}

func TestAWeeklyWindowThatTheClocksSkipIsAnchoredAWeekEarlier(t *testing.T) {
	loc := vienna(t)
	now := time.Date(2027, 3, 24, 12, 0, 0, 0, loc)
	slots, err := Offer([]Window{{Day: "sun", Start: "02:00", End: "03:00"}}, 30*time.Minute, loc, now)
	if err != nil {
		t.Fatal(err)
	}
	if day := time.Unix(slots[0].Start, 0).In(loc).Format("2006-01-02 15:04"); day != "2027-03-21 02:00" {
		t.Errorf("anchored at %s", day)
	}
}

func TestADatedWindowIsRefusedWhenItCannotBeOffered(t *testing.T) {
	loc := vienna(t)
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, loc)
	for _, c := range []struct {
		windows []Window
		d       time.Duration
		want    string
	}{
		{[]Window{{Day: "2026-10-01", Start: "09:00", End: "17:00"}}, time.Hour, "2026-10-01=09:00-17:00 has already begun."},
		{[]Window{{Day: "2026-10-05", Start: "09:00", End: "09:30"}}, time.Hour, "2026-10-05=09:00-09:30 is shorter than one 1h appointment."},
		{[]Window{{Day: "2027-03-28", Start: "02:00", End: "04:00"}}, time.Hour, "2027-03-28=02:00-04:00: 02:00 does not exist"},
	} {
		_, err := Offer(c.windows, c.d, loc, now)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("Offer(%v) = %v, want %q", c.windows, err, c.want)
		}
	}
}

func TestAPageOffersAtMostTwoHundredAppointments(t *testing.T) {
	loc := vienna(t)
	var week []Window
	for _, day := range []string{"mon", "tue", "wed", "thu", "fri", "sat", "sun"} {
		week = append(week, Window{Day: day, Start: "08:00", End: "20:00"})
	}
	_, err := Offer(week, 15*time.Minute, loc, time.Now())
	var problem *errs.Problem
	if !errors.As(err, &problem) || !strings.Contains(err.Error(), "That is 336 appointments") {
		t.Errorf("Offer = %v", err)
	}
	if _, err := Offer(week, time.Hour, loc, time.Now()); err != nil {
		t.Errorf("84 appointments were refused: %v", err)
	}
}

func TestWhatAPageOffersIsShownInTheFormItIsWritten(t *testing.T) {
	loc := vienna(t)
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, loc)
	var weekdays []Window
	for _, day := range []string{"mon", "tue", "wed", "thu", "fri"} {
		weekdays = append(weekdays,
			Window{Day: day, Start: "09:00", End: "12:00"}, Window{Day: day, Start: "13:00", End: "17:00"})
	}
	weekdays = append(weekdays, Window{Day: "sun", Start: "20:00", End: "24:00"})
	slots, err := Offer(weekdays, 30*time.Minute, loc, now)
	if err != nil {
		t.Fatal(err)
	}
	offer := windowsOf(slots)
	if !offer.weekly || offer.zone != "Europe/Vienna" || offer.duration != 30*time.Minute {
		t.Errorf("offer = %+v", offer)
	}
	got := Tokens(offer.windows)
	want := []string{"mon-fri=09:00-12:00", "mon-fri=13:00-17:00", "sun=20:00-24:00"}
	if !slices.Equal(got, want) {
		t.Errorf("tokens = %v, want %v", got, want)
	}

	dated, err := Offer([]Window{{Day: "2026-10-06", Start: "14:00", End: "17:00"},
		{Day: "2026-10-05", Start: "09:00", End: "12:00"}}, time.Hour, loc, now)
	if err != nil {
		t.Fatal(err)
	}
	if got := Tokens(windowsOf(dated).windows); !slices.Equal(got,
		[]string{"2026-10-05=09:00-12:00", "2026-10-06=14:00-17:00"}) {
		t.Errorf("dated tokens = %v", got)
	}
}

func TestANewLengthRecutsTheWindowsAPageOffersNow(t *testing.T) {
	loc := vienna(t)
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, loc)
	slots, err := Offer([]Window{{Day: "mon", Start: "09:00", End: "17:00"}}, 30*time.Minute, loc, now)
	if err != nil {
		t.Fatal(err)
	}
	longer, err := recut(slots, 90*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if len(longer) != 5 || !longer[0].Weekly {
		t.Errorf("recut = %d slots", len(longer))
	}
	if got := Tokens(windowsOf(longer).windows); !slices.Equal(got, []string{"mon=09:00-16:30"}) {
		t.Errorf("tokens = %v", got)
	}
	if _, err := recut(longer, 2*time.Hour); err != nil {
		t.Errorf("recut to 2h: %v", err)
	}
	short, err := Offer([]Window{{Day: "mon", Start: "09:00", End: "10:00"}}, time.Hour, loc, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := recut(short, 2*time.Hour); err == nil || !strings.Contains(err.Error(), "mon=09:00-10:00 is shorter") {
		t.Errorf("recut = %v", err)
	}
}

type bookingDoer struct {
	plan   string
	pages  []rawBookingPage
	posted map[string]any
	put    map[string]any
}

func (d *bookingDoer) Do(context.Context, proton.Request) (*proton.Response, error) {
	return nil, errors.New("bookingDoer only decodes")
}

func (d *bookingDoer) Decode(_ context.Context, r proton.Request, out any) error {
	answer := func(v any) error {
		raw, err := json.Marshal(v)
		if err != nil {
			return err
		}
		return json.Unmarshal(raw, out)
	}
	switch {
	case r.Path == "/core/v4/organizations":
		if d.plan == "" {
			return &proton.APIError{HTTPStatus: 422, Code: plan.NoOrganization}
		}
		return answer(map[string]any{"Organization": map[string]any{"PlanName": d.plan}})
	case r.Path == "/calendar/v1":
		return answer(map[string]any{"Calendars": []map[string]any{
			{"ID": "cal1", "Type": 0, "Owner": map[string]any{"Email": "me@proton.me"},
				"Members": []map[string]any{{"ID": "m1", "Email": "me@proton.me", "Name": "Personal", "Flags": 1}}},
			{"ID": "cal2", "Type": 0, "Owner": map[string]any{"Email": "me@proton.me"},
				"Members": []map[string]any{{"ID": "m2", "Email": "me@proton.me", "Name": "Work", "Flags": 1}}},
		}})
	case r.Method == "GET" && r.Path == "/calendar/v1/booking":
		return answer(map[string]any{"BookingPages": d.pages})
	case r.Method == "POST" && r.Path == "/calendar/v1/booking":
		d.posted = r.Body.(map[string]any)
		made := rawBookingPage{
			ID: "page1", CalendarID: "cal1", BookingUID: d.posted["BookingUID"].(string),
			BookingKeySalt:   d.posted["BookingKeySalt"].(string),
			EncryptedSecret:  d.posted["EncryptedSecret"].(string),
			EncryptedContent: d.posted["EncryptedContent"].(string),
			CreateTime:       1790590000, ModifyTime: 1790590000,
			MinimumNoticeMode:   d.posted["MinimumNoticeMode"].(int),
			ConflictCalendarIDs: d.posted["ConflictCalendarIDs"].([]string),
		}
		for _, sl := range d.posted["Slots"].([]map[string]any) {
			made.Slots = append(made.Slots, rawBookingSlot{
				StartTime: sl["StartTime"].(int64), EndTime: sl["EndTime"].(int64),
				Timezone: sl["Timezone"].(string), RRule: sl["RRule"].(*string),
				DetachedSignature: sl["DetachedSignature"].(string),
			})
		}
		d.pages = append(d.pages, made)
		return answer(map[string]any{"BookingPage": made})
	case r.Method == "GET" && r.Path == "/calendar/v1/booking/page1":
		if len(d.pages) == 0 {
			return &proton.APIError{HTTPStatus: 422, Code: 2501}
		}
		return answer(map[string]any{"BookingPage": d.pages[0]})
	case r.Method == "PUT" && r.Path == "/calendar/v1/booking/page1":
		d.put = r.Body.(map[string]any)
		return nil
	}
	return errors.New("unexpected request " + r.Method + " " + r.Path)
}

func bookingService(t *testing.T, d *bookingDoer, paidMeet bool) *Service {
	t.Helper()
	s := New(d, testKeys(&keys.Unlocked{PaidMeet: paidMeet}))
	ck := bookingKeys(t)
	for _, id := range []string{"cal1", "cal2"} {
		if _, err := s.unlocked.Do(id, func() (*calKeys, error) { return ck, nil }); err != nil {
			t.Fatal(err)
		}
	}
	return s
}

func TestAPageMadeHereReadsBackWithItsLinkAndWhatItOffers(t *testing.T) {
	loc := vienna(t)
	d := &bookingDoer{plan: "bundle2022"}
	s := bookingService(t, d, false)
	ctx := context.Background()
	slots, err := Offer([]Window{{Day: "mon", Start: "09:00", End: "10:00"}}, 30*time.Minute, loc, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	cal := Calendar{ID: "cal1", Name: "Personal", Kind: KindPersonal, active: true}
	made, err := s.BookingPageCreate(ctx, cal, NewBookingPage{
		Title: "Intro call", Slots: slots, Notice: NoticeTwoDays, BlockedBy: []string{"cal2"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if d.posted["MinimumNoticeMode"] != 2 || !slices.Equal(d.posted["ConflictCalendarIDs"].([]string), []string{"cal2"}) {
		t.Errorf("posted = %+v", d.posted)
	}
	if !strings.HasPrefix(made.URL, "https://calendar.proton.me/bookings#") || !made.Meet || made.Duration != "30m" {
		t.Errorf("made = %+v", made)
	}

	listed, err := s.BookingPages(ctx)
	if err != nil || len(listed) != 1 || listed[0].Title != "Intro call" || listed[0].Calendar != "Personal" {
		t.Fatalf("listed = %+v, %v", listed, err)
	}
	opened, err := s.BookingPageOpen(ctx, "page1")
	if err != nil {
		t.Fatal(err)
	}
	if opened.URL != made.URL || opened.Signature != pgphelper.Verified || opened.Notice != NoticeTwoDays ||
		!slices.Equal(opened.BlockedBy, []string{"Work"}) || opened.Contact != "me@proton.me" ||
		!slices.Equal(Tokens(opened.Available), []string{"mon=09:00-10:00"}) {
		t.Errorf("opened = %+v", opened)
	}

	ch, err := s.BookingPageChange(ctx, "page1")
	if err != nil {
		t.Fatal(err)
	}
	place := "Café Central"
	if err := ch.Apply(BookingPatch{Location: &place, Duration: time.Hour}); err != nil {
		t.Fatal(err)
	}
	if err := s.BookingPageSave(ctx, ch); err != nil {
		t.Fatal(err)
	}
	if d.put["EncryptedSecret"] != d.pages[0].EncryptedSecret || len(d.put["Slots"].([]map[string]any)) != 1 {
		t.Errorf("put = %+v", d.put)
	}
	ck, _ := s.unlockCalendar(ctx, "cal1")
	password, _ := bookingKeyPassword("cal1", mustSecret(t, ck, d.pages[0]), mustSalt(t, d.pages[0]))
	content, _, err := openBookingContent(ck, password, d.pages[0].BookingUID, d.put["EncryptedContent"].(string))
	if err != nil || content.Location != place || content.WithProtonMeetLink || content.Summary != "Intro call" {
		t.Errorf("saved content = %+v, %v", content, err)
	}
}

func mustSecret(t *testing.T, ck *calKeys, raw rawBookingPage) []byte {
	t.Helper()
	secret, _, err := openBookingSecret(ck, raw.CalendarID, raw.EncryptedSecret)
	if err != nil {
		t.Fatal(err)
	}
	return secret
}

func mustSalt(t *testing.T, raw rawBookingPage) []byte {
	t.Helper()
	salt, err := base64.StdEncoding.DecodeString(raw.BookingKeySalt)
	if err != nil {
		t.Fatal(err)
	}
	return salt
}

func TestHowManyPagesAnAccountMayHaveIsItsPlans(t *testing.T) {
	for _, c := range []struct {
		plan     string
		paidMeet bool
		pages    int
		want     string
	}{
		{"", false, 0, "need a paid Mail or Meet plan"},
		{"drive2022", false, 0, "need a paid Mail or Meet plan"},
		{"mail2022", false, 0, ""},
		{"bundle2022", false, 1, `and "Intro call" is it`},
		{"bundle2022", true, 1, ""},
		{"bundlepro2024", false, 1, ""},
	} {
		s := bookingService(t, &bookingDoer{plan: c.plan}, c.paidMeet)
		for range c.pages {
			slots, err := Offer([]Window{{Day: "mon", Start: "09:00", End: "10:00"}}, time.Hour, time.UTC, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			if _, err := s.BookingPageCreate(context.Background(),
				Calendar{ID: "cal1", Name: "Personal", Kind: KindPersonal, active: true},
				NewBookingPage{Title: "Intro call", Slots: slots}); err != nil {
				t.Fatal(err)
			}
		}
		err := s.BookingRoom(context.Background())
		switch {
		case c.want == "" && err != nil:
			t.Errorf("%s with %d pages: %v", c.plan, c.pages, err)
		case c.want != "" && (err == nil || !strings.Contains(err.Error(), c.want)):
			t.Errorf("%s with %d pages = %v, want %q", c.plan, c.pages, err, c.want)
		}
	}
}

func TestOnlyAPersonalCalendarInUseTakesBookings(t *testing.T) {
	for _, c := range []struct {
		cal  Calendar
		want string
	}{
		{Calendar{Name: "Personal", Kind: KindPersonal, active: true}, ""},
		{Calendar{Name: "Old", Kind: KindPersonal}, "Old is disabled"},
		{Calendar{Name: "Team", Kind: KindShared, Owner: "jane@proton.me", active: true}, "only they can"},
		{Calendar{Name: "Timetable", Kind: KindSubscribed, active: true}, "read-only"},
	} {
		err := c.cal.TakesBookings()
		if (c.want == "") != (err == nil) || (err != nil && !strings.Contains(err.Error(), c.want)) {
			t.Errorf("%s: %v, want %q", c.cal.Name, err, c.want)
		}
	}
	if err := (Calendar{Name: "Holidays in Austria", Kind: KindHolidays, active: true}).BlocksBookings(); err == nil {
		t.Error("a holidays calendar was let block bookings")
	}
	if err := (Calendar{Name: "Team", Kind: KindShared, active: true}).BlocksBookings(); err != nil {
		t.Errorf("a shared calendar may block bookings: %v", err)
	}
}
