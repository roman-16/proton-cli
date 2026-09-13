package passfile

import (
	"encoding/json"
	"strings"

	pb "github.com/roman-16/proton-cli/internal/service/pass/proto"
)

// NordPass writes one CSV with a type column, and rows of its own for the
// folders it holds.

var nordPassIdentityFields = map[string]func(*pb.ItemIdentity) *string{
	"address1":     func(i *pb.ItemIdentity) *string { return &i.StreetAddress },
	"city":         func(i *pb.ItemIdentity) *string { return &i.City },
	"country":      func(i *pb.ItemIdentity) *string { return &i.CountryOrRegion },
	"email":        func(i *pb.ItemIdentity) *string { return &i.Email },
	"full_name":    func(i *pb.ItemIdentity) *string { return &i.FullName },
	"phone_number": func(i *pb.ItemIdentity) *string { return &i.PhoneNumber },
	"state":        func(i *pb.ItemIdentity) *string { return &i.StateOrProvince },
	"url":          func(i *pb.ItemIdentity) *string { return &i.Website },
	"zipcode":      func(i *pb.ItemIdentity) *string { return &i.ZipOrPostalCode },
}

func readNordPass(in source) (*Document, error) {
	t, err := openCSV(in, "nordpass", "name", "url", "username", "password", "note", "type", "folder")
	if err != nil {
		return nil, err
	}
	doc := flatFile(t)
	vaults := newGrouped()
	for _, r := range t.rows {
		// A folder is a row of its own, and the items in it name it again, so
		// the row itself holds nothing to import.
		if r.get("type") == "folder" {
			continue
		}
		entry, ok := nordPassEntry(r)
		if !ok {
			doc.skip(r.get("folder"), r.get("name"), "NordPass calls it a "+r.get("type")+", which Pass has no kind for")
			continue
		}
		vaults.add(nordPassVault(r.get("folder")), entry)
	}
	vaults.into(doc)
	return doc, nil
}

// nordPassVault is the folder an item sits in, innermost first: NordPass writes
// a path and Pass has no folders inside vaults.
func nordPassVault(folder string) string {
	parts := strings.Split(folder, "/")
	return parts[len(parts)-1]
}

func nordPassEntry(r row) (Entry, bool) {
	in := item{
		Name:   r.get("name"),
		Note:   r.get("note"),
		Fields: nordPassFields(r.get("custom_fields")),
	}
	switch r.get("type") {
	case "password":
		in.Email, in.Username = identifier(r.get("username"))
		in.Password = r.get("password")
		in.URLs = append([]string{r.get("url")}, nordPassURLs(r.get("additional_urls"))...)
		return in.login(), true
	case "credit_card":
		in.Holder, in.Number, in.CVV = r.get("cardholdername"), r.get("cardnumber"), r.get("cvc")
		in.Expiry = nordPassExpiry(r.get("expirydate"))
		in.PIN = r.get("pin")
		return in.card(), true
	case "identity":
		idn := &pb.ItemIdentity{}
		for column, at := range nordPassIdentityFields {
			*at(idn) = r.get(column)
		}
		in.Identity = idn
		return in.identity(), true
	case "note":
		return in.note(), true
	}
	return Entry{}, false
}

// nordPassExpiry turns the MM/YY NordPass writes into the MMYYYY Pass stores.
func nordPassExpiry(value string) string {
	if len(value) < 5 {
		return ""
	}
	return value[:2] + "20" + value[3:5]
}

func nordPassURLs(raw string) []string {
	var urls []string
	if err := json.Unmarshal([]byte(raw), &urls); err != nil {
		return nil
	}
	return urls
}

func nordPassFields(raw string) []Field {
	var fields []struct {
		Label string `json:"label"`
		Type  string `json:"type"`
		Value string `json:"value"`
	}
	if err := json.Unmarshal([]byte(raw), &fields); err != nil {
		return nil
	}
	out := make([]Field, 0, len(fields))
	for _, f := range fields {
		out = append(out, Field{Name: f.Label, Value: f.Value, Hidden: f.Type == "hidden"})
	}
	return out
}
