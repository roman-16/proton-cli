package live

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/roman-16/proton-cli/tests/account"
)

// The local index: building one, searching what only it can read, and keeping
// it current.
//
// Every test here removes the index it built. It is a decrypted copy of the
// account's mail, so leaving one behind on a machine that ran the suite is the
// one failure worth guarding against more than a red test.

// indexedMail builds the mail index and removes it when the test ends.
//
// Building is idempotent: a run over an index that is already there applies what
// has changed and fetches nothing else.
func indexedMail(t *testing.T) {
	t.Helper()
	runOK(t, "index", "create", "mail")
	cleanupRun(t, "Delete the local index: proton index delete --yes mail",
		"index", "delete", "--yes", "mail")
}

// A built index is one row of `index list`, and removing it takes the row away.
func TestIndexCreateListDelete(t *testing.T) {
	runOK(t, "index", "create", "mail")
	cleanupRun(t, "Delete the local index: proton index delete --yes mail",
		"index", "delete", "--yes", "mail")

	data := runJSON(t, "index", "list")
	indexes, ok := data["indexes"].([]interface{})
	if !ok || len(indexes) == 0 {
		t.Fatalf("index list shows nothing after a build: %v", data)
	}
	first := indexes[0].(map[string]interface{})
	if first["app"] != "mail" {
		t.Errorf("app = %v, want mail", first["app"])
	}
	if indexed, _ := first["indexed"].(float64); indexed <= 0 {
		t.Errorf("indexed = %v, want the messages the account holds", first["indexed"])
	}
	if complete, _ := first["complete"].(bool); !complete {
		t.Error("a build that ran to the end should report itself complete")
	}

	runOK(t, "index", "delete", "--yes", "mail")
	after := runJSON(t, "index", "list")
	if left, _ := after["count"].(float64); left != 0 {
		t.Errorf("count = %v after a removal, want 0", after["count"])
	}
}

// The whole reason the index exists: a word that only the body says.
//
// Proton cannot search a body, so the same search has to find nothing before the
// index is built and the message afterwards. Both halves are the test - one of
// them alone would pass on a CLI that searched nothing at all.
func TestIndexMakesBodiesSearchable(t *testing.T) {
	token := testID() + "-inthebody"
	subject := testID() + "-body-index"
	runOK(t, "mail", "messages", "send",
		"--to", selfEmail(), "--subject", subject, "--body", "A body that says "+token+" once.")
	if sentID := findMessage(t, "sent", subject); sentID != "" {
		cleanupRun(t, "Delete sent mail: proton mail messages delete "+sentID,
			"mail", "messages", "delete", "--", sentID)
	}
	inboxID := findMessage(t, "inbox", subject)
	if inboxID == "" {
		t.Fatalf("mail %q was not delivered", subject)
	}
	cleanupRun(t, "Delete inbox mail: proton mail messages delete "+inboxID,
		"mail", "messages", "delete", "--", inboxID)

	server := runJSONArray(t, "mail", "messages", "list", "--keyword", token, "--folder", "all")
	if len(server) != 0 {
		t.Fatalf("Proton answered a body search with %d messages; the index would be proving nothing", len(server))
	}

	indexedMail(t)

	found := runJSONArray(t, "mail", "messages", "list", "--keyword", token, "--folder", "all")
	if len(found) != 1 {
		t.Fatalf("keyword in a body matched %d messages with an index, want 1", len(found))
	}
	if got := found[0].(map[string]interface{})["subject"]; got != subject {
		t.Errorf("matched %v, want the message whose body says it", got)
	}

	if got := runJSONArray(t, "mail", "messages", "list", "--keyword", token, "--folder", "all", "--sort", "size"); len(got) != 1 {
		t.Errorf("a body keyword ordered by size matched %d messages, want 1", len(got))
	}
	if got := runJSONArray(t, "mail", "messages", "list", "--keyword", token, "--folder", "all", "--via", selfEmail()); len(got) != 1 {
		t.Errorf("a body keyword on the address it came to matched %d messages, want 1", len(got))
	}
	if got := runJSONArray(t, "mail", "messages", "list", "--keyword", token, "--folder", "all", "--has-attachments"); len(got) != 0 {
		t.Errorf("a body keyword with --has-attachments matched %d messages, which carry none", len(got))
	}
}

