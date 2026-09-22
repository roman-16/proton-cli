package live

import (
	"strings"
	"testing"
)

// The senders that write as a list.
//
// What a test account can reach is the listing. A record exists only once mail
// carrying List- headers has arrived from a real sender, and nothing a run does
// puts one there - so the account under test answers with an empty set, and what
// there is to hold to is the listing itself: its shape, its two sets, every
// ordering it offers, and the refusals that are settled before anything is
// fetched.

func TestMailMailingListsList(t *testing.T) {
	assertListingShape(t, runOKBothStreams(t, "mail", "mailing-lists", "list"))
	// The second set is the one the web client keeps in its other tab.
	assertListingShape(t, runOKBothStreams(t, "mail", "mailing-lists", "list", "--unsubscribed"))
}

// The envelope is what a script reads, and it is there whether or not the
// account is on any list.
func TestMailMailingListsListAsJSON(t *testing.T) {
	if rows := runJSONArray(t, "mail", "mailing-lists", "list"); len(rows) > 0 {
		first, ok := rows[0].(map[string]interface{})
		if !ok {
			t.Fatalf("expected an object per mailing list, got %T", rows[0])
		}
		for _, field := range []string{"id", "name", "sender_address"} {
			if _, has := first[field]; !has {
				t.Errorf("a mailing list should carry %q", field)
			}
		}
	}
}

// Every ordering is applied to what came back, so a key the listing offers and
// cannot order by is a listing disagreeing with itself.
func TestMailMailingListsSortEveryKey(t *testing.T) {
	for _, key := range []string{"received", "name", "unread", "frequency", "read"} {
		runOK(t, "mail", "mailing-lists", "list", "--sort", key)
		runOK(t, "mail", "mailing-lists", "list", "--sort", key, "--desc")
	}
}

// A reference nothing matches is a not-found rather than a request that acts on
// something else.
func TestMailMailingListsUnknownReference(t *testing.T) {
	ref := "no-such-mailing-list-" + testID()
	_, stderr, code := run(t, "mail", "mailing-lists", "get", ref)
	if code != 3 {
		t.Fatalf("an unknown mailing list should exit 3, got %d", code)
	}
	assertContains(t, stderr, ref)
}

// `update` with nothing to change is refused before a list is resolved, so a
// bare invocation cannot quietly rewrite the rule a list already had.
func TestMailMailingListsUpdateNeedsSomethingToChange(t *testing.T) {
	_, stderr, code := run(t, "mail", "mailing-lists", "update", "anything")
	if code == 0 {
		t.Fatal("update with neither --into nor --mark-read should be refused")
	}
	assertContains(t, stderr, "--into")
}

// assertListingShape accepts either the table or the sentence an empty
// collection answers with, because whether this account is on a mailing list is
// not the suite's to arrange. The two land on different streams, so both are
// read.
func assertListingShape(t *testing.T, answer string) {
	t.Helper()
	if strings.Contains(answer, "No mailing lists") {
		return
	}
	assertContains(t, answer, "NAME")
	assertContains(t, answer, "SENDER")
}
