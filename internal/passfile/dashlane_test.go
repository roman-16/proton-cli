package passfile

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"
)

var dashlaneFiles = map[string]string{
	"credentials.csv": "username,username2,username3,title,password,note,url,category,otpSecret,otpUrl\n" +
		"jane@example.test,jane2,,Proton,proton123,a note,https://account.proton.me,Work,,otpauth://totp/x?secret=JBSWY3DPEHPK3PXP\n",
	"securenotes.csv": "title,note\nRouter,the note\n",
	"payments.csv": "type,account_name,account_holder,cc_number,code,expiration_month,expiration_year,routing_number,account_number,country,issuing_bank,note,name\n" +
		"credit_card,Jane Doe,Jane Doe,4242424242424242,123,3,2027,,,AT,Example Bank,the card,Visa\n",
	"ids.csv": "type,number,name,issue_date,expiration_date,place_of_issue,state\n" +
		"passport,P123,Jane Doe,2020-01-01,2030-01-01,Vienna,\n",
	"personalInfo.csv": "type,address,address_apartment,address_building,address_door_code,address_floor,address_recipient,city,country,date_of_birth,email,email_type,first_name,item_name,job_title,last_name,login,middle_name,phone_number,place_of_birth,state,title,url,zip\n" +
		"name,,,,,,,,,1990-12-01,jane@example.test,personal,Jane,,Engineer,Doe,janedoe,Q,555123,,,Ms,https://jane.test,1010\n",
}

// dashlaneZip is the archive Dashlane exports, one CSV per kind of item.
func dashlaneZip(t *testing.T) string {
	t.Helper()
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	for name, body := range dashlaneFiles {
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
	return written(t, "dashlane.zip", buf.Bytes())
}

func TestADashlaneArchiveIsRead(t *testing.T) {
	doc := opened(t, dashlaneZip(t), "dashlane", nil)
	got := kinds(doc)
	if got["login"] != 1 || got["note"] != 1 || got["credit-card"] != 1 || got["identity"] != 2 {
		t.Errorf("read %v", got)
	}
}

// A single CSV says nowhere what is in it, so the columns decide.
func TestADashlaneCSVIsReadByItsColumns(t *testing.T) {
	for name, body := range dashlaneFiles {
		path := written(t, name, []byte(body))
		doc, err := Open(path, "dashlane", nil)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if doc.Count() != 1 {
			t.Errorf("%s read %d items, want 1", name, doc.Count())
		}
	}
}

func TestADashlaneLoginIsRead(t *testing.T) {
	entry := itemNamed(t, opened(t, dashlaneZip(t), "dashlane", nil), "Proton")
	login := entry.Item.GetContent().GetLogin()
	if login.GetItemEmail() != "jane@example.test" {
		t.Errorf("the address came back as %q", login.GetItemEmail())
	}
	if login.GetTotpUri() == "" {
		t.Error("the one-time code did not come back")
	}
	// The second and third names a site accepts are kept beside the first.
	fields := entry.Item.GetExtraFields()
	if len(fields) != 1 || fields[0].GetText().GetContent() != "jane2" {
		t.Errorf("the other user names came back as %v", fields)
	}
}

func TestADashlaneCardIsRead(t *testing.T) {
	card := itemNamed(t, opened(t, dashlaneZip(t), "dashlane", nil), "Visa").
		Item.GetContent().GetCreditCard()
	if card.GetNumber() != "4242424242424242" || card.GetCardholderName() != "Jane Doe" {
		t.Errorf("the card came back as %v", card)
	}
	if card.GetExpirationDate() != "032027" {
		t.Errorf("the expiry came back as %q, want 032027", card.GetExpirationDate())
	}
}

// A document row has no title of its own, so it is named after what it is.
func TestADashlaneDocumentIsNamedAfterItsKind(t *testing.T) {
	doc := opened(t, dashlaneZip(t), "dashlane", nil)
	passport := itemNamed(t, doc, "Passport (Jane Doe)")
	idn := passport.Item.GetContent().GetIdentity()
	if idn.GetPassportNumber() != "P123" {
		t.Errorf("the passport number came back as %q", idn.GetPassportNumber())
	}
}

func TestADashlanePersonalInfoIsRead(t *testing.T) {
	doc := opened(t, dashlaneZip(t), "dashlane", nil)
	jane := itemNamed(t, doc, "Jane Q Doe")
	idn := jane.Item.GetContent().GetIdentity()
	if idn.GetEmail() != "jane@example.test" || idn.GetJobTitle() != "Engineer" {
		t.Errorf("the identity came back as %v", idn)
	}
	if idn.GetXHandle() != "janedoe" {
		t.Errorf("the login name came back as %q", idn.GetXHandle())
	}
}

func TestSomethingThatIsNotADashlaneExportIsRefused(t *testing.T) {
	path := written(t, "other.csv", []byte("a,b\n1,2\n"))
	_, err := Open(path, "dashlane", nil)
	if err == nil {
		t.Fatal("a file of two columns was read as a Dashlane export")
	}
	if !strings.Contains(err.Error(), "Dashlane") {
		t.Errorf("the refusal does not name Dashlane: %v", err)
	}
}
