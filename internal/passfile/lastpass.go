package passfile

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	pb "github.com/roman-16/proton-cli/internal/service/pass/proto"
)

// LastPass writes one CSV for everything it holds.
//
// A row whose address is http://sn is not a login at all: it is one of the other
// kinds, and which one is the first line of its note. The rest of that note is
// the item's fields, one "Label:value" to a line.

// lastPassSecureNote is the address LastPass gives a row that is not a login.
const lastPassSecureNote = "http://sn"

// lastPassIdentityFields are the labels LastPass writes an address book entry
// under.
var lastPassIdentityFields = map[string]func(*pb.ItemIdentity) *string{
	"Address 1":         func(i *pb.ItemIdentity) *string { return &i.StreetAddress },
	"Birthday":          func(i *pb.ItemIdentity) *string { return &i.Birthdate },
	"City / Town":       func(i *pb.ItemIdentity) *string { return &i.City },
	"Company":           func(i *pb.ItemIdentity) *string { return &i.Company },
	"Country":           func(i *pb.ItemIdentity) *string { return &i.CountryOrRegion },
	"County":            func(i *pb.ItemIdentity) *string { return &i.County },
	"Email Address":     func(i *pb.ItemIdentity) *string { return &i.Email },
	"First Name":        func(i *pb.ItemIdentity) *string { return &i.FirstName },
	"Gender":            func(i *pb.ItemIdentity) *string { return &i.Gender },
	"Last Name":         func(i *pb.ItemIdentity) *string { return &i.LastName },
	"Middle Name":       func(i *pb.ItemIdentity) *string { return &i.MiddleName },
	"Phone":             func(i *pb.ItemIdentity) *string { return &i.PhoneNumber },
	"State":             func(i *pb.ItemIdentity) *string { return &i.StateOrProvince },
	"Zip / Postal Code": func(i *pb.ItemIdentity) *string { return &i.ZipOrPostalCode },
}

func readLastPass(in source) (*Document, error) {
	t, err := openCSV(in, "lastpass", "url", "username", "password", "extra", "name", "grouping")
	if err != nil {
		return nil, err
	}
	doc := flatFile(t)
	vaults := newGrouped()
	for _, r := range t.rows {
		vaults.add(lastPassVault(r.get("grouping")), lastPassEntry(r))
	}
	vaults.into(doc)
	return doc, nil
}

// lastPassVault is the folder an item sits in. LastPass writes a whole path and
// Pass has no folders inside vaults, so the innermost one names the vault.
func lastPassVault(grouping string) string {
	parts := strings.Split(grouping, `\`)
	return parts[len(parts)-1]
}

func lastPassEntry(r row) Entry {
	note := r.get("extra")
	in := item{Name: r.get("name"), Note: note}
	if r.get("url") != lastPassSecureNote {
		in.Email, in.Username = identifier(r.get("username"))
		in.Password, in.TOTP = r.get("password"), r.get("totp")
		in.URLs = []string{r.get("url")}
		return in.login()
	}

	kind := lastPassField(note, "NoteType")
	if kind == "" {
		return in.note()
	}
	in.Note = lastPassField(note, "Notes")
	switch kind {
	case "Credit Card":
		in.Holder = lastPassField(note, "Name on Card")
		in.Number = lastPassField(note, "Number")
		in.CVV = lastPassField(note, "Security Code")
		in.Expiry = lastPassExpiry(lastPassField(note, "Expiration Date"))
		return in.card()
	case "Address":
		in.Identity = lastPassIdentity(note)
		return in.identity()
	case "SSH Key":
		in.PrivateKey = lastPassField(note, "Private Key")
		in.PublicKey = lastPassField(note, "Public Key")
		in.Sections = section(kind, lastPassFields(note, "Notes", "Private Key", "Public Key"), in.Name)
		return in.sshKey()
	case "Wi-Fi Password":
		in.SSID = lastPassField(note, "SSID")
		in.Password = lastPassField(note, "Password")
		in.Sections = section(kind, lastPassFields(note, "Notes", "SSID", "Password"), in.Name)
		return in.wifi()
	default:
		in.Sections = section(kind, lastPassFields(note, "Notes"), in.Name)
		return in.custom()
	}
}

// lastPassField reads one "Label:value" line out of a note.
func lastPassField(note, label string) string {
	for _, line := range strings.Split(note, "\n") {
		if name, value, found := strings.Cut(line, ":"); found && name == label {
			return value
		}
	}
	return ""
}

// lastPassFields is everything under the first line of a note, which is what an
// item of that kind carries beyond the fields Pass has of its own.
func lastPassFields(note string, except ...string) []Field {
	lines := strings.Split(note, "\n")
	if len(lines) < 2 {
		return nil
	}
	var out []Field
	for _, line := range lines[1:] {
		name, value, found := strings.Cut(line, ":")
		// Language is which spelling of the form LastPass showed, not something
		// anybody stored.
		if !found || name == "Language" || slices.Contains(except, name) {
			continue
		}
		if name == "" {
			name = "Text"
		}
		out = append(out, Field{
			Name: name, Value: value,
			Hidden: strings.Contains(strings.ToLower(name), "password"),
		})
	}
	return out
}

func lastPassIdentity(note string) *pb.ItemIdentity {
	idn := &pb.ItemIdentity{}
	for _, f := range lastPassFields(note) {
		at, ok := lastPassIdentityFields[f.Name]
		if !ok || f.Value == "" {
			continue
		}
		switch f.Name {
		case "Birthday":
			*at(idn) = lastPassBirthday(f.Value)
		case "Phone":
			*at(idn) = lastPassPhone(f.Value)
		default:
			*at(idn) = f.Value
		}
	}
	return idn
}

// lastPassExpiry turns "January, 2025" into the MMYYYY Pass stores.
func lastPassExpiry(value string) string {
	month, year, found := strings.Cut(value, ",")
	if !found {
		return ""
	}
	months := []string{
		"January", "February", "March", "April", "May", "June",
		"July", "August", "September", "October", "November", "December",
	}
	for i, name := range months {
		if strings.EqualFold(strings.TrimSpace(month), name) {
			return fmt.Sprintf("%02d%s", i+1, strings.TrimSpace(year))
		}
	}
	return ""
}

// lastPassPhone unpacks the number and extension LastPass keeps as JSON.
func lastPassPhone(value string) string {
	var phone struct {
		Num string `json:"num"`
		Ext string `json:"ext"`
	}
	if err := json.Unmarshal([]byte(value), &phone); err != nil {
		return ""
	}
	return phone.Num + phone.Ext
}

// lastPassBirthday joins the parts of a date LastPass lets you leave half empty.
func lastPassBirthday(value string) string {
	var parts []string
	for _, part := range strings.Split(value, ",") {
		if part = strings.TrimSpace(part); part != "" {
			parts = append(parts, part)
		}
	}
	return strings.Join(parts, ", ")
}
