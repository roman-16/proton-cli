package passfile

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"strings"
	"testing"

	pb "github.com/roman-16/proton-cli/internal/service/pass/proto"
)

// archived is the zip an export of this document would write, with no
// attachments in it.
func archived(t *testing.T, doc *ExportDocument, passphrase string) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := WriteArchive(&buf, doc, nil, passphrase); err != nil {
		t.Fatalf("WriteArchive: %v", err)
	}
	return buf.Bytes()
}

// loginExport is one item as a document carries it.
func loginExport(t *testing.T) ExportedItem {
	t.Helper()
	login := &pb.ItemLogin{
		ItemEmail: "jane@example.com", ItemUsername: "jane", Password: "hunter2",
		TotpUri: "otpauth://totp/GitHub?secret=JBSWY3DPEHPK3PXP",
	}
	SetLoginURLs(login, []string{"https://github.com"})
	item := &pb.Item{
		Metadata: &pb.Metadata{Name: "GitHub", Note: "the note", ItemUuid: "u-1"},
		Content:  &pb.Content{Content: &pb.Content_Login{Login: login}},
		ExtraFields: []*pb.ExtraField{{
			FieldName: "Recovery",
			Content:   &pb.ExtraField_Hidden{Hidden: &pb.ExtraHiddenField{Content: "abc"}},
		}},
	}
	out, err := ExportItem(StoredItem{
		ShareID: "s-1", ItemID: "i-1", Item: item, State: 1,
		CreateTime: 1707735320, ModifyTime: 1707735349,
	})
	if err != nil {
		t.Fatalf("ExportItem: %v", err)
	}
	return *out
}

// An export this writes reads back as the same items, which is the only thing
// that makes it a backup.
func TestAnExportReadsBackAsWhatWentIn(t *testing.T) {
	doc := NewDocument("u-1")
	doc.Vaults["s-1"] = &ExportedVault{Name: "Personal", Items: []ExportedItem{loginExport(t)}}

	back := opened(t, written(t, "backup.zip", archived(t, doc, "")), Proton, neverAsked(t))
	entry := back.Vaults[0].Items[0]
	if entry.Kind != "login" || entry.Name != "GitHub" {
		t.Fatalf("the item came back as %s %q", entry.Kind, entry.Name)
	}
	login := entry.Item.GetContent().GetLogin()
	if login.GetPassword() != "hunter2" || login.GetItemEmail() != "jane@example.com" {
		t.Errorf("the login came back as %v", login)
	}
	if got := LoginURLs(login); len(got) != 1 || got[0] != "https://github.com" {
		t.Errorf("the URLs came back as %v", got)
	}
	if len(entry.Item.GetExtraFields()) != 1 ||
		entry.Item.GetExtraFields()[0].GetHidden().GetContent() != "abc" {
		t.Errorf("the custom field came back as %v", entry.Item.GetExtraFields())
	}
	if entry.Item.GetMetadata().GetNote() != "the note" {
		t.Errorf("the note came back as %q", entry.Item.GetMetadata().GetNote())
	}
	if entry.CreateTime != 1707735320 || entry.ModifyTime != 1707735349 {
		t.Errorf("the dates came back as %d and %d", entry.CreateTime, entry.ModifyTime)
	}
}

// An item in the trash is written as one, and comes back to the trash.
func TestTheTrashSurvivesAnExport(t *testing.T) {
	item := loginExport(t)
	item.State = StateTrashed
	doc := NewDocument("u-1")
	doc.Vaults["s-1"] = &ExportedVault{Name: "Personal", Items: []ExportedItem{item}}

	back := opened(t, written(t, "backup.zip", archived(t, doc, "")), Proton, neverAsked(t))
	if !back.Vaults[0].Items[0].Trashed {
		t.Error("an item that was in the trash came back active")
	}
}

// Proton writes a field it has no value for as an empty one rather than leaving
// it out, so an export has to do the same or its own importer finds a gap.
func TestAnExportWritesEveryFieldEvenTheEmptyOnes(t *testing.T) {
	exported, err := ExportItem(StoredItem{Item: &pb.Item{
		Metadata: &pb.Metadata{Name: "empty"},
		Content:  &pb.Content{Content: &pb.Content_Login{Login: &pb.ItemLogin{}}},
	}})
	if err != nil {
		t.Fatalf("ExportItem: %v", err)
	}
	for _, want := range []string{`"password"`, `"itemEmail"`, `"itemUsername"`, `"totpUri"`, `"autofillUrls"`} {
		if !strings.Contains(string(exported.Data.Content), want) {
			t.Errorf("the content leaves out %s: %s", want, exported.Data.Content)
		}
	}
	// An item with no custom fields carries an empty list, not a null. So does
	// one with no attachments.
	if string(exported.Data.ExtraFields) != "[]" {
		t.Errorf("extraFields = %s, want []", exported.Data.ExtraFields)
	}
	if body, err := json.Marshal(exported); err != nil {
		t.Fatalf("marshal: %v", err)
	} else if !strings.Contains(string(body), `"files":[]`) {
		t.Errorf("the item leaves out its files: %s", body)
	}
	// Only an alias has an address, so everything else says so with a null.
	if exported.AliasEmail != nil {
		t.Errorf("aliasEmail = %v on an item that is not an alias", *exported.AliasEmail)
	}
}

