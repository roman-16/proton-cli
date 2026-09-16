package offline

import (
	"strings"
	"testing"
)

// What this machine keeps is a question this machine can answer.
//
// `index list` and `index delete` read and remove files, so they work with
// nobody signed in and without reaching Proton at all - which is the whole of
// what makes them usable after a session expires, and what lets somebody see
// what is on their disk before deciding to sign in again.

// An empty answer is an answer: nothing is indexed, and the way to change that
// is on the screen.
func TestListingIndexesWithNoneWorksSignedOut(t *testing.T) {
	stdout, stderr, code := run(t, "index", "list")
	if code != 0 {
		t.Fatalf("exit %d, want 0\nstderr: %s", code, truncate(stderr))
	}
	if stdout != "" {
		t.Errorf("wrote a table for an empty listing: %q", truncate(stdout))
	}
	for _, want := range []string{"No indexes.", "proton index create"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr does not mention %q\nstderr: %s", want, truncate(stderr))
		}
	}
	if strings.Contains(stderr, "connection refused") {
		t.Errorf("reached the network to read the disk\nstderr: %s", truncate(stderr))
	}
}

// Removing what is not there is a refusal that names the remedy, not a silent
// success and not a request to Proton.
func TestDeletingAnIndexThatIsNotThereIsRefused(t *testing.T) {
	refuses(t, 3, []string{"index", "delete", "mail"}, "Nothing is indexed", "proton index create")
}

// An app that cannot be indexed is refused from the command line, whoever is
// signed in, and the refusal says which words are the words.
func TestIndexingSomethingThatIsNotAnAppIsRefused(t *testing.T) {
	refuses(t, 1, []string{"index", "create", "photos"}, "not an app that can be indexed", "mail")
}

// Building an index is the account's content, so it needs an account: the
// refusal is the one every command gives when nobody is signed in.
func TestBuildingAnIndexNeedsAnAccount(t *testing.T) {
	refuses(t, 2, []string{"index", "create"}, "not signed in")
}

// A preview of a build asserts an account too, because building one is not work
// this machine does alone.
func TestPreviewingABuildNeedsAnAccount(t *testing.T) {
	refuses(t, 2, []string{"index", "create", "--dry-run"}, "not signed in")
}
