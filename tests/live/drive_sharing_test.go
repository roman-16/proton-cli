package live

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Sharing something out of Drive: a public link anybody can open, and the people
// invited to it by name.
//
// A link's key travels in the URL fragment, which a browser never sends to
// Proton - so the whole URL is the secret, and a link that cannot be opened
// publicly still looks fine from this end.

func tokenOf(t *testing.T, url string) string {
	t.Helper()
	i := strings.Index(url, "/urls/")
	if i < 0 {
		t.Fatalf("url %q has no /urls/ segment", url)
	}
	rest := url[i+len("/urls/"):]
	if h := strings.Index(rest, "#"); h >= 0 {
		return rest[:h]
	}
	return rest
}

func TestDriveShareLinkLifecycle(t *testing.T) {
	folder := "/" + testID() + "-share"
	runOK(t, "drive", "items", "create", folder)
	cleanupRun(t, fmt.Sprintf("Delete folder: proton drive items delete --permanent %s", folder),
		"drive", "items", "delete", folder)

	url := strings.TrimSpace(runOK(t, "drive", "items", "share", "link", folder))
	if !strings.Contains(url, "/urls/") {
		t.Fatalf("link stdout has no public URL: %q", url)
	}
	if !strings.Contains(url, "#") {
		t.Errorf("public link is missing the password fragment: %q", url)
	}

	status := runOK(t, "drive", "items", "share", "get", folder)
	assertContains(t, status, "Public Link:")
	assertContains(t, status, tokenOf(t, url))

	runOK(t, "drive", "items", "share", "unlink", folder)
	after := runOKBothStreams(t, "drive", "items", "share", "get", folder)
	assertField(t, after, "Shared:", "no")
}

// TestDriveShareLinkPublicHandshake guards the SRP-salt regression: a created
// link must be publicly resolvable. The public handshake fails auth (and the
// link 404s in a browser) if UrlPasswordSalt is not exactly 10 bytes, even
// though creation and status both still succeed.
func TestDriveShareLinkPublicHandshake(t *testing.T) {
	folder := "/" + testID() + "-handshake"
	runOK(t, "drive", "items", "create", folder)
	cleanupRun(t, fmt.Sprintf("Delete folder: proton drive items delete --permanent %s", folder),
		"drive", "items", "delete", folder)

	url := strings.TrimSpace(runOK(t, "drive", "items", "share", "link", folder))
	token := tokenOf(t, url)
	linkID := strings.TrimSpace(runJSON(t, "drive", "items", "get", folder)["link_id"].(string))

	info := runJSON(t, "api", "GET", "/drive/urls/"+token+"/info")
	if code, _ := info["Code"].(float64); int(code) != 1000 {
		t.Fatalf("public handshake Code = %v, want 1000", info["Code"])
	}
	salt, _ := info["UrlPasswordSalt"].(string)
	decoded, err := base64.StdEncoding.DecodeString(salt)
	if err != nil {
		t.Fatalf("UrlPasswordSalt not base64: %v", err)
	}
	if len(decoded) != 10 {
		t.Errorf("UrlPasswordSalt = %d bytes, want 10 (Proton SRP salt); a wrong length breaks public auth", len(decoded))
	}
	da, _ := info["DirectAccess"].(map[string]interface{})
	if da == nil || da["LinkID"] != linkID {
		t.Errorf("public link binds to %v, want LinkID %s", da["LinkID"], linkID)
	}
}

func TestDriveShareLinkIdempotent(t *testing.T) {
	folder := "/" + testID() + "-idem"
	runOK(t, "drive", "items", "create", folder)
	cleanupRun(t, fmt.Sprintf("Delete folder: proton drive items delete --permanent %s", folder),
		"drive", "items", "delete", folder)

	first := strings.TrimSpace(runOK(t, "drive", "items", "share", "link", folder))
	second := strings.TrimSpace(runOK(t, "drive", "items", "share", "link", folder))
	if tokenOf(t, first) != tokenOf(t, second) {
		t.Errorf("link is not idempotent: %q vs %q", first, second)
	}
}

