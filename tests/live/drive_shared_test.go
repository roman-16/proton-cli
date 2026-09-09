package live

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// What other people have shared with you: items you were invited into, and
// public links you saved.
//
// The two arrive differently and sit in one listing, so what these prove is that
// each is reachable by the same `--shared REF` and removed by the verb that
// matches what removing it costs.

// sharedLink puts a file behind a public link on the primary account and hands
// back the URL, which is everything the second account needs to open it.
func sharedLink(t *testing.T, name, content string, args ...string) (folder, url string) {
	t.Helper()
	folder = "/" + testID() + "-" + name
	src := filepath.Join(t.TempDir(), "payload.txt")
	if err := os.WriteFile(src, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	runOK(t, "drive", "items", "create", folder)
	cleanupRun(t, fmt.Sprintf("Delete folder: proton drive items delete --permanent %s", folder),
		"drive", "items", "delete", folder)
	runOK(t, "drive", "items", "upload", src, folder)

	// The link is read as the record it is: `share link` answers with the URL and
	// what else it set, so the URL is a field of the answer rather than the whole
	// of it.
	link := runJSON(t, append([]string{"drive", "items", "share", "link", folder}, args...)...)
	url, _ = link["url"].(string)
	if !strings.Contains(url, "#") {
		t.Fatalf("public link carries no password: %q", url)
	}
	return folder, url
}

// A link somebody sent you is a tree: the second account reads it without being
// a member of anything, and without the folder appearing in its own files.
func TestDriveSharedLinkIsReadableByAnotherAccount(t *testing.T) {
	_, url := sharedLink(t, "openlink", "link-payload")

	listing := runOKSecondary(t, "drive", "items", "list", "/", "--link", url)
	assertContains(t, listing, "payload.txt")

	out := filepath.Join(t.TempDir(), "downloaded")
	runOKSecondary(t, "drive", "items", "download", "/payload.txt", "--link", url, "--dest", out)
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "link-payload" {
		t.Errorf("downloaded %q, want %q", got, "link-payload")
	}
}

// A link is the one thing here that needs no account, so it is read with none:
// a profile nobody signed in lists the tree, downloads from it and shows what it
// holds.
//
// A link names nobody as the author of what its owner put there, so the
// signature reads anonymous and nothing is warned about - there is no guarantee
// here that was lost.
func TestDriveSharedLinkOpensWithoutAnAccount(t *testing.T) {
	_, url := sharedLink(t, "nobody", "nobody-payload")
	profile := "no-such-" + testID()
	nobody := map[string]string{"PROTON_PROFILE": profile}

	listing, stderr, code := runWithEnv(t, nobody, "drive", "items", "list", "/", "--link", url)
	if code != 0 {
		t.Fatalf("listing a link with no account exited %d:\n%s", code, truncateOutput(stderr))
	}
	assertContains(t, listing, "payload.txt")

	out := filepath.Join(t.TempDir(), "downloaded")
	_, stderr, code = runWithEnv(t, nobody, "drive", "items", "download", "/payload.txt",
		"--link", url, "--dest", out)
	if code != 0 {
		t.Fatalf("downloading from a link with no account exited %d:\n%s", code, truncateOutput(stderr))
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "nobody-payload" {
		t.Errorf("downloaded %q, want %q", got, "nobody-payload")
	}
	assertNotContains(t, stderr, "who wrote it cannot be confirmed")

	stdout, stderr, code := runWithEnv(t, nobody,
		asJSON([]string{"drive", "items", "get", "/payload.txt", "--link", url})...)
	if code != 0 {
		t.Fatalf("showing an item in a link with no account exited %d:\n%s", code, truncateOutput(stderr))
	}
	details := parseJSONObject(t, stdout)
	if details["signature"] != "anonymous" {
		t.Errorf("signature = %v, want anonymous for an item that names no author", details["signature"])
	}
	if by, named := details["created_by"]; named {
		t.Errorf("created_by = %v, want a link to name nobody", by)
	}

	// A link mints a session of its own, and it is nobody's account: a profile
	// that opened one is still a profile nobody signed in.
	configDir, err := os.UserConfigDir()
	if err != nil {
		t.Fatalf("user config dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(configDir, "proton-cli", "sessions", profile+".json")); err == nil {
		t.Errorf("opening a link wrote a session file for %q", profile)
	}
}

// The details of something reached through a link say which link it was, so a
// script that was handed one can report where the bytes came from.
func TestDriveSharedLinkItemDetailsNameTheLink(t *testing.T) {
	_, url := sharedLink(t, "linkget", "get-payload")

	info := runJSONSecondary(t, "drive", "items", "get", "/payload.txt", "--link", url)
	if info["url"] != url {
		t.Errorf("item reports url %v, want %s", info["url"], url)
	}
	// Being signed in buys no more of the story about somebody else's file: what a
	// link tells its reader is the tree, and not whose address put it there.
	if info["signature"] != "anonymous" {
		t.Errorf("signature = %v, want anonymous for an item that names no author", info["signature"])
	}
	if by, named := info["created_by"]; named {
		t.Errorf("created_by = %v, want a link to name nobody", by)
	}
	if info["shared"] != true {
		t.Errorf("an item behind a public link reports shared %v, want true", info["shared"])
	}
}

// A link with a password of its own is refused until it is given, and the
// refusal says how to give it.
func TestDriveSharedLinkPasswordIsAskedForAndAccepted(t *testing.T) {
	_, url := sharedLink(t, "linkpw", "pw-payload",
		"--link-password-file", passwordFile(t, "hunter2"))

	_, stderr, code := runSecondary(t, "drive", "items", "list", "/", "--link", url)
	if code != 1 {
		t.Errorf("opening a password-protected link without the password exited %d, want 1", code)
	}
	assertContains(t, stderr, "This link has a password")
	assertContains(t, stderr, "--link-password-file")

	_, wrong, code := runSecondary(t, "drive", "items", "list", "/", "--link", url,
		"--link-password-file", passwordFile(t, "not-the-password"))
	if code != 1 {
		t.Errorf("a wrong link password exited %d, want 1", code)
	}
	assertContains(t, wrong, "not this link's password")

	listing := runOKSecondary(t, "drive", "items", "list", "/", "--link", url,
		"--link-password-file", passwordFile(t, "hunter2"))
	assertContains(t, listing, "payload.txt")
}

// A link that no longer exists is the answer somebody gets most often, so it is
// a sentence rather than a code, and exits as anything else that is not there.
func TestDriveSharedLinkThatIsGoneIsRefused(t *testing.T) {
	folder, url := sharedLink(t, "unlinked", "gone-payload")
	runOK(t, "drive", "items", "share", "unlink", folder)

	_, stderr, code := runSecondary(t, "drive", "items", "list", "/", "--link", url)
	if code != 3 {
		t.Errorf("a removed link exited %d, want 3", code)
	}
	assertContains(t, stderr, "does not exist any more")
}

// Saving a link puts it in the one listing of what other people have shared,
// where it is opened by name and needs neither the URL nor the password again.
func TestDriveSharedAddListOpenAndRemove(t *testing.T) {
	folder, url := sharedLink(t, "bookmark", "saved-payload",
		"--link-password-file", passwordFile(t, "hunter2"))
	token := tokenOf(t, url)

	stdout, stderr := runOKStderrSecondary(t, "drive", "shared", "add", url,
		"--link-password-file", passwordFile(t, "hunter2"))
	if strings.TrimSpace(stdout) != token {
		t.Errorf("shared add wrote %q on stdout, want the token %s", strings.TrimSpace(stdout), token)
	}
	assertContains(t, stderr, "Added shared item")
	cleanupRunSecondary(t, fmt.Sprintf("Forget saved link: proton drive shared remove %s", token),
		"drive", "shared", "remove", token)

	// The link points at the folder, so that is what the listing names - decrypted
	// with the password that came back with it.
	listing := runOKSecondary(t, "drive", "shared", "list")
	assertContains(t, listing, token)
	assertContains(t, listing, "public link")
	assertContains(t, listing, strings.TrimPrefix(folder, "/"))

	// The password came back with it, so nothing has to be passed a second time.
	out := filepath.Join(t.TempDir(), "downloaded")
	runOKSecondary(t, "drive", "items", "download", "/payload.txt", "--shared", token, "--dest", out)
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "saved-payload" {
		t.Errorf("downloaded %q, want %q", got, "saved-payload")
	}

	details := runJSONSecondary(t, "drive", "items", "get", "/payload.txt", "--shared", token)
	if details["link_password"] != "hunter2" {
		t.Errorf("a saved link reports link_password %v, want the one its owner set", details["link_password"])
	}

	runOKSecondary(t, "drive", "shared", "remove", token)
	assertNotContains(t, runOKSecondary(t, "drive", "shared", "list"), token)
}

// A link that allows editing takes new files and new folders, from whoever holds
// the URL, and what lands there is the owner's to read back.
func TestDriveSharedLinkTakesUploadsWhenItAllowsEditing(t *testing.T) {
	folder, url := sharedLink(t, "linkedit", "edit-payload", "--edit")

	root := runJSONSecondary(t, "drive", "items", "get", "/", "--link", url)
	if root["link_access"] != "edit" {
		t.Errorf("link_access = %v, want edit", root["link_access"])
	}

	dir := t.TempDir()
	src := filepath.Join(dir, "uploaded.txt")
	writeLocal(t, src, "uploaded-into-a-link")
	runOKSecondary(t, "drive", "items", "upload", src, "/", "--link", url)

	// The same name again is the question every upload can be asked, and a link
	// answers it the way your own files do.
	_, renamed := runOKStderrSecondary(t, "drive", "items", "upload", "--if-exists", "rename",
		src, "/", "--link", url)
	assertContains(t, renamed, "uploaded (1).txt")

	inner := filepath.Join(dir, "album", "inner")
	if err := os.MkdirAll(inner, 0o700); err != nil {
		t.Fatal(err)
	}
	writeLocal(t, filepath.Join(inner, "photo.txt"), "photo-bytes")
	runOKSecondary(t, "drive", "items", "upload", "--recursive", filepath.Join(dir, "album"), "/", "--link", url)
	runOKSecondary(t, "drive", "items", "create", "/2026", "--link", url)

	listing := runOK(t, "drive", "items", "list", folder)
	for _, want := range []string{"uploaded.txt", "uploaded (1).txt", "album", "2026"} {
		assertContains(t, listing, want)
	}
	out := filepath.Join(dir, "roundtrip")
	runOK(t, "drive", "items", "download", folder+"/album/inner/photo.txt", "--dest", out)
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "photo-bytes" {
		t.Errorf("what came back out of the link reads %q, want photo-bytes", got)
	}

	// Signed in, an upload into somebody's link names the address that made it,
	// which is what tells the owner who put it there.
	info := runJSON(t, "drive", "items", "get", folder+"/uploaded.txt")
	by, _ := info["created_by"].(string)
	if !strings.EqualFold(by, secondaryEmail()) {
		t.Errorf("created_by = %q, want the account that uploaded it (%s)", by, secondaryEmail())
	}
}

// A link is for people who have no account, and uploading is the other half of
// that: a profile nobody signed in previews the upload and then makes it.
//
// What lands names nobody, because there was nobody to name - which is what the
// owner sees, and the same thing a browser upload from a signed-out visitor
// leaves behind.
func TestDriveSharedLinkTakesUploadsWithoutAnAccount(t *testing.T) {
	folder, url := sharedLink(t, "linknobody", "nobody-edit", "--edit")
	nobody := map[string]string{"PROTON_PROFILE": "no-such-" + testID()}

	src := filepath.Join(t.TempDir(), "anonymous.txt")
	writeLocal(t, src, "anonymous-payload")

	_, stderr, code := runWithEnv(t, nobody, "drive", "items", "upload", src, "/", "--link", url, "--dry-run")
	if code != 0 {
		t.Fatalf("previewing an upload into a link with no account exited %d:\n%s", code, truncateOutput(stderr))
	}
	assertContains(t, stderr, "would upload")
	assertNotContains(t, stderr, "not signed in")

	_, stderr, code = runWithEnv(t, nobody, "drive", "items", "upload", src, "/", "--link", url)
	if code != 0 {
		t.Fatalf("uploading into a link with no account exited %d:\n%s", code, truncateOutput(stderr))
	}

	info := runJSON(t, "drive", "items", "get", folder+"/anonymous.txt")
	if by, named := info["created_by"]; named {
		t.Errorf("created_by = %v, want nobody named for an upload nobody was behind", by)
	}
	if info["signature"] != "anonymous" {
		t.Errorf("signature = %v, want anonymous", info["signature"])
	}

	dir := t.TempDir()
	runOK(t, "drive", "items", "download", folder+"/anonymous.txt", "--dest-dir", dir)
	got, err := os.ReadFile(filepath.Join(dir, "anonymous.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "anonymous-payload" {
		t.Errorf("what nobody uploaded reads %q, want anonymous-payload", got)
	}
}

// A link that allows viewing only says so before anything is planned, whether it
// was named by its URL or saved - which is what the owner's own page shows as no
// way to upload at all.
func TestDriveSharedLinkThatAllowsViewingRefusesUploads(t *testing.T) {
	_, url := sharedLink(t, "linkview", "view-payload")
	token := tokenOf(t, url)
	src := filepath.Join(t.TempDir(), "note.txt")
	writeLocal(t, src, "nope")

	info := runJSONSecondary(t, "drive", "items", "get", "/", "--link", url)
	if info["link_access"] != "view" {
		t.Errorf("link_access = %v, want view", info["link_access"])
	}

	_, stderr, code := runSecondary(t, "drive", "items", "upload", src, "/", "--link", url)
	if code != 1 {
		t.Errorf("uploading into a view-only link exited %d, want 1", code)
	}
	assertContains(t, stderr, "This link allows viewing only")
	assertContains(t, stderr, "allows editing")

	_, stderr, code = runSecondary(t, "drive", "items", "create", "/2026", "--link", url)
	if code != 1 {
		t.Errorf("creating a folder in a view-only link exited %d, want 1", code)
	}
	assertContains(t, stderr, "allows viewing only")

	runOKSecondary(t, "drive", "shared", "add", url)
	cleanupRunSecondary(t, fmt.Sprintf("Forget saved link: proton drive shared remove %s", token),
		"drive", "shared", "remove", token)
	_, stderr, code = runSecondary(t, "drive", "items", "upload", src, "/", "--shared", token)
	if code != 1 {
		t.Errorf("uploading into a saved view-only link exited %d, want 1", code)
	}
	assertContains(t, stderr, "allows viewing only")
}

// What you uploaded into a saved link is yours to take back, and nothing else in
// it is: a link serves one version of a file, has no trash, and what its owner
// put there is theirs.
func TestDriveSavedLinkGivesBackWhatYouUploadedAndNothingElse(t *testing.T) {
	folder, url := sharedLink(t, "savedlink", "owner-payload", "--edit")
	token := tokenOf(t, url)

	runOKSecondary(t, "drive", "shared", "add", url)
	cleanupRunSecondary(t, fmt.Sprintf("Forget saved link: proton drive shared remove %s", token),
		"drive", "shared", "remove", token)

	src := filepath.Join(t.TempDir(), "note.txt")
	writeLocal(t, src, "a-note")
	runOKSecondary(t, "drive", "items", "upload", src, "/", "--shared", token)

	// Your own upload, renamed and then taken back, through the saved link alone.
	_, renamed := runOKStderrSecondary(t, "drive", "items", "update", "/note.txt",
		"--name", "kept.txt", "--shared", token)
	assertContains(t, renamed, "Updated")
	assertContains(t, runOK(t, "drive", "items", "list", folder), "kept.txt")

	_, deleted := runOKStderrSecondary(t, "drive", "items", "delete", "/kept.txt",
		"--shared", token, "--yes")
	assertContains(t, deleted, "Deleted 1 item")
	assertNotContains(t, runOK(t, "drive", "items", "list", folder), "kept.txt")

	// The owner's own file is not the visitor's to touch.
	_, stderr, code := runSecondary(t, "drive", "items", "update", "/payload.txt",
		"--name", "renamed.txt", "--shared", token)
	if code != 1 {
		t.Errorf("renaming somebody else's file in a saved link exited %d, want 1", code)
	}
	assertContains(t, stderr, "not yours to rename here")
	assertContains(t, stderr, "what you uploaded yourself")

	// A link has no trash, and the verbs about what is under an item never reach
	// one - both said as what a link does allow.
	_, stderr, code = runSecondary(t, "drive", "items", "trash", "/payload.txt", "--shared", token)
	if code != 1 {
		t.Errorf("trashing through a saved link exited %d, want 1", code)
	}
	assertContains(t, stderr, "public link")
	assertContains(t, stderr, "rename or delete what you uploaded yourself")

	_, stderr, code = runSecondary(t, "drive", "items", "upload", "--if-exists", "replace",
		src, "/", "--shared", token)
	if code != 1 {
		t.Errorf("writing a new revision through a saved link exited %d, want 1", code)
	}
	assertContains(t, stderr, "cannot take a new revision")
	assertContains(t, stderr, "--if-exists rename")
}

// The link's own visitor takes back what they uploaded, by URL and signed in:
// the rename and the deletion land where the owner can see them, and a folder
// goes with everything in it.
func TestDriveSharedLinkGivesBackYourOwnUploads(t *testing.T) {
	folder, url := sharedLink(t, "linkmine", "mine-payload", "--edit")

	dir := t.TempDir()
	src := filepath.Join(dir, "uploaded.txt")
	writeLocal(t, src, "uploaded-into-a-link")
	runOKSecondary(t, "drive", "items", "upload", src, "/", "--link", url)

	inner := filepath.Join(dir, "album", "inner")
	if err := os.MkdirAll(inner, 0o700); err != nil {
		t.Fatal(err)
	}
	writeLocal(t, filepath.Join(inner, "photo.txt"), "photo-bytes")
	runOKSecondary(t, "drive", "items", "upload", "--recursive", filepath.Join(dir, "album"), "/", "--link", url)

	// What you put into a link is attributed to you where you can see it: your own
	// upload is the one thing a link names an author for, which is what makes it
	// yours to take back.
	mine := runJSONSecondary(t, "drive", "items", "get", "/uploaded.txt", "--link", url)
	if by, _ := mine["created_by"].(string); !strings.EqualFold(by, secondaryEmail()) {
		t.Errorf("created_by = %q, want the account that uploaded it (%s)", by, secondaryEmail())
	}
	if mine["signature"] != "verified" {
		t.Errorf("signature = %v, want verified for your own upload", mine["signature"])
	}

	_, renamed := runOKStderrSecondary(t, "drive", "items", "update", "/uploaded.txt",
		"--name", "holiday.txt", "--link", url)
	assertContains(t, renamed, "Updated")

	listing := runOK(t, "drive", "items", "list", folder)
	assertContains(t, listing, "holiday.txt")
	assertNotContains(t, listing, "uploaded.txt")

	// A folder means its contents here as everywhere else, and a link has no
	// trash for either of them to land in.
	_, deleted := runOKStderrSecondary(t, "drive", "items", "delete", "/holiday.txt", "/album",
		"--link", url, "--yes")
	assertContains(t, deleted, "Deleted 2 items")

	listing = runOK(t, "drive", "items", "list", folder)
	for _, gone := range []string{"holiday.txt", "album"} {
		assertNotContains(t, listing, gone)
	}
	assertContains(t, listing, "payload.txt")
}

// What is not yours is said before the question rather than after the answer: a
// bulk delete names what it will not touch, deletes the rest, and exits 0.
func TestDriveSharedLinkNamesWhatIsNotYoursToDelete(t *testing.T) {
	folder, url := sharedLink(t, "linktheirs", "theirs-payload", "--edit")

	src := filepath.Join(t.TempDir(), "mine.txt")
	writeLocal(t, src, "mine")
	runOKSecondary(t, "drive", "items", "upload", src, "/", "--link", url)

	_, stderr, code := runSecondary(t, "drive", "items", "delete", "/payload.txt", "/mine.txt",
		"--link", url, "--yes")
	if code != 0 {
		t.Fatalf("deleting a mix of yours and theirs exited %d:\n%s", code, truncateOutput(stderr))
	}
	assertContains(t, stderr, "/payload.txt is not yours to delete here")
	assertContains(t, stderr, "Deleted 1 item")

	listing := runOK(t, "drive", "items", "list", folder)
	assertContains(t, listing, "payload.txt")
	assertNotContains(t, listing, "mine.txt")

	// Nothing left to delete is an answer rather than a failure.
	_, nothing, code := runSecondary(t, "drive", "items", "delete", "/payload.txt", "--link", url, "--yes")
	if code != 0 {
		t.Fatalf("deleting only what is not yours exited %d:\n%s", code, truncateOutput(nothing))
	}
	assertContains(t, nothing, "Nothing to delete")
}

// Without an account there is nothing to take an upload back with: Proton ties
// it to the session that made it, and that session ends with the run. The
// refusal says so before the link is even read for what is in it.
func TestDriveSharedLinkRefusesToGiveBackWithoutAnAccount(t *testing.T) {
	_, url := sharedLink(t, "linkanon", "anon-payload", "--edit")
	nobody := map[string]string{"PROTON_PROFILE": "no-such-" + testID()}

	src := filepath.Join(t.TempDir(), "anonymous.txt")
	writeLocal(t, src, "anonymous-payload")
	_, stderr, code := runWithEnv(t, nobody, "drive", "items", "upload", src, "/", "--link", url)
	if code != 0 {
		t.Fatalf("uploading into a link with no account exited %d:\n%s", code, truncateOutput(stderr))
	}

	for _, args := range [][]string{
		{"drive", "items", "update", "/anonymous.txt", "--name", "renamed.txt", "--link", url},
		{"drive", "items", "delete", "/anonymous.txt", "--link", url, "--yes"},
	} {
		_, stderr, code := runWithEnv(t, nobody, args...)
		if code != 1 {
			t.Errorf("%s with no account exited %d, want 1", args[2], code)
		}
		assertContains(t, stderr, "Without an account")
		assertContains(t, stderr, "yours to change for an hour")
	}
}

// Leaving a saved link is refused: a link is not a share anybody is a member of,
// and the refusal names the verb that does remove it.
func TestDriveSharedLeaveRefusesASavedLink(t *testing.T) {
	_, url := sharedLink(t, "verbs", "verbs-payload")
	token := tokenOf(t, url)
	runOKSecondary(t, "drive", "shared", "add", url)
	cleanupRunSecondary(t, fmt.Sprintf("Forget saved link: proton drive shared remove %s", token),
		"drive", "shared", "remove", token)

	_, stderr, code := runSecondary(t, "drive", "shared", "leave", token)
	if code != 1 {
		t.Errorf("leaving a saved link exited %d, want 1", code)
	}
	assertContains(t, stderr, "public link")
	assertContains(t, stderr, "shared remove")
}

// Leaving is the one thing in this collection nobody can undo from here: it is
// refused for the kind it is not about, it stops for a yes, and with nobody to
// ask it declines rather than acting.
func TestDriveSharedLeaveGivesUpAnItemSomebodyShared(t *testing.T) {
	folder := "/" + testID() + "-leave"
	runOK(t, "drive", "items", "create", folder)
	cleanupRun(t, fmt.Sprintf("Delete folder: proton drive items delete --permanent %s", folder),
		"drive", "items", "delete", folder)
	id, _ := sharedWithSecondary(t, folder)["link_id"].(string)

	_, stderr, code := runSecondary(t, "drive", "shared", "remove", id)
	if code != 1 {
		t.Errorf("forgetting a directly shared item exited %d, want 1", code)
	}
	assertContains(t, stderr, "shared with you directly")
	assertContains(t, stderr, "shared leave")

	_, stderr, code = runSecondary(t, "drive", "shared", "leave", id)
	if code != 1 {
		t.Errorf("leaving without a terminal exited %d, want 1", code)
	}
	assertContains(t, stderr, "cannot be undone")
	assertContains(t, stderr, "--yes")
	assertContains(t, runOKSecondary(t, "drive", "shared", "list"), id)

	runOKSecondary(t, "drive", "shared", "leave", id, "--yes")
	assertNotContains(t, runOKSecondary(t, "drive", "shared", "list"), id)
}
