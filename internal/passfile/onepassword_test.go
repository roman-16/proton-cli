package passfile

import (
	"archive/zip"
	"bytes"
	"io"
	"testing"

	pb "github.com/roman-16/proton-cli/internal/service/pass/proto"
)

const onePasswordExport = `{
  "accounts": [{
    "attrs": {"name": "Jane"},
    "vaults": [{
      "attrs": {"name": "Private"},
      "items": [
        {
          "uuid": "i-1", "categoryUuid": "001", "state": "active",
          "createdAt": 1707735320, "updatedAt": 1707735349,
          "overview": {
            "title": "Proton", "url": "https://account.proton.me",
            "urls": [
              {"label": "website", "url": "https://account.proton.me"},
              {"label": "never", "url": "https://proton.me", "mode": "never"}
            ]
          },
          "details": {
            "notesPlain": "a note",
            "loginFields": [
              {"designation": "username", "value": "jane@example.test"},
              {"designation": "password", "value": "proton123"}
            ],
            "sections": [{"title": "Extras", "name": "extras", "fields": [
              {"title": "One-time code", "id": "totp", "value": {"totp": "JBSWY3DPEHPK3PXP"}},
              {"title": "Ticket", "id": "ticket", "value": {"string": "T-1"}},
              {"title": "Recovery", "id": "recovery", "value": {"concealed": "abc"}},
              {"title": "Report", "id": "report", "value": {"file": {"documentId": "d-1", "fileName": "report.pdf"}}}
            ]}]
          }
        },
        {
          "uuid": "i-2", "categoryUuid": "003", "state": "archived",
          "createdAt": 1, "updatedAt": 2,
          "overview": {"title": "A note"}, "details": {"notesPlain": "the note"}
        },
        {
          "uuid": "i-3", "categoryUuid": "002", "state": "active",
          "overview": {"title": "Visa"},
          "details": {"notesPlain": "the card", "sections": [{"title": "", "name": "", "fields": [
            {"title": "Cardholder", "id": "cardholder", "value": {"string": "Jane Doe"}},
            {"title": "Number", "id": "ccnum", "value": {"creditCardNumber": "4242424242424242"}},
            {"title": "CVV", "id": "cvv", "value": {"concealed": "123"}},
            {"title": "Expiry", "id": "expiry", "value": {"monthYear": 202703}},
            {"title": "PIN", "id": "pin", "value": {"concealed": "1234"}}
          ]}]}
        },
        {
          "uuid": "i-4", "categoryUuid": "004", "state": "active",
          "overview": {"title": "Jane"},
          "details": {"sections": [
            {"title": "Personal", "name": "name", "fields": [
              {"title": "First name", "id": "firstname", "value": {"string": "Jane"}},
              {"title": "Last name", "id": "lastname", "value": {"string": "Doe"}}
            ]},
            {"title": "Address", "name": "address", "fields": [
              {"title": "Address", "id": "address", "value": {"address": {"street": "Main Street 1", "city": "Vienna", "zip": "1010", "country": "at"}}}
            ]},
            {"title": "Internet", "name": "internet", "fields": [
              {"title": "Email", "id": "email", "value": {"string": "jane@example.test"}}
            ]}
          ]}
        },
        {
          "uuid": "i-5", "categoryUuid": "114", "state": "active",
          "overview": {"title": "Server"},
          "details": {"sections": [{"title": "", "name": "", "fields": [
            {"title": "Private key", "id": "private_key", "value": {"sshKey": {"privateKey": "PRIVATE", "metadata": {"privateKey": "PRIVATE", "publicKey": "ssh-ed25519 AAAA", "fingerprint": "SHA256:xyz", "keyType": "ed25519"}}}}
          ]}]}
        },
        {
          "uuid": "i-6", "categoryUuid": "109", "state": "active",
          "overview": {"title": "Home"},
          "details": {"sections": [{"title": "", "name": "", "fields": [
            {"title": "Network", "id": "network_name", "value": {"string": "Home"}},
            {"title": "Password", "id": "wireless_password", "value": {"concealed": "wifipass"}},
            {"title": "Security", "id": "wireless_security", "value": {"string": "wpa2p"}}
          ]}]}
        }
      ]
    }]
  }]
}`

