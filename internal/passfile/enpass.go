package passfile

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"os"
	"slices"

	pb "github.com/roman-16/proton-cli/internal/service/pass/proto"
)

// Enpass writes JSON in which every item is a list of typed fields, and carries
// its attachments inside the file as base64.

type enpassFile struct {
	Items []enpassItem `json:"items"`
}

type enpassItem struct {
	Category    string        `json:"category"`
	Title       string        `json:"title"`
	Note        string        `json:"note"`
	Archived    int           `json:"archived"`
	Trashed     int           `json:"trashed"`
	CreatedAt   int64         `json:"createdAt"`
	UpdatedAt   int64         `json:"updated_at"`
	Fields      []enpassField `json:"fields"`
	Attachments []struct {
		Data string `json:"data"`
		Name string `json:"name"`
	} `json:"attachments"`
}

type enpassField struct {
	Label     string `json:"label"`
	Type      string `json:"type"`
	UID       int    `json:"uid"`
	Sensitive int    `json:"sensitive"`
	Value     string `json:"value"`
}

// enpassIdentityFields are the numbers Enpass gives the fields of an identity.
var enpassIdentityFields = map[int]func(*pb.ItemIdentity) *string{
	130: func(i *pb.ItemIdentity) *string { return &i.FirstName },
	131: func(i *pb.ItemIdentity) *string { return &i.MiddleName },
	132: func(i *pb.ItemIdentity) *string { return &i.LastName },
	133: func(i *pb.ItemIdentity) *string { return &i.Gender },
	134: func(i *pb.ItemIdentity) *string { return &i.Birthdate },
	137: func(i *pb.ItemIdentity) *string { return &i.Company },
	139: func(i *pb.ItemIdentity) *string { return &i.JobTitle },
	140: func(i *pb.ItemIdentity) *string { return &i.WorkPhoneNumber },
	143: func(i *pb.ItemIdentity) *string { return &i.StreetAddress },
	144: func(i *pb.ItemIdentity) *string { return &i.City },
	145: func(i *pb.ItemIdentity) *string { return &i.StateOrProvince },
	146: func(i *pb.ItemIdentity) *string { return &i.CountryOrRegion },
	147: func(i *pb.ItemIdentity) *string { return &i.ZipOrPostalCode },
	149: func(i *pb.ItemIdentity) *string { return &i.PhoneNumber },
	151: func(i *pb.ItemIdentity) *string { return &i.SecondPhoneNumber },
	153: func(i *pb.ItemIdentity) *string { return &i.XHandle },
	154: func(i *pb.ItemIdentity) *string { return &i.Facebook },
	155: func(i *pb.ItemIdentity) *string { return &i.Linkedin },
	157: func(i *pb.ItemIdentity) *string { return &i.Instagram },
	158: func(i *pb.ItemIdentity) *string { return &i.Yahoo },
	163: func(i *pb.ItemIdentity) *string { return &i.Website },
	165: func(i *pb.ItemIdentity) *string { return &i.Email },
	212: func(i *pb.ItemIdentity) *string { return &i.SocialSecurityNumber },
	213: func(i *pb.ItemIdentity) *string { return &i.Organization },
}

func readEnpass(in source) (*Document, error) {
	raw, err := os.ReadFile(in.path)
	if err != nil {
		return nil, err
	}
	var file enpassFile
	if err := json.Unmarshal(raw, &file); err != nil || file.Items == nil {
		return nil, notThisFormat(in, "enpass")
	}

	doc := &Document{}
	var items []Entry
	for _, record := range file.Items {
		entry := enpassEntry(record)
		entry.Files = enpassAttachments(record)
		items = append(items, entry)
	}
	doc.vault("", items)
	return doc, nil
}

func enpassEntry(record enpassItem) Entry {
	in := item{
		Name: record.Title, Note: record.Note,
		Trashed:    record.Archived != 0 || record.Trashed != 0,
		CreateTime: record.CreatedAt, ModifyTime: record.UpdatedAt,
	}
	switch record.Category {
	case "login", "password":
		taken, rest := enpassTake(record.Fields, "username", "email", "totp", "password", "url")
		in.Email, in.Username = taken["email"], taken["username"]
		in.Password, in.TOTP = taken["password"], taken["totp"]
		if url := taken["url"]; url != "" {
			in.URLs = []string{url}
		}
		in.Fields = enpassFields(rest)
		return in.login()
	case "note":
		return in.note()
	case "creditcard":
		taken, rest := enpassTake(record.Fields, "ccName", "ccType", "ccNumber", "ccCvc", "ccPin", "ccExpiry")
		in.Holder, in.Number = taken["ccName"], taken["ccNumber"]
		in.CVV, in.PIN, in.Expiry = taken["ccCvc"], taken["ccPin"], taken["ccExpiry"]
		in.Fields = enpassFields(rest)
		return in.card()
	case "identity":
		idn := &pb.ItemIdentity{}
		var rest []enpassField
		for _, f := range record.Fields {
			at, ok := enpassIdentityFields[f.UID]
			switch {
			case ok:
				*at(idn) = f.Value
			case f.Value != "":
				rest = append(rest, f)
			}
		}
		idn.ExtraSections = section("Extra fields", enpassFields(rest), record.Title)
		in.Identity = idn
		return in.identity()
	default:
		_, rest := enpassTake(record.Fields)
		in.Sections = section("Extra fields", enpassFields(rest), record.Title)
		return in.custom()
	}
}

// enpassTake pulls out the fields a kind of item has of its own, and hands back
// what is left to become custom fields.
//
// A section field is a heading rather than a value, so it is neither taken nor
// kept.
func enpassTake(fields []enpassField, wanted ...string) (map[string]string, []enpassField) {
	taken := map[string]string{}
	var rest []enpassField
	for _, f := range fields {
		switch {
		case f.Value == "":
			continue
		case slices.Contains(wanted, f.Type) && taken[f.Type] == "":
			taken[f.Type] = f.Value
		case f.Type == "section":
			continue
		default:
			rest = append(rest, f)
		}
	}
	return taken, rest
}

func enpassFields(fields []enpassField) []Field {
	out := make([]Field, 0, len(fields))
	for _, f := range fields {
		out = append(out, Field{Name: f.Label, Value: f.Value, Hidden: f.Sensitive != 0})
	}
	return out
}

func enpassAttachments(record enpassItem) []File {
	var out []File
	for _, a := range record.Attachments {
		body, err := base64.StdEncoding.DecodeString(a.Data)
		if err != nil {
			continue
		}
		out = append(out, File{
			Name: a.Name, Size: int64(len(body)),
			Open: func() (io.ReadCloser, error) {
				return io.NopCloser(bytes.NewReader(body)), nil
			},
		})
	}
	return out
}
