package passfile

import (
	"archive/zip"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	"github.com/ProtonMail/gopenpgp/v2/helper"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	pb "github.com/roman-16/proton-cli/internal/service/pass/proto"
)

// The file Proton Pass writes when you ask it for a backup, and reads when you
// give one back.
//
// It is a zip holding one JSON document, and the shape of that document is
// Proton's: a map of vaults keyed by share, each with its items, each item's
// content rendered from the same protobuf the app stores. Writing our own shape
// would make an export this tool alone could read, which is not what a backup is
// for - the point is that Proton Pass can read it, and that this can read what
// Proton Pass wrote.
//
// The content JSON is protobuf's own JSON encoding with nothing omitted, which
// is what the app produces: a field it has no value for is written empty rather
// than left out, and a reader that expects one finds it.

// archiveDir is the folder inside the zip, and archiveFiles the folder inside
// that one holding the attachments. Proton names them after the app, and its
// importer looks for exactly these paths.
const (
	archiveDir   = "Proton Pass"
	archiveFiles = archiveDir + "/files"
)

// exportVersion is what the document claims to be. Proton's importer compares it
// against the versions whose shape differed, so it has to be at least the one
// where the current shape settled.
const exportVersion = "1.31.0"

// ExportDocument is the JSON inside the archive.
type ExportDocument struct {
	Encrypted bool                      `json:"encrypted"`
	UserID    string                    `json:"userId"`
	Vaults    map[string]*ExportedVault `json:"vaults"`
	Version   string                    `json:"version"`
}

// ExportedVault is one vault and everything in it.
type ExportedVault struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Display     ExportedDisplay `json:"display"`
	Items       []ExportedItem  `json:"items"`
}

// ExportedDisplay is how the vault looks, as the numbers the protobuf stores
// rather than the ones a person is shown.
type ExportedDisplay struct {
	Color int `json:"color"`
	Icon  int `json:"icon"`
}

// ExportedItem is one item, with its content in Proton's own encoding.
type ExportedItem struct {
	ItemID               string       `json:"itemId"`
	ShareID              string       `json:"shareId"`
	Data                 ExportedData `json:"data"`
	State                int          `json:"state"`
	AliasEmail           *string      `json:"aliasEmail"`
	ContentFormatVersion int          `json:"contentFormatVersion"`
	CreateTime           int64        `json:"createTime"`
	ModifyTime           int64        `json:"modifyTime"`
	// Files are the item's attachments, named as they are inside the archive.
	// An item with none carries an empty list, which is what the app writes.
	Files []string `json:"files"`
}

// ExportedData is an item's decrypted content: what kind it is, what it is
// called, and the fields of that kind.
type ExportedData struct {
	Metadata    ExportedMetadata `json:"metadata"`
	ExtraFields json.RawMessage  `json:"extraFields"`
	Type        string           `json:"type"`
	Content     json.RawMessage  `json:"content"`
}

// ExportedMetadata is the name and note every item has.
type ExportedMetadata struct {
	Name     string `json:"name"`
	Note     string `json:"note"`
	ItemUUID string `json:"itemUuid"`
}

// StateTrashed is what the document says about an item sitting in the trash.
const StateTrashed = 2

// ContentFormatVersion is the version of the item protobuf this writes. It
// travels with every item, so a reader knows how to take the content apart.
const ContentFormatVersion = 7

// exportJSON renders a protobuf message the way Proton Pass does: its own JSON
// encoding, with the fields it has no value for written empty rather than
// dropped.
var exportJSON = protojson.MarshalOptions{EmitUnpopulated: true, UseProtoNames: false}

// StoredItem is one item as the account holds it, on its way into a document.
type StoredItem struct {
	ShareID    string
	ItemID     string
	Item       *pb.Item
	State      int
	Alias      string
	CreateTime int64
	ModifyTime int64
	Files      []string
}

// NewDocument is an empty export for an account.
func NewDocument(userID string) *ExportDocument {
	return &ExportDocument{
		UserID:  userID,
		Vaults:  map[string]*ExportedVault{},
		Version: exportVersion,
	}
}

// Count is how many items the document holds.
func (d *ExportDocument) Count() int {
	var n int
	for _, v := range d.Vaults {
		n += len(v.Items)
	}
	return n
}

// ExportItem renders one item the way the app writes it.
func ExportItem(in StoredItem) (*ExportedItem, error) {
	content, kind, err := exportContent(in.Item)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", in.Item.GetMetadata().GetName(), err)
	}
	extra, err := exportList(in.Item.GetExtraFields())
	if err != nil {
		return nil, fmt.Errorf("%s: %w", in.Item.GetMetadata().GetName(), err)
	}
	files := in.Files
	if files == nil {
		files = []string{}
	}
	out := &ExportedItem{
		ItemID: in.ItemID, ShareID: in.ShareID,
		Data: ExportedData{
			Metadata: ExportedMetadata{
				Name:     in.Item.GetMetadata().GetName(),
				Note:     in.Item.GetMetadata().GetNote(),
				ItemUUID: in.Item.GetMetadata().GetItemUuid(),
			},
			ExtraFields: extra, Type: kind, Content: content,
		},
		State: in.State, ContentFormatVersion: ContentFormatVersion,
		CreateTime: in.CreateTime, ModifyTime: in.ModifyTime,
		Files: files,
	}
	// An alias carries the address Proton gave it, and only an alias has one, so
	// the field is null rather than empty for everything else.
	if in.Alias != "" {
		alias := in.Alias
		out.AliasEmail = &alias
	}
	return out, nil
}

