package passfile

import (
	"archive/zip"
	"bytes"
	"io"
	"strings"
	"testing"

	pb "github.com/roman-16/proton-cli/internal/service/pass/proto"
)

const bitwardenExport = `{
  "encrypted": false,
  "folders": [{"id": "f-1", "name": "Work"}],
  "items": [
    {
      "id": "i-1", "type": 1, "name": "Proton", "notes": "a note", "folderId": "f-1",
      "fields": [
        {"name": "Ticket", "type": 0, "value": "T-1"},
        {"name": "Recovery", "type": 1, "value": "abc"},
        {"name": "Renews", "type": 2, "value": "true"}
      ],
      "login": {
        "username": "jane@example.test", "password": "proton123",
        "totp": "JBSWY3DPEHPK3PXP",
        "uris": [
          {"uri": "https://account.proton.me", "match": null},
          {"uri": "https://proton.me", "match": 5},
          {"uri": "androidapp://me.proton.android.pass", "match": null}
        ]
      }
    },
    {"id": "i-2", "type": 2, "name": "A note", "notes": "the note", "folderId": null},
    {
      "id": "i-3", "type": 3, "name": "Visa", "notes": "the card", "folderId": null,
      "card": {"cardholderName": "Jane Doe", "number": "4242424242424242", "code": "123", "expMonth": "3", "expYear": "2027"}
    },
    {
      "id": "i-4", "type": 4, "name": "Jane", "folderId": null,
      "fields": [{"name": "Nickname", "type": 0, "value": "JD"}],
      "identity": {
        "firstName": "Jane", "lastName": "Doe", "address1": "Main Street 1", "address2": "Floor 4",
        "city": "Vienna", "postalCode": "1010", "country": "AT",
        "email": "jane@example.test", "phone": "555123", "ssn": "1234", "username": "janedoe"
      }
    },
    {
      "id": "i-5", "type": 5, "name": "Server", "folderId": null,
      "sshKey": {"privateKey": "PRIVATE", "publicKey": "ssh-ed25519 AAAA", "keyFingerprint": "SHA256:xyz"}
    }
  ]
}`

func bitwardenExportFile(t *testing.T) string {
	t.Helper()
	return written(t, "bitwarden.json", []byte(bitwardenExport))
}

func TestABitwardenExportIsRead(t *testing.T) {
	doc := opened(t, bitwardenExportFile(t), "bitwarden", nil)
	got := kinds(doc)
	for kind, want := range map[string]int{
		"login": 1, "note": 1, "credit-card": 1, "identity": 1, "ssh-key": 1,
	} {
		if got[kind] != want {
			t.Errorf("read %d %s items, want %d", got[kind], kind, want)
		}
	}
}

// A folder becomes a vault, and an item in none lands wherever the import is
// pointed.
func TestABitwardenFolderBecomesAVault(t *testing.T) {
	doc := opened(t, bitwardenExportFile(t), "bitwarden", nil)
	if len(doc.Vaults) != 2 {
		t.Fatalf("read %d vaults, want 2", len(doc.Vaults))
	}
	var work, unfiled bool
	for _, v := range doc.Vaults {
		work = work || v.Name == "Work"
		unfiled = unfiled || v.Name == ""
	}
	if !work || !unfiled {
		t.Errorf("the vaults came back as %v", vaultNames(doc))
	}
}

// An organisation's export files items in collections and leaves every folder
// empty.
func TestABitwardenOrganisationExportFollowsItsCollections(t *testing.T) {
	body := `{"encrypted":false,"folders":[],"collections":[{"id":"c-1","name":"Engineering"}],
		"items":[{"id":"i-1","type":2,"name":"A note","folderId":null,"collectionIds":["c-1"]}]}`
	doc := opened(t, written(t, "org.json", []byte(body)), "bitwarden", nil)
	if len(doc.Vaults) != 1 || doc.Vaults[0].Name != "Engineering" {
		t.Errorf("the vaults came back as %v", vaultNames(doc))
	}
}

