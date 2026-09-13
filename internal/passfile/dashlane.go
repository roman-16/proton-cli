package passfile

import (
	"archive/zip"
	"fmt"
	"os"
	"strings"

	pb "github.com/roman-16/proton-cli/internal/service/pass/proto"
)

// Dashlane exports either a zip of one CSV per kind of item, or a single CSV of
// whichever kind you asked for.
//
// The single file says nowhere what is in it, so the columns it has are what
// decide: only its logins have a password column, only its payments have a card
// number, and so on.

// dashlaneKinds are the files in the zip, the columns that identify a bare CSV
// of that kind, and what to make of a row.
var dashlaneKinds = []struct {
	file    string
	columns []string
	read    func(row) Entry
}{
	{"credentials.csv", []string{"username", "password"}, dashlaneLogin},
	{"ids.csv", []string{"issue_date", "expiration_date"}, dashlaneID},
	{"payments.csv", []string{"cc_number", "account_number"}, dashlaneCard},
	{"personalInfo.csv", []string{"date_of_birth", "email"}, dashlanePersonalInfo},
	{"securenotes.csv", []string{"title", "note"}, dashlaneNote},
}

var dashlaneIdentityFields = map[string]func(*pb.ItemIdentity) *string{
	"address":       func(i *pb.ItemIdentity) *string { return &i.StreetAddress },
	"address_floor": func(i *pb.ItemIdentity) *string { return &i.Floor },
	"city":          func(i *pb.ItemIdentity) *string { return &i.City },
	"country":       func(i *pb.ItemIdentity) *string { return &i.CountryOrRegion },
	"date_of_birth": func(i *pb.ItemIdentity) *string { return &i.Birthdate },
	"email":         func(i *pb.ItemIdentity) *string { return &i.Email },
	"first_name":    func(i *pb.ItemIdentity) *string { return &i.FirstName },
	"job_title":     func(i *pb.ItemIdentity) *string { return &i.JobTitle },
	"last_name":     func(i *pb.ItemIdentity) *string { return &i.LastName },
	"login":         func(i *pb.ItemIdentity) *string { return &i.XHandle },
	"middle_name":   func(i *pb.ItemIdentity) *string { return &i.MiddleName },
	"name":          func(i *pb.ItemIdentity) *string { return &i.FullName },
	"phone_number":  func(i *pb.ItemIdentity) *string { return &i.PhoneNumber },
	"state":         func(i *pb.ItemIdentity) *string { return &i.StateOrProvince },
	"url":           func(i *pb.ItemIdentity) *string { return &i.Website },
	"zip":           func(i *pb.ItemIdentity) *string { return &i.ZipOrPostalCode },
}

// dashlaneNumbered is what the number column of an identity means, which depends
// on what kind of document the row is about.
var dashlaneNumbered = map[string]func(*pb.ItemIdentity) *string{
	"license":         func(i *pb.ItemIdentity) *string { return &i.LicenseNumber },
	"passport":        func(i *pb.ItemIdentity) *string { return &i.PassportNumber },
	"social_security": func(i *pb.ItemIdentity) *string { return &i.SocialSecurityNumber },
}

func readDashlane(in source) (*Document, error) {
	if z, err := zip.OpenReader(in.path); err == nil {
		defer func() { _ = z.Close() }()
		return readDashlaneZip(in, z)
	}
	raw, err := os.ReadFile(in.path)
	if err != nil {
		return nil, err
	}
	t, err := readCSV(raw)
	if err != nil {
		return nil, notThisFormat(in, "dashlane")
	}
	// A single CSV says nowhere which kind of item it holds, so the columns are
	// what decide.
	for _, kind := range dashlaneKinds {
		if !t.has(kind.columns...) {
			continue
		}
		doc := flatFile(t)
		var items []Entry
		for _, r := range t.rows {
			items = append(items, kind.read(r))
		}
		doc.vault("", items)
		return doc, nil
	}
	return nil, notThisFormat(in, "dashlane")
}

