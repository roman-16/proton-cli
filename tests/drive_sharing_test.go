package tests

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ── public links ──

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
	skipIfNoCredentials(t)
	folder := "/" + testID() + "-share"
	runOK(t, "drive", "folders", "create", folder)
	cleanupRun(t, fmt.Sprintf("Delete folder: proton-cli drive items delete --permanent %s", folder),
		"drive", "items", "delete", "--permanent", folder)

	url := strings.TrimSpace(runOK(t, "drive", "share", "link", folder))
	if !strings.Contains(url, "/urls/") {
		t.Fatalf("link stdout has no public URL: %q", url)
	}
	if !strings.Contains(url, "#") {
		t.Errorf("public link is missing the password fragment: %q", url)
	}

	status := runOK(t, "drive", "share", "status", folder)
	assertContains(t, status, "Public links:")
	assertContains(t, status, tokenOf(t, url))

	runOK(t, "drive", "share", "unlink", folder)
	after := runOKStderr2(t, "drive", "share", "status", folder)
	assertContains(t, after, "Not shared.")
}

// TestDriveShareLinkPublicHandshake guards the SRP-salt regression: a created
// link must be publicly resolvable. The public handshake fails auth (and the
// link 404s in a browser) if UrlPasswordSalt is not exactly 10 bytes, even
// though creation and status both still succeed.
func TestDriveShareLinkPublicHandshake(t *testing.T) {
	skipIfNoCredentials(t)
	folder := "/" + testID() + "-handshake"
	runOK(t, "drive", "folders", "create", folder)
	cleanupRun(t, fmt.Sprintf("Delete folder: proton-cli drive items delete --permanent %s", folder),
		"drive", "items", "delete", "--permanent", folder)

	url := strings.TrimSpace(runOK(t, "drive", "share", "link", folder))
	token := tokenOf(t, url)
	linkID := strings.TrimSpace(runJSON(t, "drive", "items", "info", folder)["link_id"].(string))

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
	skipIfNoCredentials(t)
	folder := "/" + testID() + "-idem"
	runOK(t, "drive", "folders", "create", folder)
	cleanupRun(t, fmt.Sprintf("Delete folder: proton-cli drive items delete --permanent %s", folder),
		"drive", "items", "delete", "--permanent", folder)

	first := strings.TrimSpace(runOK(t, "drive", "share", "link", folder))
	second := strings.TrimSpace(runOK(t, "drive", "share", "link", folder))
	if tokenOf(t, first) != tokenOf(t, second) {
		t.Errorf("link is not idempotent: %q vs %q", first, second)
	}
}

func TestDriveShareLinkExpires(t *testing.T) {
	skipIfNoCredentials(t)
	folder := "/" + testID() + "-exp"
	runOK(t, "drive", "folders", "create", folder)
	cleanupRun(t, fmt.Sprintf("Delete folder: proton-cli drive items delete --permanent %s", folder),
		"drive", "items", "delete", "--permanent", folder)

	link := runJSON(t, "drive", "share", "link", folder, "--expires", "7d")
	if link["expire_time"] == nil {
		t.Errorf("expected expire_time to be set, got %v", link["expire_time"])
	}
}

func TestDriveShareLinkPassword(t *testing.T) {
	skipIfNoCredentials(t)
	folder := "/" + testID() + "-pw"
	runOK(t, "drive", "folders", "create", folder)
	cleanupRun(t, fmt.Sprintf("Delete folder: proton-cli drive items delete --permanent %s", folder),
		"drive", "items", "delete", "--permanent", folder)

	stdout, stderr := runOKStderr(t, "drive", "share", "link", folder, "--password", "hunter2")
	if !strings.Contains(strings.TrimSpace(stdout), "#") {
		t.Errorf("link should still carry a generated fragment: %q", stdout)
	}
	if !strings.Contains(stderr, "hunter2") {
		t.Errorf("expected the custom password reported on stderr, got: %q", stderr)
	}
}

func TestDriveShareLinkDryRun(t *testing.T) {
	skipIfNoCredentials(t)
	folder := "/" + testID() + "-sharedry"
	runOK(t, "drive", "folders", "create", folder)
	cleanupRun(t, fmt.Sprintf("Delete folder: proton-cli drive items delete --permanent %s", folder),
		"drive", "items", "delete", "--permanent", folder)

	_, stderr := runOKStderr(t, "--dry-run", "drive", "share", "link", folder)
	assertContains(t, stderr, "dry-run")

	status := runOKStderr2(t, "drive", "share", "status", folder)
	assertContains(t, status, "Not shared.")
}

