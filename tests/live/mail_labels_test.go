package live

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/roman-16/proton-cli/tests/account"
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

	// The parent is named rather than given as an ID, which is what every
	// reference into another collection accepts.
	childName := testID() + "-child"
	childID := strings.TrimSpace(runOK(t, "mail", "settings", "folders", "create", "--name", childName, "--parent", parentName, "--color", "#8080FF"))
	cleanupRun(t, fmt.Sprintf("Delete child folder: proton mail settings folders delete %s", childID),
		"mail", "settings", "folders", "delete", "--", childID)

	if got := folderParent(t, childName); got != parentID {
		t.Errorf("child folder parent = %q, want %q", got, parentID)
	}

	// A folder leaves the one that held it, which nothing but naming no parent
	// at all can express.
	runOK(t, "mail", "settings", "folders", "update", "--parent", "none", childID)
	if got := folderParent(t, childName); got != "" {
		t.Errorf("a folder moved to the top level reports parent %q", got)
	}
}

// Naming folders puts them in front of the folders they sit beside, and the
// listing is where that shows.
//
// A free account keeps three folders and one of them is the fixture's, so two
// is the whole of what this may make.
func TestMailFoldersReorder(t *testing.T) {
	second := testID() + "-b"
	secondID := strings.TrimSpace(runOK(t, "mail", "settings", "folders", "create",
		"--name", second, "--color", "#8080FF"))
	cleanupRun(t, fmt.Sprintf("Delete folder: proton mail settings folders delete %s", secondID),
		"mail", "settings", "folders", "delete", "--", secondID)

	first := testID() + "-a"
	firstID := strings.TrimSpace(runOK(t, "mail", "settings", "folders", "create",
		"--name", first, "--color", "#8080FF"))
	cleanupRun(t, fmt.Sprintf("Delete folder: proton mail settings folders delete %s", firstID),
		"mail", "settings", "folders", "delete", "--", firstID)

	runOK(t, "mail", "settings", "folders", "reorder", first, second)
	got := folderOrder(t, "")
	if len(got) < 2 || got[0] != first || got[1] != second {
		t.Errorf("the folders are in the order %v, want %q and %q in front", got, first, second)
	}
}

// A folder inside another is ordered among that folder's own children, which is
// a list of its own.
//
// The pair goes inside the fixture's folder, which is what leaves room for both
// of them: three folders is the whole of what a free account keeps.
func TestMailFoldersReorderInsideAFolder(t *testing.T) {
	parent := pinned(t, account.Primary, "folder", "Projects")
	parentName, _ := parent["name"].(string)
	parentID, _ := parent["id"].(string)

	var children []string
	for _, suffix := range []string{"-b", "-a"} {
		name := testID() + suffix
		id := strings.TrimSpace(runOK(t, "mail", "settings", "folders", "create",
			"--name", name, "--parent", parentName, "--color", "#8080FF"))
		cleanupRun(t, fmt.Sprintf("Delete folder: proton mail settings folders delete %s", id),
			"mail", "settings", "folders", "delete", "--", id)
		children = append(children, name)
	}

	runOK(t, "mail", "settings", "folders", "reorder", children[1])
	want := []string{children[1], children[0]}
	if got := folderOrder(t, parentID); !equalStrings(got, want) {
		t.Errorf("what is inside the folder reads %v, want %v", got, want)
	}

	// A listing shows each folder before what is inside it.
	var seen []string
	for _, row := range runJSONArray(t, "mail", "settings", "folders", "list") {
		m := row.(map[string]interface{})
		if m["name"] == parentName || parentOf(m) == parentID {
			seen = append(seen, fmt.Sprint(m["name"]))
		}
	}
	if !equalStrings(seen, append([]string{parentName}, want...)) {
		t.Errorf("the listing reads %v, want the folder ahead of what is inside it", seen)
	}
}

// Labels are one list, and --alphabetical is the whole of it sorted by name.
//
// A free account keeps three labels and one of them is the fixture's, so two is
// the whole of what this may make.
func TestMailLabelsReorder(t *testing.T) {
	var names []string
	for _, suffix := range []string{"-b", "-a"} {
		name := testID() + suffix
		id := strings.TrimSpace(runOK(t, "mail", "settings", "labels", "create",
			"--name", name, "--color", "#8080FF"))
		cleanupRun(t, fmt.Sprintf("Delete label: proton mail settings labels delete %s", id),
			"mail", "settings", "labels", "delete", "--", id)
		names = append(names, name)
	}

	runOK(t, "mail", "settings", "labels", "reorder", names[1])
	if got := labelOrder(t)[0]; got != names[1] {
		t.Errorf("the first label is %q, want %q", got, names[1])
	}

	runOK(t, "mail", "settings", "labels", "reorder", "--alphabetical")
	got := labelOrder(t)
	sorted := append([]string(nil), got...)
	sort.SliceStable(sorted, func(i, j int) bool {
		return strings.ToLower(sorted[i]) < strings.ToLower(sorted[j])
	})
	if !equalStrings(got, sorted) {
		t.Errorf("the labels are in the order %v, which is not alphabetical", got)
	}
}

// folderOrder is what one folder holds, in the order the account keeps it. The
// top level is a folder like any other here, named by the empty parent.
func folderOrder(t *testing.T, parentID string) []string {
	t.Helper()
	return folderNames(runJSONArray(t, "mail", "settings", "folders", "list"), parentID)
}

func folderNames(rows []interface{}, parentID string) []string {
	var out []string
	for _, row := range rows {
		if m := row.(map[string]interface{}); parentOf(m) == parentID {
			out = append(out, fmt.Sprint(m["name"]))
		}
	}
	return out
}

// parentOf is the folder a row sits in, and "" for one at the top level, which
// reports no parent at all.
func parentOf(row map[string]interface{}) string {
	if v, ok := row["parent"]; ok {
		return fmt.Sprint(v)
	}
	return ""
}

func labelOrder(t *testing.T) []string {
	t.Helper()
	var out []string
	for _, row := range runJSONArray(t, "mail", "settings", "labels", "list") {
		out = append(out, fmt.Sprint(row.(map[string]interface{})["name"]))
	}
	return out
}

// folderParent is where the listing says one folder sits.
func folderParent(t *testing.T, name string) string {
	t.Helper()
	for _, row := range runJSONArray(t, "mail", "settings", "folders", "list") {
		if m := row.(map[string]interface{}); m["name"] == name {
			return parentOf(m)
		}
	}
	t.Fatalf("folder %q is not in the list", name)
	return ""
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
