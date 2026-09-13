package passfile

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	pb "github.com/roman-16/proton-cli/internal/service/pass/proto"
)

// 1Password exports either a .1pux, which is a zip holding one JSON document and
// the attachments, or a .1pif, which is one JSON object per line with separator
// lines between them.
//
// Both keep an item's fields in sections, each field a single-key object whose
// key says what kind of value it is.

const onePasswordEntrySeparator = "***"

type onePasswordFile struct {
	Accounts []struct {
		Vaults []struct {
			Attrs struct {
				Name string `json:"name"`
			} `json:"attrs"`
			Items []onePasswordItem `json:"items"`
		} `json:"vaults"`
	} `json:"accounts"`
}

type onePasswordItem struct {
	UUID         string              `json:"uuid"`
	CategoryUUID string              `json:"categoryUuid"`
	State        string              `json:"state"`
	CreatedAt    int64               `json:"createdAt"`
	UpdatedAt    int64               `json:"updatedAt"`
	Overview     onePasswordOverview `json:"overview"`
	Details      onePasswordDetails  `json:"details"`
}

type onePasswordOverview struct {
	Title string `json:"title"`
	URL   string `json:"url"`
	URLs  []struct {
		URL  string `json:"url"`
		Mode string `json:"mode"`
	} `json:"urls"`
}

type onePasswordDetails struct {
	NotesPlain  string               `json:"notesPlain"`
	Password    string               `json:"password"`
	Sections    []onePasswordSection `json:"sections"`
	LoginFields []struct {
		Value       string `json:"value"`
		Designation string `json:"designation"`
	} `json:"loginFields"`
	DocumentAttributes *onePasswordFileRef `json:"documentAttributes"`
}

type onePasswordSection struct {
	Title  string             `json:"title"`
	Name   string             `json:"name"`
	Fields []onePasswordField `json:"fields"`
}

type onePasswordField struct {
	Title string                     `json:"title"`
	ID    string                     `json:"id"`
	Value map[string]json.RawMessage `json:"value"`
}

type onePasswordFileRef struct {
	DocumentID string `json:"documentId"`
	FileName   string `json:"fileName"`
}

// onePasswordIdentityFields are the fields of an identity, which 1Password keeps
// in three sections it always names the same way.
var onePasswordIdentityFields = map[string]func(*pb.ItemIdentity) *string{
	"address1":  func(i *pb.ItemIdentity) *string { return &i.StreetAddress },
	"birthdate": func(i *pb.ItemIdentity) *string { return &i.Birthdate },
	"busphone":  func(i *pb.ItemIdentity) *string { return &i.WorkPhoneNumber },
	"cellphone": func(i *pb.ItemIdentity) *string { return &i.SecondPhoneNumber },
	"city":      func(i *pb.ItemIdentity) *string { return &i.City },
	"company":   func(i *pb.ItemIdentity) *string { return &i.Organization },
	"country":   func(i *pb.ItemIdentity) *string { return &i.CountryOrRegion },
	"defphone":  func(i *pb.ItemIdentity) *string { return &i.PhoneNumber },
	"email":     func(i *pb.ItemIdentity) *string { return &i.Email },
	"firstname": func(i *pb.ItemIdentity) *string { return &i.FirstName },
	"gender":    func(i *pb.ItemIdentity) *string { return &i.Gender },
	"jobtitle":  func(i *pb.ItemIdentity) *string { return &i.JobTitle },
	"lastname":  func(i *pb.ItemIdentity) *string { return &i.LastName },
	"state":     func(i *pb.ItemIdentity) *string { return &i.StateOrProvince },
	"street":    func(i *pb.ItemIdentity) *string { return &i.StreetAddress },
	"website":   func(i *pb.ItemIdentity) *string { return &i.Website },
	"yahoo":     func(i *pb.ItemIdentity) *string { return &i.Yahoo },
	"zip":       func(i *pb.ItemIdentity) *string { return &i.ZipOrPostalCode },
}

// onePasswordWiFiFields are the fields of a network, which are not in a section
// of their own and so are found by name wherever they sit.
var onePasswordWiFiFields = []string{"network_name", "wireless_password", "wireless_security"}

