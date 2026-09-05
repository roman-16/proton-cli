package offline

import (
	"strings"
	"testing"
)

// The skill is read before anything has been signed in to, so printing it cannot
// depend on an account existing - and an agent that had to sign in to find out
// how to ask the user to sign in would be stuck at the first step.
//
// It goes to stdout whole, because `proton skill > SKILL.md` is how it is
// installed and a stray line on that stream is a file an agent cannot parse.
func TestTheSkillIsPrintedWithoutAnAccount(t *testing.T) {
	stdout, stderr, code := run(t, "skill")
	if code != 0 {
		t.Fatalf("exit %d, want 0\nstderr: %s", code, truncate(stderr))
	}
	if !strings.HasPrefix(stdout, "---\nname: proton-cli\n") {
		t.Errorf("does not open with the frontmatter an agent loads it by:\n%s", truncate(stdout))
	}
	if !strings.HasSuffix(stdout, "\n") || strings.HasSuffix(stdout, "\n\n") {
		t.Error("the file it writes does not end in exactly one newline")
	}
	if strings.Contains(stderr, "not signed in") {
		t.Errorf("asked for an account before saying how to ask for one\nstderr: %s", truncate(stderr))
	}
}

// --body-only is the same document with the frontmatter left off, for an agent
// that reads it as it runs rather than saving it as a file.
func TestTheSkillCanBePrintedWithoutItsFrontmatter(t *testing.T) {
	whole, _, code := run(t, "skill")
	if code != 0 {
		t.Fatalf("whole: exit %d, want 0", code)
	}
	body, stderr, code := run(t, "skill", "--body-only")
	if code != 0 {
		t.Fatalf("--body-only: exit %d, want 0\nstderr: %s", code, truncate(stderr))
	}
	if strings.HasPrefix(body, "---") {
		t.Errorf("--body-only printed the frontmatter:\n%s", truncate(body))
	}
	if !strings.HasSuffix(whole, body) {
		t.Error("--body-only is not the tail of the whole document")
	}
}
