package offline

import (
	"strings"
	"testing"
)

// A leading-dash ID is a parsing problem, so it is answered before anything is
// sent: what these assert is that cobra accepted the argument, which is decided
// entirely by argv.
//
// dashedSyntheticID is a syntactically valid Proton ID (88 chars, URL-safe
// base64, ends "==") starting with '-'.
const dashedSyntheticID = "-bJxDLEMvt-Z6t4Yna7V8SYQ_FIHWT2_QbBr-whe-bIE8rbZunzr5RhXGaihvQ43z2qcxcqFgVRwi7A=="

// dashedSyntheticPair is the same ID used as a two-part reference, which the CLI
// takes as one slash-joined token.
const dashedSyntheticPair = dashedSyntheticID + "/" + dashedSyntheticID

// dashedInvitationID is the other shape Proton issues: a Drive share invitation
// is sixteen bytes of URL-safe base64, unpadded, so 22 characters with no "=".
const dashedInvitationID = "-e7KRBCZiwIuhVvaE2v41A"

// assertNotFlagParseError fails the test if stderr looks like cobra's
// "unknown shorthand flag" complaint - i.e. arg parsing rejected the ID.
func assertNotFlagParseError(t *testing.T, stderr string) {
	t.Helper()
	for _, marker := range []string{
		"unknown shorthand flag",
		"unknown flag",
	} {
		if strings.Contains(stderr, marker) {
			t.Errorf("expected no flag-parse error, but stderr contains %q:\n%s", marker, truncate(stderr))
		}
	}
}

// TestLeadingDashIDIsAccepted: each affected command parses cleanly when given a
// synthetic leading-dash ID. What the command then does with it does not matter -
// only that cobra did not reject the argument.
func TestLeadingDashIDIsAccepted(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"mail messages read", []string{"mail", "messages", "get", dashedSyntheticID}},
		{"mail messages trash", []string{"mail", "messages", "trash", dashedSyntheticID}},
		{"mail messages delete", []string{"mail", "messages", "delete", dashedSyntheticID}},
		{"mail messages star", []string{"mail", "messages", "star", dashedSyntheticID}},
		{"mail messages unstar", []string{"mail", "messages", "unstar", dashedSyntheticID}},
		{"mail messages unschedule", []string{"mail", "messages", "unschedule", dashedSyntheticID}},
		{"mail messages move", []string{"mail", "messages", "move", "--into", "archive", dashedSyntheticID}},
		{"mail messages mark read", []string{"mail", "messages", "mark", "read", dashedSyntheticID}},
		{"mail conversations read", []string{"mail", "conversations", "get", dashedSyntheticID}},
		{"mail conversations trash", []string{"mail", "conversations", "trash", dashedSyntheticID}},
		{"mail conversations delete", []string{"mail", "conversations", "delete", dashedSyntheticID}},
		{"mail conversations mark read", []string{"mail", "conversations", "mark", "read", dashedSyntheticID}},
		{"mail labels delete", []string{"mail", "settings", "labels", "delete", dashedSyntheticID}},
		{"mail filters delete", []string{"mail", "settings", "filters", "delete", dashedSyntheticID}},
		{"mail filters enable", []string{"mail", "settings", "filters", "enable", dashedSyntheticID}},
		{"calendar calendars delete", []string{"calendar", "settings", "calendars", "delete", dashedSyntheticID}},
		{"calendar events get", []string{"calendar", "events", "get", dashedSyntheticPair}},
		{"calendar events delete", []string{"calendar", "events", "delete", dashedSyntheticPair}},
		{"contacts get", []string{"contacts", "get", dashedSyntheticID}},
		{"contacts delete", []string{"contacts", "delete", dashedSyntheticID}},
		{"pass vaults delete", []string{"pass", "vaults", "delete", dashedSyntheticID}},
		{"pass items get", []string{"pass", "items", "get", dashedSyntheticPair}},
		{"pass items delete", []string{"pass", "items", "delete", dashedSyntheticPair}},
		{"drive trash restore", []string{"drive", "trash", "restore", dashedSyntheticID}},
		{"drive invitations accept", []string{"drive", "invitations", "accept", dashedInvitationID}},
		{"drive invitations decline", []string{"drive", "invitations", "decline", dashedInvitationID}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, stderr, _ := run(t, tc.args...)
			assertNotFlagParseError(t, stderr)
		})
	}
}

// TestLeadingDashIDWithFlagsBeforeParsesCleanly: `--render raw <leading-dash-id>`
// works because preprocessArgs inserts `--` before the ID after flag parsing
// has consumed `--render raw`.
func TestLeadingDashIDWithFlagsBeforeParsesCleanly(t *testing.T) {
	_, stderr, _ := run(t, "mail", "messages", "get", "--render", "raw", dashedSyntheticID)
	assertNotFlagParseError(t, stderr)
}

// TestLeadingDashIDWithFlagsAfterErrors: putting flags AFTER a leading-dash ID
// leaves them as arguments. The ID is protected by a `--` written in front of
// it, and everything after that is positional by definition - so the command
// reports more arguments than it has places for, and says where the flags went.
func TestLeadingDashIDWithFlagsAfterErrors(t *testing.T) {
	refuses(t, 1, []string{"mail", "messages", "get", dashedSyntheticID, "--render", "raw"},
		"takes REF, but 3 arguments were given", "put the flags first")
}
