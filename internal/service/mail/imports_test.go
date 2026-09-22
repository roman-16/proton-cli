package mail

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// The mapping an import starts with is the one thing about this feature nothing
// live can check: Proton connects to the other mailbox itself, so no test
// account can be the source. What a folder turns into is therefore pinned here,
// against the shapes Proton's own client handles.

// mapped payload, read back the way the request carries it.
func destinationOf(t *testing.T, m mapped, source string) map[string]any {
	t.Helper()
	for _, entry := range m.payload {
		if entry["Source"] == source {
			d, _ := entry["Destinations"].(map[string]any)
			return d
		}
	}
	t.Fatalf("%q is not in the mapping", source)
	return nil
}

func mapping(t *testing.T, spec ImportSpec, folders ...rawImportFolder) mapped {
	t.Helper()
	m, err := importMapping(context.Background(), folders, spec)
	if err != nil {
		t.Fatalf("mapping: %v", err)
	}
	return m
}

func folder(source, separator string) rawImportFolder {
	return rawImportFolder{Source: source, Separator: separator, Size: 1}
}

// A folder Proton recognises lands where Proton says, whatever it is called on
// the other server: a mailbox's inbox is this mailbox's inbox.
func TestASystemFolderLandsWhereProtonSaysItDoes(t *testing.T) {
	in := rawImportFolder{Source: "INBOX", Separator: "/", DestinationFolder: "Inbox"}
	sent := rawImportFolder{Source: "[Gmail]/Sent Mail", Separator: "/", DestinationFolder: "Sent"}
	m := mapping(t, ImportSpec{}, in, sent)

	if got := destinationOf(t, m, "INBOX")["FolderPath"]; got != "Inbox" {
		t.Errorf("INBOX lands in %v, want Inbox", got)
	}
	if got := destinationOf(t, m, "[Gmail]/Sent Mail")["FolderPath"]; got != "Sent" {
		t.Errorf("sent mail lands in %v, want Sent", got)
	}
}

// Anything else keeps the name it had, level by level.
func TestAFolderKeepsItsOwnPath(t *testing.T) {
	m := mapping(t, ImportSpec{}, folder("Work", "/"), folder("Work/Clients", "/"))
	if got := destinationOf(t, m, "Work/Clients")["FolderPath"]; got != "Work/Clients" {
		t.Errorf("the subfolder lands in %v, want Work/Clients", got)
	}
}

// A server whose separator is not a slash still lands in a path Proton reads.
func TestAnotherSeparatorBecomesAPath(t *testing.T) {
	m := mapping(t, ImportSpec{}, folder("Work", "."), folder("Work.Clients", "."))
	if got := destinationOf(t, m, "Work.Clients")["FolderPath"]; got != "Work/Clients" {
		t.Errorf("the subfolder lands in %v, want Work/Clients", got)
	}
}

// Proton holds three levels of folder, so what is deeper is folded into the
// third rather than refused or truncated.
func TestAPathDeeperThanProtonAllowsIsFolded(t *testing.T) {
	deep := []rawImportFolder{
		folder("a", "/"), folder("a/b", "/"), folder("a/b/c", "/"), folder("a/b/c/d", "/"),
	}
	m := mapping(t, ImportSpec{}, deep...)
	if got := destinationOf(t, m, "a/b/c/d")["FolderPath"]; got != `a/b/c\/d` {
		t.Errorf("the fourth level lands in %v, want a/b/c\\/d", got)
	}
}

// A slash inside a name is part of the name and not a level, so it is never a
// place the path breaks. It leaves escaped exactly as Proton's own client
// escapes it, which is the only spelling known to be one Proton reads.
func TestASlashInsideANameIsNotALevel(t *testing.T) {
	m := mapping(t, ImportSpec{}, folder(`Receipts\/2024`, "/"))
	if got := destinationOf(t, m, `Receipts\/2024`)["FolderPath"]; got != `Receipts\\/2024` {
		t.Errorf(`the folder lands in %v, want Receipts\\/2024`, got)
	}
}

// A name that breaks into levels nobody has is one name. Servers hand back
// "a/b" without an "a", and filing its mail under a parent that does not exist
// would invent one.
func TestAPathWithNoParentIsOneName(t *testing.T) {
	m := mapping(t, ImportSpec{}, folder("a/b", "/"))
	if got := destinationOf(t, m, "a/b")["FolderPath"]; got != `a\/b` {
		t.Errorf("the folder lands in %v, want a\\/b", got)
	}
}

// Nothing may sit inside a folder Proton owns, so a child of one carries its
// parent in its name instead of being filed under it.
func TestAChildOfASystemFolderCarriesItsParentInItsName(t *testing.T) {
	in := rawImportFolder{Source: "Inbox", Separator: "/", DestinationFolder: "Inbox"}
	m := mapping(t, ImportSpec{}, in, folder("Inbox/Receipts", "/"))
	if got := destinationOf(t, m, "Inbox/Receipts")["FolderPath"]; got != "[Inbox]Receipts" {
		t.Errorf("the subfolder lands in %v, want [Inbox]Receipts", got)
	}
}