func readOnePassword(in source) (*Document, error) {
	z, err := zip.OpenReader(in.path)
	if err != nil {
		raw, err := os.ReadFile(in.path)
		if err != nil {
			return nil, err
		}
		return readOnePasswordPIF(in, raw, nil)
	}
	doc, err := readOnePasswordArchive(in, z)
	if err != nil {
		_ = z.Close()
		return nil, err
	}
	return doc, nil
}

// readOnePasswordArchive reads a .1pux, or a zip holding a .1pif export and the
// files that went with it.
func readOnePasswordArchive(in source, z *zip.ReadCloser) (*Document, error) {
	files := map[string]File{}
	for _, f := range z.File {
		if f.FileInfo().IsDir() {
			continue
		}
		files[strings.TrimPrefix(f.Name, "/")] = File{
			Name: onePasswordFileName(f.Name),
			Size: int64(f.UncompressedSize64),
			Open: func() (io.ReadCloser, error) { return f.Open() },
		}
	}

	var doc *Document
	if data := zipEntry(z, "export.data"); data != nil {
		raw, err := readZipEntry(in, data)
		if err != nil {
			return nil, err
		}
		if doc, err = readOnePasswordPUX(in, raw, files); err != nil {
			return nil, err
		}
	} else {
		pif := onePasswordPIFEntry(z)
		if pif == nil {
			return nil, notThisFormat(in, "1password")
		}
		raw, err := readZipEntry(in, pif)
		if err != nil {
			return nil, err
		}
		if doc, err = readOnePasswordPIF(in, raw, files); err != nil {
			return nil, err
		}
	}
	doc.closers = append(doc.closers, z)
	return doc, nil
}

func onePasswordPIFEntry(z *zip.ReadCloser) *zip.File {
	for _, f := range z.File {
		if strings.HasSuffix(f.Name, ".1pif") || strings.HasSuffix(f.Name, "/data.1pif") {
			return f
		}
	}
	return nil
}

func readOnePasswordPUX(in source, raw []byte, files map[string]File) (*Document, error) {
	var file onePasswordFile
	if err := json.Unmarshal(raw, &file); err != nil || len(file.Accounts) == 0 {
		return nil, notThisFormat(in, "1password")
	}
	doc := &Document{}
	for _, account := range file.Accounts {
		for _, vault := range account.Vaults {
			var items []Entry
			for _, record := range vault.Items {
				entry := onePasswordEntry(record)
				entry.Files = onePasswordFiles(doc, record, files)
				items = append(items, entry)
			}
			doc.vault(vault.Attrs.Name, items)
		}
	}
	return doc, nil
}

func onePasswordEntry(record onePasswordItem) Entry {
	fields, totp := onePasswordFields(record.Details.Sections)
	in := item{
		Name: record.Overview.Title,
		Note: record.Details.NotesPlain,
		// 1Password calls an item you put away archived, which is what Pass
		// calls the trash.
		Trashed:    record.State == "archived",
		CreateTime: record.CreatedAt,
		ModifyTime: record.UpdatedAt,
		Fields:     fields,
		TOTP:       totp,
	}
	switch record.CategoryUUID {
	case "001", "005":
		if record.CategoryUUID == "001" {
			in.Email, in.Username = identifier(onePasswordLoginField(record, "username"))
			in.Password = onePasswordLoginField(record, "password")
		} else {
			in.Password = record.Details.Password
		}
		in.URLs, in.Modes = onePasswordURLs(record.Overview)
		return in.login()
	case "003":
		return in.note()
	case "002":
		card := onePasswordSectionFields(record.Details.Sections)
		in.Holder = onePasswordText(card["cardholder"])
		in.Number = onePasswordText(card["ccnum"])
		in.CVV = onePasswordText(card["cvv"])
		in.PIN = onePasswordText(card["pin"])
		in.Expiry = onePasswordMonthYear(card["expiry"])
		return in.card()
	case "004":
		in.Identity = onePasswordIdentity(record.Details.Sections)
		in.Fields = nil
		return in.identity()
	case "114":
		key := onePasswordSSHKey(record.Details.Sections)
		in.PrivateKey, in.PublicKey = key["privateKey"], key["publicKey"]
		var extra []Field
		for _, f := range []Field{
			{Name: "Key fingerprint", Value: key["fingerprint"], Hidden: true},
			{Name: "Key type", Value: key["keyType"]},
		} {
			if f.Value != "" {
				extra = append(extra, f)
			}
		}
		in.Sections = section("OpenSSH", extra, in.Name)
		return in.sshKey()
	case "109":
		wifi := onePasswordSectionFields(record.Details.Sections)
		in.SSID = onePasswordText(wifi["network_name"])
		in.Password = onePasswordText(wifi["wireless_password"])
		in.Security = onePasswordWiFiSecurity(onePasswordText(wifi["wireless_security"]))
		return in.wifi()
	default:
		if len(record.Details.LoginFields) > 0 {
			in.Email, in.Username = identifier(onePasswordLoginField(record, "username"))
			in.Password = onePasswordLoginField(record, "password")
			in.URLs, in.Modes = onePasswordURLs(record.Overview)
			return in.login()
		}
		in.Sections = onePasswordSections(record.Details.Sections)
		in.Fields = nil
		return in.custom()
	}
}

