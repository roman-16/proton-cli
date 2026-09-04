package live

import (
	"fmt"
	"strings"
	"testing"
)

// The trash, and getting something back out of it.
//
// A trashed item has no path any more, so its own ID is what identifies it -
// which is why these note the ID before trashing rather than picking whatever
// the listing puts first. Emptying it covers every volume the account has, the
// photo library's included.

func TestDriveItemsDeleteAndTrashRestore(t *testing.T) {
	folder := "/" + testID() + "-trash"
	runOK(t, "drive", "items", "create", folder)
	cleanupRun(t, fmt.Sprintf("Final delete: proton drive items delete --permanent %s", folder),
		"drive", "items", "delete", folder)

	// Noted before the trash: a trashed item has no path any more, so its own ID
	// is what identifies it. Picking the first folder in the trash instead would
	// restore whatever else happened to be in there.
	linkID, _ := runJSON(t, "drive", "items", "get", folder)["link_id"].(string)
	if linkID == "" {
		t.Fatal("drive items get should report the folder's link ID")
	}

	// Non-permanent → trash
	runOK(t, "drive", "items", "trash", folder)

	// Should appear in trash, by name and with the moment it was trashed: the
	// listing is what somebody decides from, so it says what each item is.
	var entry map[string]interface{}
	for _, e := range runJSONArray(t, "drive", "trash", "list") {
		if row, ok := e.(map[string]interface{}); ok && row["link_id"] == linkID {
			entry = row
		}
	}
	if entry == nil {
		t.Fatal("the trashed folder should appear in the trash")
	}
	if name, _ := entry["name"].(string); name != strings.TrimPrefix(folder, "/") {
		t.Errorf("trash list reports name %q, want %q", name, strings.TrimPrefix(folder, "/"))
	}
	if trashed, _ := entry["trashed"].(float64); trashed <= 0 {
		t.Errorf("trash list reports trashed %v, want the moment it was trashed", entry["trashed"])
	}

	// A trashed item has no path, so it is restored by the ID the listing showed.
	runOK(t, "drive", "trash", "restore", "--", linkID)

	// It should be back in root
	top := runJSONArray(t, "drive", "items", "list")
	back := false
	folderName := strings.TrimPrefix(folder, "/")
	for _, c := range top {
		if c.(map[string]interface{})["name"].(string) == folderName {
			back = true
		}
	}
	if !back {
		t.Error("restored folder should be back in root")
	}
}

// Emptying the trash is the one command that acts on everything in it, so it
// takes the trash to itself: another test restoring something would find it
// permanently gone.
func TestDriveTrashEmpty(t *testing.T) {
	folder := "/" + testID() + "-emptytrash"
	runOK(t, "drive", "items", "create", folder)
	linkID, _ := runJSON(t, "drive", "items", "get", folder)["link_id"].(string)
	if linkID == "" {
		t.Fatal("drive items get should report the folder's link ID")
	}
	// No cleanup: emptying the trash is what removes it, and a folder left in
	// the trash by a failure is swept by the next seed.
	runOK(t, "drive", "items", "trash", folder)

	if !trashHolds(t, linkID) {
		t.Fatal("the trashed folder should be in the trash")
	}
	runOK(t, "drive", "trash", "empty")
	if trashHolds(t, linkID) {
		t.Error("the trash still holds the folder after being emptied")
	}
}

func trashHolds(t *testing.T, linkID string) bool {
	t.Helper()
	for _, row := range runJSONArray(t, "drive", "trash", "list") {
		if row.(map[string]interface{})["link_id"] == linkID {
			return true
		}
	}
	return false
}
