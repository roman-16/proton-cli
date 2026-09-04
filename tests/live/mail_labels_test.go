package live

import (
	"fmt"
	"strings"
	"testing"
)

// Labels and folders, which Proton stores as one thing and this CLI keeps apart
// because they behave differently.
//
// Proton replaces a whole label rather than patching it, so a change to one
// field has to carry the rest back: a recolour must not rename, and a rename
// must not reset the colour.

func TestMailLabelsList(t *testing.T) {
	name := testID() + "-list"
	id := strings.TrimSpace(runOK(t, "mail", "settings", "labels", "create", "--name", name, "--color", "#8080FF"))
	cleanupRun(t, fmt.Sprintf("Delete label: proton mail settings labels delete %s", id),
		"mail", "settings", "labels", "delete", "--", id)

	stdout := runOK(t, "mail", "settings", "labels", "list")
	assertContains(t, stdout, "NAME")
	assertContains(t, stdout, name)
}

func TestMailLabelsCreateDeleteLabel(t *testing.T) {
	name := testID() + "-label"

	stdout, stderr := runOKStderr(t, "mail", "settings", "labels", "create", "--name", name, "--color", "#8080FF")
	id := assertBareID(t, stdout, "labels create")
	// The record goes to stdout so a script can capture it; the human message
	// goes to stderr so capturing does not swallow it.
	assertContains(t, stderr, "✓")
	cleanupRun(t, fmt.Sprintf("Delete label: proton mail settings labels delete -- %s", id),
		"mail", "settings", "labels", "delete", "--", id)

	list := runOK(t, "mail", "settings", "labels", "list")
	assertContains(t, list, name)
}

// A folder is its own collection, not a label wearing a flag.
func TestMailFoldersCreateDelete(t *testing.T) {
	name := testID() + "-folder"
	stdout := runOK(t, "mail", "settings", "folders", "create", "--name", name, "--color", "#8080FF")
	id := strings.TrimSpace(stdout)
	if !looksLikeID(id) {
		t.Fatalf("expected bare ID on stdout, got %q", stdout)
	}
	cleanupRun(t, fmt.Sprintf("Delete folder: proton mail settings folders delete %s", id),
		"mail", "settings", "folders", "delete", "--", id)

	list := runOK(t, "mail", "settings", "folders", "list")
	assertContains(t, list, name)
	assertContains(t, list, "PATH")
	assertNotContains(t, runOK(t, "mail", "settings", "labels", "list"), name)
}

// The NOTIFY column is what `messages watch` leans on for its default, so it
// has to round-trip: a folder created without telling you, and one turned on.
func TestMailFoldersNotifyToggles(t *testing.T) {
	name := testID() + "-quiet"

	notify := func(name string) string {
		t.Helper()
		for _, r := range runJSONArray(t, "mail", "settings", "folders", "list") {
			row, ok := r.(map[string]interface{})
			if !ok {
				continue
			}
			if row["name"] == name {
				return fmt.Sprint(row["notify"])
			}
		}
		t.Fatalf("folder %q not found in folders list", name)
		return ""
	}

	id := strings.TrimSpace(runOK(t, "mail", "settings", "folders", "create",
		"--name", name, "--color", "#8080FF", "--notify=false"))
	cleanupRun(t, fmt.Sprintf("Delete folder: proton mail settings folders delete %s", id),
		"mail", "settings", "folders", "delete", "--", id)
	if got := notify(name); got != "false" {
		t.Fatalf("folder created with --notify=false reports %s, want false", got)
	}

	runOK(t, "mail", "settings", "folders", "update", "--notify", id)
	if got := notify(name); got != "true" {
		t.Fatalf("folder updated with --notify reports %s, want true", got)
	}
}

func TestMailLabelsUpdate(t *testing.T) {
	name := testID() + "-label"
	id := strings.TrimSpace(runOK(t, "mail", "settings", "labels", "create", "--name", name, "--color", "#8080FF"))
	cleanupRun(t, fmt.Sprintf("Delete label: proton mail settings labels delete %s", id),
		"mail", "settings", "labels", "delete", "--", id)

	newName := name + "-renamed"
	runOK(t, "mail", "settings", "labels", "update", "--name", newName, "--color", "#DB60D6", id)
	assertContains(t, runOK(t, "mail", "settings", "labels", "list"), newName)

	// Proton replaces the whole label rather than patching it, so a change to one
	// field has to carry the rest back: a recolour must not rename, and a rename
	// must not reset the colour.
	runOK(t, "mail", "settings", "labels", "update", "--color", "#3CBB3A", id)
	assertLabel(t, id, newName, "#3CBB3A")

	again := newName + "-again"
	runOK(t, "mail", "settings", "labels", "update", "--name", again, id)
	assertLabel(t, id, again, "#3CBB3A")
}

// assertLabel checks one label's whole record, for the fields an update replaces.
func assertLabel(t *testing.T, id, name, color string) {
	t.Helper()
	for _, row := range runJSONArray(t, "mail", "settings", "labels", "list") {
		m := row.(map[string]interface{})
		if m["id"] != id {
			continue
		}
		if m["name"] != name || m["color"] != color {
			t.Errorf("label is %v/%v, want %v/%v", m["name"], m["color"], name, color)
		}
		return
	}
	t.Errorf("label %s is not in the list", id)
}

func TestMailFoldersNestedReportsParent(t *testing.T) {
	parentName := testID() + "-parent"
	parentID := strings.TrimSpace(runOK(t, "mail", "settings", "folders", "create", "--name", parentName, "--color", "#8080FF"))
	cleanupRun(t, fmt.Sprintf("Delete parent folder: proton mail settings folders delete %s", parentID),
		"mail", "settings", "folders", "delete", "--", parentID)

	childName := testID() + "-child"
	childID := strings.TrimSpace(runOK(t, "mail", "settings", "folders", "create", "--name", childName, "--parent", parentID, "--color", "#8080FF"))
	cleanupRun(t, fmt.Sprintf("Delete child folder: proton mail settings folders delete %s", childID),
		"mail", "settings", "folders", "delete", "--", childID)

	data := runJSON(t, "api", "GET", "/core/v4/labels", "--query", "Type=3")
	labels, _ := data["Labels"].([]interface{})
	var gotParent string
	for _, l := range labels {
		m := l.(map[string]interface{})
		if m["Name"] == childName {
			gotParent, _ = m["ParentID"].(string)
		}
	}
	if gotParent != parentID {
		t.Errorf("child folder ParentID = %q, want %q", gotParent, parentID)
	}
}