// An address Bitwarden fills under a rule of its own keeps that rule, and one
// that is an app rather than a site is not an address at all.
func TestABitwardenLoginIsRead(t *testing.T) {
	entry := itemNamed(t, opened(t, bitwardenExportFile(t), "bitwarden", nil), "Proton")
	login := entry.Item.GetContent().GetLogin()
	if login.GetItemEmail() != "jane@example.test" || login.GetPassword() != "proton123" {
		t.Errorf("the login came back as %v", login)
	}
	if login.GetTotpUri() == "" {
		t.Error("the one-time code did not come back")
	}
	urls := login.GetAutofillUrls()
	if len(urls) != 2 {
		t.Fatalf("the URLs came back as %v", urls)
	}
	if urls[0].GetMode() != pb.AutofillUrl_Default || urls[1].GetMode() != pb.AutofillUrl_Never {
		t.Errorf("the autofill rules came back as %v", urls)
	}
	// Only the ones that fill anywhere are repeated for an older client.
	if len(login.GetUrls()) != 1 {
		t.Errorf("the plain URLs came back as %v", login.GetUrls())
	}
	var app bool
	for _, f := range entry.Item.GetExtraFields() {
		app = app || f.GetFieldName() == "Android app"
	}
	if !app {
		t.Error("the app the login is for did not come back")
	}
}

func TestABitwardenCustomFieldKeepsItsKind(t *testing.T) {
	entry := itemNamed(t, opened(t, bitwardenExportFile(t), "bitwarden", nil), "Proton")
	hidden := map[string]bool{}
	for _, f := range entry.Item.GetExtraFields() {
		hidden[f.GetFieldName()] = f.GetHidden() != nil
	}
	if hidden["Ticket"] || !hidden["Recovery"] {
		t.Errorf("the fields came back as %v", hidden)
	}
	if _, ok := hidden["Renews"]; !ok {
		t.Error("a checkbox did not come back")
	}
}

// Bitwarden keeps three lines of a street address and Pass keeps one.
func TestABitwardenIdentityJoinsItsAddressLines(t *testing.T) {
	idn := itemNamed(t, opened(t, bitwardenExportFile(t), "bitwarden", nil), "Jane").
		Item.GetContent().GetIdentity()
	if idn.GetStreetAddress() != "Main Street 1 Floor 4" {
		t.Errorf("the address came back as %q", idn.GetStreetAddress())
	}
	if idn.GetSocialSecurityNumber() != "1234" || idn.GetXHandle() != "janedoe" {
		t.Errorf("the identity came back as %v", idn)
	}
	if len(idn.GetExtraSections()) != 1 {
		t.Errorf("the custom fields came back as %v", idn.GetExtraSections())
	}
}

func TestABitwardenSSHKeyIsRead(t *testing.T) {
	key := itemNamed(t, opened(t, bitwardenExportFile(t), "bitwarden", nil), "Server").
		Item.GetContent().GetSshKey()
	if key.GetPrivateKey() != "PRIVATE" {
		t.Errorf("the private key came back as %q", key.GetPrivateKey())
	}
	if len(key.GetSections()) != 1 {
		t.Errorf("the fingerprint came back as %v", key.GetSections())
	}
}

// The archive holds the items and a folder of attachments per item.
func TestABitwardenArchiveCarriesTheAttachments(t *testing.T) {
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	for name, body := range map[string]string{
		"data.json":                   bitwardenExport,
		"attachments/i-1/report.pdf":  "hello",
		"attachments/i-9/orphan.pdf":  "nobody",
		"attachments/i-1/deep/no.pdf": "too deep",
	} {
		w, err := z.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	doc := opened(t, written(t, "bitwarden.zip", buf.Bytes()), "bitwarden", nil)

	entry := itemNamed(t, doc, "Proton")
	if len(entry.Files) != 1 {
		t.Fatalf("the item came back with %d files", len(entry.Files))
	}
	if entry.Files[0].Name != "report.pdf" {
		t.Errorf("the file came back as %q", entry.Files[0].Name)
	}
	r, err := entry.Files[0].Open()
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = r.Close() }()
	body, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(body) != "hello" {
		t.Errorf("the file holds %q", body)
	}
}

// An export nothing can read without the password it was locked with says so.
func TestAnEncryptedBitwardenExportIsRefused(t *testing.T) {
	path := written(t, "locked.json", []byte(`{"encrypted":true,"items":[]}`))
	_, err := Open(path, "bitwarden", nil)
	if err == nil {
		t.Fatal("an encrypted export was read")
	}
	if !strings.Contains(err.Error(), "encrypted") {
		t.Errorf("the refusal does not say it is encrypted: %v", err)
	}
}
