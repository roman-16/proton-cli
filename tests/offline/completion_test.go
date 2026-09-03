package offline

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A shell completes by running the binary: every generated script calls
// `proton __complete <words>` and reads what comes back. So the completion
// request is part of the interface, it costs no account and no network, and it
// is checked here rather than trusted.
func TestTheShellCanAskWhatComesNext(t *testing.T) {
	for _, request := range []string{"__complete", "__completeNoDesc"} {
		stdout, stderr, code := run(t, request, "")
		if code != 0 {
			t.Errorf("%s: exit %d, want 0\nstderr: %s", request, code, truncate(stderr))
			continue
		}
		for _, want := range []string{"changelog", "drive", "mail", "update"} {
			if !strings.Contains(stdout, want) {
				t.Errorf("%s: does not offer %q\nstdout: %s", request, want, truncate(stdout))
			}
		}
	}
}

func TestTheShellCanAskWhatComesAfterAGroup(t *testing.T) {
	stdout, _, code := run(t, "__complete", "mail", "messages", "")
	if code != 0 {
		t.Fatalf("exit %d, want 0", code)
	}
	for _, want := range []string{"list", "send", "trash"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("does not offer %q\nstdout: %s", want, truncate(stdout))
		}
	}
}

// ── what a listing left behind ──

// seen writes a profile's memory of what it was shown, which is the only thing a
// completion reads. Each test gets a profile of its own, so what one remembers is
// not what another completes from.
func seen(t *testing.T, profile string, entries ...map[string]any) {
	t.Helper()
	dir := filepath.Join(configDir, "proton-cli", "idcache")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatalf("make the cache directory: %v", err)
	}
	data, err := json.Marshal(entries)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, profile+".json"), data, 0600); err != nil {
		t.Fatalf("write the cache: %v", err)
	}
}

func entry(collection, ref string, handles ...string) map[string]any {
	return map[string]any{"collection": collection, "ref": ref, "handles": handles}
}

// completing runs the request a shell makes, and returns the lines it offers,
// with cobra's trailing directive line dropped.
func completing(t *testing.T, profile string, words ...string) []string {
	t.Helper()
	args := append([]string{"__complete", "--profile", profile}, words...)
	stdout, stderr, code := run(t, args...)
	if code != 0 {
		t.Fatalf("%v: exit %d, want 0\nstderr: %s", args, code, truncate(stderr))
	}
	var out []string
	for _, line := range strings.Split(strings.TrimSpace(stdout), "\n") {
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		out = append(out, line)
	}
	return out
}

const (
	sushi   = "ketTSogwyRvQ2mLpN4vRs8kZc1yXd7fGh3jAe6bUi0oQm2nWr5tYvAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=="
	invoice = "5bH2mQxKT9wLpN4vRs8kZc1yXd7fGh3jAe6bUi0oQm2nWr5tYvAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=="
	jane    = "QmxLp2RtT9wLpN4vRs8kZc1yXd7fGh3jAe6bUi0oQm2nWr5tYvAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=="
)

// The short ID a listing printed is what the next command line starts with, so
// it is what the shell offers back, with the subject beside it to choose by.
func TestAShortIDCompletesToWhatTheListingShowed(t *testing.T) {
	seen(t, "short-ids",
		entry("mail conversations", sushi, "Reservation Confirmation - Vero Sushi"))
	got := completing(t, "short-ids", "mail", "conversations", "get", "ket")
	want := "ketTSogw\tReservation Confirmation - Vero Sushi"
	if len(got) != 1 || got[0] != want {
		t.Errorf("got %q, want [%q]", got, want)
	}
}

// A subject is as much a reference as an ID is, and the shell decides between
// them by what has been typed.
func TestASubjectCompletesToo(t *testing.T) {
	seen(t, "subjects", entry("mail messages", invoice, "Invoice #2291 is ready"))
	got := completing(t, "subjects", "mail", "messages", "get", "Invo")
	if len(got) != 1 || got[0] != "Invoice #2291 is ready" {
		t.Errorf("got %q, want the subject", got)
	}
}