// onePasswordURLs are the addresses an item carries, the first of them the one
// 1Password calls primary.
func onePasswordURLs(overview onePasswordOverview) ([]string, map[string]pb.AutofillUrl_Mode) {
	modes := map[string]pb.AutofillUrl_Mode{}
	var urls []string
	for _, u := range overview.URLs {
		urls = append(urls, u.URL)
		switch u.Mode {
		case "host":
			modes[u.URL] = pb.AutofillUrl_Exact
		case "never":
			modes[u.URL] = pb.AutofillUrl_Never
		}
	}
	if primary := overview.URL; primary != "" && !slices.Contains(urls, primary) {
		urls = append([]string{primary}, urls...)
	}
	return urls, modes
}

func onePasswordLoginField(record onePasswordItem, designation string) string {
	var out string
	for _, f := range record.Details.LoginFields {
		if f.Designation == designation {
			out = f.Value
		}
	}
	return out
}

// onePasswordFields turns an item's sections into custom fields, and picks out
// the one-time code, which Pass keeps on the item itself.
func onePasswordFields(sections []onePasswordSection) ([]Field, string) {
	var out []Field
	var totp string
	for _, s := range sections {
		for _, f := range s.Fields {
			field, kind := onePasswordFieldOf(f)
			switch {
			case kind == "":
				continue
			case kind == "totp" && totp == "":
				totp = field.Value
			default:
				out = append(out, field)
			}
		}
	}
	return out, totp
}

// onePasswordSections keeps the item's own grouping, which is what a custom item
// is made of.
func onePasswordSections(sections []onePasswordSection) []*pb.CustomSection {
	var out []*pb.CustomSection
	for _, s := range sections {
		var fields []Field
		for _, f := range s.Fields {
			if field, kind := onePasswordFieldOf(f); kind != "" {
				fields = append(fields, field)
			}
		}
		name := s.Title
		if name == "" {
			name = "Untitled"
		}
		out = append(out, section(name, fields, "")...)
	}
	return out
}

// onePasswordFieldOf reads one field: its single key says what kind of value it
// holds, and a few kinds are not fields at all.
func onePasswordFieldOf(f onePasswordField) (Field, string) {
	for _, key := range sortedKeys(f.Value) {
		if slices.Contains(onePasswordWiFiFields, f.ID) || key == "file" || key == "sshKey" {
			return Field{}, ""
		}
		name, value := f.Title, onePasswordValue(key, f.Value[key])
		switch key {
		case "totp":
			return Field{Name: named(name, "", "TOTP"), Value: value, TOTP: true}, "totp"
		case "concealed", "creditCardNumber":
			return Field{Name: named(name, "", "Hidden"), Value: value, Hidden: true}, "hidden"
		default:
			return Field{Name: named(name, "", "Text"), Value: value}, "text"
		}
	}
	return Field{}, ""
}

