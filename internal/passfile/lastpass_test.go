package passfile

import (
	"strings"
	"testing"
)

// lastPassFile is an export with one row of each kind LastPass writes.
func lastPassFile(t *testing.T) string {
	t.Helper()
	rows := []string{
		"url,username,password,totp,extra,name,grouping,fav",
		"https://account.proton.me,nobody@proton.me,proton123,JBSWY3DPEHPK3PXP,a note,Proton,,0",
		`https://example.test,jane,pw,,,Example,company\services,0`,
		"http://sn,,,,This is a secure note,Secure note,,0",
		`http://sn,,,,"NoteType:Credit Card` + "\n" +
			`Name on Card:Jane Doe` + "\n" +
			`Number:4242424242424242` + "\n" +
			`Security Code:123` + "\n" +
			`Expiration Date:January,2027` + "\n" +
			`Notes:the card",Visa,,0`,
		`http://sn,,,,"NoteType:Address` + "\n" +
			`Language:en-GB` + "\n" +
			`First Name:Jane` + "\n" +
			`Last Name:Doe` + "\n" +
			`Birthday:December,,1990` + "\n" +
			`Phone:{""num"":""555123"",""ext"":""99""}` + "\n" +
			`Email Address:jane@example.test",Jane,,0`,
		`http://sn,,,,"NoteType:Wi-Fi Password` + "\n" +
			`SSID:Home` + "\n" +
			`Password:wifipass` + "\n" +
			`Notes:upstairs",Router,,0`,
		`http://sn,,,,"NoteType:Bank Account` + "\n" +
			`Bank Name:Example Bank` + "\n" +
			`Account Number:123",Bank,,0`,
	}
	return written(t, "lastpass.csv", []byte(strings.Join(rows, "\n")+"\n"))
}

func TestALastPassExportIsRead(t *testing.T) {
	doc := opened(t, lastPassFile(t), "lastpass", nil)

	got := kinds(doc)
	for kind, want := range map[string]int{
		"login": 2, "note": 1, "credit-card": 1, "identity": 1, "wifi": 1, "custom": 1,
	} {
		if got[kind] != want {
			t.Errorf("read %d %s items, want %d", got[kind], kind, want)
		}
	}
}

// A folder becomes a vault, and the path LastPass writes keeps only its
// innermost name.
func TestALastPassFolderBecomesAVault(t *testing.T) {
	doc := opened(t, lastPassFile(t), "lastpass", nil)
	var found bool
	for _, v := range doc.Vaults {
		if v.Name == "services" {
			found = true
		}
	}
	if !found {
		t.Errorf("the vaults came back as %v", vaultNames(doc))
	}
}

// A card's fields are lines of its note, and the date it expires is a month
// written out in words.
func TestALastPassCardIsRead(t *testing.T) {
	card := itemNamed(t, opened(t, lastPassFile(t), "lastpass", nil), "Visa")
	content := card.Item.GetContent().GetCreditCard()
	if content.GetCardholderName() != "Jane Doe" || content.GetNumber() != "4242424242424242" {
		t.Errorf("the card came back as %v", content)
	}
	if content.GetExpirationDate() != "012027" {
		t.Errorf("the expiry came back as %q, want 012027", content.GetExpirationDate())
	}
	if got := card.Item.GetMetadata().GetNote(); got != "the card" {
		t.Errorf("the note came back as %q", got)
	}
}

// An address book entry keeps the fields Pass has room for, and the phone number
// LastPass writes as JSON is put back together.
func TestALastPassAddressIsRead(t *testing.T) {
	idn := itemNamed(t, opened(t, lastPassFile(t), "lastpass", nil), "Jane").
		Item.GetContent().GetIdentity()
	if idn.GetFirstName() != "Jane" || idn.GetLastName() != "Doe" {
		t.Errorf("the name came back as %v", idn)
	}
	if idn.GetPhoneNumber() != "55512399" {
		t.Errorf("the phone number came back as %q", idn.GetPhoneNumber())
	}
	if idn.GetBirthdate() != "December, 1990" {
		t.Errorf("the birthday came back as %q", idn.GetBirthdate())
	}
	if idn.GetEmail() != "jane@example.test" {
		t.Errorf("the address came back as %q", idn.GetEmail())
	}
}

// A kind Pass has no item for keeps its fields under a heading of its own.
func TestALastPassNoteOfAnotherKindKeepsItsFields(t *testing.T) {
	custom := itemNamed(t, opened(t, lastPassFile(t), "lastpass", nil), "Bank")
	sections := custom.Item.GetContent().GetCustom().GetSections()
	if len(sections) != 1 || sections[0].GetSectionName() != "Bank Account" {
		t.Fatalf("the sections came back as %v", sections)
	}
	if len(sections[0].GetSectionFields()) != 2 {
		t.Errorf("the fields came back as %v", sections[0].GetSectionFields())
	}
}

// vaultNames is what a document calls its vaults.
func vaultNames(doc *Document) []string {
	out := make([]string, 0, len(doc.Vaults))
	for _, v := range doc.Vaults {
		out = append(out, v.Name)
	}
	return out
}

// itemNamed finds one item by name, and fails the test when it is not there.
func itemNamed(t *testing.T, doc *Document, name string) Entry {
	t.Helper()
	for _, v := range doc.Vaults {
		for _, entry := range v.Items {
			if entry.Name == name {
				return entry
			}
		}
	}
	t.Fatalf("no item called %q came back", name)
	return Entry{}
}
