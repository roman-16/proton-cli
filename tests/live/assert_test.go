package live

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"
	"time"
)

// What a test asserts about an answer, and how it names what it makes.

// testID is a unique prefix for anything a test creates, so it is identifiable
// in the Proton UI if cleanup ever fails - and so a sweep can find what an
// interrupted run left behind. Never use a short or common name that could
// collide with real data.
func testID() string {
	return fmt.Sprintf("proton-cli-test-%d-%d", time.Now().UnixMilli(), rand.Intn(10000))
}

func assertContains(t *testing.T, stdout, substr string) {
	t.Helper()
	if !strings.Contains(stdout, substr) {
		t.Errorf("expected output to contain %q, got:\n%s", substr, truncateOutput(stdout))
	}
}

func assertNotContains(t *testing.T, stdout, substr string) {
	t.Helper()
	if strings.Contains(stdout, substr) {
		t.Errorf("expected output NOT to contain %q, got:\n%s", substr, truncateOutput(stdout))
	}
}

// assertField checks that a "Key: Value" line exists and reads as expected.
func assertField(t *testing.T, stdout, field, expected string) {
	t.Helper()
	for _, line := range strings.Split(stdout, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, field) {
			value := strings.TrimSpace(strings.TrimPrefix(line, field))
			if value == expected {
				return
			}
			t.Errorf("field %s: got %q, want %q", field, value, expected)
			return
		}
	}
	t.Errorf("field %s not found in:\n%s", field, truncateOutput(stdout))
}

// looksLikeID matches a Proton base64 ID: about 88 characters, ending "==".
func looksLikeID(s string) bool {
	return len(s) > 60 && strings.HasSuffix(s, "==")
}

// looksLikePairRef matches a two-part reference - the shape a Pass item or a
// calendar event is addressed by, and the shape creating one answers with.
// looksLikeID accepts it too, so asserting the halves is what tells the two
// apart.
func looksLikePairRef(s string) bool {
	left, right, ok := strings.Cut(s, "/")
	return ok && looksLikeID(left) && looksLikeID(right)
}

// assertBareID checks the stdout=ID convention: a create command writes just
// the new ID, on one line, no JSON and no trailing text, so `ID=$(proton ...
// create ...)` works and the human message goes to stderr.
func assertBareID(t *testing.T, stdout, where string) string {
	t.Helper()
	return assertBareRef(t, stdout, where, looksLikeID)
}

// assertBarePairRef is the same for the things addressed by two IDs: a Pass item
// and a calendar event, which creating one answers with as a single token.
func assertBarePairRef(t *testing.T, stdout, where string) string {
	t.Helper()
	return assertBareRef(t, stdout, where, looksLikePairRef)
}

func assertBareRef(t *testing.T, stdout, where string, shaped func(string) bool) string {
	t.Helper()
	ref := strings.TrimSpace(stdout)
	if lines := strings.Split(ref, "\n"); len(lines) != 1 {
		t.Fatalf("%s: expected 1 line on stdout, got %d:\n%s", where, len(lines), stdout)
	}
	if !shaped(ref) {
		t.Fatalf("%s: stdout is not the reference the new thing is addressed by: %q", where, ref)
	}
	return ref
}

// keysOf is what an assertion about a missing JSON field reports, so the
// complaint says what was there instead.
func keysOf(m map[string]interface{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// lastNonEmpty is the last line of a stream, which for a listing is its footer.
func lastNonEmpty(s string) string {
	lines := strings.Split(s, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if line := strings.TrimSpace(lines[i]); line != "" {
			return line
		}
	}
	return ""
}

func truncateOutput(s string) string {
	if len(s) > 500 {
		return s[:500] + "...(truncated)"
	}
	return s
}

func contains(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}