// Gmail's folders are labels: a message carrying three of them appears in three
// IMAP folders, and imported as folders it would land in one and lose the rest.
func TestGmailFoldersArriveAsLabels(t *testing.T) {
	m := mapping(t, ImportSpec{Server: gmailIMAP},
		folder("Work", "/"), folder("Work/Clients", "/"))

	labels, ok := destinationOf(t, m, "Work/Clients")["Labels"].([]map[string]any)
	if !ok || len(labels) != 1 {
		t.Fatalf("the folder did not become a label: %v", destinationOf(t, m, "Work/Clients"))
	}
	if labels[0]["Name"] != "Work-Clients" {
		t.Errorf("the label is named %v, want Work-Clients", labels[0]["Name"])
	}
	if colour, _ := labels[0]["Color"].(string); !strings.HasPrefix(colour, "#") {
		t.Errorf("the label has no colour: %v", labels[0]["Color"])
	}
	if _, folders := destinationOf(t, m, "Work/Clients")["FolderPath"]; folders {
		t.Error("a label mapping also asked for a folder")
	}
}

// A Gmail tab is a place inside the inbox rather than a folder beside it.
func TestAGmailCategoryLandsInTheInbox(t *testing.T) {
	promotions := rawImportFolder{
		Source: "CATEGORY_PROMOTIONS", Separator: "/", DestinationCategory: "Promotions",
	}
	m := mapping(t, ImportSpec{Server: gmailIMAP}, promotions)
	d := destinationOf(t, m, "CATEGORY_PROMOTIONS")
	if d["FolderPath"] != "Inbox" || d["Category"] != "Promotions" {
		t.Errorf("the tab lands as %v, want the inbox with its category", d)
	}
}

// Skipping a folder skips what is inside it: somebody who leaves out the spam
// folder means the folder, not its name.
func TestSkippingAFolderSkipsWhatIsInsideIt(t *testing.T) {
	m := mapping(t, ImportSpec{Skip: []string{"Junk"}},
		folder("INBOX", "/"), folder("Junk", "/"), folder("Junk/Old", "/"))

	if len(m.payload) != 1 {
		t.Fatalf("%d folders are being imported, want 1", len(m.payload))
	}
	if m.payload[0]["Source"] != "INBOX" {
		t.Errorf("the import carries %v, want INBOX", m.payload[0]["Source"])
	}
}

// A glob picks folders out the way it does everywhere else, and without caring
// about case.
func TestSkippingTakesAGlob(t *testing.T) {
	m := mapping(t, ImportSpec{Skip: []string{"archive/*", "SPAM"}},
		folder("INBOX", "/"), folder("Archive", "/"),
		folder("Archive/2023", "/"), folder("Spam", "/"))

	var kept []string
	for _, entry := range m.payload {
		kept = append(kept, entry["Source"].(string))
	}
	if strings.Join(kept, ",") != "INBOX,Archive" {
		t.Errorf("the import carries %v, want INBOX and Archive", kept)
	}
}

// Skipping everything is a mistake rather than an empty import.
func TestSkippingEverythingIsRefused(t *testing.T) {
	_, err := importMapping(context.Background(),
		[]rawImportFolder{folder("INBOX", "/")}, ImportSpec{Skip: []string{"*"}})
	if err == nil {
		t.Fatal("an import of nothing was accepted")
	}
	if !strings.Contains(err.Error(), "skipped") {
		t.Errorf("the refusal says %q, which does not say what went wrong", err)
	}
}

// What Proton would refuse is refused here, before any mail moves, and the
// folder it is about is named.
func TestAFolderProtonWouldRefuseIsRefusedFirst(t *testing.T) {
	cases := []struct {
		name   string
		folder rawImportFolder
		says   string
	}{
		{"reserved", folder("Outbox", "/"), "keeps"},
		{"too long", folder(strings.Repeat("n", 100), "/"), "bytes"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := importMapping(context.Background(), []rawImportFolder{tc.folder}, ImportSpec{})
			if err == nil {
				t.Fatal("the folder was accepted")
			}
			if !strings.Contains(err.Error(), tc.says) {
				t.Errorf("the refusal says %q, which does not say what is wrong", err)
			}
			if !strings.Contains(err.Error(), tc.folder.Source) {
				t.Errorf("the refusal does not say which folder: %q", err)
			}
		})
	}
}

