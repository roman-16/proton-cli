package passfile

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// testdata/protonpass-export.zip is an archive Proton Pass itself wrote, with
// the identifiers and secrets replaced. The point of the format is that Proton
// can read what this writes and this can read what Proton wrote, so the file
// Proton wrote is what the reader is held to.
const protonExport = "testdata/protonpass-export.zip"

func neverAsked(t *testing.T) func() (string, error) {
	t.Helper()
	return func() (string, error) {
		t.Error("a passphrase was asked for on an export that is not encrypted")
		return "", nil
	}
}

// opened reads a file as the format named, and fails the test if it will not
// open.
func opened(t *testing.T, path, format string, ask func() (string, error)) *Document {
	t.Helper()
	doc, err := Open(path, format, ask)
	if err != nil {
		t.Fatalf("Open(%s, %s): %v", path, format, err)
	}
	t.Cleanup(func() { _ = doc.Close() })
	return doc
}

// written puts bytes where a reader can be pointed at them, which is how a test
// hands over a document it built itself.
func written(t *testing.T, name string, body []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}

// kinds is how many items of each kind a document holds.
func kinds(doc *Document) map[string]int {
	out := map[string]int{}
	for _, v := range doc.Vaults {
		for _, item := range v.Items {
			out[item.Kind]++
		}
	}
	return out
}

func TestAnArchiveProtonWroteIsRead(t *testing.T) {
	doc := opened(t, protonExport, Proton, neverAsked(t))
	if len(doc.Vaults) != 3 {
		t.Errorf("read %d vaults, want 3", len(doc.Vaults))
	}
	got := kinds(doc)
	for _, want := range []string{"login", "note", "credit-card", "identity", "alias"} {
		if got[want] == 0 {
			t.Errorf("no %s came out of the archive", want)
		}
	}
	for _, skipped := range doc.Skipped {
		t.Errorf("an item was skipped: %v", skipped)
	}
}

// What a login holds has to arrive, not just the item around it.
func TestALoginsContentSurvivesTheTrip(t *testing.T) {
	doc := opened(t, protonExport, Proton, neverAsked(t))
	var found bool
	for _, v := range doc.Vaults {
		for _, entry := range v.Items {
			login := entry.Item.GetContent().GetLogin()
			if login == nil {
				continue
			}
			if login.GetPassword() != "" || login.GetItemEmail() != "" {
				found = true
			}
		}
	}
	if !found {
		t.Error("no login came back with anything in it")
	}
}

// An item in the trash comes back to the trash, and the dates it was made and
// last changed on are its own rather than today's.
func TestWhatAnArchiveSaysAboutAnItemComesBack(t *testing.T) {
	doc := opened(t, protonExport, Proton, neverAsked(t))
	var dated int
	for _, v := range doc.Vaults {
		for _, entry := range v.Items {
			if entry.CreateTime > 0 && entry.ModifyTime > 0 {
				dated++
			}
		}
	}
	if dated == 0 {
		t.Error("no item kept the dates the archive gave it")
	}
}

// An export older than Pass 1.18 kept one field for what you sign in with, and
// it held an address.
func TestALoginFromBeforeTheUsernameSplit(t *testing.T) {
	doc := readDocument(t, `{"version":"1.17.0","vaults":{"s-1":{"name":"Personal","items":[
		{"data":{"metadata":{"name":"Old"},"type":"login","content":{"username":"jane@example.com","password":"pw"}}}]}}}`)
	login := doc.Vaults[0].Items[0].Item.GetContent().GetLogin()
	if login.GetItemEmail() != "jane@example.com" {
		t.Errorf("email = %q, want jane@example.com", login.GetItemEmail())
	}
	if login.GetItemUsername() != "" {
		t.Errorf("username = %q, want it empty", login.GetItemUsername())
	}
}

// A newer export keeps the two apart, and this one leaves them where they are.
func TestALoginFromAfterTheUsernameSplit(t *testing.T) {
	doc := readDocument(t, `{"version":"1.31.0","vaults":{"s-1":{"name":"Personal","items":[
		{"data":{"metadata":{"name":"New"},"type":"login","content":{"itemUsername":"jane","itemEmail":"jane@example.com"}}}]}}}`)
	login := doc.Vaults[0].Items[0].Item.GetContent().GetLogin()
	if login.GetItemUsername() != "jane" || login.GetItemEmail() != "jane@example.com" {
		t.Errorf("the login came back as %v", login)
	}
}

// An item written before the autofill rules existed carries its addresses in the
// older field alone, and they are read into both.
func TestALoginWrittenBeforeAutofillRules(t *testing.T) {
	doc := readDocument(t, `{"version":"1.20.0","vaults":{"s-1":{"name":"Personal","items":[
		{"data":{"metadata":{"name":"GitHub"},"type":"login","content":{"urls":["https://github.com"]}}}]}}}`)
	login := doc.Vaults[0].Items[0].Item.GetContent().GetLogin()
	if got := LoginURLs(login); len(got) != 1 || got[0] != "https://github.com" {
		t.Errorf("the URLs came back as %v", got)
	}
	if len(login.GetAutofillUrls()) != 1 {
		t.Errorf("the autofill rules came back as %v", login.GetAutofillUrls())
	}
}

// readDocument reads a bare document a test wrote out by hand.
func readDocument(t *testing.T, body string) *Document {
	t.Helper()
	return opened(t, written(t, "data.json", []byte(body)), Proton, neverAsked(t))
}