func TestDriveShareLinkExpires(t *testing.T) {
	folder := "/" + testID() + "-exp"
	runOK(t, "drive", "items", "create", folder)
	cleanupRun(t, fmt.Sprintf("Delete folder: proton drive items delete --permanent %s", folder),
		"drive", "items", "delete", folder)

	link := runJSON(t, "drive", "items", "share", "link", folder, "--expires", "7d")
	if link["expire_time"] == nil {
		t.Errorf("expected expire_time to be set, got %v", link["expire_time"])
	}

	permanent := runJSON(t, "drive", "items", "share", "link", folder, "--expires", "never")
	if permanent["expire_time"] != nil {
		t.Errorf("--expires never left expire_time %v", permanent["expire_time"])
	}
}

func TestDriveShareLinkPassword(t *testing.T) {
	folder := "/" + testID() + "-pw"
	runOK(t, "drive", "items", "create", folder)
	cleanupRun(t, fmt.Sprintf("Delete folder: proton drive items delete --permanent %s", folder),
		"drive", "items", "delete", folder)

	// The record is the answer, so the custom password is reported there rather
	// than in the confirmation on stderr.
	stdout := runOK(t, "drive", "items", "share", "link", folder,
		"--link-password-file", passwordFile(t, "hunter2"))
	if !strings.Contains(stdout, "#") {
		t.Errorf("link should still carry a generated fragment: %q", stdout)
	}
	assertField(t, stdout, "Password:", "hunter2")
	assertField(t, runOK(t, "drive", "items", "share", "get", folder), "Link Password:", "hunter2")

	// A password read from a file has no way of saying "none", so taking one off
	// is its own word.
	runOK(t, "drive", "items", "share", "link", folder, "--clear-link-password")
	assertNotContains(t, runOK(t, "drive", "items", "share", "get", folder), "Link Password:")
}

