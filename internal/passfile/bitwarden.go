package passfile

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/roman-16/proton-cli/internal/errs"
	pb "github.com/roman-16/proton-cli/internal/service/pass/proto"
)

// Bitwarden exports JSON, or a zip holding that JSON and a folder of
// attachments per item.
//
// A personal export files its items in folders; an organisation's files them in
// collections and leaves every folder empty, so which of the two the file
// carries decides where an item lands.

// bitwardenAndroidApp is how Bitwarden writes an address that is not a website
// at all but an app on a phone.
const bitwardenAndroidApp = "androidapp://"

type bitwardenFile struct {
	Encrypted   bool              `json:"encrypted"`
	Items       []bitwardenItem   `json:"items"`
	Folders     []bitwardenGroup  `json:"folders"`
	Collections *[]bitwardenGroup `json:"collections"`
}

type bitwardenGroup struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type bitwardenItem struct {
	ID            string            `json:"id"`
	Type          int               `json:"type"`
	Name          string            `json:"name"`
	Notes         string            `json:"notes"`
	FolderID      string            `json:"folderId"`
	CollectionIDs []string          `json:"collectionIds"`
	Fields        []bitwardenField  `json:"fields"`
	Login         *bitwardenLogin   `json:"login"`
	Card          *bitwardenCard    `json:"card"`
	Identity      map[string]string `json:"identity"`
	SSHKey        *bitwardenSSHKey  `json:"sshKey"`
}

type bitwardenField struct {
	Name  string `json:"name"`
	Type  int    `json:"type"`
	Value string `json:"value"`
}

type bitwardenLogin struct {
	Username string `json:"username"`
	Password string `json:"password"`
	TOTP     string `json:"totp"`
	URIs     []struct {
		URI   string `json:"uri"`
		Match *int   `json:"match"`
	} `json:"uris"`
}

type bitwardenCard struct {
	CardholderName string `json:"cardholderName"`
	Number         string `json:"number"`
	Code           string `json:"code"`
	ExpMonth       string `json:"expMonth"`
	ExpYear        string `json:"expYear"`
}

type bitwardenSSHKey struct {
	PrivateKey     string `json:"privateKey"`
	PublicKey      string `json:"publicKey"`
	KeyFingerprint string `json:"keyFingerprint"`
}

// bitwardenIdentityFields are the fields of an address book entry. Bitwarden
// keeps three lines of a street address and Pass keeps one, so those three are
// joined rather than each overwriting the last.
var bitwardenIdentityFields = map[string]func(*pb.ItemIdentity) *string{
	"address1":       func(i *pb.ItemIdentity) *string { return &i.StreetAddress },
	"address2":       func(i *pb.ItemIdentity) *string { return &i.StreetAddress },
	"address3":       func(i *pb.ItemIdentity) *string { return &i.StreetAddress },
	"city":           func(i *pb.ItemIdentity) *string { return &i.City },
	"company":        func(i *pb.ItemIdentity) *string { return &i.Company },
	"country":        func(i *pb.ItemIdentity) *string { return &i.CountryOrRegion },
	"email":          func(i *pb.ItemIdentity) *string { return &i.Email },
	"firstName":      func(i *pb.ItemIdentity) *string { return &i.FirstName },
	"lastName":       func(i *pb.ItemIdentity) *string { return &i.LastName },
	"licenseNumber":  func(i *pb.ItemIdentity) *string { return &i.LicenseNumber },
	"middleName":     func(i *pb.ItemIdentity) *string { return &i.MiddleName },
	"passportNumber": func(i *pb.ItemIdentity) *string { return &i.PassportNumber },
	"phone":          func(i *pb.ItemIdentity) *string { return &i.PhoneNumber },
	"postalCode":     func(i *pb.ItemIdentity) *string { return &i.ZipOrPostalCode },
	"ssn":            func(i *pb.ItemIdentity) *string { return &i.SocialSecurityNumber },
	"state":          func(i *pb.ItemIdentity) *string { return &i.StateOrProvince },
	"username":       func(i *pb.ItemIdentity) *string { return &i.XHandle },
}

// bitwardenIdentityOrder is the order the fields are read in, so the three
// address lines join in the order they were written.
var bitwardenIdentityOrder = []string{
	"firstName", "middleName", "lastName", "address1", "address2", "address3",
	"city", "state", "postalCode", "country", "company", "email", "phone",
	"ssn", "username", "passportNumber", "licenseNumber",
}

func readBitwarden(in source) (*Document, error) {
	if z, err := zip.OpenReader(in.path); err == nil {
		doc, err := readBitwardenZip(in, z)
		if err != nil {
			_ = z.Close()
			return nil, err
		}
		return doc, nil
	}
	raw, err := os.ReadFile(in.path)
	if err != nil {
		return nil, err
	}
	return readBitwardenJSON(in, raw, nil)
}

// readBitwardenZip reads the archive, whose items' attachments sit in a folder
// named after the item.
func readBitwardenZip(in source, z *zip.ReadCloser) (*Document, error) {
	data := zipEntry(z, "data.json")
	if data == nil {
		return nil, notThisFormat(in, "bitwarden")
	}
	raw, err := readZipEntry(in, data)
	if err != nil {
		return nil, err
	}
	attachments := map[string][]File{}
	for _, f := range z.File {
		id, ok := bitwardenAttachmentOwner(f.Name)
		if !ok || f.FileInfo().IsDir() {
			continue
		}
		attachments[id] = append(attachments[id], File{
			Name: zipBase(f.Name),
			Size: int64(f.UncompressedSize64),
			Open: func() (io.ReadCloser, error) { return f.Open() },
		})
	}
	doc, err := readBitwardenJSON(in, raw, attachments)
	if err != nil {
		return nil, err
	}
	doc.closers = append(doc.closers, z)
	return doc, nil
}