// exportContent renders the message for whichever kind of item this is, and the
// word the document calls that kind.
func exportContent(it *pb.Item) (json.RawMessage, string, error) {
	var msg proto.Message
	switch c := it.GetContent().GetContent().(type) {
	case *pb.Content_Login:
		msg = c.Login
	case *pb.Content_Note:
		msg = c.Note
	case *pb.Content_Alias:
		msg = c.Alias
	case *pb.Content_CreditCard:
		msg = c.CreditCard
	case *pb.Content_Identity:
		msg = c.Identity
	case *pb.Content_SshKey:
		msg = c.SshKey
	case *pb.Content_Wifi:
		msg = c.Wifi
	case *pb.Content_Custom:
		msg = c.Custom
	default:
		return nil, "", fmt.Errorf("this item is of a kind this version does not know how to write out")
	}
	raw, err := exportJSON.Marshal(msg)
	if err != nil {
		return nil, "", err
	}
	return raw, DocumentKind(KindOf(it)), nil
}

// exportList renders a list of extra fields, which is empty rather than null
// when there are none.
func exportList(fields []*pb.ExtraField) (json.RawMessage, error) {
	out := make([]json.RawMessage, 0, len(fields))
	for _, f := range fields {
		raw, err := exportJSON.Marshal(f)
		if err != nil {
			return nil, err
		}
		out = append(out, raw)
	}
	return json.Marshal(out)
}

// KindOf is what the CLI calls the kind of an item.
func KindOf(it *pb.Item) string {
	switch it.GetContent().GetContent().(type) {
	case *pb.Content_Login:
		return "login"
	case *pb.Content_Note:
		return "note"
	case *pb.Content_Alias:
		return "alias"
	case *pb.Content_CreditCard:
		return "credit-card"
	case *pb.Content_Identity:
		return "identity"
	case *pb.Content_SshKey:
		return "ssh-key"
	case *pb.Content_Wifi:
		return "wifi"
	case *pb.Content_Custom:
		return "custom"
	}
	return "unknown"
}

// DocumentKind is what the document calls a kind of item.
//
// The CLI spells the two-word kinds with a hyphen because that is how a flag
// value reads; Proton's document spells them the way its own code does.
func DocumentKind(kind string) string {
	switch kind {
	case "credit-card":
		return "creditCard"
	case "ssh-key":
		return "sshKey"
	}
	return kind
}

// CLIKind is the inverse, so a document read back names the kinds the CLI does.
func CLIKind(kind string) string {
	switch kind {
	case "creditCard":
		return "credit-card"
	case "sshKey":
		return "ssh-key"
	}
	return kind
}

// ArchiveFile is one attachment on its way into an archive: where it lands, and
// how to produce its bytes.
type ArchiveFile struct {
	Entry string
	Write func(io.Writer) error
}

// WriteArchive writes the zip Proton Pass reads.
//
// The attachments go in first and the document last, each file streamed into the
// archive rather than held whole, so an account with more attachments than
// memory still has a backup. An attachment that will not come stops the export:
// an archive quietly missing a file is worse than no archive.
//
// With a passphrase the JSON is encrypted to it and stored as data.pgp, which is
// what Proton's importer looks for first; without one it is stored as plain
// data.json. The attachments are not encrypted either way, as the app leaves
// them.
func WriteArchive(w io.Writer, doc *ExportDocument, files []ArchiveFile, passphrase string) error {
	z := zip.NewWriter(w)
	for _, f := range files {
		// An attachment is stored as it is. What people attach is mostly already
		// compressed, so squeezing it again would spend the time a large backup
		// has least of and save nothing.
		entry, err := z.CreateHeader(&zip.FileHeader{
			Name: archiveFiles + "/" + f.Entry, Method: zip.Store,
		})
		if err != nil {
			return err
		}
		if err := f.Write(entry); err != nil {
			return err
		}
	}
	name, body, err := documentBody(doc, passphrase)
	if err != nil {
		return err
	}
	entry, err := z.Create(archiveDir + "/" + name)
	if err != nil {
		return err
	}
	if _, err := entry.Write(body); err != nil {
		return err
	}
	return z.Close()
}

// WriteJSON writes the document on its own, as it sits inside the archive.
func WriteJSON(w io.Writer, doc *ExportDocument, passphrase string) error {
	_, body, err := documentBody(doc, passphrase)
	if err != nil {
		return err
	}
	if _, err := w.Write(body); err != nil {
		return err
	}
	_, err = w.Write([]byte("\n"))
	return err
}