// onePasswordValue renders whatever a field holds as text.
func onePasswordValue(key string, raw json.RawMessage) string {
	switch key {
	case "date":
		var epoch int64
		if err := json.Unmarshal(raw, &epoch); err != nil {
			return ""
		}
		return time.Unix(epoch, 0).UTC().Format("2006-01-02")
	case "monthYear":
		return onePasswordMonthYear(raw)
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text
	}
	var number json.Number
	if err := json.Unmarshal(raw, &number); err == nil {
		return number.String()
	}
	// A value 1Password nests one level deeper, of which the first entry is the
	// one it shows.
	nested := map[string]json.RawMessage{}
	if err := json.Unmarshal(raw, &nested); err != nil {
		return ""
	}
	for _, key := range sortedKeys(nested) {
		var text string
		if err := json.Unmarshal(nested[key], &text); err == nil {
			return text
		}
	}
	return ""
}

// onePasswordMonthYear turns the YYYYMM 1Password writes into the MMYYYY Pass
// stores.
func onePasswordMonthYear(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var n int
	if err := json.Unmarshal(raw, &n); err != nil {
		var text string
		if err := json.Unmarshal(raw, &text); err != nil {
			return ""
		}
		if n, err = strconv.Atoi(text); err != nil {
			return ""
		}
	}
	value := strconv.Itoa(n)
	if len(value) != 6 {
		return ""
	}
	return value[4:6] + value[0:4]
}

func onePasswordText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text
	}
	return onePasswordValue("", raw)
}

// onePasswordSectionFields is every field of every section, by its id, which is
// how the kinds that have fixed fields find them.
func onePasswordSectionFields(sections []onePasswordSection) map[string]json.RawMessage {
	out := map[string]json.RawMessage{}
	for _, s := range sections {
		for _, f := range s.Fields {
			for _, key := range sortedKeys(f.Value) {
				if _, taken := out[f.ID]; !taken {
					out[f.ID] = f.Value[key]
				}
			}
		}
	}
	return out
}

func onePasswordSSHKey(sections []onePasswordSection) map[string]string {
	out := map[string]string{}
	for _, s := range sections {
		for _, f := range s.Fields {
			raw, ok := f.Value["sshKey"]
			if !ok {
				continue
			}
			var key struct {
				PrivateKey string `json:"privateKey"`
				Metadata   struct {
					Fingerprint string `json:"fingerprint"`
					KeyType     string `json:"keyType"`
					PrivateKey  string `json:"privateKey"`
					PublicKey   string `json:"publicKey"`
				} `json:"metadata"`
			}
			if err := json.Unmarshal(raw, &key); err != nil {
				continue
			}
			out["privateKey"] = named(key.Metadata.PrivateKey, key.PrivateKey, "")
			out["publicKey"] = key.Metadata.PublicKey
			out["fingerprint"] = key.Metadata.Fingerprint
			out["keyType"] = key.Metadata.KeyType
		}
	}
	return out
}

func onePasswordIdentity(sections []onePasswordSection) *pb.ItemIdentity {
	idn := &pb.ItemIdentity{}
	for _, s := range sections {
		if !slices.Contains([]string{"name", "address", "internet"}, s.Name) {
			continue
		}
		for _, f := range s.Fields {
			if address, ok := f.Value["address"]; ok {
				onePasswordAddress(idn, address)
				continue
			}
			at, ok := onePasswordIdentityFields[f.ID]
			if !ok {
				continue
			}
			for _, key := range sortedKeys(f.Value) {
				*at(idn) = onePasswordValue(key, f.Value[key])
			}
		}
	}
	return idn
}

func onePasswordAddress(idn *pb.ItemIdentity, raw json.RawMessage) {
	parts := map[string]string{}
	if err := json.Unmarshal(raw, &parts); err != nil {
		return
	}
	for key, value := range parts {
		if at, ok := onePasswordIdentityFields[key]; ok {
			*at(idn) = value
		}
	}
}

func onePasswordWiFiSecurity(value string) pb.WifiSecurity {
	switch value {
	case "wpa3p", "wpa3e":
		return pb.WifiSecurity_WPA3
	case "wpa2p", "wpa2e":
		return pb.WifiSecurity_WPA2
	case "wpa":
		return pb.WifiSecurity_WPA
	case "wep":
		return pb.WifiSecurity_WEP
	}
	return pb.WifiSecurity_UnspecifiedWifiSecurity
}

