package live

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// Invitations to something somebody else shared.
//
// Answering one is where the real crypto is: the session key is unwrapped and
// the signature checked, which a single account can only dry-run. So the
// primary shares and the secondary answers.

func TestDriveInvitationsList(t *testing.T) {
	// Single-account runs can't produce an incoming invite, so only assert the
	// command itself succeeds.
	_, _, code := run(t, "drive", "invitations", "list")
	if code != 0 {
		t.Errorf("invitations list should exit 0, got %d", code)
	}
}

func TestDriveInvitationsAcceptRejectDryRun(t *testing.T) {
	_, stderr := runOKStderr(t, "--dry-run", "drive", "invitations", "accept", "some-invitation-id")
	assertContains(t, stderr, "Dry run")
	_, stderr = runOKStderr(t, "--dry-run", "drive", "invitations", "decline", "some-invitation-id")
	assertContains(t, stderr, "Dry run")
}

func altInvitationIDs(t *testing.T) map[string]bool {
	t.Helper()
	set := map[string]bool{}
	for _, i := range runJSONArraySecondary(t, "drive", "invitations", "list") {
		if id, ok := i.(map[string]interface{})["invitation_id"].(string); ok {
			set[id] = true
		}
	}
	return set
}

// The other answer an invitation can be given. Accepting is the round trip above;
// declining has to leave the share without a member.
func TestDriveShareInvitationCanBeDeclined(t *testing.T) {
	folder := "/" + testID() + "-decline"
	runOK(t, "drive", "items", "create", folder)
	cleanupRun(t, fmt.Sprintf("Delete folder: proton drive items delete --permanent %s", folder),
		"drive", "items", "delete", folder)

	before := altInvitationIDs(t)
	runOK(t, "drive", "items", "share", "add", folder, secondaryEmail())
	cleanupRun(t, fmt.Sprintf("Revoke member: proton drive items share remove %s %s", folder, secondaryEmail()),
		"drive", "items", "share", "remove", folder, secondaryEmail())

	var invID string
	waitFor(45*time.Second, 3*time.Second, func() bool {
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

	runOKSecondary(t, "drive", "invitations", "decline", invID)

	if altInvitationIDs(t)[invID] {
		t.Error("the invitation is still waiting for an answer after being declined")
	}
	st := runJSON(t, "drive", "items", "share", "get", folder)
	members, _ := st["members"].([]interface{})
	if strings.Contains(fmt.Sprintf("%v", members), secondaryEmail()) {
		t.Errorf("declining made the second account a member anyway: %v", members)
	}
}

func TestDriveShareInvitationRoundTrip(t *testing.T) {
	folder := "/" + testID() + "-share-rt"
	runOK(t, "drive", "items", "create", folder)
	// Permanent delete cascades the share + membership.
	cleanupRun(t, fmt.Sprintf("Delete folder: proton drive items delete --permanent %s", folder),
		"drive", "items", "delete", folder)

	before := altInvitationIDs(t)

	runOK(t, "drive", "items", "share", "add", folder, secondaryEmail(), "--edit")
	cleanupRun(t, fmt.Sprintf("Revoke member: proton drive items share remove %s %s", folder, secondaryEmail()),
		"drive", "items", "share", "remove", folder, secondaryEmail())

	// The alt sees the new pending invitation and accepts it.
	var invID string
	waitFor(45*time.Second, 3*time.Second, func() bool {
		for id := range altInvitationIDs(t) {
			if !before[id] {
				invID = id
				return true
			}
		}
		return false
	})
	if invID == "" {
		t.Fatal("alt did not receive the share invitation")
	}
	runOKSecondary(t, "drive", "invitations", "accept", invID)

	// The primary now sees the alt as a member, not a pending invitee.
	var members string
	waitFor(30*time.Second, 3*time.Second, func() bool {
		st := runJSON(t, "drive", "items", "share", "get", folder)
		ms, _ := st["members"].([]interface{})
		members = fmt.Sprintf("%v", ms)
		return strings.Contains(members, secondaryEmail())
	})
	if !strings.Contains(members, secondaryEmail()) {
		t.Errorf("alt is not listed as a member after accepting; members=%s", members)
	}
}
