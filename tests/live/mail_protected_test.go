package live

import (
	"strings"
	"testing"
)

// Reading a message somebody sent behind a password.
//
// Only a mailbox outside Proton is ever sent such a link, and the suite reads
// no mailbox outside Proton - so what can be checked against the live API is
// the answer Proton gives for a message that is not there, which is the answer
// a lapsed message gives and the one this has to phrase. Opening a real one is
// covered against messages built the way Proton builds them, in
// internal/service/mail.

func TestMailProtectedSaysWhenThereIsNothingBehindTheLink(t *testing.T) {
	stdout, stderr, code := run(t, "mail", "protected", "get",
		"--eo-password-file", eoPasswordFile(t), testID()+"-nothing")
	if code != 3 {
		t.Errorf("exit %d, want 3\nstderr: %s", code, truncateOutput(stderr))
	}
	if stdout != "" {
		t.Errorf("wrote to stdout while refusing: %q", truncateOutput(stdout))
	}
	assertContains(t, stderr, "does not open anything")
	// The sentence has to stand on its own: whoever reads it has a link and a
	// password and no way to tell which of them Proton objected to.
	if strings.Contains(stderr, "16001") || strings.Contains(stderr, "HTTP 400") {
		t.Errorf("the refusal reads as Proton's rather than as a sentence: %s", truncateOutput(stderr))
	}
}