// onePasswordFiles are the attachments an item points at, which sit under files/
// named after the item's own reference to them.
func onePasswordFiles(doc *Document, record onePasswordItem, files map[string]File) []File {
	var out []File
	refs := make([]*onePasswordFileRef, 0, 1)
	if record.Details.DocumentAttributes != nil {
		refs = append(refs, record.Details.DocumentAttributes)
	}
	for _, s := range record.Details.Sections {
		for _, f := range s.Fields {
			raw, ok := f.Value["file"]
			if !ok {
				continue
			}
			var ref onePasswordFileRef
			if err := json.Unmarshal(raw, &ref); err == nil && ref.DocumentID != "" {
				refs = append(refs, &ref)
			}
		}
	}
	for _, ref := range refs {
		entry := fmt.Sprintf("files/%s__%s", ref.DocumentID, ref.FileName)
		file, ok := files[entry]
		if !ok {
			doc.SkippedFiles = append(doc.SkippedFiles, SkippedFile{
				Name: ref.FileName, Item: record.Overview.Title,
				Reason: "the export does not hold it",
			})
			continue
		}
		out = append(out, file)
	}
	return out
}

// onePasswordFileName is the name a file had before the export put the item's
// reference in front of it.
func onePasswordFileName(path string) string {
	name := zipBase(path)
	if _, rest, found := strings.Cut(name, "__"); found {
		return rest
	}
	return name
}

// readOnePasswordPIF reads the older export, which is one item per line with
// lines of asterisks between them.
func readOnePasswordPIF(in source, raw []byte, files map[string]File) (*Document, error) {
	var records []onePasswordLegacyItem
	for _, line := range bytes.Split(stripBOM(raw), []byte("\n")) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 || bytes.HasPrefix(line, []byte(onePasswordEntrySeparator)) {
			continue
		}
		var record onePasswordLegacyItem
		// A line that parses but carries none of what every item has is a file
		// that happens to be JSON rather than one of these.
		if err := json.Unmarshal(line, &record); err != nil || record.TypeName == "" {
			continue
		}
		records = append(records, record)
	}
	if len(records) == 0 {
		return nil, notThisFormat(in, "1password")
	}
	doc := &Document{}
	var items []Entry
	for _, record := range records {
		entry := onePasswordLegacyEntry(record)
		for _, f := range files {
			if strings.HasPrefix(f.Name, record.UUID) {
				entry.Files = append(entry.Files, f)
			}
		}
		items = append(items, entry)
	}
	doc.vault("", items)
	return doc, nil
}

type onePasswordLegacyItem struct {
	UUID      string `json:"uuid"`
	TypeName  string `json:"typeName"`
	Title     string `json:"title"`
	CreatedAt int64  `json:"createdAt"`
	UpdatedAt int64  `json:"updatedAt"`
	Secure    struct {
		NotesPlain string `json:"notesPlain"`
		Password   string `json:"password"`
		Cardholder string `json:"cardholder"`
		CCNum      string `json:"ccnum"`
		CVV        string `json:"cvv"`
		PIN        string `json:"pin"`
		ExpiryMM   int    `json:"expiry_mm"`
		ExpiryYY   int    `json:"expiry_yy"`
		Fields     []struct {
			Value       string `json:"value"`
			Designation string `json:"designation"`
		} `json:"fields"`
		URLs []struct {
			URL string `json:"url"`
		} `json:"URLs"`
		Sections       []onePasswordLegacySection `json:"sections"`
		UnknownDetails struct {
			Sections []onePasswordLegacySection `json:"sections"`
		} `json:"unknown_details"`
	} `json:"secureContents"`
}

type onePasswordLegacySection struct {
	Title  string `json:"title"`
	Name   string `json:"name"`
	Fields []struct {
		Kind  string          `json:"k"`
		Name  string          `json:"n"`
		Title string          `json:"t"`
		Value json.RawMessage `json:"v"`
	} `json:"fields"`
}

