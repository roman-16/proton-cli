package live

import (
	"crypto/hkdf"
	"crypto/sha256"
	"encoding/base64"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestCalendarBookingPagesOfferTimesToAnybody(t *testing.T) {
	calendar := testID() + "-bookings"
	out, stderr, code := runPaid(t, "--yes", "calendar", "settings", "calendars", "create", "--name", calendar)
	if code != 0 {
		t.Fatalf("could not make a calendar to take bookings: %s", truncateOutput(stderr))
	}
	ref := strings.TrimSpace(out)
	cleanupRunPaid(t, "Delete the booking calendar: proton calendar settings calendars delete "+ref,
		"calendar", "settings", "calendars", "delete", ref)

	title := testID() + "-intro"
	shown, warned := runOKStderrPaid(t, "calendar", "booking-pages", "create",
		"--title", title, "--calendar", ref, "--duration", "30m", "--notice", "2h")
	cleanupRunPaid(t, "Delete the booking page: proton calendar booking-pages delete "+title,
		"calendar", "booking-pages", "delete", title)
	assertContains(t, warned, "Anyone with this link")
	assertField(t, shown, "Title:", title)
	assertField(t, shown, "Calendar:", calendar)
	assertField(t, shown, "Available:", "mon-fri=09:00-17:00")
	assertField(t, shown, "Duration:", "30m")
	assertField(t, shown, "Notice:", "2h")
	assertField(t, shown, "Location:", "Proton Meet")
	link := urlShown(t, shown)

	var listed map[string]interface{}
	for _, row := range runJSONArrayPaid(t, "calendar", "booking-pages", "list") {
		m, _ := row.(map[string]interface{})
		if u, _ := m["url"].(string); u != "" {
			t.Errorf("a booking page listing carries the link: %q", u)
		}
		if m["title"] == title {
			listed = m
		}
	}
	if listed == nil {
		t.Fatal("the booking page this test made is not in the listing")
	}
	id, _ := listed["id"].(string)

	page := runJSONPaid(t, "calendar", "booking-pages", "get", id)
	if page["url"] != link || page["signature"] != "verified" || page["weekly"] != true {
		t.Errorf("`booking-pages get` answered %v, %v, weekly %v; want the link create handed over, verified",
			page["url"], page["signature"], page["weekly"])
	}

	opened := openedAsAGuest(t, link)
	if opened["Email"] != page["contact"] {
		t.Errorf("the page shows %v as who is booked, want %v", opened["Email"], page["contact"])
	}
	if slots, _ := opened["AvailableSlots"].([]interface{}); len(slots) == 0 {
		t.Error("the page offers another account no time in the coming week")
	}

	day := time.Now().AddDate(0, 0, 3).Format("2006-01-02")
	runOKPaid(t, "calendar", "booking-pages", "update", "--available", day+"=09:00-12:00",
		"--duration", "1h", "--notice", "48h", "--location", "Room 3", id)
	changed, _ := runOKStderrPaid(t, "calendar", "booking-pages", "get", id)
	assertField(t, changed, "Available:", day+"=09:00-12:00")
	assertField(t, changed, "Duration:", "1h")
	assertField(t, changed, "Notice:", "48h")
	assertField(t, changed, "Location:", "Room 3")
	assertField(t, changed, "Signature:", "verified")
	if urlShown(t, changed) != link {
		t.Error("changing a booking page changed its link")
	}

	runOKPaid(t, "calendar", "booking-pages", "update", "--meet", id)
	if got := runJSONPaid(t, "calendar", "booking-pages", "get", id); got["meet"] != true {
		t.Errorf("--meet left the page at %v", got["location"])
	}

	runOKPaid(t, "--yes", "calendar", "booking-pages", "delete", id)
	if _, _, code := runPaid(t, "calendar", "booking-pages", "get", id); code != 3 {
		t.Errorf("a deleted booking page answers with exit %d, want 3", code)
	}
}

func openedAsAGuest(t *testing.T, link string) map[string]interface{} {
	t.Helper()
	_, fragment, ok := strings.Cut(link, "#")
	if !ok {
		t.Fatalf("the link carries no secret: %s", link)
	}
	secret, err := base64.URLEncoding.DecodeString(fragment)
	if err != nil {
		t.Fatalf("the link's secret is not base64url: %v", err)
	}
	uid, err := hkdf.Key(sha256.New, secret, nil, "bookings.booking_id", 32)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	answer := runJSONSecondary(t, "api", "GET",
		"/calendar/v1/booking/external/"+base64.URLEncoding.EncodeToString(uid),
		"--query", "Start="+strconv.FormatInt(now.Unix(), 10),
		"--query", "End="+strconv.FormatInt(now.AddDate(0, 0, 7).Unix(), 10))
	page, ok := answer["BookingPage"].(map[string]interface{})
	if !ok {
		t.Fatalf("the page did not open for another account: %v", answer)
	}
	return page
}

func TestCalendarBookingPagesNeedAPaidPlan(t *testing.T) {
	if rows := runJSONArray(t, "calendar", "booking-pages", "list"); len(rows) != 0 {
		t.Errorf("a free account lists %d booking pages", len(rows))
	}
	_, stderr, code := run(t, "calendar", "booking-pages", "create", "--title", testID()+"-refused")
	if code != 1 {
		t.Errorf("a free account making a booking page exits %d, want 1", code)
	}
	assertContains(t, stderr, "Booking pages need a paid Mail or Meet plan.")
}
