package live

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// Links that open a calendar for somebody with no Proton account.
//
// A plan gates calendar sharing, so this is the paid account's - acting on a
// calendar the test makes and deletes again, never on one of the account's own.
//
// The proof a link works is following it: the URL carries the keys that open
// the feed, so the run fetches it with no account at all and reads what came
// back. That fetch is the only thing in the suite that goes out without a
// session, and it is the whole point of the feature.

// The life of a link: published, followed, listed, rebuilt, renamed and
// revoked.
func TestCalendarLinkPublishesACalendarToAnybody(t *testing.T) {
	calendar := testID() + "-published"
	out, stderr, code := runPaid(t, "--yes", "calendar", "settings", "calendars",
		"create", "--name", calendar)
	if code != 0 {
		t.Fatalf("could not make a calendar to publish: %s", truncateOutput(stderr))
	}
	ref := strings.TrimSpace(out)
	cleanupRunPaid(t, "Delete the published calendar: proton calendar settings calendars delete "+ref,
		"calendar", "settings", "calendars", "delete", ref)

	// Something to look for in the feed. A full link shows its title; a limited
	// one shows that the time is taken and nothing else.
	title := testID() + "-lunch"
	runOKPaid(t, "--yes", "calendar", "events", "create", "--calendar", ref,
		"--title", title, "--start", time.Now().Add(24*time.Hour).Format("2006-01-02T15:04"),
		"--duration", "1h")

	name := testID() + "-busy"
	shown, warned := runOKStderrPaid(t, "calendar", "settings", "links", "create", "--name", name, ref)
	assertField(t, shown, "Access:", "limited")
	assertField(t, shown, "Calendar:", calendar)
	// The warning belongs on stderr, so capturing the URL does not capture it.
	assertContains(t, warned, "Anyone with this link")
	limited := urlShown(t, shown)

	feed := follow(t, limited)
	assertContains(t, feed, "BEGIN:VCALENDAR")
	if strings.Contains(feed, title) {
		t.Errorf("a limited link gave away what an event is called: %s", truncateOutput(feed))
	}

	// The second level is the one that hands Proton the key to the calendar, so
	// what comes back carries the details themselves.
	full, warnedFull := runOKStderrPaid(t, "calendar", "settings", "links", "create",
		"--access", "full", "--name", testID()+"-detail", ref)
	assertField(t, full, "Access:", "full")
	assertContains(t, warnedFull, "Proton can read them")
	detailed := follow(t, urlShown(t, full))
	assertContains(t, detailed, title)

	var listed map[string]interface{}
	for _, row := range runJSONArrayPaid(t, "calendar", "settings", "links", "list", "--calendar", ref) {
		m, _ := row.(map[string]interface{})
		// The URL opens the calendar for anybody holding it, so a listing has none.
		if u, _ := m["url"].(string); u != "" {
			t.Errorf("a link listing carries the URL that opens the calendar: %q", u)
		}
		if m["name"] == name {
			listed = m
		}
	}
	if listed == nil {
		t.Fatal("the link this test made is not in the listing")
	}
	if listed["calendar"] != calendar || listed["access"] != "limited" {
		t.Errorf("the listing says %v, %v", listed["calendar"], listed["access"])
	}

	// Proton keeps the key sealed under the calendar's own, so the whole URL can
	// be put back together - which is what makes a mislaid link recoverable.
	back := runJSONPaid(t, "calendar", "settings", "links", "get", name)
	if back["url"] != limited {
		t.Errorf("`links get` rebuilt %v, want the URL create handed over", back["url"])
	}

	renamed := name + "-renamed"
	runOKPaid(t, "calendar", "settings", "links", "update", "--name", renamed, name)
	if got := runJSONPaid(t, "calendar", "settings", "links", "get", renamed); got["id"] != listed["id"] {
		t.Errorf("after renaming, the name answers for %v, want %v", got["id"], listed["id"])
	}
	runOKPaid(t, "calendar", "settings", "links", "update", "--clear-name", renamed)
	if got := runJSONPaid(t, "calendar", "settings", "links", "get", listed["id"].(string)); got["name"] != nil {
		t.Errorf("a cleared name came back as %v", got["name"])
	}

	runOKPaid(t, "calendar", "settings", "links", "revoke", listed["id"].(string))
	if _, _, code := runPaid(t, "--yes", "calendar", "settings", "links", "get",
		listed["id"].(string)); code != 3 {
		t.Errorf("a revoked link answers with exit %d, want 3", code)
	}
	// Following it afterwards is what a person holding the URL would find.
	if status := statusOf(t, limited); status == http.StatusOK {
		t.Error("a revoked link still opens the calendar")
	}
}

// urlShown reads the URL out of what a command put on stdout.
func urlShown(t *testing.T, stdout string) string {
	t.Helper()
	for _, line := range strings.Split(stdout, "\n") {
		line = strings.TrimSpace(line)
		if rest, ok := strings.CutPrefix(line, "URL:"); ok {
			return strings.TrimSpace(rest)
		}
	}
	t.Fatalf("no URL in what the command answered with: %s", truncateOutput(stdout))
	return ""
}

// follow fetches a published calendar the way anybody holding the link would,
// with no account and nothing signed in.
//
// A link Proton has not finished publishing answers with something other than
// the feed for a moment, so this waits rather than failing on the first try.
func follow(t *testing.T, link string) string {
	t.Helper()
	var body string
	if !waitFor(60*time.Second, 3*time.Second, func() bool {
		resp, err := http.Get(link) //nolint:gosec,noctx // the URL is the one the command just answered with
		if err != nil {
			return false
		}
		defer func() { _ = resp.Body.Close() }()
		raw, err := io.ReadAll(resp.Body)
		if err != nil || resp.StatusCode != http.StatusOK {
			return false
		}
		body = string(raw)
		return strings.Contains(body, "BEGIN:VCALENDAR")
	}) {
		t.Fatalf("the link did not serve a calendar within a minute: %s", truncateOutput(body))
	}
	return body
}

// statusOf is what following a link answers with, for a link that should no
// longer open anything.
func statusOf(t *testing.T, link string) int {
	t.Helper()
	resp, err := http.Get(link) //nolint:gosec,noctx // the URL is the one the command answered with
	if err != nil {
		t.Fatalf("follow the link: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode
}
