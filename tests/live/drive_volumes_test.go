package live

import "testing"

// The volumes files and photos are kept on, and what a password reset does to
// them.
//
// A locked volume is the state a reset leaves behind, and no test may reset an
// account: the volume would stay locked for good and the keys with it. So what
// is live here is the listing - what Proton actually answers for an account in
// use - and the two refusals that keep a verb meant for a locked volume off an
// active one. Restoring is pinned against the key material itself, in
// internal/service/drive/volumes_test.go.

func TestDriveVolumesListsTheActiveVolume(t *testing.T) {
	volumes := runJSONArray(t, "drive", "volumes", "list")
	if len(volumes) == 0 {
		t.Fatal("an account with files has at least one volume")
	}
	var active map[string]interface{}
	for _, v := range volumes {
		row, ok := v.(map[string]interface{})
		if !ok {
			t.Fatalf("a volume reads as %T, want an object", v)
		}
		for _, key := range []string{"id", "type", "state", "used_space", "created"} {
			if _, ok := row[key]; !ok {
				t.Errorf("missing %q in %v", key, keysOf(row))
			}
		}
		if row["state"] == "active" && row["type"] == "files" {
			active = row
		}
	}
	if active == nil {
		t.Fatalf("no active files volume in %v; every command works in one", volumes)
	}
	if id, _ := active["id"].(string); id == "" {
		t.Error("the active volume has no ID to name it by")
	}
	// Nothing is said about a restore nobody asked for.
	if restore, ok := active["restore"]; ok {
		t.Errorf("restore = %v, want the field absent on a volume nobody has restored", restore)
	}
}

// The columns are what somebody decides from, so the text listing names the
// state and what the volume holds.
func TestDriveVolumesListNamesTheStateAndKind(t *testing.T) {
	stdout := runOK(t, "drive", "volumes", "list")
	for _, want := range []string{"ID", "TYPE", "STATE", "USED", "CREATED", "active", "files"} {
		assertContains(t, stdout, want)
	}
}

// Restoring and deleting are for a volume a password reset locked. Pointed at
// the volume in use they are refused from the listing, before anything is sent.
func TestDriveVolumesRefuseAnActiveVolume(t *testing.T) {
	var id string
	for _, v := range runJSONArray(t, "drive", "volumes", "list") {
		row, _ := v.(map[string]interface{})
		if row["state"] == "active" {
			id, _ = row["id"].(string)
			break
		}
	}
	if id == "" {
		t.Fatal("the account has no active volume to point the refusal at")
	}

	for _, verb := range []string{"restore", "delete"} {
		_, stderr, code := run(t, "drive", "volumes", verb, "--yes", id)
		if code != 1 {
			t.Errorf("%s on the volume in use exited %d, want 1", verb, code)
		}
		assertContains(t, stderr, "is not locked")
	}
}

// With nothing named and no --all, a verb that acts on a volume says so rather
// than reading an empty command line as every volume there is.
func TestDriveVolumesRefuseABareCommandLine(t *testing.T) {
	for _, verb := range []string{"restore", "delete"} {
		_, stderr, code := run(t, "drive", "volumes", verb)
		if code != 1 {
			t.Errorf("%s with nothing named exited %d, want 1", verb, code)
		}
		assertContains(t, stderr, "Nothing selected.")
		assertContains(t, stderr, "--all")
	}
}

// An account with nothing locked has nothing --all can act on, and a change of
// nothing is reported as such rather than refused.
func TestDriveVolumesRestoreAllWithNothingLocked(t *testing.T) {
	stdout, stderr := runOKStderr(t, "drive", "volumes", "restore", "--all")
	assertContains(t, stdout+stderr, "Nothing to restore.")
}
