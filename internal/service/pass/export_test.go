package pass

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	pb "github.com/roman-16/proton-cli/internal/service/pass/proto"
)

// testdata/protonpass-export.zip is an archive Proton Pass itself wrote, with
// the identifiers and secrets replaced. The point of the format is that Proton
// can read what this writes and this can read what Proton wrote, so the file
// Proton wrote is what the reader is held to.
func protonExport(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile("testdata/protonpass-export.zip")
	if err != nil {
		t.Fatalf("read the fixture: %v", err)
	}
	return raw
}

func neverAsked(t *testing.T) func() (string, error) {
	t.Helper()
	return func() (string, error) {
		t.Error("a passphrase was asked for on an archive that is not encrypted")
		return "", nil
	}
}

func TestAnArchiveProtonWroteIsRead(t *testing.T) {
	doc, err := Unarchive(protonExport(t), neverAsked(t))
	if err != nil {
		t.Fatalf("Unarchive: %v", err)
	}
	if len(doc.Vaults) != 3 {
		t.Errorf("read %d vaults, want 3", len(doc.Vaults))
	}

	kinds := map[string]int{}
	for _, v := range doc.Vaults {
		for _, item := range v.Items {
			built, err := importItem(item)
			if err != nil {
				// An alias is the one kind that cannot be read back, and it says so.
				if item.Data.Type == "alias" {
					kinds["alias"]++
					continue
				}
				t.Errorf("%s %q: %v", item.Data.Type, item.Data.Metadata.Name, err)
				continue
			}
			kinds[item.Data.Type]++
			if built.GetMetadata().GetName() != item.Data.Metadata.Name {
				t.Errorf("the name did not survive: %q", built.GetMetadata().GetName())
			}
		}
	}
	for _, want := range []string{"login", "note", "creditCard", "identity", "alias"} {
		if kinds[want] == 0 {
			t.Errorf("no %s came out of the archive", want)
		}
	}
}

// What a login holds has to arrive, not just the item around it.
func TestALoginsContentSurvivesTheTrip(t *testing.T) {
	doc, err := Unarchive(protonExport(t), neverAsked(t))
	if err != nil {
		t.Fatalf("Unarchive: %v", err)
	}
	var found bool
	for _, v := range doc.Vaults {
		for _, item := range v.Items {
			if item.Data.Type != "login" {
				continue
			}
			var raw struct {
				Password  string `json:"password"`
				ItemEmail string `json:"itemEmail"`
			}
			if err := json.Unmarshal(item.Data.Content, &raw); err != nil {
				t.Fatalf("the fixture's own content will not parse: %v", err)
			}
			if raw.Password == "" && raw.ItemEmail == "" {
				continue
			}
			built, err := importItem(item)
			if err != nil {
				t.Fatalf("importItem: %v", err)
			}
			login := built.GetContent().GetLogin()
			if login.GetPassword() != raw.Password {
				t.Errorf("password = %q, want %q", login.GetPassword(), raw.Password)
			}
			if login.GetItemEmail() != raw.ItemEmail {
				t.Errorf("email = %q, want %q", login.GetItemEmail(), raw.ItemEmail)
			}
			found = true
		}
	}
	if !found {
		t.Skip("the fixture holds no login with contents to check")
	}
}

// An export this writes reads back as the same items, which is the only thing
// that makes it a backup.
func TestAnExportReadsBackAsWhatWentIn(t *testing.T) {
	item := &pb.Item{
		Metadata: &pb.Metadata{Name: "GitHub", Note: "the note", ItemUuid: "u-1"},
		Content: &pb.Content{Content: &pb.Content_Login{Login: &pb.ItemLogin{
			ItemEmail: "jane@example.com", Password: "hunter2", Urls: []string{"https://github.com"},
		}}},
		ExtraFields: []*pb.ExtraField{{
			FieldName: "Recovery",
			Content:   &pb.ExtraField_Hidden{Hidden: &pb.ExtraHiddenField{Content: "abc"}},
		}},
	}
	exported, err := exportItem(FullItem{
		raw:  item,
		Item: Item{Name: "GitHub", ItemID: "i-1", ShareID: "s-1", State: 1},
	})
	if err != nil {
		t.Fatalf("exportItem: %v", err)
	}
	if exported.Data.Type != "login" {
		t.Errorf("type = %q, want login", exported.Data.Type)
	}

	back, err := importItem(*exported)
	if err != nil {
		t.Fatalf("importItem: %v", err)
	}
	login := back.GetContent().GetLogin()
	if login.GetPassword() != "hunter2" || login.GetItemEmail() != "jane@example.com" {
		t.Errorf("the login came back as %v", login)
	}
	if len(login.GetUrls()) != 1 || login.GetUrls()[0] != "https://github.com" {
		t.Errorf("the URLs came back as %v", login.GetUrls())
	}
	if len(back.GetExtraFields()) != 1 ||
		back.GetExtraFields()[0].GetHidden().GetContent() != "abc" {
		t.Errorf("the custom field came back as %v", back.GetExtraFields())
	}
	if back.GetMetadata().GetNote() != "the note" {
		t.Errorf("the note came back as %q", back.GetMetadata().GetNote())
	}
}

