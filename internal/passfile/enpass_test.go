package passfile

import (
	"encoding/base64"
	"io"
	"testing"
)

const enpassExport = `{
  "folders": [],
  "items": [
    {
      "category": "login", "title": "Proton", "note": "a note",
      "archived": 0, "trashed": 0, "createdAt": 1707735320, "updated_at": 1707735349,
      "attachments": [{"name": "passport.pdf", "kind": "application/pdf", "data": "aGVsbG8="}],
      "fields": [
        {"label": "Username", "type": "username", "uid": 1, "sensitive": 0, "value": "jane"},
        {"label": "Email", "type": "email", "uid": 2, "sensitive": 0, "value": "jane@example.test"},
        {"label": "Password", "type": "password", "uid": 3, "sensitive": 1, "value": "proton123"},
        {"label": "Website", "type": "url", "uid": 4, "sensitive": 0, "value": "https://account.proton.me"},
        {"label": "One-time code", "type": "totp", "uid": 5, "sensitive": 1, "value": "JBSWY3DPEHPK3PXP"},
        {"label": "Section", "type": "section", "uid": 6, "sensitive": 0, "value": "heading"},
        {"label": "Ticket", "type": "text", "uid": 7, "sensitive": 0, "value": "T-1"}
      ]
    },
    {
      "category": "note", "title": "A note", "note": "the note",
      "archived": 1, "trashed": 0, "createdAt": 1, "updated_at": 2
    },
    {
      "category": "creditcard", "title": "Visa", "note": "the card",
      "archived": 0, "trashed": 0,
      "fields": [
        {"label": "Cardholder", "type": "ccName", "uid": 10, "sensitive": 0, "value": "Jane Doe"},
        {"label": "Number", "type": "ccNumber", "uid": 11, "sensitive": 1, "value": "4242424242424242"},
        {"label": "CVC", "type": "ccCvc", "uid": 12, "sensitive": 1, "value": "123"},
        {"label": "Expiry", "type": "ccExpiry", "uid": 13, "sensitive": 0, "value": "032027"}
      ]
    },
    {
      "category": "identity", "title": "Jane", "archived": 0, "trashed": 0,
      "fields": [
        {"label": "First name", "type": "text", "uid": 130, "sensitive": 0, "value": "Jane"},
        {"label": "Last name", "type": "text", "uid": 132, "sensitive": 0, "value": "Doe"},
        {"label": "Email", "type": "email", "uid": 165, "sensitive": 0, "value": "jane@example.test"},
        {"label": "Nickname", "type": "text", "uid": 999, "sensitive": 0, "value": "JD"},
        {"label": "Signature", "type": "text", "uid": 998, "sensitive": 0, "value": ""}
      ]
    },
    {
      "category": "license", "title": "Windows", "archived": 0, "trashed": 0,
      "fields": [{"label": "Key", "type": "text", "uid": 20, "sensitive": 1, "value": "AAAA"}]
    }
  ]
}`

func enpassExportFile(t *testing.T) string {
	t.Helper()
	return written(t, "enpass.json", []byte(enpassExport))
}

func TestAnEnpassExportIsRead(t *testing.T) {
	doc := opened(t, enpassExportFile(t), "enpass", nil)
	got := kinds(doc)
	for kind, want := range map[string]int{
		"login": 1, "note": 1, "credit-card": 1, "identity": 1, "custom": 1,
	} {
		if got[kind] != want {
			t.Errorf("read %d %s items, want %d", got[kind], kind, want)
		}
	}
}

// Enpass keeps every value in a typed field, and the fields a kind of item has
// of its own are taken out of the list rather than left as custom fields.
func TestAnEnpassLoginIsRead(t *testing.T) {
	entry := itemNamed(t, opened(t, enpassExportFile(t), "enpass", nil), "Proton")
	login := entry.Item.GetContent().GetLogin()
	if login.GetItemEmail() != "jane@example.test" || login.GetItemUsername() != "jane" {
		t.Errorf("the login came back as %v", login)
	}
	if login.GetPassword() != "proton123" || login.GetTotpUri() == "" {
		t.Errorf("the secrets came back as %v", login)
	}
	if got := LoginURLs(login); len(got) != 1 {
		t.Errorf("the URLs came back as %v", got)
	}
	if len(entry.Item.GetExtraFields()) != 1 {
		t.Fatalf("the custom fields came back as %v", entry.Item.GetExtraFields())
	}
	if entry.Item.GetExtraFields()[0].GetFieldName() != "Ticket" {
		t.Errorf("a heading came back as a field: %v", entry.Item.GetExtraFields())
	}
	if entry.CreateTime != 1707735320 || entry.ModifyTime != 1707735349 {
		t.Errorf("the dates came back as %d and %d", entry.CreateTime, entry.ModifyTime)
	}
}

// An item Enpass put away arrives in the trash.
func TestAnArchivedEnpassItemIsTrashed(t *testing.T) {
	entry := itemNamed(t, opened(t, enpassExportFile(t), "enpass", nil), "A note")
	if !entry.Trashed {
		t.Error("an archived item came back active")
	}
}

// The attachments travel inside the file itself.
func TestAnEnpassAttachmentIsRead(t *testing.T) {
	entry := itemNamed(t, opened(t, enpassExportFile(t), "enpass", nil), "Proton")
	if len(entry.Files) != 1 {
		t.Fatalf("the item came back with %d files", len(entry.Files))
	}
	file := entry.Files[0]
	if file.Name != "passport.pdf" || file.Size != 5 {
		t.Errorf("the file came back as %q (%d bytes)", file.Name, file.Size)
	}
	r, err := file.Open()
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = r.Close() }()
	body, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	want, _ := base64.StdEncoding.DecodeString("aGVsbG8=")
	if string(body) != string(want) {
		t.Errorf("the file holds %q", body)
	}
}

// An identity keeps the fields Pass has room for, and the rest go under a
// heading. A field Enpass left empty is not one anybody stored.
func TestAnEnpassIdentityIsRead(t *testing.T) {
	idn := itemNamed(t, opened(t, enpassExportFile(t), "enpass", nil), "Jane").
		Item.GetContent().GetIdentity()
	if idn.GetFirstName() != "Jane" || idn.GetEmail() != "jane@example.test" {
		t.Errorf("the identity came back as %v", idn)
	}
	if len(idn.GetExtraSections()) != 1 {
		t.Fatalf("the extra fields came back as %v", idn.GetExtraSections())
	}
	fields := idn.GetExtraSections()[0].GetSectionFields()
	if len(fields) != 1 || fields[0].GetFieldName() != "Nickname" {
		t.Errorf("the extra fields came back as %v", fields)
	}
}
