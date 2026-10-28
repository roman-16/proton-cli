package live

import (
	"strings"
	"testing"
)

func TestMeetMeetingsLifecycle(t *testing.T) {
	title := testID() + "-meeting"
	made := runJSON(t, "meet", "meetings", "create",
		"--title", title, "--start", "2027-03-03T09:00", "--duration", "45m", "--rrule", "FREQ=WEEKLY;COUNT=4")
	id, _ := made["id"].(string)
	url, _ := made["url"].(string)
	cleanupRun(t, "Delete meeting: proton meet meetings delete "+id, "meet", "meetings", "delete", id)
	if id == "" || !strings.HasPrefix(url, "https://meet.proton.me/join/id-"+id+"#pwd-") {
		t.Fatalf("create answered %v", made)
	}

	listed := false
	for _, r := range listAll(t, "meet", "meetings", "list") {
		row, _ := r.(map[string]interface{})
		if row["id"] == id {
			listed = true
			if _, ok := row["url"]; ok {
				t.Error("a listing carried a meeting's link")
			}
		}
	}
	if !listed {
		t.Fatalf("meeting %s is not listed", id)
	}

	got := runJSON(t, "meet", "meetings", "get", title)
	if got["url"] != url || got["rrule"] != "FREQ=WEEKLY;COUNT=4" {
		t.Errorf("get answered %v", got)
	}

	runOK(t, "meet", "meetings", "update", "--start", "2027-03-03T10:00", "--title", title+"-moved", id)
	moved := runJSON(t, "meet", "meetings", "get", url)
	if moved["title"] != title+"-moved" || !strings.Contains(moved["start"].(string), "2027-03-03") {
		t.Errorf("after the update: %v", moved)
	}
	if moved["url"] != url {
		t.Error("moving a meeting changed its link")
	}

	runOK(t, "meet", "meetings", "delete", id)
	if _, _, code := run(t, "meet", "meetings", "get", id); code != 3 {
		t.Errorf("a deleted meeting answers exit %d, want 3", code)
	}
}
