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
// A link names nobody as the author of what is in it, so the signature reads
// anonymous and nothing is warned about - there is no guarantee here that was
// lost.
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
	// Being signed in buys no more of the story: what a link tells its reader is
	// the tree, and not whose address wrote what is in it.
	if info["signature"] != "anonymous" {
		t.Errorf("signature = %v, want anonymous for an item that names no author", info["signature"])
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

// A saved link can be read and not written, and the refusal points at what is
// wrong rather than at whatever Proton would have said.
func TestDriveSharedLinkRefusesToBeWrittenTo(t *testing.T) {
	_, url := sharedLink(t, "readonly", "readonly-payload")
	token := tokenOf(t, url)

	runOKSecondary(t, "drive", "shared", "add", url)
	cleanupRunSecondary(t, fmt.Sprintf("Forget saved link: proton drive shared remove %s", token),
		"drive", "shared", "remove", token)

	src := filepath.Join(t.TempDir(), "note.txt")
	if err := os.WriteFile(src, []byte("nope"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, stderr, code := runSecondary(t, "drive", "items", "upload", src, "/", "--shared", token)
	if code != 1 {
		t.Errorf("uploading into a saved link exited %d, want 1", code)
	}
	assertContains(t, stderr, "public link")
	assertContains(t, stderr, "listed and downloaded")
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
