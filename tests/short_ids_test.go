package tests

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// firstIDLineCell extracts the first cell of the first non-header table row.
// Returns "" when no row is found.
func firstIDLineCell(stdout string) string {
	lines := strings.Split(stdout, "\n")
	// Skip header and separator lines.
	for i := 2; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) > 0 {
			return fields[0]
		}
	}
	return ""
}

// shortID is what a listing prints for an ID: eight characters, begun after any
// leading dashes, so the token is never handed to the flag parser. The rule is
// written out here rather than borrowed from the binary, because a test that
// takes the rule from the code under test cannot notice the code getting it
// wrong.
func shortID(id string) string {
	return strings.TrimLeft(id, "-")[:8]
}

func TestShortIDDisplayInTTY(t *testing.T) {
	t.Parallel()
	stdout, _, code := runWithEnv(t,
		map[string]string{"PROTON_CLI_FORCE_TTY": "1"},
		"mail", "messages", "list", "--page-size", "3")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	id := firstIDLineCell(stdout)
	if len(id) != 8 {
		t.Errorf("expected 8-char ID under PROTON_CLI_FORCE_TTY, got %q (len %d)", id, len(id))
	}
	if strings.HasPrefix(id, "-") {
		t.Errorf("a short ID a shell would read as a flag: %q", id)
	}
}

func TestShortIDPipeFullIDs(t *testing.T) {
	t.Parallel()
	stdout := runOK(t, "mail", "messages", "list", "--page-size", "3")
	id := firstIDLineCell(stdout)
	if len(id) <= 8 {
		t.Errorf("expected full ID when piped, got %q (len %d)", id, len(id))
	}
	if !strings.HasSuffix(id, "==") {
		t.Errorf("piped ID should end ==, got %q", id)
	}
}

func TestShortIDFullIDsFlagOverrides(t *testing.T) {
	t.Parallel()
	stdout, _, code := runWithEnv(t,
		map[string]string{"PROTON_CLI_FORCE_TTY": "1"},
		"--full-ids", "mail", "messages", "list", "--page-size", "3")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	id := firstIDLineCell(stdout)
	if len(id) <= 8 {
		t.Errorf("--full-ids should keep full ID even on TTY; got %q (len %d)", id, len(id))
	}
}

func TestShortIDJSONAlwaysFull(t *testing.T) {
	t.Parallel()
	// Even with TTY forced.
	stdout, _, code := runWithEnv(t,
		map[string]string{"PROTON_CLI_FORCE_TTY": "1"},
		"mail", "messages", "list", "--page-size", "1", "--output", "json")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &data); err != nil {
		t.Fatalf("parse json: %v", err)
	}
	msgs := data["messages"].([]interface{})
	if len(msgs) == 0 {
		t.Skip("inbox empty")
	}
	id := msgs[0].(map[string]interface{})["id"].(string)
	if len(id) <= 8 {
		t.Errorf("JSON should always carry full ID, got %q", id)
	}
}

// idcachePath returns the production cache file path for the
// profile the suite acts as. Tests inspect this file directly to verify cache
// population and to set up ambiguous-prefix scenarios, so it has to name the
// same profile the runner does.
func idcachePath(t *testing.T) string {
	t.Helper()
	cd, err := os.UserConfigDir()
	if err != nil {
		t.Fatalf("UserConfigDir: %v", err)
	}
	return filepath.Join(cd, "proton-cli", "idcache", primary+".json")
}

// cacheEntry mirrors what the CLI writes down about something it showed. The
// suite reads the file rather than the package so that the shape a completion
// depends on is checked as it lands on disk.
type cacheEntry struct {
	Collection string   `json:"collection"`
	Ref        string   `json:"ref"`
	Handles    []string `json:"handles"`
}

func TestShortIDCacheFilePopulated(t *testing.T) {
	t.Parallel()
	// Run any list command to populate the cache.
	runOK(t, "mail", "messages", "list", "--page-size", "1")

	path := idcachePath(t)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read cache: %v", err)
	}
	var entries []cacheEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		t.Fatalf("cache file is not a JSON array: %v\n%s", err, data)
	}
	if len(entries) == 0 {
		t.Errorf("cache should be non-empty after list command")
	}
	// The cache exists to turn a short prefix back into the whole thing, so what
	// it stores must never itself be shortened. Not every Proton ID is base64
	// ending in "==" - a session UID is 32 lowercase characters - so length is
	// the invariant, not shape.
	for _, e := range entries {
		if len(e.Ref) <= 8 {
			t.Errorf("cached reference is not a full one: %q", e.Ref)
		}
		if e.Collection == "" {
			t.Errorf("cached reference %q says nothing about what it is", e.Ref)
		}
	}
}

// A listing remembers the name beside the reference, which is what lets a shell
// show what it is offering and lets a subject be typed back in place of an ID.
// Only a real listing proves the columns are marked where the handles are.
func TestShortIDCacheRemembersSubjects(t *testing.T) {
	t.Parallel()
	_, _, subject := plainMail(t)
	runOK(t, "mail", "messages", "list", "--keyword", subject)

	data, err := os.ReadFile(idcachePath(t))
	if err != nil {
		t.Fatalf("read cache: %v", err)
	}
	var entries []cacheEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		t.Fatalf("cache file is not a JSON array: %v\n%s", err, data)
	}
	for _, e := range entries {
		if e.Collection != "mail messages" {
			continue
		}
		for _, h := range e.Handles {
			if h == subject {
				return
			}
		}
	}
	t.Errorf("no cached message carries the subject %q that was just listed", subject)
}