// The size an import reports is the size of what it is importing, and what is
// skipped is not part of it.
func TestTheSizeIsWhatIsBeingImported(t *testing.T) {
	big := rawImportFolder{Source: "Archive", Separator: "/", Size: 1000}
	small := rawImportFolder{Source: "INBOX", Separator: "/", Size: 25}
	m := mapping(t, ImportSpec{Skip: []string{"Archive"}}, big, small)
	if m.size != 25 {
		t.Errorf("the import is %d bytes, want 25", m.size)
	}
}

// The listing merges what is running with what is over, newest first, and says
// enough about each for a person to tell them apart.
func TestTheListingMergesRunningAndFinishedImports(t *testing.T) {
	running := []rawImporter{{
		ID: "run-1", Account: "jane@fastmail.com", Provider: providerIMAP,
		ImapHost: "imap.fastmail.com", ImapPort: "993",
		Active: map[string]rawImporterActive{importProduct: {
			CreateTime: 200, State: 1, Processed: 12, Total: 40,
			Mapping: []rawImporterFolder{{SourceFolder: "INBOX", DestinationFolder: "Inbox", Processed: 12, Total: 40}},
		}},
	}}
	finished := []rawReport{{
		ID: "report-1", Account: "old@yahoo.com", Provider: providerIMAP,
		CreateTime: 100, EndTime: 150, TotalSize: 4096,
		Summary: map[string]rawReportSummary{importProduct: {
			State: 2, NumMessages: 90, TotalSize: 4096, RollbackState: rollbackAvailable,
		}},
	}}

	ctx := context.Background()
	var rows []Import
	for _, raw := range running {
		rows = append(rows, raw.imported(ctx, raw.Active[importProduct]))
	}
	for _, raw := range finished {
		summary := raw.Summary[importProduct]
		rows = append(rows, Import{
			ID: raw.ID, Account: raw.Account, State: importFinishedState(ctx, summary),
			Ended: raw.EndTime, Finished: true, Undoable: summary.RollbackState == rollbackAvailable,
		})
	}

	if rows[0].State != "importing" || rows[0].Server != "imap.fastmail.com:993" {
		t.Errorf("the running import reads as %+v", rows[0])
	}
	if rows[0].Progress() != "12/40" {
		t.Errorf("progress reads %q, want 12/40", rows[0].Progress())
	}
	if rows[1].State != "imported" || !rows[1].Undoable {
		t.Errorf("the finished import reads as %+v", rows[1])
	}
}

// An import being taken back is no longer described by how it went.
func TestAnImportBeingUndoneSaysSo(t *testing.T) {
	ctx := context.Background()
	if got := importFinishedState(ctx, rawReportSummary{State: 2, RollbackState: rollbackRunning}); got != "undoing" {
		t.Errorf("an import being undone reads as %q", got)
	}
	if got := importFinishedState(ctx, rawReportSummary{State: 2, RollbackState: rollbackDone}); got != "undone" {
		t.Errorf("an import that was undone reads as %q", got)
	}
}

// Stopping is one word while Proton is still doing it and another once it is
// done, because they are different facts to somebody deciding what to do next.
func TestCancellingAndCancelledAreDifferentWords(t *testing.T) {
	ctx := context.Background()
	if got := importState(ctx, 5, false); got != "cancelling" {
		t.Errorf("an import being stopped reads as %q", got)
	}
	if got := importState(ctx, 5, true); got != "cancelled" {
		t.Errorf("an import that was stopped reads as %q", got)
	}
}

// The label an import puts on everything says where the mail came from and when
// it was fetched, so two imports of the same mailbox are told apart.
func TestTheDefaultLabelNamesTheMailboxAndTheMoment(t *testing.T) {
	at := time.Date(2026, 4, 15, 9, 12, 0, 0, time.UTC)
	got := ImportLabel("jane@fastmail.com", at)
	if !strings.HasPrefix(got, "fastmail.com ") {
		t.Errorf("the label is %q, which does not name the mailbox it came from", got)
	}
	if !strings.Contains(got, "2026-04-15") {
		t.Errorf("the label is %q, which does not say when", got)
	}
}

// A colour is picked from the name rather than at random, so an import run twice
// looks the same both times.
func TestALabelColourIsTheSameForTheSameName(t *testing.T) {
	if accentFor("Work") != accentFor("work") {
		t.Error("the same name got two colours")
	}
	if accentFor("Work") == accentFor("Clients") && accentFor("Work") == accentFor("Receipts") {
		t.Error("every name got the same colour")
	}
}

// What a listing hands back is a document a script can walk: the folders of an
// import are always a list, never null.
func TestAnImportAlwaysCarriesAFolderList(t *testing.T) {
	raw := rawImporter{ID: "x", Account: "jane@fastmail.com"}
	b, err := json.Marshal(raw.imported(context.Background(), rawImporterActive{}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"folders":[]`) {
		t.Errorf("an import with no mapping serializes as %s", b)
	}
}