func onePasswordLegacyEntry(record onePasswordLegacyItem) Entry {
	fields, totp := onePasswordLegacyFields(record.Secure.Sections)
	unknown, _ := onePasswordLegacyFields(record.Secure.UnknownDetails.Sections)
	in := item{
		Name: record.Title, Note: record.Secure.NotesPlain,
		CreateTime: record.CreatedAt, ModifyTime: record.UpdatedAt,
		Fields: fields, TOTP: totp,
	}
	for _, u := range record.Secure.URLs {
		in.URLs = append(in.URLs, u.URL)
	}
	switch record.TypeName {
	case "webforms.WebForm":
		in.Email, in.Username = identifier(onePasswordLegacyLogin(record, "username"))
		in.Password = onePasswordLegacyLogin(record, "password")
		return in.login()
	case "passwords.Password":
		in.Password = record.Secure.Password
		return in.login()
	case "securenotes.SecureNote":
		in.URLs = nil
		return in.note()
	case "wallet.financial.CreditCard":
		in.Holder, in.Number = record.Secure.Cardholder, record.Secure.CCNum
		in.CVV, in.PIN = record.Secure.CVV, record.Secure.PIN
		if record.Secure.ExpiryMM > 0 && record.Secure.ExpiryYY > 0 {
			in.Expiry = fmt.Sprintf("%02d%d", record.Secure.ExpiryMM, record.Secure.ExpiryYY)
		}
		return in.card()
	case "identities.Identity":
		in.Identity = onePasswordLegacyIdentity(record.Secure.Sections)
		in.Fields = nil
		return in.identity()
	case "wallet.computer.Router":
		wifi := onePasswordLegacyByName(record.Secure.Sections)
		in.SSID = onePasswordText(wifi["network_name"])
		in.Password = onePasswordText(wifi["wireless_password"])
		in.Security = onePasswordWiFiSecurity(onePasswordText(wifi["wireless_security"]))
		in.Fields = append(unknown, fields...)
		return in.wifi()
	case "114":
		keys := onePasswordLegacyByName(record.Secure.UnknownDetails.Sections)
		in.PrivateKey = onePasswordText(keys["sshKey-privateKey"])
		in.PublicKey = onePasswordText(keys["sshKey-publicKey"])
		in.Fields = append(unknown, fields...)
		return in.sshKey()
	default:
		in.Fields = append(unknown, fields...)
		return in.custom()
	}
}

func onePasswordLegacyLogin(record onePasswordLegacyItem, designation string) string {
	for _, f := range record.Secure.Fields {
		if f.Designation == designation {
			return f.Value
		}
	}
	return ""
}

func onePasswordLegacyFields(sections []onePasswordLegacySection) ([]Field, string) {
	var out []Field
	var totp string
	for _, s := range sections {
		for _, f := range s.Fields {
			if slices.Contains(onePasswordWiFiFields, f.Name) || slices.Contains([]string{"sshKey-privateKey", "sshKey-publicKey"}, f.Name) {
				continue
			}
			value := onePasswordValue(f.Kind, f.Value)
			switch {
			case f.Kind == "concealed" && strings.HasPrefix(f.Name, "TOTP"):
				if totp == "" {
					totp = value
				}
			case f.Kind == "concealed":
				out = append(out, Field{Name: named(f.Title, "", "Hidden"), Value: value, Hidden: true})
			default:
				out = append(out, Field{Name: named(f.Title, "", "Text"), Value: value})
			}
		}
	}
	return out, totp
}

func onePasswordLegacyByName(sections []onePasswordLegacySection) map[string]json.RawMessage {
	out := map[string]json.RawMessage{}
	for _, s := range sections {
		for _, f := range s.Fields {
			if _, taken := out[f.Name]; !taken {
				out[f.Name] = f.Value
			}
		}
	}
	return out
}

func onePasswordLegacyIdentity(sections []onePasswordLegacySection) *pb.ItemIdentity {
	idn := &pb.ItemIdentity{}
	for _, s := range sections {
		if !slices.Contains([]string{"name", "address", "internet"}, s.Name) {
			continue
		}
		for _, f := range s.Fields {
			if f.Kind == "address" {
				onePasswordAddress(idn, f.Value)
				continue
			}
			if at, ok := onePasswordIdentityFields[f.Name]; ok {
				*at(idn) = onePasswordValue(f.Kind, f.Value)
			}
		}
	}
	return idn
}
