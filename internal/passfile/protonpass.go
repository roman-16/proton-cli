package passfile

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	pb "github.com/roman-16/proton-cli/internal/service/pass/proto"
)

var protonCSVColumns = []string{"name", "url", "username", "password", "note", "totp"}

func readProton(in source) (*Document, error) {
	if z, err := zip.OpenReader(in.path); err == nil {
		doc, err := readProtonArchive(in, z)
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
	switch head := bytes.TrimSpace(raw); {
	case bytes.HasPrefix(head, []byte("-----BEGIN PGP MESSAGE-----")):
		body, err := decryptDocument(in, raw)
		if err != nil {
			return nil, err
		}
		return readProtonJSON(in, body, nil)
	case bytes.HasPrefix(head, []byte("{")):
		return readProtonJSON(in, raw, nil)
	default:
		return readProtonCSV(in, raw)
	}
}

func readProtonArchive(in source, z *zip.ReadCloser) (*Document, error) {
	document, files := archiveParts(z)
	if document == nil {
		return nil, notThisFormat(in, Proton)
	}
	body, err := readZipEntry(in, document)
	if err != nil {
		return nil, err
	}
	if strings.HasSuffix(document.Name, ".pgp") {
		if body, err = decryptDocument(in, body); err != nil {
			return nil, err
		}
	}
	doc, err := readProtonJSON(in, body, files)
	if err != nil {
		return nil, err
	}
	doc.closers = append(doc.closers, z)
	return doc, nil
}

func archiveParts(z *zip.ReadCloser) (document *zip.File, files map[string]*zip.File) {
	files = map[string]*zip.File{}
	for _, f := range z.File {
		switch {
		case strings.HasSuffix(f.Name, "data.json"), strings.HasSuffix(f.Name, "data.pgp"):
			if document == nil {
				document = f
			}
		default:
			if base := zipBase(f.Name); base != "" {
				files[base] = f
			}
		}
	}
	return document, files
}

func decryptDocument(in source, body []byte) ([]byte, error) {
	if in.passphrase == nil {
		return nil, errors.New("this export is encrypted")
	}
	passphrase, err := in.passphrase()
	if err != nil {
		return nil, err
	}
	out, err := decryptExport(string(body), passphrase)
	if err != nil {
		return nil, notWithThatPassphrase()
	}
	return out, nil
}

func readProtonJSON(in source, body []byte, files map[string]*zip.File) (*Document, error) {
	var export ExportDocument
	if err := json.Unmarshal(body, &export); err != nil {
		return nil, notThisFormat(in, Proton)
	}
	if len(export.Vaults) == 0 {
		return nil, notThisFormat(in, Proton)
	}

	doc := &Document{UserID: export.UserID}
	for _, shareID := range sortedShares(&export) {
		vault := export.Vaults[shareID]
		var items []Entry
		for _, item := range vault.Items {
			entry, err := protonEntry(item, export.Version)
			if err != nil {
				doc.skip(vault.Name, item.Data.Metadata.Name, err.Error())
				continue
			}
			entry.Files = doc.archived(item, files)
			items = append(items, *entry)
		}
		doc.vault(vault.Name, items)
	}
	return doc, nil
}

func (d *Document) archived(item ExportedItem, files map[string]*zip.File) []File {
	var out []File
	for _, entry := range item.Files {
		f, ok := files[entry]
		if !ok {
			d.SkippedFiles = append(d.SkippedFiles, SkippedFile{
				Name: ArchiveEntryName(entry), Item: item.Data.Metadata.Name,
				Reason: "the archive does not hold it",
			})
			continue
		}
		out = append(out, File{
			Name: ArchiveEntryName(f.Name),
			Size: int64(f.UncompressedSize64),
			Open: func() (io.ReadCloser, error) { return f.Open() },
		})
	}
	return out
}

func protonEntry(in ExportedItem, version string) (*Entry, error) {
	kind := CLIKind(in.Data.Type)
	content, err := protonContent(kind, in.Data.Content, version)
	if err != nil {
		return nil, err
	}
	item := &pb.Item{
		Metadata: &pb.Metadata{
			Name: in.Data.Metadata.Name, Note: in.Data.Metadata.Note,
			ItemUuid: in.Data.Metadata.ItemUUID,
		},
		Content: content,
	}
	if len(in.Data.ExtraFields) > 0 {
		var raws []json.RawMessage
		if err := json.Unmarshal(in.Data.ExtraFields, &raws); err != nil {
			return nil, fmt.Errorf("its custom fields could not be read")
		}
		for _, raw := range raws {
			var f pb.ExtraField
			if err := protojson.Unmarshal(raw, &f); err != nil {
				return nil, fmt.Errorf("one of its custom fields could not be read")
			}
			item.ExtraFields = append(item.ExtraFields, &f)
		}
	}
	out := &Entry{
		Item: item, Kind: kind, Name: in.Data.Metadata.Name,
		Trashed:    in.State == StateTrashed,
		CreateTime: in.CreateTime, ModifyTime: in.ModifyTime,
	}
	if in.AliasEmail != nil {
		out.AliasEmail = *in.AliasEmail
	}
	return out, nil
}

func protonContent(kind string, raw json.RawMessage, version string) (*pb.Content, error) {
	if kind == "login" {
		var err error
		if raw, err = loginBeforeEmailAndUsername(raw, version); err != nil {
			return nil, err
		}
	}
	content, err := DecodeContent(kind, raw)
	if err != nil {
		return nil, err
	}
	if login, ok := content.GetContent().(*pb.Content_Login); ok {
		if len(login.Login.GetAutofillUrls()) == 0 {
			SetLoginURLs(login.Login, defaultRules(login.Login.GetUrls()))
		}
	}
	return content, nil
}

func loginBeforeEmailAndUsername(raw json.RawMessage, version string) (json.RawMessage, error) {
	if !versionBefore(version, "1.18.0") {
		return raw, nil
	}
	fields := map[string]any{}
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, fmt.Errorf("its contents could not be read")
	}
	username, ok := fields["username"]
	if !ok {
		return raw, nil
	}
	delete(fields, "username")
	text, _ := username.(string)
	fields["itemEmail"], fields["itemUsername"] = text, ""
	out, err := json.Marshal(fields)
	if err != nil {
		return nil, fmt.Errorf("its contents could not be read")
	}
	return out, nil
}

