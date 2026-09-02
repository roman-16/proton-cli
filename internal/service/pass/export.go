package pass

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

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

// archiveDir is the folder inside the zip. Proton names it after the app, and
// its importer looks for exactly this path.
const archiveDir = "Proton Pass"

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
	Files                []string     `json:"files,omitempty"`
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

// exportJSON renders a protobuf message the way Proton Pass does: its own JSON
// encoding, with the fields it has no value for written empty rather than
// dropped.
var exportJSON = protojson.MarshalOptions{EmitUnpopulated: true, UseProtoNames: false}

// Export gathers the vaults this account owns, and their items, into the
// document Proton Pass reads.
//
// What somebody else shared is theirs to back up: it stays out, as it does in
// Proton's own export, so restoring this file cannot turn a vault you were let
// into to a second copy of it under your name. Skipped says how many were left
// out, because an archive quietly smaller than the account is worth a word.
func (s *Service) Export(ctx context.Context, userID string) (doc *ExportDocument, skipped int, err error) {
	vaults, err := s.VaultsList(ctx)
	if err != nil {
		return nil, 0, err
	}
	items, err := s.itemsFull(ctx, "")
	if err != nil {
		return nil, 0, err
	}

	doc = &ExportDocument{
		UserID:  userID,
		Vaults:  make(map[string]*ExportedVault, len(vaults)),
		Version: exportVersion,
	}
	for _, v := range vaults {
		if !v.Owner {
			skipped++
			continue
		}
		doc.Vaults[v.ShareID] = &ExportedVault{
			Name: v.Name, Description: v.Description,
			Display: ExportedDisplay{Color: v.Color, Icon: v.Icon},
			Items:   []ExportedItem{},
		}
	}
	for _, it := range items {
		vault, ok := doc.Vaults[it.ShareID]
		if !ok || it.raw == nil {
			continue
		}
		exported, err := exportItem(it)
		if err != nil {
			return nil, 0, err
		}
		vault.Items = append(vault.Items, *exported)
	}
	return doc, skipped, nil
}

// exportItem renders one item the way the app writes it.
func exportItem(it FullItem) (*ExportedItem, error) {
	content, kind, err := exportContent(it.raw)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", it.Name, err)
	}
	extra, err := exportList(it.raw.GetExtraFields())
	if err != nil {
		return nil, fmt.Errorf("%s: %w", it.Name, err)
	}
	out := &ExportedItem{
		ItemID: it.ItemID, ShareID: it.ShareID,
		Data: ExportedData{
			Metadata: ExportedMetadata{
				Name:     it.raw.GetMetadata().GetName(),
				Note:     it.raw.GetMetadata().GetNote(),
				ItemUUID: it.raw.GetMetadata().GetItemUuid(),
			},
			ExtraFields: extra, Type: kind, Content: content,
		},
		State: it.State, ContentFormatVersion: contentFormatVersion,
		CreateTime: it.CreateTime, ModifyTime: it.ModifyTime,
	}
	// An alias carries the address Proton gave it, and only an alias has one, so
	// the field is null rather than empty for everything else.
	if it.Alias != "" {
		alias := it.Alias
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
	return raw, exportTypeName(itemTypeName(it)), nil
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

// exportTypeName is what the document calls a kind of item.
//
// The CLI spells the two-word kinds with a hyphen because that is how a flag
// value reads; Proton's document spells them the way its own code does.
func exportTypeName(kind string) string {
	switch kind {
	case "credit-card":
		return "creditCard"
	case "ssh-key":
		return "sshKey"
	}
	return kind
}

// importTypeName is the inverse, so a document read back names the kinds the
// CLI does.
func importTypeName(kind string) string {
	switch kind {
	case "creditCard":
		return "credit-card"
	case "sshKey":
		return "ssh-key"
	}
	return kind
}

// Archive packs the document into the zip Proton Pass reads.
//
// With a passphrase the JSON is encrypted to it and stored as data.pgp, which is
// what Proton's importer looks for first; without one it is stored as plain
// data.json. Everything else about the archive is the same either way.
func Archive(doc *ExportDocument, passphrase string) ([]byte, error) {
	doc.Encrypted = passphrase != ""
	body, err := json.Marshal(doc)
	if err != nil {
		return nil, err
	}
	name := "data.json"
	if passphrase != "" {
		armored, err := encryptExport(body, passphrase)
		if err != nil {
			return nil, err
		}
		name, body = "data.pgp", []byte(armored)
	}

	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	w, err := z.Create(archiveDir + "/" + name)
	if err != nil {
		return nil, err
	}
	if _, err := w.Write(body); err != nil {
		return nil, err
	}
	if err := z.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Unarchive takes the document back out of an archive, or reads a bare JSON
// document as it is.
//
// askPassphrase is called only when the archive turns out to be encrypted, so a
// plain one is read without asking for anything.
func Unarchive(raw []byte, askPassphrase func() (string, error)) (*ExportDocument, error) {
	body, encrypted, err := archiveBody(raw)
	if err != nil {
		return nil, err
	}
	if encrypted {
		passphrase, err := askPassphrase()
		if err != nil {
			return nil, err
		}
		body, err = decryptExport(string(body), passphrase)
		if err != nil {
			return nil, fmt.Errorf("could not open the archive with that passphrase")
		}
	}
	var doc ExportDocument
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, fmt.Errorf("this is not a Proton Pass export")
	}
	if len(doc.Vaults) == 0 {
		return nil, fmt.Errorf("the export holds no vaults")
	}
	return &doc, nil
}

// archiveBody finds the document inside whatever was handed over: an archive
// written by Proton Pass, or the JSON on its own.
func archiveBody(raw []byte) (body []byte, encrypted bool, err error) {
	z, zipErr := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if zipErr != nil {
		// Not an archive. A bare document is either the JSON itself or the
		// encrypted form of it, and PGP says which in its first line.
		if bytes.HasPrefix(bytes.TrimSpace(raw), []byte("-----BEGIN PGP MESSAGE-----")) {
			return raw, true, nil
		}
		return raw, false, nil
	}
	for _, f := range z.File {
		name := f.Name
		if !strings.HasSuffix(name, "data.json") && !strings.HasSuffix(name, "data.pgp") {
			continue
		}
		r, err := f.Open()
		if err != nil {
			return nil, false, err
		}
		defer func() { _ = r.Close() }()
		var buf bytes.Buffer
		if _, err := buf.ReadFrom(r); err != nil {
			return nil, false, err
		}
		return buf.Bytes(), strings.HasSuffix(name, ".pgp"), nil
	}
	return nil, false, fmt.Errorf("the archive holds no Proton Pass export")
}