// A passphrase makes the file one nothing can read without it, and the document
// inside is the one Proton's importer looks for first.
func TestAnEncryptedArchiveNeedsItsPassphrase(t *testing.T) {
	doc := NewDocument("u-1")
	doc.Vaults["s-1"] = &ExportedVault{Name: "Personal", Items: []ExportedItem{loginExport(t)}}
	raw := archived(t, doc, "correct horse")
	if !doc.Encrypted {
		t.Error("the document does not say it is encrypted")
	}
	if strings.Contains(string(raw), "Personal") {
		t.Error("the vault name is readable in an encrypted archive")
	}
	path := written(t, "locked.zip", raw)

	if _, err := Open(path, Proton, func() (string, error) { return "wrong", nil }); err == nil {
		t.Error("the wrong passphrase opened the archive")
	}
	back := opened(t, path, Proton, func() (string, error) { return "correct horse", nil })
	if back.Vaults[0].Name != "Personal" {
		t.Errorf("the vault came back as %v", back.Vaults[0])
	}
}

// A document handed over on its own is read as one, so an export written to
// stdout and piped straight back in works.
func TestABareDocumentIsReadWithoutAnArchive(t *testing.T) {
	doc := NewDocument("u-1")
	doc.Vaults["s-1"] = &ExportedVault{Name: "Personal", Items: []ExportedItem{loginExport(t)}}
	body, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	back := opened(t, written(t, "data.json", body), Proton, neverAsked(t))
	if back.Vaults[0].Name != "Personal" {
		t.Errorf("the vault came back as %v", back.Vaults[0])
	}
}

func TestSomethingThatIsNotAnExportIsRefused(t *testing.T) {
	for _, raw := range []string{"", "{}", "not json at all", `{"vaults":{}}`} {
		path := written(t, "whatever.json", []byte(raw))
		if _, err := Open(path, Proton, neverAsked(t)); err == nil {
			t.Errorf("%q was read as an export", raw)
		}
	}
	if _, err := Open(filepath.Join(t.TempDir(), "missing.zip"), Proton, neverAsked(t)); err == nil {
		t.Error("a file that is not there was read as an export")
	}
}

// A CSV from another program is refused by the name of the flag that would read
// it, since that is the one thing the reader cannot work out for itself.
func TestACSVInSomebodyElsesColumnsSaysWhichFlagToUse(t *testing.T) {
	path := written(t, "passwords.csv", []byte("name,url,username,password\nx,y,z,w\n"))
	_, err := Open(path, Proton, neverAsked(t))
	if err == nil {
		t.Fatal("a file in the wrong columns was read as a Proton Pass CSV")
	}
	var hints interface{ Hints() []string }
	if !as(err, &hints) {
		t.Fatalf("the refusal carries no hint: %v", err)
	}
	if !strings.Contains(strings.Join(hints.Hints(), " "), "--manager") {
		t.Errorf("the refusal does not name --manager: %v", hints.Hints())
	}
}

// The CSV the app writes is the CSV it reads, whichever of the two wrote it.
func TestTheAppsOwnCSVIsRead(t *testing.T) {
	body := strings.Join([]string{
		strings.Join(csvColumns, ","),
		`login,GitHub,https://github.com,"[{""url"":""https://github.com"",""mode"":0}]",jane@example.com,jane,hunter2,a note,,1707735320,1707735349,Work`,
		`note,Router,,,,,,the note,,0,0,Work`,
		`creditCard,Visa,,,,,,"{""cardholderName"":""Jane"",""number"":""4242"",""note"":""mine""}",,0,0,Personal`,
	}, "\n")
	doc := opened(t, written(t, "export.csv", []byte(body)), Proton, neverAsked(t))

	if len(doc.Vaults) != 2 {
		t.Fatalf("read %d vaults, want 2", len(doc.Vaults))
	}
	login := doc.Vaults[0].Items[0]
	if login.Kind != "login" || login.Name != "GitHub" {
		t.Errorf("the first item came back as %s %q", login.Kind, login.Name)
	}
	content := login.Item.GetContent().GetLogin()
	if content.GetItemEmail() != "jane@example.com" || content.GetPassword() != "hunter2" {
		t.Errorf("the login came back as %v", content)
	}
	if login.CreateTime != 1707735320 || login.ModifyTime != 1707735349 {
		t.Errorf("the dates came back as %d and %d", login.CreateTime, login.ModifyTime)
	}
	card := doc.Vaults[1].Items[0]
	if card.Kind != "credit-card" {
		t.Fatalf("the card came back as a %s", card.Kind)
	}
	if got := card.Item.GetContent().GetCreditCard().GetNumber(); got != "4242" {
		t.Errorf("the card number came back as %q", got)
	}
	if got := card.Item.GetMetadata().GetNote(); got != "mine" {
		t.Errorf("the card's note came back as %q", got)
	}
}

// A CSV written before the app split the address from the user name has one
// column, and it held the address.
func TestACSVFromBeforeTheEmailColumn(t *testing.T) {
	body := "name,url,username,password,note,totp\nGitHub,https://github.com,jane@example.com,pw,,\n"
	doc := opened(t, written(t, "old.csv", []byte(body)), Proton, neverAsked(t))
	login := doc.Vaults[0].Items[0].Item.GetContent().GetLogin()
	if login.GetItemEmail() != "jane@example.com" || login.GetItemUsername() != "" {
		t.Errorf("the login came back as %v", login)
	}
}

// as is errors.As without the import, for the one test that wants a hint.
func as(err error, target *interface{ Hints() []string }) bool {
	for err != nil {
		if h, ok := err.(interface{ Hints() []string }); ok {
			*target = h
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}