// passwordFile is how a secret reaches the CLI: a file only its owner can read,
// never a flag value.
func passwordFile(t *testing.T, password string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "password")
	if err := os.WriteFile(path, []byte(password), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestDriveShareLinkDryRun(t *testing.T) {
	folder := "/" + testID() + "-sharedry"
	runOK(t, "drive", "items", "create", folder)
	cleanupRun(t, fmt.Sprintf("Delete folder: proton drive items delete --permanent %s", folder),
		"drive", "items", "delete", folder)

	_, stderr := runOKStderr(t, "--dry-run", "drive", "items", "share", "link", folder)
	assertContains(t, stderr, "Dry run")

	status := runOKBothStreams(t, "drive", "items", "share", "get", folder)
	assertField(t, status, "Shared:", "no")
}

// A link that already exists is changed rather than replaced, so the address
// people were given keeps working.
func TestDriveShareLinkUpdatesTheLinkItAlreadyHas(t *testing.T) {
	folder := "/" + testID() + "-relink"
	runOK(t, "drive", "items", "create", folder)
	cleanupRun(t, fmt.Sprintf("Delete folder: proton drive items delete --permanent %s", folder),
		"drive", "items", "delete", folder)

	first := strings.TrimSpace(runOK(t, "drive", "items", "share", "link", folder))
	updated := runOK(t, "drive", "items", "share", "link", folder,
		"--link-password-file", passwordFile(t, "hunter2"))

	assertField(t, updated, "Password:", "hunter2")
	if !strings.Contains(updated, tokenOf(t, first)) {
		t.Errorf("setting a password made a new link:\nwas %q\nnow %s", first, truncateOutput(updated))
	}
	assertField(t, runOK(t, "drive", "items", "share", "get", folder), "Link Password:", "hunter2")
}

func TestDriveShareAddNotProtonUser(t *testing.T) {
	folder := "/" + testID() + "-member"
	runOK(t, "drive", "items", "create", folder)
	cleanupRun(t, fmt.Sprintf("Delete folder: proton drive items delete --permanent %s", folder),
		"drive", "items", "delete", folder)

	_, stderr, code := run(t, "drive", "items", "share", "add", folder, "nobody-"+testID()+"@example.invalid")
	if code == 0 {
		t.Error("expected non-zero exit inviting a non-Proton address")
	}
	if !strings.Contains(stderr, "not a Proton address") {
		t.Errorf("expected 'not a Proton address' error, got: %s", stderr)
	}
}

func TestDriveShareAddDryRun(t *testing.T) {
	folder := "/" + testID() + "-memberdry"
	runOK(t, "drive", "items", "create", folder)
	cleanupRun(t, fmt.Sprintf("Delete folder: proton drive items delete --permanent %s", folder),
		"drive", "items", "delete", folder)

	_, stderr := runOKStderr(t, "--dry-run", "drive", "items", "share", "add", folder, "someone@example.invalid")
	assertContains(t, stderr, "Dry run")
}

// TestDriveShareMemberRoundTrip invites a real Proton address, verifies it shows
// as pending, then revokes it.
func TestDriveShareMemberRoundTrip(t *testing.T) {
	invitee := secondaryEmail()
	folder := "/" + testID() + "-memberrt"
	tmp := t.TempDir()
	src := filepath.Join(tmp, "m.txt")
	_ = os.WriteFile(src, []byte("member round-trip"), 0644)
	runOK(t, "drive", "items", "create", folder)
	// Permanent folder deletion cascades the share + any pending invitation.
	cleanupRun(t, fmt.Sprintf("Delete folder: proton drive items delete --permanent %s", folder),
		"drive", "items", "delete", folder)
	runOK(t, "drive", "items", "upload", src, folder)
	file := folder + "/m.txt"

	runOK(t, "drive", "items", "share", "add", file, invitee)
	status := runOK(t, "drive", "items", "share", "get", file)
	assertContains(t, status, invitee)
	assertContains(t, status, "not yet accepted")

	runOK(t, "drive", "items", "share", "remove", file, invitee)
	after := runOKBothStreams(t, "drive", "items", "share", "get", file)
	assertNotContains(t, after, invitee)
}

func TestDriveShareRemoveNotFound(t *testing.T) {
	folder := "/" + testID() + "-rm"
	runOK(t, "drive", "items", "create", folder)
	cleanupRun(t, fmt.Sprintf("Delete folder: proton drive items delete --permanent %s", folder),
		"drive", "items", "delete", folder)

	_, _, code := run(t, "drive", "items", "share", "remove", folder, "nobody@example.invalid")
	if code != 3 {
		t.Errorf("expected exit 3 removing an unknown member, got %d", code)
	}
}

// An item somebody shared with you is reachable once the invitation is accepted,
// and this listing is where it is reachable from.
func TestDriveSharedListsWhatOthersHaveShared(t *testing.T) {
	// The primary shares a folder with the secondary, which accepts it.
	folder := "/" + testID() + "-sharedwithme"
	runOK(t, "drive", "items", "create", folder)
	cleanupRun(t, fmt.Sprintf("Delete: proton drive items delete %s", folder),
		"drive", "items", "delete", folder)

	// The invitation this test caused is the one that was not there before it
	// shared: an invitation names no item, so taking whichever came first would
	// accept something another test left behind.
	before := altInvitationIDs(t)
	runOK(t, "drive", "items", "share", "add", folder, secondaryEmail())
	cleanupRun(t, fmt.Sprintf("Revoke member: proton drive items share remove %s %s", folder, secondaryEmail()),
		"drive", "items", "share", "remove", folder, secondaryEmail())

	var invitationID string
	waitFor(60*time.Second, 3*time.Second, func() bool {
		for id := range altInvitationIDs(t) {
			if !before[id] {
				invitationID = id
				return true
			}
		}
		return false
	})
	if invitationID == "" {
		t.Fatal("the second account never saw the invitation")
	}
	runOKSecondary(t, "drive", "invitations", "accept", "--", invitationID)
	cleanupRunSecondary(t, "Leave the share: remove the member from the primary side",
		"drive", "invitations", "list")

	// It is now reachable, which is the whole point.
	var found bool
	for _, row := range runJSONArraySecondary(t, "drive", "shared", "list") {
		m, _ := row.(map[string]interface{})
		if by, _ := m["shared_by"].(string); strings.EqualFold(by, selfEmail()) {
			found = true
		}
	}
	if !found {
		t.Error("after accepting, the shared folder should appear in `drive shared list`")
	}

	// And the primary can see it going the other way.
	var mine bool
	for _, row := range runJSONArray(t, "drive", "sharing", "list") {
		m, _ := row.(map[string]interface{})
		if name, _ := m["name"].(string); strings.Contains(folder, name) && name != "" {
			mine = true
		}
	}
	if !mine {
		t.Error("what the primary shared should appear in `drive sharing list`")
	}
	runOK(t, "drive", "items", "share", "remove", folder, secondaryEmail())
}

// What somebody may do can be changed without cancelling and re-inviting them,
// and it applies whether they have accepted yet or not.
func TestDriveShareUpdateChangesWhatAnInviteeMayDo(t *testing.T) {
	folder := "/" + testID() + "-perm"
	runOK(t, "drive", "items", "create", folder)
	cleanupRun(t, fmt.Sprintf("Delete: proton drive items delete %s", folder),
		"drive", "items", "delete", folder)

	before := altInvitationIDs(t)
	runOK(t, "drive", "items", "share", "add", folder, secondaryEmail())
	cleanupRun(t, fmt.Sprintf("Revoke: proton drive items share remove %s %s", folder, secondaryEmail()),
		"drive", "items", "share", "remove", folder, secondaryEmail())

	// While the invitation is still unanswered, the role lives on the invitation.
	assertContains(t, runOK(t, "drive", "items", "share", "get", folder), "viewer")
	runOK(t, "drive", "items", "share", "update", folder, secondaryEmail(), "--edit")
	assertContains(t, runOK(t, "drive", "items", "share", "get", folder), "editor")

	// Once it is accepted the role lives on the member instead, which is a
	// different endpoint - so the claim that this works either way is only
	// tested by doing both.
	var invID string
	waitFor(60*time.Second, 3*time.Second, func() bool {
		for id := range altInvitationIDs(t) {
			if !before[id] {
				invID = id
				return true
			}
		}
		return false
	})
	if invID == "" {
		t.Fatal("the second account never saw the invitation")
	}
	runOKSecondary(t, "drive", "invitations", "accept", "--", invID)

	runOK(t, "drive", "items", "share", "update", folder, secondaryEmail(), "--edit=false")
	assertContains(t, runOK(t, "drive", "items", "share", "get", folder), "viewer")
}

// An invitation nobody answered can be sent again; one that was accepted has
// nothing to resend, and says so rather than failing vaguely.
func TestDriveShareResendAnUnansweredInvitation(t *testing.T) {
	folder := "/" + testID() + "-resend"
	runOK(t, "drive", "items", "create", folder)
	cleanupRun(t, fmt.Sprintf("Delete: proton drive items delete %s", folder),
		"drive", "items", "delete", folder)

	runOK(t, "drive", "items", "share", "add", folder, secondaryEmail())
	cleanupRun(t, fmt.Sprintf("Revoke: proton drive items share remove %s %s", folder, secondaryEmail()),
		"drive", "items", "share", "remove", folder, secondaryEmail())

	runOK(t, "drive", "items", "share", "resend", folder, secondaryEmail())

	_, stderr, code := run(t, "drive", "items", "share", "resend", folder, "nobody@example.com")
	if code != 3 {
		t.Errorf("resending to somebody who was never invited should exit 3, got %d", code)
	}
	if !strings.Contains(stderr, "waiting for an answer") {
		t.Errorf("the refusal should say there is no pending invitation, got: %s", stderr)
	}
}
