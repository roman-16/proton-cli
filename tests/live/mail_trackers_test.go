package live

import (
	"testing"

	"github.com/roman-16/proton-cli/tests/fixture"
)

const (
	trackedLink  = "https://trailhead.example/north?utm_source=newsletter&utm_medium=email&utm_campaign=april"
	trackedClean = "https://trailhead.example/north"
)

func requireTrackingBlocked(t *testing.T) {
	t.Helper()
	if imageProxyBits(t)&2 == 0 {
		t.Fatal("the primary account has image-proxy off, which every test here reads with it on: " +
			"proton mail settings set image-proxy on")
	}
}

func TestMailTrackersListNamesThePixelAndTheLink(t *testing.T) {
	requireTrackingBlocked(t)
	msgID, _ := trackedMail(t)

	rows := runJSONArray(t, "mail", "messages", "trackers", "list", msgID)

	var pixel, link map[string]interface{}
	for _, row := range rows {
		r := row.(map[string]interface{})
		switch {
		case r["kind"] == "image" && r["url"] == fixture.TrackerPixel:
			pixel = r
		case r["kind"] == "link" && r["url"] == trackedLink:
			link = r
		case r["kind"] == "image":
			t.Errorf("an image no tracker serves was reported: %v", r)
		}
	}
	if pixel == nil || pixel["tracker"] == "" {
		t.Errorf("the tracker's pixel was not named, in %v", rows)
	}
	if link == nil {
		t.Fatalf("the tracked link was not reported, in %v", rows)
	}
	if link["cleaned"] != trackedClean || link["tracker"] != "utm_source, utm_medium, utm_campaign" {
		t.Errorf("link row = %v, want it cleaned to %s with the three utm parameters named", link, trackedClean)
	}
}

func TestMailMessagesGetShowsLinksWithoutTheirTracking(t *testing.T) {
	requireTrackingBlocked(t)
	msgID, _ := trackedMail(t)

	text := runOK(t, "mail", "messages", "get", msgID)
	assertContains(t, text, "Links:")
	assertContains(t, text, "1 cleaned")
	assertContains(t, text, "["+trackedClean+"]")
	assertNotContains(t, text, "utm_source")

	if links := runJSON(t, "mail", "messages", "get", msgID)["links_cleaned"]; links != float64(1) {
		t.Errorf("links_cleaned = %v, want 1", links)
	}

	raw := runOK(t, "mail", "messages", "get", "--render", "raw", msgID)
	assertContains(t, raw, "utm_source=newsletter")
	assertNotContains(t, raw, "Links:")
}