// ── members ──

func TestDriveShareAddNotProtonUser(t *testing.T) {
	skipIfNoCredentials(t)
	folder := "/" + testID() + "-member"
	runOK(t, "drive", "folders", "create", folder)
	cleanupRun(t, fmt.Sprintf("Delete folder: proton-cli drive items delete --permanent %s", folder),
		"drive", "items", "delete", "--permanent", folder)

	_, stderr, code := run(t, "drive", "share", "add", folder, "nobody-"+testID()+"@example.invalid")
	if code == 0 {
		t.Error("expected non-zero exit inviting a non-Proton address")
	}
	if !strings.Contains(stderr, "not a Proton user") {
		t.Errorf("expected 'not a Proton user' error, got: %s", stderr)
	}
}

func TestDriveShareAddDryRun(t *testing.T) {
	skipIfNoCredentials(t)
	folder := "/" + testID() + "-memberdry"
	runOK(t, "drive", "folders", "create", folder)
	cleanupRun(t, fmt.Sprintf("Delete folder: proton-cli drive items delete --permanent %s", folder),
		"drive", "items", "delete", "--permanent", folder)

	_, stderr := runOKStderr(t, "--dry-run", "drive", "share", "add", folder, "someone@example.invalid")
	assertContains(t, stderr, "dry-run")
}

// testInvitee receives an immediately-cancelled invitation in the member
// round-trip test.
const testInvitee = "protonalt.sessions986@proton.me"

// TestDriveShareMemberRoundTrip invites a real Proton address, verifies it shows
// as pending, then revokes it.
func TestDriveShareMemberRoundTrip(t *testing.T) {
	skipIfNoCredentials(t)
	invitee := testInvitee
	folder := "/" + testID() + "-memberrt"
	tmp := t.TempDir()
	src := filepath.Join(tmp, "m.txt")
	_ = os.WriteFile(src, []byte("member round-trip"), 0644)
	runOK(t, "drive", "folders", "create", folder)
	// Permanent folder deletion cascades the share + any pending invitation.
	cleanupRun(t, fmt.Sprintf("Delete folder: proton-cli drive items delete --permanent %s", folder),
		"drive", "items", "delete", "--permanent", folder)
	runOK(t, "drive", "items", "upload", src, folder)
	file := folder + "/m.txt"

	runOK(t, "drive", "share", "add", file, invitee)
	status := runOK(t, "drive", "share", "status", file)
	assertContains(t, status, invitee)
	assertContains(t, status, "pending")

	runOK(t, "drive", "share", "remove", file, invitee)
	after := runOKStderr2(t, "drive", "share", "status", file)
	assertNotContains(t, after, invitee)
}

func TestDriveShareRemoveNotFound(t *testing.T) {
	skipIfNoCredentials(t)
	folder := "/" + testID() + "-rm"
	runOK(t, "drive", "folders", "create", folder)
	cleanupRun(t, fmt.Sprintf("Delete folder: proton-cli drive items delete --permanent %s", folder),
		"drive", "items", "delete", "--permanent", folder)

	_, _, code := run(t, "drive", "share", "remove", folder, "nobody@example.invalid")
	if code != 3 {
		t.Errorf("expected exit 3 removing an unknown member, got %d", code)
	}
}

// ── incoming invitations ──

func TestDriveInvitationsList(t *testing.T) {
	skipIfNoCredentials(t)
	// Single-account runs can't produce an incoming invite, so only assert the
	// command itself succeeds.
	_, _, code := run(t, "drive", "invitations", "list")
	if code != 0 {
		t.Errorf("invitations list should exit 0, got %d", code)
	}
}

func TestDriveInvitationsAcceptRejectDryRun(t *testing.T) {
	skipIfNoCredentials(t)
	_, stderr := runOKStderr(t, "--dry-run", "drive", "invitations", "accept", "some-invitation-id")
	assertContains(t, stderr, "dry-run")
	_, stderr = runOKStderr(t, "--dry-run", "drive", "invitations", "reject", "some-invitation-id")
	assertContains(t, stderr, "dry-run")
}

// runOKStderr2 joins stdout+stderr because status prints "Not shared." to
// stderr (via Info), not stdout.
func runOKStderr2(t *testing.T, args ...string) string {
	t.Helper()
	stdout, stderr := runOKStderr(t, args...)
	return stdout + stderr
}