// A thread is found by a word in any of its messages, so the same search
// narrows the verbs that act on threads.
func TestIndexSearchesThreadsByTheirBodies(t *testing.T) {
	token := testID() + "-inthethread"
	subject := testID() + "-thread-index"
	runOK(t, "mail", "messages", "send",
		"--to", selfEmail(), "--subject", subject, "--body", "A body that says "+token+" once.")
	if sentID := findMessage(t, "sent", subject); sentID != "" {
		cleanupRun(t, "Delete sent mail: proton mail messages delete "+sentID,
			"mail", "messages", "delete", "--", sentID)
	}
	inboxID := findMessage(t, "inbox", subject)
	if inboxID == "" {
		t.Fatalf("mail %q was not delivered", subject)
	}
	cleanupRun(t, "Delete inbox mail: proton mail messages delete "+inboxID,
		"mail", "messages", "delete", "--", inboxID)

	indexedMail(t)

	threads := runJSONArray(t, "mail", "conversations", "list", "--keyword", token, "--folder", "all")
	if len(threads) != 1 {
		t.Fatalf("keyword in a body matched %d threads, want 1", len(threads))
	}
	if got := threads[0].(map[string]interface{})["subject"]; got != subject {
		t.Errorf("matched %v, want the thread whose message says it", got)
	}
}

// A search is never older than the command that asked for it: a message trashed
// after the index was built is answered as being in the trash.
func TestIndexFollowsAMessageOutOfTheInbox(t *testing.T) {
	moved := mutableMail(t)
	subject, _ := runJSON(t, "mail", "messages", "get", "--", moved)["subject"].(string)
	if subject == "" {
		t.Fatalf("message %s has no subject to search for", moved)
	}
	indexedMail(t)

	runOK(t, "mail", "messages", "trash", "--", moved)
	cleanupRun(t, "Put the message back: proton mail messages move --into inbox "+moved,
		"mail", "messages", "move", "--into", "inbox", "--", moved)

	for _, row := range runJSONArray(t, "mail", "messages", "list", "--keyword", subject, "--folder", "inbox") {
		if row.(map[string]interface{})["id"] == moved {
			t.Error("a trashed message is still answered as being in the inbox")
		}
	}
	found := false
	for _, row := range runJSONArray(t, "mail", "messages", "list", "--keyword", subject, "--folder", "trash") {
		if row.(map[string]interface{})["id"] == moved {
			found = true
		}
	}
	if !found {
		t.Error("a trashed message is not answered as being in the trash")
	}
}

// indexedDrive builds the Drive index and removes it when the test ends.
func indexedDrive(t *testing.T) {
	t.Helper()
	runOK(t, "index", "create", "drive")
	cleanupRun(t, "Delete the local index: proton index delete --yes drive",
		"index", "delete", "--yes", "drive")
}