// Proton writes a field it has no value for as an empty one rather than leaving
// it out, so an export has to do the same or its own importer finds a gap.
func TestAnExportWritesEveryFieldEvenTheEmptyOnes(t *testing.T) {
	exported, err := exportItem(FullItem{
		raw: &pb.Item{
			Metadata: &pb.Metadata{Name: "empty"},
			Content:  &pb.Content{Content: &pb.Content_Login{Login: &pb.ItemLogin{}}},
		},
	})
	if err != nil {
		t.Fatalf("exportItem: %v", err)
	}
	for _, want := range []string{`"password"`, `"itemEmail"`, `"itemUsername"`, `"totpUri"`} {
		if !strings.Contains(string(exported.Data.Content), want) {
			t.Errorf("the content leaves out %s: %s", want, exported.Data.Content)
		}
	}
	// An item with no custom fields carries an empty list, not a null.
	if string(exported.Data.ExtraFields) != "[]" {
		t.Errorf("extraFields = %s, want []", exported.Data.ExtraFields)
	}
	// Only an alias has an address, so everything else says so with a null.
	if exported.AliasEmail != nil {
		t.Errorf("aliasEmail = %v on an item that is not an alias", *exported.AliasEmail)
	}
}

// A passphrase makes the archive one nothing can read without it, and the file
// inside is the one Proton's importer looks for first.
func TestAnEncryptedArchiveNeedsItsPassphrase(t *testing.T) {
	doc := &ExportDocument{
		Vaults:  map[string]*ExportedVault{"s-1": {Name: "Personal", Items: []ExportedItem{}}},
		Version: exportVersion,
	}
	raw, err := Archive(doc, "correct horse")
	if err != nil {
		t.Fatalf("Archive: %v", err)
	}
	if !doc.Encrypted {
		t.Error("the document does not say it is encrypted")
	}
	if strings.Contains(string(raw), "Personal") {
		t.Error("the vault name is readable in an encrypted archive")
	}

	if _, err := Unarchive(raw, func() (string, error) { return "wrong", nil }); err == nil {
		t.Error("the wrong passphrase opened the archive")
	}
	back, err := Unarchive(raw, func() (string, error) { return "correct horse", nil })
	if err != nil {
		t.Fatalf("Unarchive: %v", err)
	}
	if back.Vaults["s-1"].Name != "Personal" {
		t.Errorf("the vault came back as %v", back.Vaults["s-1"])
	}
}

// A document handed over on its own is read as one, so an export written to
// stdout and piped straight back in works.
func TestABareDocumentIsReadWithoutAnArchive(t *testing.T) {
	doc := &ExportDocument{
		Vaults:  map[string]*ExportedVault{"s-1": {Name: "Personal", Items: []ExportedItem{}}},
		Version: exportVersion,
	}
	body, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	back, err := Unarchive(body, neverAsked(t))
	if err != nil {
		t.Fatalf("Unarchive: %v", err)
	}
	if back.Vaults["s-1"].Name != "Personal" {
		t.Errorf("the vault came back as %v", back.Vaults["s-1"])
	}
}

func TestSomethingThatIsNotAnExportIsRefused(t *testing.T) {
	for _, raw := range []string{"", "{}", "not json at all", `{"vaults":{}}`} {
		if _, err := Unarchive([]byte(raw), neverAsked(t)); err == nil {
			t.Errorf("%q was read as an export", raw)
		}
	}
}
