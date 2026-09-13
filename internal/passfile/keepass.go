package passfile

import (
	"encoding/xml"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
)

// KeePass exports XML: groups inside groups, each holding entries, each entry a
// list of key and value pairs. A value marked as protected in memory is one to
// hide.

type keePassFile struct {
	XMLName xml.Name     `xml:"KeePassFile"`
	Root    keePassGroup `xml:"Root"`
}

type keePassGroup struct {
	Name    string         `xml:"Name"`
	Entries []keePassEntry `xml:"Entry"`
	Groups  []keePassGroup `xml:"Group"`
}

type keePassEntry struct {
	Strings []struct {
		Key   string `xml:"Key"`
		Value struct {
			Text      string `xml:",chardata"`
			Protected string `xml:"ProtectInMemory,attr"`
		} `xml:"Value"`
	} `xml:"String"`
}

func readKeePass(in source) (*Document, error) {
	raw, err := os.ReadFile(in.path)
	if err != nil {
		return nil, err
	}
	var file keePassFile
	if err := xml.Unmarshal(raw, &file); err != nil {
		return nil, notThisFormat(in, "keepass")
	}
	doc := &Document{}
	keePassGroups(doc, file.Root)
	return doc, nil
}

// keePassGroups walks the tree, and every group that holds entries becomes a
// vault: Pass has no folders inside vaults, so a nested group is a vault of its
// own rather than a place inside one.
func keePassGroups(doc *Document, group keePassGroup) {
	var items []Entry
	for _, entry := range group.Entries {
		items = append(items, keePassEntryItem(entry))
	}
	doc.vault(group.Name, items)
	for _, nested := range group.Groups {
		keePassGroups(doc, nested)
	}
}

func keePassEntryItem(entry keePassEntry) Entry {
	in := item{}
	var seed, settings, otpauth string
	for _, field := range entry.Strings {
		value := field.Value.Text
		switch field.Key {
		case "Title":
			in.Name = value
		case "Notes":
			in.Note = value
		case "UserName":
			in.Email, in.Username = identifier(value)
		case "Password":
			in.Password = value
		case "URL":
			in.URLs = []string{value}
		case "otp":
			otpauth = value
		case "TOTP Seed":
			seed = value
		case "TOTP Settings":
			settings = value
		default:
			hidden := field.Value.Protected != ""
			name := field.Key
			if name == "" {
				name = "Text"
				if hidden {
					name = "Hidden"
				}
			}
			in.Fields = append(in.Fields, Field{Name: name, Value: value, Hidden: hidden})
		}
	}
	in.TOTP = otpauth
	if seed != "" && settings != "" {
		in.TOTP = keePassLegacyTOTP(in.Name, seed, settings)
	}
	return in.login()
}

// keePassLegacyTOTP builds the URI out of the two fields older KeePass plugins
// wrote instead of one, where the settings are "period;digits".
func keePassLegacyTOTP(name, seed, settings string) string {
	period, digits, found := strings.Cut(settings, ";")
	if !found {
		return ""
	}
	if _, err := strconv.Atoi(period); err != nil {
		return ""
	}
	if _, err := strconv.Atoi(digits); err != nil {
		return ""
	}
	return fmt.Sprintf("otpauth://totp/%s:none?secret=%s&period=%s&digits=%s",
		url.PathEscape(name), url.QueryEscape(seed), period, digits)
}