func readDashlaneZip(in source, z *zip.ReadCloser) (*Document, error) {
	doc := &Document{}
	var items []Entry
	var found bool
	for _, kind := range dashlaneKinds {
		f := zipEntry(z, kind.file)
		if f == nil {
			continue
		}
		found = true
		raw, err := readZipEntry(in, f)
		if err != nil {
			return nil, err
		}
		t, err := readCSV(raw)
		if err != nil || !t.has(kind.columns...) {
			doc.Warnings = append(doc.Warnings,
				fmt.Sprintf("%s in the export was not readable and was left out.", kind.file))
			continue
		}
		if w := t.warning(); w != "" {
			doc.Warnings = append(doc.Warnings, w)
		}
		for _, r := range t.rows {
			items = append(items, kind.read(r))
		}
	}
	if !found {
		return nil, notThisFormat(in, "dashlane")
	}
	doc.vault("", items)
	return doc, nil
}

func dashlaneLogin(r row) Entry {
	in := item{
		Name:     r.get("title"),
		Note:     r.get("note"),
		Password: r.get("password"),
		URLs:     []string{r.get("url")},
		TOTP:     r.first("otpUrl", "otpSecret"),
	}
	in.Email, in.Username = identifier(r.get("username"))
	for i, extra := range []string{r.get("username2"), r.get("username3")} {
		if extra != "" {
			in.Fields = append(in.Fields, Field{
				Name: fmt.Sprintf("username%d", i+1), Value: extra,
			})
		}
	}
	return in.login()
}

func dashlaneNote(r row) Entry {
	return item{Name: r.get("title"), Note: r.get("note")}.note()
}

func dashlaneCard(r row) Entry {
	in := item{
		Name:   r.get("name"),
		Note:   r.get("note"),
		Holder: r.get("account_name"),
		Number: r.get("cc_number"),
		CVV:    r.get("code"),
	}
	if month, year := r.get("expiration_month"), r.get("expiration_year"); month != "" && year != "" {
		in.Expiry = fmt.Sprintf("%02s%s", month, year)
	}
	return in.card()
}

func dashlaneID(r row) Entry {
	return item{Name: dashlaneIDName(r), Identity: dashlaneIdentity(r)}.identity()
}

func dashlanePersonalInfo(r row) Entry {
	name := r.get("item_name")
	if r.get("type") == "name" {
		var parts []string
		for _, part := range []string{r.get("first_name"), r.get("middle_name"), r.get("last_name")} {
			if part != "" {
				parts = append(parts, part)
			}
		}
		name = strings.Join(parts, " ")
	}
	return item{Name: name, Identity: dashlaneIdentity(r)}.identity()
}

// dashlaneIDName says what kind of document an identity row is about, since the
// row itself has no title.
func dashlaneIDName(r row) string {
	kinds := map[string]string{
		"card":            "ID Card",
		"license":         "License",
		"passport":        "Passport",
		"social_security": "Social security",
		"tax_number":      "Tax number",
	}
	kind := kinds[r.get("type")]
	if kind != "" && r.get("name") != "" {
		return fmt.Sprintf("%s (%s)", kind, r.get("name"))
	}
	return kind
}

func dashlaneIdentity(r row) *pb.ItemIdentity {
	idn := &pb.ItemIdentity{}
	for column, at := range dashlaneIdentityFields {
		if v := r.get(column); v != "" {
			*at(idn) = v
		}
	}
	kind := r.get("type")
	if number := r.get("number"); number != "" {
		switch at, ok := dashlaneNumbered[kind]; {
		case ok:
			*at(idn) = number
		case kind == "card":
			idn.ExtraContactDetails = extraFields([]Field{{Name: "ID Card number", Value: number}}, "")
		case kind == "tax_number":
			idn.ExtraContactDetails = extraFields([]Field{{Name: "Tax number", Value: number}}, "")
		}
	}
	if kind == "company" {
		idn.Company = r.get("item_name")
	}
	return idn
}