// bitwardenAttachmentOwner is the item a file in the archive belongs to.
func bitwardenAttachmentOwner(path string) (string, bool) {
	rest, found := strings.CutPrefix(strings.TrimPrefix(path, "/"), "attachments/")
	if !found {
		return "", false
	}
	id, name, found := strings.Cut(rest, "/")
	// A file directly under a folder of the item's own, and nothing deeper.
	if !found || name == "" || strings.Contains(name, "/") {
		return "", false
	}
	return id, true
}

func readBitwardenJSON(in source, raw []byte, attachments map[string][]File) (*Document, error) {
	var file bitwardenFile
	if err := json.Unmarshal(raw, &file); err != nil || file.Items == nil {
		return nil, notThisFormat(in, "bitwarden")
	}
	if file.Encrypted {
		return nil, errs.Problemf("%s is an encrypted Bitwarden export, which cannot be read.", in.path).
			Hint("export it again with encryption turned off.")
	}

	// An organisation's export files items in collections and leaves every
	// folder empty, so the file says which of the two to follow by carrying it.
	groups, byCollection := file.Folders, file.Collections != nil
	if byCollection {
		groups = *file.Collections
	}
	names := make(map[string]string, len(groups))
	for _, g := range groups {
		names[g.ID] = g.Name
	}

	doc := &Document{}
	vaults := newGrouped()
	for _, record := range file.Items {
		group := record.FolderID
		if byCollection {
			group = ""
			if len(record.CollectionIDs) > 0 {
				group = record.CollectionIDs[0]
			}
		}
		entry, ok := bitwardenEntry(record)
		if !ok {
			doc.skip(names[group], record.Name, "Bitwarden has a kind of item Pass has not")
			continue
		}
		entry.Files = attachments[record.ID]
		vaults.add(names[group], entry)
	}
	vaults.into(doc)
	return doc, nil
}

func bitwardenEntry(record bitwardenItem) (Entry, bool) {
	in := item{
		Name:   record.Name,
		Note:   record.Notes,
		Fields: bitwardenFields(record.Fields),
	}
	switch record.Type {
	case 1:
		login := record.Login
		if login == nil {
			login = &bitwardenLogin{}
		}
		in.Email, in.Username = identifier(login.Username)
		in.Password, in.TOTP = login.Password, login.TOTP
		in.Modes = map[string]pb.AutofillUrl_Mode{}
		for _, u := range login.URIs {
			if app, found := strings.CutPrefix(u.URI, bitwardenAndroidApp); found {
				in.Fields = append(in.Fields, Field{Name: "Android app", Value: app})
				continue
			}
			in.URLs = append(in.URLs, u.URI)
			in.Modes[u.URI] = bitwardenAutofillMode(u.Match)
		}
		return in.login(), true
	case 2:
		return in.note(), true
	case 3:
		card := record.Card
		if card == nil {
			card = &bitwardenCard{}
		}
		in.Holder, in.Number, in.CVV = card.CardholderName, card.Number, card.Code
		if card.ExpMonth != "" && card.ExpYear != "" {
			in.Expiry = fmt.Sprintf("%02s%s", card.ExpMonth, card.ExpYear)
		}
		return in.card(), true
	case 4:
		idn := &pb.ItemIdentity{}
		for _, key := range bitwardenIdentityOrder {
			value := record.Identity[key]
			at, ok := bitwardenIdentityFields[key]
			if !ok || value == "" {
				continue
			}
			if current := *at(idn); current != "" {
				value = current + " " + value
			}
			*at(idn) = value
		}
		idn.ExtraSections = section("Extra fields", in.Fields, record.Name)
		in.Fields, in.Identity = nil, idn
		return in.identity(), true
	case 5:
		key := record.SSHKey
		if key == nil {
			key = &bitwardenSSHKey{}
		}
		in.PrivateKey, in.PublicKey = key.PrivateKey, key.PublicKey
		if key.KeyFingerprint != "" {
			in.Sections = section("Extra fields",
				[]Field{{Name: "Key fingerprint", Value: key.KeyFingerprint, Hidden: true}}, record.Name)
		}
		return in.sshKey(), true
	}
	return Entry{}, false
}

// bitwardenAutofillMode is the rule Pass fills an address under, from the one
// Bitwarden matched it with.
func bitwardenAutofillMode(match *int) pb.AutofillUrl_Mode {
	if match == nil {
		return pb.AutofillUrl_Default
	}
	switch *match {
	case 1:
		return pb.AutofillUrl_Exact
	case 2:
		return pb.AutofillUrl_StartWith
	case 3:
		return pb.AutofillUrl_ExactPath
	case 4:
		return pb.AutofillUrl_RegularExpression
	case 5:
		return pb.AutofillUrl_Never
	}
	return pb.AutofillUrl_Default
}

func bitwardenFields(fields []bitwardenField) []Field {
	var out []Field
	for _, f := range fields {
		name, hidden := f.Name, false
		switch f.Type {
		case 0:
			if name == "" {
				name = "Text"
			}
		case 1:
			hidden = true
			if name == "" {
				name = "Hidden"
			}
		case 2:
			if name == "" {
				name = "Checkbox"
			}
		default:
			continue
		}
		out = append(out, Field{Name: name, Value: f.Value, Hidden: hidden})
	}
	return out
}