// A filtered listing answers from the index, and answers what the walk answers.
//
// Both halves matter: the rows have to be the same rows, and what happens to the
// tree afterwards has to reach them - an index that answered yesterday's tree
// would be a listing that is confidently wrong.
func TestIndexDriveAnswersAFilteredListing(t *testing.T) {
	folder := "/" + testID() + "-indexed"
	tmp := t.TempDir()
	for _, name := range []string{"a.log", "b.log", "keep.txt"} {
		if err := os.WriteFile(filepath.Join(tmp, name), []byte("x"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	runOK(t, "drive", "items", "create", folder)
	cleanupRun(t, "Delete folder: proton drive items delete "+folder,
		"drive", "items", "delete", folder)
	for _, name := range []string{"a.log", "b.log"} {
		runOK(t, "drive", "items", "upload", filepath.Join(tmp, name), folder)
	}

	indexedDrive(t)

	logs := runJSONArray(t, "drive", "items", "list", "--pattern", "*.log", "--recursive", folder)
	if len(logs) != 2 {
		t.Fatalf("the index answered with %d items, want the 2 uploaded", len(logs))
	}

	// A file added after the build is in the account, so it is in the answer.
	runOK(t, "drive", "items", "upload", filepath.Join(tmp, "keep.txt"), folder)
	after := runJSONArray(t, "drive", "items", "list", "--pattern", "*.txt", "--recursive", folder)
	if len(after) != 1 {
		t.Errorf("a file uploaded after the build matched %d items, want 1", len(after))
	}
}

// A keyword reads what a file says, which no Proton client can answer and
// nothing but the copy on this machine can.
//
// The three things it has to get right are the three the design turns on: a
// text file is found by a word inside it, a file that is not text is not
// searched for words it does not have, and a file uploaded over is found by
// what it says now rather than by what the version before it said.
func TestIndexDriveSearchesFileText(t *testing.T) {
	folder := "/" + testID() + "-texts"
	phrase := testID() + "-parking-permit"
	tmp := t.TempDir()
	notes := filepath.Join(tmp, "notes.md")
	if err := os.WriteFile(notes, []byte("# Car\n\nThe "+phrase+" is in the glovebox.\n"), 0600); err != nil {
		t.Fatal(err)
	}
	// A file whose bytes are not text, named so that only its contents could
	// match: a keyword that finds it is one that indexed a photograph.
	photo := filepath.Join(tmp, "holiday.png")
	if err := os.WriteFile(photo, append([]byte("\x89PNG\r\n\x1a\n"), []byte(phrase)...), 0600); err != nil {
		t.Fatal(err)
	}
	runOK(t, "drive", "items", "create", folder)
	cleanupRun(t, "Delete folder: proton drive items delete "+folder,
		"drive", "items", "delete", folder)
	runOK(t, "drive", "items", "upload", notes, folder)
	runOK(t, "drive", "items", "upload", photo, folder)

	indexedDrive(t)

	found := runJSONArray(t, "drive", "items", "list", "--keyword", phrase, "--recursive", folder)
	if len(found) != 1 {
		t.Fatalf("a keyword inside a file matched %d items, want the one text file that says it", len(found))
	}
	if name := found[0].(map[string]interface{})["name"].(string); !strings.HasSuffix(name, "notes.md") {
		t.Errorf("a keyword inside a file matched %q, want notes.md", name)
	}

	// A new version is a new text: what the file said before is not what it says.
	after := testID() + "-garage-code"
	if err := os.WriteFile(notes, []byte("# Car\n\nThe "+after+" is 4417.\n"), 0600); err != nil {
		t.Fatal(err)
	}
	runOK(t, "drive", "items", "upload", "--if-exists", "replace", notes, folder)

	if got := runJSONArray(t, "drive", "items", "list", "--keyword", after, "--recursive", folder); len(got) != 1 {
		t.Errorf("a keyword inside the new version matched %d items, want the file it was uploaded over", len(got))
	}
	if got := runJSONArray(t, "drive", "items", "list", "--keyword", phrase, "--recursive", folder); len(got) != 0 {
		t.Errorf("a keyword from the version before matched %d items, want none", len(got))
	}
}

// A keyword search over events covers whatever is there rather than a range,
// which is what makes "when was that" answerable at all.
//
// It runs twice on purpose: once with nothing indexed, which fetches the events
// and searches them, and once with an index, which answers from the copy. Both
// have to find the same event - a search that only worked one way would be a
// flag that means two things.
func TestIndexCalendarSearchesEventText(t *testing.T) {
	title := testID() + "-indexed-event"
	place := testID() + "-mariahilf"
	start := time.Now().AddDate(0, 2, 0).Format("2006-01-02T15:04")
	ref := strings.TrimSpace(runOK(t, "calendar", "events", "create",
		"--title", title, "--start", start, "--duration", "30m", "--location", place))
	cleanupRun(t, "Delete event: proton calendar events delete "+ref,
		"calendar", "events", "delete", "--", ref)

	// No range, and the event is months away: only a search over everything finds
	// it, and this is the half that runs without an index.
	found := runJSONArray(t, "calendar", "events", "list", "--keyword", title)
	if len(found) != 1 {
		t.Fatalf("a keyword with no range matched %d events, want the one just made", len(found))
	}

	runOK(t, "index", "create", "calendar")
	cleanupRun(t, "Delete the local index: proton index delete --yes calendar",
		"index", "delete", "--yes", "calendar")

	indexed := runJSONArray(t, "calendar", "events", "list", "--keyword", place)
	for _, row := range indexed {
		if row.(map[string]interface{})["title"] == title {
			return
		}
	}
	t.Errorf("the index answered a search over a location with %d events, none of them the one made", len(indexed))
}

// A watch keeps the index current while it is attached, so what arrives during
// one is searchable without anything else being run.
func TestIndexWatchAppliesAnArrival(t *testing.T) {
	indexedMail(t)

	w, err := watchAs(account.Primary, "index", "watch")
	if err != nil {
		t.Fatalf("start watch: %v", err)
	}
	defer w.stop(t)
	w.waitReady(t, 10*time.Second)

	subject := testID() + "-watched-index"
	sendTestMailSecondary(t, subject)

	line := w.waitForLine(t, 120*time.Second, func(l string) bool {
		return strings.Contains(l, "mail") && strings.Contains(l, "indexed")
	})
	if !strings.Contains(line, "indexed") {
		t.Fatalf("watch line %q does not report anything indexed", line)
	}
}