func DecodeContent(kind string, raw json.RawMessage) (*pb.Content, error) {
	out := &pb.Content{}
	switch kind {
	case "login":
		m := &pb.ItemLogin{}
		out.Content = &pb.Content_Login{Login: m}
		return out, decodeInto(raw, m)
	case "note":
		m := &pb.ItemNote{}
		out.Content = &pb.Content_Note{Note: m}
		return out, decodeInto(raw, m)
	case "alias":
		m := &pb.ItemAlias{}
		out.Content = &pb.Content_Alias{Alias: m}
		return out, decodeInto(raw, m)
	case "credit-card":
		m := &pb.ItemCreditCard{}
		out.Content = &pb.Content_CreditCard{CreditCard: m}
		return out, decodeInto(raw, m)
	case "identity":
		m := &pb.ItemIdentity{}
		out.Content = &pb.Content_Identity{Identity: m}
		return out, decodeInto(raw, m)
	case "ssh-key":
		m := &pb.ItemSSHKey{}
		out.Content = &pb.Content_SshKey{SshKey: m}
		return out, decodeInto(raw, m)
	case "wifi":
		m := &pb.ItemWifi{}
		out.Content = &pb.Content_Wifi{Wifi: m}
		return out, decodeInto(raw, m)
	case "custom":
		m := &pb.ItemCustom{}
		out.Content = &pb.Content_Custom{Custom: m}
		return out, decodeInto(raw, m)
	}
	return nil, fmt.Errorf("%q is a kind of item this version does not know how to read", kind)
}

func decodeInto(raw json.RawMessage, into proto.Message) error {
	if len(raw) == 0 {
		return nil
	}
	opts := protojson.UnmarshalOptions{DiscardUnknown: true}
	if err := opts.Unmarshal(raw, into); err != nil {
		return fmt.Errorf("its contents could not be read")
	}
	return nil
}

func versionBefore(version, than string) bool {
	left, right := versionParts(version), versionParts(than)
	for i := range right {
		switch {
		case left[i] < right[i]:
			return true
		case left[i] > right[i]:
			return false
		}
	}
	return false
}

