package passfile

import (
	"strings"
	"testing"
)

func nordPassFile(t *testing.T) string {
	t.Helper()
	rows := []string{
		"name,url,additional_urls,username,password,note,cardholdername,cardnumber,cvc,pin,expirydate,zipcode,folder,full_name,phone_number,email,address1,address2,city,country,state,type,custom_fields",
		"folder1/folder2,,,,,,,,,,,,,,,,,,,,,folder,",
		`Proton,https://account.proton.me,"[""https://proton.me""]",nobody@proton.me,proton123,a note,,,,,,,folder1/folder2,,,,,,,,,password,"[{""label"":""PIN"",""type"":""hidden"",""value"":""1234""}]"`,
		"Visa,,,,,the card,Jane Doe,4242424242424242,123,9999,03/27,,,,,,,,,,,credit_card,",
		"Jane,https://jane.test,,,,,,,,,,1010,,Jane Doe,555123,jane@example.test,Main Street 1,,Vienna,Austria,Wien,identity,",
		"Router,,,,,the note,,,,,,,,,,,,,,,,note,",
	}
	return written(t, "nordpass.csv", []byte(strings.Join(rows, "\n")+"\n"))
}

func TestANordPassExportIsRead(t *testing.T) {
	doc := opened(t, nordPassFile(t), "nordpass", nil)

	got := kinds(doc)
	for kind, want := range map[string]int{
		"login": 1, "credit-card": 1, "identity": 1, "note": 1,
	} {
		if got[kind] != want {
			t.Errorf("read %d %s items, want %d", got[kind], kind, want)
		}
	}
}

// A folder is a row of its own as well as a column on the items in it, and the
// row itself holds nothing to import.
func TestANordPassFolderRowIsNotAnItem(t *testing.T) {
	doc := opened(t, nordPassFile(t), "nordpass", nil)
	for _, entry := range doc.Vaults[0].Items {
		if entry.Name == "folder1/folder2" {
			t.Error("a folder came back as an item")
		}
	}
	if names := vaultNames(doc); names[0] != "folder2" {
		t.Errorf("the first vault is %q, want the innermost folder", names[0])
	}
}

// A login keeps every address it had, and the custom fields NordPass writes as
// JSON.
func TestANordPassLoginIsRead(t *testing.T) {
	entry := itemNamed(t, opened(t, nordPassFile(t), "nordpass", nil), "Proton")
	login := entry.Item.GetContent().GetLogin()
	if got := LoginURLs(login); len(got) != 2 {
		t.Errorf("the URLs came back as %v", got)
	}
	fields := entry.Item.GetExtraFields()
	if len(fields) != 1 || fields[0].GetFieldName() != "PIN" {
		t.Fatalf("the custom fields came back as %v", fields)
	}
	if fields[0].GetHidden().GetContent() != "1234" {
		t.Errorf("a hidden field came back as %v", fields[0])
	}
}

// NordPass writes the expiry as MM/YY and Pass stores MMYYYY.
func TestANordPassCardIsRead(t *testing.T) {
	card := itemNamed(t, opened(t, nordPassFile(t), "nordpass", nil), "Visa").
		Item.GetContent().GetCreditCard()
	if card.GetExpirationDate() != "032027" {
		t.Errorf("the expiry came back as %q, want 032027", card.GetExpirationDate())
	}
	if card.GetPin() != "9999" || card.GetVerificationNumber() != "123" {
		t.Errorf("the card came back as %v", card)
	}
}

func TestANordPassIdentityIsRead(t *testing.T) {
	idn := itemNamed(t, opened(t, nordPassFile(t), "nordpass", nil), "Jane").
		Item.GetContent().GetIdentity()
	if idn.GetFullName() != "Jane Doe" || idn.GetEmail() != "jane@example.test" {
		t.Errorf("the identity came back as %v", idn)
	}
	if idn.GetCity() != "Vienna" || idn.GetZipOrPostalCode() != "1010" {
		t.Errorf("the address came back as %v", idn)
	}
	if idn.GetWebsite() != "https://jane.test" {
		t.Errorf("the website came back as %q", idn.GetWebsite())
	}
}