func TestShortIDRoundTripMail(t *testing.T) {
	t.Parallel()
	msgID, _, subject := plainMail(t)

	// Run a list command so the cache learns the ID.
	runOK(t, "mail", "messages", "list", "--page-size", "20")

	prefix := shortID(msgID)
	stdout, stderr, code := run(t, "mail", "messages", "get", prefix)
	if code != 0 {
		t.Fatalf("read by short prefix exit %d; stderr: %s", code, stderr)
	}
	if !strings.Contains(stdout, subject) {
		t.Errorf("read by short prefix should resolve to message containing %q; stdout:\n%s",
			subject, truncateOutput(stdout))
	}
}

func TestShortIDPrefixCacheMissOK(t *testing.T) {
	t.Parallel()
	// On cache miss, ResolvePrefix passes the input through unchanged
	// (so commands like `pass items list --vault Personal` work even
	// when "Personal" looks short-ID-shaped). The downstream service
	// layer's keyword-search runs against the API; if nothing matches,
	// we still get a clean exit 3, but with the service's own "no X
	// matching Y" message rather than a cache-specific hint.
	prefix := "ZZZZ____" // 4 Z + 4 underscores; very unlikely to match
	_, stderr, code := run(t, "mail", "messages", "get", prefix)
	if code == 0 {
		t.Errorf("expected non-zero exit on no-match prefix, got 0")
	}
	// The error should NOT come from ResolvePrefix (cache-specific hint)
	// any more - it should be the downstream search's not-found error.
	if strings.Contains(stderr, "run a list command") {
		t.Errorf("unexpected cache-hint error; ResolvePrefix should fall through to search:\n%s", stderr)
	}
	if !strings.Contains(stderr, "matching") && !strings.Contains(stderr, prefix) {
		t.Errorf("expected downstream not-found error mentioning the input, got: %s", stderr)
	}
}

func TestShortIDAmbiguousErrors(t *testing.T) {
	// Hand-craft a cache file with two IDs that share an 8-char prefix.
	path := idcachePath(t)
	backup := path + ".bak-" + testID()
	if _, err := os.Stat(path); err == nil {
		if err := os.Rename(path, backup); err != nil {
			t.Fatalf("backup cache: %v", err)
		}
		t.Cleanup(func() { _ = os.Rename(backup, path) })
	} else {
		t.Cleanup(func() { _ = os.Remove(path) })
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	// 88-char IDs sharing the first 8 chars.
	pad := strings.Repeat("A", 78)
	idA := "abcd1234" + "FIRSTabc" + pad + "=="
	idB := "abcd1234" + "SECONDab" + pad + "=="
	body, _ := json.Marshal([]cacheEntry{
		{Collection: "mail messages", Ref: idA},
		{Collection: "mail messages", Ref: idB},
	})
	if err := os.WriteFile(path, body, 0600); err != nil {
		t.Fatalf("write cache: %v", err)
	}

	_, stderr, code := run(t, "mail", "messages", "get", "abcd1234")
	if code != 4 {
		t.Errorf("expected exit 4 on ambiguous prefix, got %d", code)
	}
	if !strings.Contains(stderr, "matches 2 cached IDs") {
		t.Errorf("expected stderr to say the prefix matched several, got: %s", stderr)
	}
	if !strings.Contains(stderr, idA) || !strings.Contains(stderr, idB) {
		t.Errorf("expected both candidate IDs in stderr, got: %s", stderr)
	}
}

func TestShortIDRoundTripContacts(t *testing.T) {
	t.Parallel()
	name := testID() + "-shortid-contact"
	stdout := runOK(t, "contacts", "create",
		"--name", name, "--email", "t+"+name+"@x.invalid")
	id := strings.TrimSpace(stdout)
	cleanupRun(t, "Delete contact: proton contacts delete -- "+id,
		"contacts", "delete", "--", id)

	// Populate cache.
	runOK(t, "contacts", "list")

	prefix := shortID(id)
	got := runOK(t, "contacts", "get", prefix)
	if !strings.Contains(got, name) {
		t.Errorf("contacts get by short prefix should resolve; stdout:\n%s", got)
	}
}

func TestShortIDRoundTripPass(t *testing.T) {
	t.Parallel()
	name := testID() + "-shortid-pass"
	stdout := runOK(t, "pass", "items", "create",
		"--type", "note", "--name", name, "--note", "x")
	// Creating answers with SHARE_ID/ITEM_ID; a short ID works on either half.
	shareID, itemID, ok := strings.Cut(strings.TrimSpace(stdout), "/")
	if !ok {
		t.Fatalf("expected SHARE_ID/ITEM_ID on stdout, got %q", stdout)
	}
	cleanupRun(t, "Delete pass item: proton pass items delete "+name,
		"pass", "items", "delete", name)

	// Populate cache.
	runOK(t, "pass", "items", "list")

	got := runOK(t, "pass", "items", "get", shortID(shareID)+"/"+shortID(itemID))
	if !strings.Contains(got, name) {
		t.Errorf("pass items get by short prefixes should resolve; stdout:\n%s", got)
	}
}
