package passfile

import "testing"

const keeperExport = `{
  "shared_folders": [],
  "records": [
    {
      "uid": "1", "title": "Proton", "$type": "login",
      "login": "nobody@proton.me", "password": "proton123",
      "login_url": "https://account.proton.me", "notes": "a note",
      "folders": [{"folder": "Work\\Services"}],
      "custom_fields": {
        "$oneTimeCode::1": "otpauth://totp/proton?secret=JBSWY3DPEHPK3PXP",
        "$text:Ticket:1": "T-1",
        "$secret:Recovery:1": "abc",
        "$phone:Mobile:1": {"region": "AT", "number": "555123", "ext": "9"}
      }
    },
    {
      "uid": "2", "title": "Visa", "$type": "bankCard", "notes": "the card",
      "custom_fields": {
        "$paymentCard::1": {"cardNumber": "4242424242424242", "cardExpirationDate": "03/2027", "cardSecurityCode": "123"},
        "$text:cardholderName:1": "Jane Doe",
        "$pinCode::1": "1234"
      }
    },
    {
      "uid": "3", "title": "Jane", "$type": "contact",
      "custom_fields": {
        "$name::1": {"first": "Jane", "middle": "Q", "last": "Doe"},
        "$text:company:1": "Example GmbH",
        "$email::1": "jane@example.test",
        "$text:Desk:1": "4th floor"
      }
    },
    {
      "uid": "4", "title": "Server", "$type": "sshKeys",
      "custom_fields": {"$keyPair::1": {"publicKey": "ssh-ed25519 AAAA", "privateKey": "PRIVATE"}}
    },
    {
      "uid": "5", "title": "Home", "$type": "wifiCredentials", "password": "wifipass",
      "custom_fields": {"$text:SSID:1": "Home"}
    },
    {"uid": "6", "title": "A note", "$type": "encryptedNotes", "notes": "the note"},
    {"uid": "7", "title": "Passport", "$type": "file", "custom_fields": {"$text:Number:1": "P123"}}
  ]
}`

func keeperExportFile(t *testing.T) string {
	t.Helper()
	return written(t, "keeper.json", []byte(keeperExport))
}

func TestAKeeperExportIsRead(t *testing.T) {
	doc := opened(t, keeperExportFile(t), "keeper", nil)
	got := kinds(doc)
	for kind, want := range map[string]int{
		"login": 1, "credit-card": 1, "identity": 1, "ssh-key": 1,
		"wifi": 1, "note": 1, "custom": 1,
	} {
		if got[kind] != want {
			t.Errorf("read %d %s items, want %d", got[kind], kind, want)
		}
	}
}

// A folder becomes a vault, innermost name first.
func TestAKeeperFolderBecomesAVault(t *testing.T) {
	doc := opened(t, keeperExportFile(t), "keeper", nil)
	var found bool
	for _, name := range vaultNames(doc) {
		found = found || name == "Services"
	}
	if !found {
		t.Errorf("the vaults came back as %v", vaultNames(doc))
	}
}

// The keys Keeper wraps a field's type in are not part of what the field is
// called, and a phone number spread over several keys is put back together.
func TestAKeeperLoginsFieldsAreRead(t *testing.T) {
	entry := itemNamed(t, opened(t, keeperExportFile(t), "keeper", nil), "Proton")
	login := entry.Item.GetContent().GetLogin()
	if login.GetItemEmail() != "nobody@proton.me" {
		t.Errorf("the address came back as %q", login.GetItemEmail())
	}
	if login.GetTotpUri() == "" {
		t.Error("the one-time code did not come back")
	}
	got := map[string]string{}
	for _, f := range entry.Item.GetExtraFields() {
		switch {
		case f.GetHidden() != nil:
			got[f.GetFieldName()] = f.GetHidden().GetContent()
		default:
			got[f.GetFieldName()] = f.GetText().GetContent()
		}
	}
	if got["Ticket"] != "T-1" {
		t.Errorf("a text field came back as %v", got)
	}
	if got["Recovery"] != "abc" {
		t.Errorf("a hidden field came back as %v", got)
	}
	if got["Mobile"] != "AT 555123 9" {
		t.Errorf("the phone number came back as %q", got["Mobile"])
	}
	if _, ok := got["$oneTimeCode::1"]; ok {
		t.Error("the one-time code came back as a custom field as well")
	}
}

func TestAKeeperCardIsRead(t *testing.T) {
	card := itemNamed(t, opened(t, keeperExportFile(t), "keeper", nil), "Visa").
		Item.GetContent().GetCreditCard()
	if card.GetNumber() != "4242424242424242" || card.GetCardholderName() != "Jane Doe" {
		t.Errorf("the card came back as %v", card)
	}
	if card.GetExpirationDate() != "032027" {
		t.Errorf("the expiry came back as %q, want 032027", card.GetExpirationDate())
	}
	if card.GetPin() != "1234" {
		t.Errorf("the PIN came back as %q", card.GetPin())
	}
}

func TestAKeeperContactIsRead(t *testing.T) {
	idn := itemNamed(t, opened(t, keeperExportFile(t), "keeper", nil), "Jane").
		Item.GetContent().GetIdentity()
	if idn.GetFirstName() != "Jane" || idn.GetLastName() != "Doe" || idn.GetMiddleName() != "Q" {
		t.Errorf("the name came back as %v", idn)
	}
	if idn.GetCompany() != "Example GmbH" || idn.GetEmail() != "jane@example.test" {
		t.Errorf("the contact came back as %v", idn)
	}
	if len(idn.GetExtraSections()) != 1 {
		t.Errorf("what Pass has no field for came back as %v", idn.GetExtraSections())
	}
}

func TestAKeeperSSHKeyIsRead(t *testing.T) {
	key := itemNamed(t, opened(t, keeperExportFile(t), "keeper", nil), "Server").
		Item.GetContent().GetSshKey()
	if key.GetPrivateKey() != "PRIVATE" || key.GetPublicKey() != "ssh-ed25519 AAAA" {
		t.Errorf("the keys came back as %v", key)
	}
}

func TestAKeeperNetworkIsRead(t *testing.T) {
	wifi := itemNamed(t, opened(t, keeperExportFile(t), "keeper", nil), "Home").
		Item.GetContent().GetWifi()
	if wifi.GetSsid() != "Home" || wifi.GetPassword() != "wifipass" {
		t.Errorf("the network came back as %v", wifi)
	}
}