// A contact answers to a name and to an address, because its listing shows both
// and both resolve. Case is not what a person keeps track of when typing a name
// back, so it is not what decides whether it is offered.
func TestAContactCompletesByNameAndByAddress(t *testing.T) {
	seen(t, "contacts", entry("contacts", jane, "Jane Doe", "jane@example.com"))
	if got := completing(t, "contacts", "contacts", "get", "jane"); len(got) != 2 ||
		got[0] != "Jane Doe" || got[1] != "jane@example.com" {
		t.Errorf("got %q, want the name and the address", got)
	}
	if got := completing(t, "contacts", "contacts", "get", "jane@"); len(got) != 1 || got[0] != "jane@example.com" {
		t.Errorf("by address: got %q", got)
	}
}

// A full ID pasted from a script is longer than the eight characters a listing
// prints, so the completion grows it rather than shrinking what is already there.
func TestAFullIDCompletesToItsWholeSelf(t *testing.T) {
	seen(t, "full-ids", entry("mail messages", invoice, "Invoice #2291 is ready"))
	got := completing(t, "full-ids", "mail", "messages", "get", invoice[:20])
	if len(got) != 1 || !strings.HasPrefix(got[0], invoice) {
		t.Errorf("got %q, want the whole ID", got)
	}
}

// What was shown for one collection is not offered for another: a conversation
// is not a Pass item, however alike two IDs begin.
func TestOneCollectionDoesNotCompleteAnother(t *testing.T) {
	seen(t, "collections", entry("mail conversations", sushi, "Reservation Confirmation"))
	if got := completing(t, "collections", "pass", "items", "get", "ket"); len(got) != 0 {
		t.Errorf("a Pass item completed from mail: %q", got)
	}
}

// Silence would read as a broken completion. A collection with nothing in it
// says which listing would fill it, through cobra's active help.
func TestAnEmptyCollectionSaysWhichListingWouldFillIt(t *testing.T) {
	seen(t, "nothing-seen")
	got := completing(t, "nothing-seen", "pass", "items", "get", "")
	if len(got) != 1 || !strings.Contains(got[0], "proton pass items list") {
		t.Errorf("got %q, want a hint naming the listing", got)
	}
}

// A command's REF names what its collection holds, and a sub-collection reaches
// past itself for it: an attachment is downloaded from a message, so the message
// is what the first argument completes from and the attachment the second.
func TestASubCollectionCompletesTheThingItHangsOff(t *testing.T) {
	seen(t, "attachments",
		entry("mail messages", invoice, "Invoice #2291 is ready"),
		entry("mail messages attachments", "kQ81mDx4T9wLpN4vRs8kZc1yXd7fGh3jAe6bUi0oQm2nWr5tYvAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA==", "invoice.pdf"))
	first := completing(t, "attachments", "mail", "messages", "attachments", "download", "")
	if len(first) == 0 || !strings.HasPrefix(first[0], "5bH2mQxK\t") {
		t.Errorf("first argument: got %q, want the message", first)
	}
	second := completing(t, "attachments", "mail", "messages", "attachments", "download", "5bH2mQxK", "")
	if len(second) == 0 || !strings.HasPrefix(second[0], "kQ81mDx4\t") {
		t.Errorf("second argument: got %q, want the attachment", second)
	}
}

// A Drive path is not one of this CLI's references, so the shell keeps its own
// answer for it: the local filenames it would have offered anyway.
func TestAPathIsLeftToTheShell(t *testing.T) {
	seen(t, "paths")
	stdout, _, code := run(t, "__complete", "--profile", "paths", "drive", "items", "get", "")
	if code != 0 {
		t.Fatalf("exit %d, want 0", code)
	}
	if !strings.HasSuffix(strings.TrimSpace(stdout), ":0") {
		t.Errorf("a path should be left to the shell's own completion, got %q", truncate(stdout))
	}
}

// A completion is not an invocation: it reads one file and answers, so it leaves
// no diagnostic log behind. A shell asks on every press of the tab key, and a
// directory of those would crowd out the runs a bug report needs.
func TestACompletionWritesNoRunLog(t *testing.T) {
	logs := filepath.Join(configDir, "proton-cli", "logs")
	before, _ := os.ReadDir(logs)
	completing(t, "no-log", "mail", "conversations", "get", "ket")
	after, _ := os.ReadDir(logs)
	if len(after) != len(before) {
		t.Errorf("a completion wrote to %s: %d entries before, %d after", logs, len(before), len(after))
	}
}