func versionParts(v string) [3]int {
	var out [3]int
	for i, part := range strings.SplitN(strings.TrimSpace(v), ".", 3) {
		if i > 2 {
			break
		}
		digits := strings.TrimLeft(part, "v")
		if end := strings.IndexFunc(digits, func(r rune) bool { return r < '0' || r > '9' }); end >= 0 {
			digits = digits[:end]
		}
		n, err := strconv.Atoi(digits)
		if err != nil {
			return out
		}
		out[i] = n
	}
	return out
}

func readProtonCSV(in source, raw []byte) (*Document, error) {
	t, err := readCSV(raw)
	if err != nil {
		return nil, notThisFormat(in, Proton)
	}
	if !t.has(protonCSVColumns...) {
		return nil, notInProtonColumns(in, t)
	}
	identifierIsEmail := !t.has("email")

	doc := &Document{}
	if w := t.warning(); w != "" {
		doc.Warnings = append(doc.Warnings, w)
	}
	vaults := newGrouped()
	for _, r := range t.rows {
		kind := r.get("type")
		if kind == "alias" {
			doc.skip(r.get("vault"), r.get("name"), "an alias cannot be read out of a CSV")
			continue
		}
		entry, err := protonCSVEntry(r, kind, identifierIsEmail)
		if err != nil {
			doc.skip(r.get("vault"), r.get("name"), err.Error())
			continue
		}
		vaults.add(r.get("vault"), *entry)
	}
	vaults.into(doc)
	return doc, nil
}

func protonCSVEntry(r row, kind string, identifierIsEmail bool) (*Entry, error) {
	in := item{
		Name:       r.get("name"),
		Note:       r.get("note"),
		CreateTime: r.number("createTime"),
		ModifyTime: r.number("modifyTime"),
	}
	switch CLIKind(kind) {
	case "credit-card":
		var fields cardFields
		if err := json.Unmarshal([]byte(r.get("note")), &fields); err != nil {
			return nil, fmt.Errorf("its fields could not be read")
		}
		in.Note = fields.Note
		in.Holder, in.Number = fields.CardholderName, fields.Number
		in.CVV, in.Expiry, in.PIN = fields.VerificationNumber, fields.ExpirationDate, fields.Pin
		entry := in.card()
		return &entry, nil
	case "identity":
		idn := &pb.ItemIdentity{}
		if err := decodeInto(json.RawMessage(r.get("note")), idn); err != nil {
			return nil, fmt.Errorf("its fields could not be read")
		}
		in.Identity = idn
		in.Note = noteOf(r.get("note"))
		entry := in.identity()
		return &entry, nil
	case "login", "":
		in.URLs, in.Modes = splitList(r.get("autofillUrls"), r.get("url"))
		in.Email, in.Username = r.get("email"), r.get("username")
		if identifierIsEmail {
			in.Email, in.Username = r.get("username"), ""
		}
		in.Password, in.TOTP = r.get("password"), r.get("totp")
		entry := in.login()
		return &entry, nil
	default:
		entry := in.note()
		return &entry, nil
	}
}

type cardFields struct {
	CardholderName     string `json:"cardholderName"`
	Number             string `json:"number"`
	VerificationNumber string `json:"verificationNumber"`
	ExpirationDate     string `json:"expirationDate"`
	Pin                string `json:"pin"`
	Note               string `json:"note"`
}

func noteOf(column string) string {
	var fields struct {
		Note string `json:"note"`
	}
	_ = json.Unmarshal([]byte(column), &fields)
	return fields.Note
}

func splitList(structured, plain string) ([]string, map[string]pb.AutofillUrl_Mode) {
	var urls []autofillURL
	if err := json.Unmarshal([]byte(structured), &urls); err == nil && len(urls) > 0 {
		out := make([]string, 0, len(urls))
		modes := make(map[string]pb.AutofillUrl_Mode, len(urls))
		for _, u := range urls {
			out = append(out, u.URL)
			modes[u.URL] = pb.AutofillUrl_Mode(u.Mode)
		}
		return out, modes
	}
	if plain == "" {
		return nil, nil
	}
	return strings.Split(plain, ", "), nil
}