// documentBody is what goes in the file, and what the file is called.
func documentBody(doc *ExportDocument, passphrase string) (string, []byte, error) {
	doc.Encrypted = passphrase != ""
	body, err := json.Marshal(doc)
	if err != nil {
		return "", nil, err
	}
	if passphrase == "" {
		return "data.json", body, nil
	}
	armored, err := encryptExport(body, passphrase)
	if err != nil {
		return "", nil, err
	}
	return "data.pgp", []byte(armored), nil
}

// csvColumns are Proton Pass's own, in its own order, so the app and this both
// read back what either of them wrote.
var csvColumns = []string{
	"type", "name", "url", "autofillUrls", "email", "username",
	"password", "note", "totp", "createTime", "modifyTime", "vault",
}

// WriteCSV writes the items as the spreadsheet Proton Pass exports.
//
// It is a flat row per item, so what does not fit a column is not in the file:
// custom fields, attachments, passkeys and an SSH item's keys are left behind.
func WriteCSV(w io.Writer, doc *ExportDocument) error {
	out := csv.NewWriter(w)
	if err := out.Write(csvColumns); err != nil {
		return err
	}
	for _, shareID := range sortedShares(doc) {
		vault := doc.Vaults[shareID]
		for _, item := range vault.Items {
			row, err := csvRow(item, vault.Name)
			if err != nil {
				return err
			}
			if err := out.Write(row); err != nil {
				return err
			}
		}
	}
	out.Flush()
	return out.Error()
}

func csvRow(item ExportedItem, vault string) ([]string, error) {
	kind := CLIKind(item.Data.Type)
	content, err := DecodeContent(kind, item.Data.Content)
	if err != nil {
		return nil, err
	}
	var urls, autofill, email, username, password, totp string
	switch c := content.GetContent().(type) {
	case *pb.Content_Login:
		urls = strings.Join(LoginURLs(c.Login), ", ")
		autofill, err = autofillJSON(c.Login)
		if err != nil {
			return nil, err
		}
		email, username = c.Login.GetItemEmail(), c.Login.GetItemUsername()
		password, totp = c.Login.GetPassword(), c.Login.GetTotpUri()
	case *pb.Content_Wifi:
		password = c.Wifi.GetPassword()
	}
	if item.AliasEmail != nil {
		email = *item.AliasEmail
	}
	note := item.Data.Metadata.Note
	if kind == "credit-card" || kind == "identity" {
		if note, err = contentJSON(item); err != nil {
			return nil, err
		}
	}
	return []string{
		item.Data.Type, item.Data.Metadata.Name, urls, autofill, email, username,
		password, note, totp,
		strconv.FormatInt(item.CreateTime, 10), strconv.FormatInt(item.ModifyTime, 10),
		vault,
	}, nil
}

// autofillURL is one address and the rule that decides when it fills, in the
// shape the app's own CSV carries.
type autofillURL struct {
	URL  string `json:"url"`
	Mode int    `json:"mode"`
}

func autofillJSON(l *pb.ItemLogin) (string, error) {
	urls := l.GetAutofillUrls()
	if len(urls) == 0 {
		for _, u := range l.GetUrls() {
			urls = append(urls, &pb.AutofillUrl{Url: u, Mode: pb.AutofillUrl_Default})
		}
	}
	if len(urls) == 0 {
		return "", nil
	}
	out := make([]autofillURL, 0, len(urls))
	for _, u := range urls {
		out = append(out, autofillURL{URL: u.GetUrl(), Mode: int(u.GetMode())})
	}
	raw, err := json.Marshal(out)
	return string(raw), err
}

// contentJSON is a card's or an identity's fields as one column, which is how
// the app's CSV carries the kinds with too many fields for columns of their own.
func contentJSON(item ExportedItem) (string, error) {
	fields := map[string]any{}
	if err := json.Unmarshal(item.Data.Content, &fields); err != nil {
		return "", err
	}
	fields["note"] = item.Data.Metadata.Note
	raw, err := json.Marshal(fields)
	return string(raw), err
}

// sortedShares returns the document's vaults in a settled order, so two exports
// of the same account are the same file.
func sortedShares(doc *ExportDocument) []string {
	out := make([]string, 0, len(doc.Vaults))
	for id := range doc.Vaults {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// Proton Pass encrypts an export to a passphrase rather than to a key, so a
// backup can be opened on a machine that has never held the account's keys.
func encryptExport(plain []byte, passphrase string) (string, error) {
	return helper.EncryptMessageWithPassword([]byte(passphrase), string(plain))
}

func decryptExport(armored, passphrase string) ([]byte, error) {
	out, err := helper.DecryptMessageWithPassword([]byte(passphrase), armored)
	if err != nil {
		return nil, err
	}
	return []byte(out), nil
}
