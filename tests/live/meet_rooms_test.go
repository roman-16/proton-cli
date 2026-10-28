package live

import (
	"strings"
	"testing"
)

// Proton limits how many meetings and rooms an account makes in a day, apart from
// the ones a calendar event makes, so the rooms are made on the second account
// and the meetings on the first.
func TestMeetRoomsLifecycle(t *testing.T) {
	title := testID() + "-room"
	made := runJSONSecondary(t, "meet", "rooms", "create", "--title", title)
	id, _ := made["id"].(string)
	url, _ := made["url"].(string)
	cleanupRunSecondary(t, "Delete room: proton --profile secondary meet rooms delete "+id, "meet", "rooms", "delete", id)
	if made["type"] != "room" || !strings.HasPrefix(url, "https://meet.proton.me/join/id-"+id+"#pwd-") {
		t.Fatalf("create answered %v", made)
	}

	_, stderr, code := runSecondary(t, "meet", "meetings", "get", id)
	if code != 1 {
		t.Errorf("a room asked for as a meeting: exit %d", code)
	}
	assertContains(t, stderr, "meet rooms get "+id)

	runOKSecondary(t, "meet", "rooms", "update", "--title", title+"-renamed", title)
	if got := runJSONSecondary(t, "meet", "rooms", "get", id); got["title"] != title+"-renamed" || got["url"] != url {
		t.Errorf("after the rename: %v", got)
	}

	runOKSecondary(t, "meet", "rooms", "delete", id)
	if _, _, code := runSecondary(t, "meet", "rooms", "get", id); code != 3 {
		t.Errorf("a deleted room answers exit %d, want 3", code)
	}
}

// Proton Meet makes a personal room the first time somebody opens it in a
// browser, and nothing a run does makes one.
func personalRoom(t *testing.T) map[string]interface{} {
	t.Helper()
	stdout, stderr, code := run(t, "--output", "json", "meet", "rooms", "get", "personal")
	if code == 3 {
		t.Skip("the primary account has no personal room until Proton Meet is opened once as it")
	}
	if code != 0 {
		t.Fatalf("rooms get personal: exit %d: %s", code, stderr)
	}
	return parseJSONObject(t, stdout)
}

func TestMeetRoomsPersonal(t *testing.T) {
	room := personalRoom(t)
	if room["type"] != "personal" {
		t.Errorf("personal answered %v", room)
	}

	_, stderr, code := run(t, "meet", "rooms", "delete", "personal")
	if code != 1 {
		t.Errorf("deleting the personal room: exit %d", code)
	}
	assertContains(t, stderr, "rooms rotate personal")

	_, stderr = runOKStderr(t, "--dry-run", "meet", "rooms", "rotate", "personal")
	assertContains(t, stderr, "Dry run - would rotate the link of your personal room")
	if again := runJSON(t, "meet", "rooms", "get", "personal"); again["url"] != room["url"] {
		t.Error("a dry run changed the personal room's link")
	}
}
