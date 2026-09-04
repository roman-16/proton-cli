package live

import (
	"strings"
	"testing"
)

// Secure links, which a plan gates: showing one item to somebody with no Proton
// account.
//
// The item stays encrypted. The key that opens it travels in the URL fragment,
// which is why the whole URL is the secret and a listing must never carry it.

// The link is made against an item this test creates, and both are removed again.
func TestPassSecureLinkShowsOneItemToAnybody(t *testing.T) {
	name := testID() + "-link"
	out, stderr, code := runPaid(t, "--yes", "pass", "items", "create",
		"--name", name, "--username", "jane",
		"--secret-file", secretFile(t, "password", "hunter2"))
	if code != 0 {
		t.Fatalf("could not make an item to link to: %s", truncateOutput(stderr))
	}
	ref := strings.TrimSpace(out)
	cleanupRunPaid(t, "Delete item: proton pass items delete "+ref, "pass", "items", "delete", ref)

	shown, stderr := runOKStderrPaid(t, "pass", "links", "create", ref, "--expires", "1h", "--views", "2")

	// The URL carries the key after a '#', which is what a browser never sends
	// to Proton - so a link without one would be a link nothing can open.
	if !strings.Contains(shown, "#") {
		t.Errorf("the link carries no key: %s", truncateOutput(shown))
	}
	// The warning belongs on stderr, so capturing the link does not capture it.
	assertContains(t, stderr, "Anyone with this link")

	rows := runJSONArrayPaid(t, "pass", "links", "list")
	var linkID string
	for _, row := range rows {
		m, _ := row.(map[string]interface{})
		if id, _ := m["item_id"].(string); !strings.Contains(ref, id) {
			continue
		}
		linkID, _ = m["link_id"].(string)
		// The URL carries the key that opens the item, so a listing does not.
		if url, _ := m["url"].(string); url != "" {
			t.Errorf("a link listing carries the URL that opens the item: %q", url)
		}
	}
	if linkID == "" {
		t.Fatal("the link this test made is not in the listing")
	}
	// Proton stores the link key sealed under the item's own, so the whole URL
	// can be put back together - which is what makes a mislaid link recoverable
	// rather than lost. It takes a command that says so.
	back, warned := runOKStderrPaid(t, "pass", "links", "get", linkID)
	if !strings.Contains(back, "#") {
		t.Errorf("`links get` could not rebuild the link: %q", back)
	}
	assertContains(t, warned, "Anyone with this link")
	// The same URL is part of how that item is shared.
	assertContains(t, runOKPaid(t, "pass", "items", "share", "get", ref), "#")
	cleanupRunPaid(t, "Revoke link: proton pass links revoke "+linkID, "pass", "links", "revoke", linkID)
}