// The document written on its own is the one the archive holds.
func TestTheDocumentCanBeWrittenWithoutTheArchive(t *testing.T) {
	doc := NewDocument("u-1")
	doc.Vaults["s-1"] = &ExportedVault{Name: "Personal", Items: []ExportedItem{loginExport(t)}}

	var buf bytes.Buffer
	if err := WriteJSON(&buf, doc, ""); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}
	back := opened(t, written(t, "data.json", buf.Bytes()), Proton, neverAsked(t))
	if back.UserID != "u-1" || back.Count() != 1 {
		t.Errorf("the document came back with %d items for %q", back.Count(), back.UserID)
	}
}

// With a passphrase the document alone is the encrypted form, which reads back
// the same way the archive does.
func TestTheDocumentCanBeEncryptedWithoutTheArchive(t *testing.T) {
	doc := NewDocument("u-1")
	doc.Vaults["s-1"] = &ExportedVault{Name: "Personal", Items: []ExportedItem{loginExport(t)}}

	var buf bytes.Buffer
	if err := WriteJSON(&buf, doc, "correct horse"); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}
	if strings.Contains(buf.String(), "Personal") {
		t.Error("the vault name is readable in an encrypted document")
	}
	back := opened(t, written(t, "data.pgp", buf.Bytes()), Proton,
		func() (string, error) { return "correct horse", nil })
	if back.Vaults[0].Name != "Personal" {
		t.Errorf("the vault came back as %v", back.Vaults[0])
	}
}

// The CSV is the app's own columns, so the app reads what this writes.
func TestTheCSVIsWrittenInTheAppsColumns(t *testing.T) {
	doc := NewDocument("u-1")
	doc.Vaults["s-1"] = &ExportedVault{Name: "Work", Items: []ExportedItem{loginExport(t)}}

	var buf bytes.Buffer
	if err := WriteCSV(&buf, doc); err != nil {
		t.Fatalf("WriteCSV: %v", err)
	}
	rows, err := csv.NewReader(bytes.NewReader(buf.Bytes())).ReadAll()
	if err != nil {
		t.Fatalf("the CSV will not parse: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("the CSV holds %d rows, want 2", len(rows))
	}
	if strings.Join(rows[0], ",") != strings.Join(csvColumns, ",") {
		t.Errorf("the columns are %v", rows[0])
	}
	want := map[string]string{
		"type": "login", "name": "GitHub", "url": "https://github.com",
		"autofillUrls": `[{"url":"https://github.com","mode":0}]`,
		"email":        "jane@example.com", "username": "jane", "password": "hunter2",
		"note": "the note", "totp": "otpauth://totp/GitHub?secret=JBSWY3DPEHPK3PXP",
		"createTime": "1707735320", "modifyTime": "1707735349", "vault": "Work",
	}
	for i, column := range csvColumns {
		if rows[1][i] != want[column] {
			t.Errorf("%s = %q, want %q", column, rows[1][i], want[column])
		}
	}
}

// A card and an identity have more fields than columns, so they travel as one.
func TestTheCSVCarriesACardsFieldsInOneColumn(t *testing.T) {
	card, err := ExportItem(StoredItem{
		ShareID: "s-1", ItemID: "i-2",
		Item: &pb.Item{
			Metadata: &pb.Metadata{Name: "Visa", Note: "mine"},
			Content: &pb.Content{Content: &pb.Content_CreditCard{CreditCard: &pb.ItemCreditCard{
				CardholderName: "Jane", Number: "4242424242424242", Pin: "1234",
			}}},
		},
	})
	if err != nil {
		t.Fatalf("ExportItem: %v", err)
	}
	doc := NewDocument("u-1")
	doc.Vaults["s-1"] = &ExportedVault{Name: "Personal", Items: []ExportedItem{*card}}

	var buf bytes.Buffer
	if err := WriteCSV(&buf, doc); err != nil {
		t.Fatalf("WriteCSV: %v", err)
	}
	rows, err := csv.NewReader(bytes.NewReader(buf.Bytes())).ReadAll()
	if err != nil {
		t.Fatalf("the CSV will not parse: %v", err)
	}
	var fields map[string]any
	if err := json.Unmarshal([]byte(rows[1][7]), &fields); err != nil {
		t.Fatalf("the note column is not the card's fields: %v", err)
	}
	if fields["number"] != "4242424242424242" || fields["note"] != "mine" {
		t.Errorf("the card came out as %v", fields)
	}
}

// A CSV this writes is a CSV this reads.
func TestTheCSVReadsBackAsWhatWentIn(t *testing.T) {
	doc := NewDocument("u-1")
	doc.Vaults["s-1"] = &ExportedVault{Name: "Work", Items: []ExportedItem{loginExport(t)}}

	var buf bytes.Buffer
	if err := WriteCSV(&buf, doc); err != nil {
		t.Fatalf("WriteCSV: %v", err)
	}
	back := opened(t, written(t, "export.csv", buf.Bytes()), Proton, neverAsked(t))
	if len(back.Vaults) != 1 || back.Vaults[0].Name != "Work" {
		t.Fatalf("the vaults came back as %v", back.Vaults)
	}
	entry := back.Vaults[0].Items[0]
	login := entry.Item.GetContent().GetLogin()
	if login.GetPassword() != "hunter2" || login.GetItemUsername() != "jane" {
		t.Errorf("the login came back as %v", login)
	}
	if got := LoginURLs(login); len(got) != 1 || got[0] != "https://github.com" {
		t.Errorf("the URLs came back as %v", got)
	}
}
