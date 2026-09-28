package mail

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

const trackedBody = `<p>The north trail reopened.</p>` +
	`<img src="https://trailhead.us1.list-manage.com/track/open.php?u=1">` +
	`<img src="https://cdn.trailhead.example/logo.png">` +
	`<img src="https://pixel.refused.example/p.gif">` +
	`<img src="https://trailhead.us1.list-manage.com/track/open.php?u=2">` +
	`<img src="https://t.litmus.example/pixel">` +
	`<a href="https://trailhead.example/north?utm_source=newsletter&amp;utm_medium=email">Read the trail report</a>`

func trackedAPI(t *testing.T, imageProxy int) (*recordingAPI, *Service) {
	t.Helper()
	kr := genMailKeyRing(t)
	message := encryptedMessage(t, kr, trackedBody, "text/html")
	message["Attachments"] = []map[string]any{{
		"ID": "att-1", "Name": "pixel.gif", "Disposition": "inline",
		"Headers": map[string]any{"x-pm-tracker-provider": "HubSpot", "content-location": "https://hub.example/p.gif"},
	}}
	api := &recordingAPI{
		message:  message,
		settings: map[string]any{"ImageProxy": imageProxy},
		providers: map[string]string{
			"https://trailhead.us1.list-manage.com/track/open.php?u=1": "Mailchimp",
			"https://trailhead.us1.list-manage.com/track/open.php?u=2": "Mailchimp",
			"https://t.litmus.example/pixel":                           "Litmus",
		},
		refused: map[string]int{"https://pixel.refused.example/p.gif": 422},
	}
	return api, New(api, testKeys(unlockedRings("addr-1", kr)))
}

func TestTrackersGroupsImagesByWhoServesThemThenTheLinks(t *testing.T) {
	_, s := trackedAPI(t, ProxyRemoteImages)
	got, err := s.Trackers(context.Background(), "m1")
	if err != nil {
		t.Fatalf("Trackers: %v", err)
	}
	want := []Tracker{
		{Kind: TrackerImage, Name: "HubSpot", URL: "https://hub.example/p.gif"},
		{Kind: TrackerImage, Name: "Mailchimp", URL: "https://trailhead.us1.list-manage.com/track/open.php?u=1"},
		{Kind: TrackerImage, Name: "Mailchimp", URL: "https://trailhead.us1.list-manage.com/track/open.php?u=2"},
		{Kind: TrackerImage, Name: "Litmus", URL: "https://t.litmus.example/pixel"},
		{
			Kind: TrackerLink, Name: "utm_source, utm_medium",
			URL:     "https://trailhead.example/north?utm_source=newsletter&utm_medium=email",
			Cleaned: "https://trailhead.example/north",
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Trackers =\n%+v\nwant\n%+v", got, want)
	}
}

func TestTrackersAsksTheProxyWithoutHavingItLoadTheImage(t *testing.T) {
	api, s := trackedAPI(t, ProxyRemoteImages)
	if _, err := s.Trackers(context.Background(), "m1"); err != nil {
		t.Fatalf("Trackers: %v", err)
	}
	checked := 0
	for _, r := range api.requests {
		if r.Path != "/core/v4/images" {
			continue
		}
		checked++
		if r.Method != "GET" || r.Query.Get("DryRun") != "1" {
			t.Errorf("image check %s %v, want a GET with DryRun=1", r.Method, r.Query)
		}
	}
	if checked != 5 {
		t.Errorf("checked %d images, want each of the 5 distinct ones once", checked)
	}
}

func TestTrackersWithoutTheProxyBitChecksNoRemoteImage(t *testing.T) {
	api, s := trackedAPI(t, StoreRemoteImages)
	got, err := s.Trackers(context.Background(), "m1")
	if err != nil {
		t.Fatalf("Trackers: %v", err)
	}
	for _, r := range api.requests {
		if r.Path == "/core/v4/images" {
			t.Fatalf("checked a remote image with only the store bit set")
		}
	}
	if len(got) != 2 || got[0].Name != "HubSpot" || got[1].Kind != TrackerLink {
		t.Errorf("Trackers = %+v, want the stored tracker and the link", got)
	}
}

func TestTrackersRefusesAnAccountThatDoesNotBlockTracking(t *testing.T) {
	_, s := trackedAPI(t, 0)
	if _, err := s.Trackers(context.Background(), "m1"); !errors.Is(err, ErrTrackingNotBlocked) {
		t.Errorf("err = %v, want ErrTrackingNotBlocked", err)
	}
}

func TestEncodeImageURIEscapesTheAddressAsTheWebClientDoes(t *testing.T) {
	for in, want := range map[string]string{
		"https://cdn.example/a b/ü.png":           "https://cdn.example/a%20b/%C3%BC.png",
		"https://cdn.example/a%20b.png":           "https://cdn.example/a%20b.png",
		" https://cdn.example/p?x=a b&y=1 ":       "https://cdn.example/p?x=a b&y=1",
		"https://cdn.example/p?first?second":      "https://cdn.example/p?first",
		"https://cdn.example/[brackets]{braces}|": "https://cdn.example/%5Bbrackets%5D%7Bbraces%7D%7C",
	} {
		if got := encodeImageURI(in); got != want {
			t.Errorf("encodeImageURI(%q) = %q, want %q", in, got, want)
		}
	}
}