// onePasswordArchive is the .1pux 1Password writes: a zip holding the document
// and the files.
func onePasswordArchive(t *testing.T) string {
	t.Helper()
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	for name, body := range map[string]string{
		"export.data":           onePasswordExport,
		"files/d-1__report.pdf": "hello",
		"files/d-9__orphan.pdf": "nobody",
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
	return written(t, "1password.1pux", buf.Bytes())
}

func TestA1PasswordArchiveIsRead(t *testing.T) {
	doc := opened(t, onePasswordArchive(t), "1password", nil)
	if len(doc.Vaults) != 1 || doc.Vaults[0].Name != "Private" {
		t.Fatalf("the vaults came back as %v", vaultNames(doc))
	}
	got := kinds(doc)
	for kind, want := range map[string]int{
		"login": 1, "note": 1, "credit-card": 1, "identity": 1, "ssh-key": 1, "wifi": 1,
	} {
		if got[kind] != want {
			t.Errorf("read %d %s items, want %d", got[kind], kind, want)
		}
	}
}

func TestA1PasswordLoginIsRead(t *testing.T) {
	entry := itemNamed(t, opened(t, onePasswordArchive(t), "1password", nil), "Proton")
	login := entry.Item.GetContent().GetLogin()
	if login.GetItemEmail() != "jane@example.test" || login.GetPassword() != "proton123" {
		t.Errorf("the login came back as %v", login)
	}
	if login.GetTotpUri() == "" {
		t.Error("the one-time code did not come back")
	}
	urls := login.GetAutofillUrls()
	if len(urls) != 2 || urls[1].GetMode() != pb.AutofillUrl_Never {
		t.Errorf("the URLs came back as %v", urls)
	}
	if entry.CreateTime != 1707735320 || entry.ModifyTime != 1707735349 {
		t.Errorf("the dates came back as %d and %d", entry.CreateTime, entry.ModifyTime)
	}
	var names []string
	for _, f := range entry.Item.GetExtraFields() {
		names = append(names, f.GetFieldName())
	}
	if len(names) != 2 {
		t.Errorf("the custom fields came back as %v", names)
	}
}

// An item 1Password put away arrives in the trash.
func TestAnArchived1PasswordItemIsTrashed(t *testing.T) {
	entry := itemNamed(t, opened(t, onePasswordArchive(t), "1password", nil), "A note")
	if !entry.Trashed {
		t.Error("an archived item came back active")
	}
}

// 1Password writes the expiry as YYYYMM and Pass stores MMYYYY.
func TestA1PasswordCardIsRead(t *testing.T) {
	card := itemNamed(t, opened(t, onePasswordArchive(t), "1password", nil), "Visa").
		Item.GetContent().GetCreditCard()
	if card.GetNumber() != "4242424242424242" || card.GetCardholderName() != "Jane Doe" {
		t.Errorf("the card came back as %v", card)
	}
	if card.GetExpirationDate() != "032027" {
		t.Errorf("the expiry came back as %q, want 032027", card.GetExpirationDate())
	}
	if card.GetPin() != "1234" || card.GetVerificationNumber() != "123" {
		t.Errorf("the card came back as %v", card)
	}
}

// An address is one field holding several, and each part lands where Pass keeps
// it.
func TestA1PasswordIdentityIsRead(t *testing.T) {
	idn := itemNamed(t, opened(t, onePasswordArchive(t), "1password", nil), "Jane").
		Item.GetContent().GetIdentity()
	if idn.GetFirstName() != "Jane" || idn.GetLastName() != "Doe" {
		t.Errorf("the name came back as %v", idn)
	}
	if idn.GetStreetAddress() != "Main Street 1" || idn.GetCity() != "Vienna" {
		t.Errorf("the address came back as %v", idn)
	}
	if idn.GetZipOrPostalCode() != "1010" || idn.GetCountryOrRegion() != "at" {
		t.Errorf("the address came back as %v", idn)
	}
	if idn.GetEmail() != "jane@example.test" {
		t.Errorf("the address came back as %q", idn.GetEmail())
	}
}

func TestA1PasswordSSHKeyIsRead(t *testing.T) {
	key := itemNamed(t, opened(t, onePasswordArchive(t), "1password", nil), "Server").
		Item.GetContent().GetSshKey()
	if key.GetPrivateKey() != "PRIVATE" || key.GetPublicKey() != "ssh-ed25519 AAAA" {
		t.Errorf("the keys came back as %v", key)
	}
	if len(key.GetSections()) != 1 || key.GetSections()[0].GetSectionName() != "OpenSSH" {
		t.Errorf("what else the key carries came back as %v", key.GetSections())
	}
}

func TestA1PasswordNetworkIsRead(t *testing.T) {
	wifi := itemNamed(t, opened(t, onePasswordArchive(t), "1password", nil), "Home").
		Item.GetContent().GetWifi()
	if wifi.GetSsid() != "Home" || wifi.GetPassword() != "wifipass" {
		t.Errorf("the network came back as %v", wifi)
	}
	if wifi.GetSecurity() != pb.WifiSecurity_WPA2 {
		t.Errorf("the security came back as %v", wifi.GetSecurity())
	}
}

// A file travels under the item's own reference to it, and comes back under the
// name a person gave it.
func TestA1PasswordAttachmentIsRead(t *testing.T) {
	entry := itemNamed(t, opened(t, onePasswordArchive(t), "1password", nil), "Proton")
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

// The older export is one item to a line, with lines of asterisks between them.
func TestA1PasswordLegacyExportIsRead(t *testing.T) {
	body := `{"uuid":"u-1","typeName":"webforms.WebForm","title":"Proton","createdAt":1707735320,"updatedAt":1707735349,` +
		`"secureContents":{"notesPlain":"a note","fields":[{"designation":"username","value":"jane@example.test"},` +
		`{"designation":"password","value":"proton123"}],"URLs":[{"url":"https://account.proton.me"}],` +
		`"sections":[{"title":"Extras","name":"extras","fields":[{"k":"concealed","n":"TOTP_x","t":"One-time code","v":"JBSWY3DPEHPK3PXP"},` +
		`{"k":"string","n":"ticket","t":"Ticket","v":"T-1"}]}]}}` + "\n" +
		"***5642bee8-a5ff-11dc-8314-0800200c9a66***\n" +
		`{"uuid":"u-2","typeName":"securenotes.SecureNote","title":"A note","secureContents":{"notesPlain":"the note"}}` + "\n" +
		"***5642bee8-a5ff-11dc-8314-0800200c9a66***\n" +
		`{"uuid":"u-3","typeName":"wallet.financial.CreditCard","title":"Visa","secureContents":{"cardholder":"Jane Doe","ccnum":"4242424242424242","cvv":"123","pin":"1234","expiry_mm":3,"expiry_yy":2027}}` + "\n"
	doc := opened(t, written(t, "1password.1pif", []byte(body)), "1password", nil)

	got := kinds(doc)
	if got["login"] != 1 || got["note"] != 1 || got["credit-card"] != 1 {
		t.Errorf("read %v", got)
	}
	login := itemNamed(t, doc, "Proton").Item.GetContent().GetLogin()
	if login.GetItemEmail() != "jane@example.test" || login.GetPassword() != "proton123" {
		t.Errorf("the login came back as %v", login)
	}
	if login.GetTotpUri() == "" {
		t.Error("the one-time code did not come back")
	}
	card := itemNamed(t, doc, "Visa").Item.GetContent().GetCreditCard()
	if card.GetExpirationDate() != "032027" {
		t.Errorf("the expiry came back as %q, want 032027", card.GetExpirationDate())
	}
}

func TestSomethingThatIsNotA1PasswordExportIsRefused(t *testing.T) {
	path := written(t, "other.json", []byte(`{"items":[]}`))
	if _, err := Open(path, "1password", nil); err == nil {
		t.Error("another JSON file was read as a 1Password export")
	}
}
