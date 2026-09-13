package passfile

import (
	"encoding/json"
	"os"
	"slices"
	"sort"
	"strings"

	pb "github.com/roman-16/proton-cli/internal/service/pass/proto"
)

// Keeper writes JSON, and keeps everything that is not a login in a map of
// custom fields whose keys carry the type: "$text:cardholderName:1" is a text
// field called cardholderName, "$secret:..." is one to hide.

type keeperFile struct {
	Records []keeperRecord `json:"records"`
}

type keeperRecord struct {
	Title        string                     `json:"title"`
	Type         string                     `json:"$type"`
	Login        string                     `json:"login"`
	Password     string                     `json:"password"`
	LoginURL     string                     `json:"login_url"`
	Notes        string                     `json:"notes"`
	CustomFields map[string]json.RawMessage `json:"custom_fields"`
	Folders      []struct {
		Folder string `json:"folder"`
	} `json:"folders"`
}

func readKeeper(in source) (*Document, error) {
	raw, err := os.ReadFile(in.path)
	if err != nil {
		return nil, err
	}
	var file keeperFile
	if err := json.Unmarshal(raw, &file); err != nil || file.Records == nil {
		return nil, notThisFormat(in, "keeper")
	}

	doc := &Document{}
	vaults := newGrouped()
	for _, record := range file.Records {
		vaults.add(keeperVault(record), keeperEntry(record))
	}
	vaults.into(doc)
	return doc, nil
}

// keeperVault is the innermost folder of the first path an item is filed under.
func keeperVault(record keeperRecord) string {
	if len(record.Folders) == 0 {
		return ""
	}
	parts := strings.Split(record.Folders[0].Folder, `\`)
	return parts[len(parts)-1]
}

func keeperEntry(record keeperRecord) Entry {
	fields := record.CustomFields
	in := item{Name: record.Title, Note: record.Notes}
	switch record.Type {
	case "", "login":
		in.Email, in.Username = identifier(record.Login)
		in.Password = record.Password
		in.URLs = []string{record.LoginURL}
		in.TOTP = keeperText(fields, "$oneTimeCode::1")
		in.Fields = keeperFields(fields, "$oneTimeCode::1")
		return in.login()
	case "encryptedNotes":
		in.Fields = keeperFields(fields)
		return in.note()
	case "bankCard":
		card := keeperObject(fields, "$paymentCard::1")
		in.Holder = keeperText(fields, "$text:cardholderName:1")
		in.Number = card["cardNumber"]
		in.CVV = card["cardSecurityCode"]
		in.Expiry = keeperExpiry(card["cardExpirationDate"])
		in.PIN = keeperText(fields, "$pinCode::1")
		in.Fields = keeperFields(fields, "$paymentCard::1", "$pinCode::1", "$text:cardholderName:1")
		return in.card()
	case "contact", "ssnCard":
		name := keeperObject(fields, "$name::1")
		idn := &pb.ItemIdentity{
			FirstName:  name["first"],
			MiddleName: name["middle"],
			LastName:   name["last"],
			Company:    keeperText(fields, "$text:company:1"),
			Email:      keeperText(fields, "$email::1"),
		}
		idn.ExtraSections = section("Extra fields",
			keeperFields(fields, "$email::1", "$name::1", "$text:company:1"), record.Title)
		in.Identity = idn
		return in.identity()
	case "sshKeys":
		keys := keeperObject(fields, "$keyPair::1")
		in.PrivateKey, in.PublicKey = keys["privateKey"], keys["publicKey"]
		in.Sections = section("Extra fields",
			append(keeperBuiltins(record), keeperFields(fields, "$keyPair::1")...), record.Title)
		return in.sshKey()
	case "wifiCredentials":
		in.SSID = keeperText(fields, "$text:SSID:1")
		in.Password = record.Password
		in.Sections = section("Extra fields", keeperFields(fields, "$text:SSID:1"), record.Title)
		return in.wifi()
	default:
		in.Sections = section("Extra fields",
			append(keeperBuiltins(record), keeperFields(fields)...), record.Title)
		return in.custom()
	}
}

// keeperBuiltins are the fields every Keeper record has, kept as custom fields
// on the kinds of item that have nowhere else for them.
func keeperBuiltins(record keeperRecord) []Field {
	var out []Field
	for _, f := range []Field{
		{Name: "Username", Value: record.Login},
		{Name: "Password", Value: record.Password},
		{Name: "Website", Value: record.LoginURL},
	} {
		if f.Value != "" {
			out = append(out, f)
		}
	}
	return out
}

// keeperFields turns the custom fields into Pass's, flattening the ones Keeper
// stores as objects or lists.
func keeperFields(fields map[string]json.RawMessage, except ...string) []Field {
	var out []Field
	for _, label := range sortedKeys(fields) {
		if slices.Contains(except, label) {
			continue
		}
		out = append(out, keeperField(label, fields[label])...)
	}
	return out
}

func keeperField(label string, raw json.RawMessage) []Field {
	hidden := strings.HasPrefix(label, "$secret:")
	name := keeperLabel(label)

	var list []json.RawMessage
	if err := json.Unmarshal(raw, &list); err == nil {
		var out []Field
		for _, one := range list {
			out = append(out, keeperField(label, one)...)
		}
		return out
	}

	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return []Field{{Name: name, Value: text, Hidden: hidden}}
	}

	object := map[string]string{}
	if err := json.Unmarshal(raw, &object); err != nil {
		return nil
	}
	if strings.HasPrefix(label, "$phone:") {
		return []Field{{Name: name, Value: keeperPhone(object)}}
	}
	var out []Field
	for _, key := range sortedKeys(object) {
		out = append(out, Field{
			Name:   name + " - " + key,
			Value:  object[key],
			Hidden: strings.HasPrefix(key, "$secret:"),
		})
	}
	return out
}

// keeperLabel is what a person calls the field, with the type Keeper wrapped it
// in taken off.
func keeperLabel(label string) string {
	clean := strings.TrimSuffix(label, ":1")
	for _, prefix := range []string{"$text:", "$name:", "$phone:", "$secret:", "$"} {
		if strings.HasPrefix(clean, prefix) {
			clean = strings.TrimPrefix(clean, prefix)
			break
		}
	}
	clean = strings.TrimSuffix(clean, ":")
	if clean == "" {
		return "Text"
	}
	return clean
}

// keeperPhone joins the parts Keeper keeps a number in.
func keeperPhone(value map[string]string) string {
	var parts []string
	for _, key := range []string{"type", "region", "number", "ext"} {
		if v := strings.TrimSpace(value[key]); v != "" {
			parts = append(parts, v)
		}
	}
	return strings.Join(parts, " ")
}

// keeperExpiry turns the MM/YYYY Keeper writes into the MMYYYY Pass stores.
func keeperExpiry(value string) string {
	month, year, found := strings.Cut(value, "/")
	if !found || len(month) != 2 || len(year) != 4 {
		return ""
	}
	return month + year
}

func keeperText(fields map[string]json.RawMessage, key string) string {
	var out string
	if err := json.Unmarshal(fields[key], &out); err != nil {
		return ""
	}
	return out
}

func keeperObject(fields map[string]json.RawMessage, key string) map[string]string {
	out := map[string]string{}
	_ = json.Unmarshal(fields[key], &out)
	return out
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
