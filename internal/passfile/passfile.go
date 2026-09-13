// Package passfile reads and writes the files password managers export.
//
// A file from Proton Pass, Bitwarden, 1Password or any of the others becomes one
// shape - vaults holding items, each item the protobuf Pass stores - and the
// service layer lands that shape without knowing which program wrote it. The
// other direction is the same document rendered three ways: the archive Proton
// Pass reads back, the JSON inside it, and a CSV.
//
// Nothing here talks to Proton.
package passfile

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/roman-16/proton-cli/internal/errs"
	pb "github.com/roman-16/proton-cli/internal/service/pass/proto"
	"github.com/roman-16/proton-cli/internal/units"
)

// Document is a file's worth of items, in the shape they will be landed.
type Document struct {
	// UserID is the account the file came out of, which only a Proton Pass
	// export knows.
	UserID string
	Vaults []Vault
	// Skipped is what the file held and this could not read.
	Skipped []Skip
	// Warnings are things worth saying about the file itself rather than about
	// any one item.
	Warnings []string
	// SkippedFiles are the attachments the file named and does not hold.
	SkippedFiles []SkippedFile

	closers []io.Closer
}

// Vault is one group of items, named as the file named it.
type Vault struct {
	Name  string
	Items []Entry
}

// Entry is one item on its way in.
type Entry struct {
	Item       *pb.Item
	Kind       string
	Name       string
	Trashed    bool
	CreateTime int64
	ModifyTime int64
	// AliasEmail is the address an alias item stands for. Only Proton Pass
	// exports carry one.
	AliasEmail string
	Files      []File
}

// File is an attachment travelling with an item.
type File struct {
	Name string
	Size int64
	Open func() (io.ReadCloser, error)
}

// Skip is something the file held that will not be landed, and why.
type Skip struct {
	Name   string `json:"name,omitempty"`
	Vault  string `json:"vault,omitempty"`
	Reason string `json:"reason"`
}

// String names the item and what became of it.
func (s Skip) String() string {
	if s.Name == "" {
		return fmt.Sprintf("Skipped an item: %s.", s.Reason)
	}
	return fmt.Sprintf("Skipped %q: %s.", s.Name, s.Reason)
}

// SkippedFile is one attachment that will not travel with its item, and why.
//
// It is apart from Skip because an item whose file will not go is still an item
// that lands, and counting it as lost would be a wrong count.
type SkippedFile struct {
	Name   string `json:"name"`
	Item   string `json:"item"`
	Reason string `json:"reason"`
}

// String names the file and the item it belonged to.
func (s SkippedFile) String() string {
	return fmt.Sprintf("Skipped attachment %q of %q: %s.", s.Name, s.Item, s.Reason)
}

// Count is how many items the document holds.
func (d *Document) Count() int {
	var n int
	for _, v := range d.Vaults {
		n += len(v.Items)
	}
	return n
}

// Close releases whatever the document is still reading from.
func (d *Document) Close() error {
	var err error
	for _, c := range d.closers {
		if cerr := c.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}
	return err
}

func (d *Document) skip(vault, name, reason string) {
	d.Skipped = append(d.Skipped, Skip{Name: name, Vault: vault, Reason: reason})
}

// vault appends a vault, dropping one that turned out to hold nothing.
func (d *Document) vault(name string, items []Entry) {
	if len(items) == 0 {
		return
	}
	d.Vaults = append(d.Vaults, Vault{Name: name, Items: items})
}

// source is the file a reader was pointed at.
type source struct {
	path       string
	passphrase func() (string, error)
}

type reader func(source) (*Document, error)

// Proton is the format a file is read as when nothing else was named: the
// backups this CLI and the app itself write.
const Proton = "proton-pass"

// titles are the programs whose exports this reads, by the name the flag takes.
var titles = map[string]string{
	"1password":       "1Password",
	"apple-passwords": "Apple Passwords",
	"bitwarden":       "Bitwarden",
	"brave":           "Brave",
	"chrome":          "Chrome",
	"dashlane":        "Dashlane",
	"edge":            "Edge",
	"enpass":          "Enpass",
	"firefox":         "Firefox",
	"kaspersky":       "Kaspersky",
	"keepass":         "KeePass",
	"keeper":          "Keeper",
	"lastpass":        "LastPass",
	"nordpass":        "NordPass",
	Proton:            "Proton Pass",
	"roboform":        "RoboForm",
	"safari":          "Safari",
}

// readers are how each of them is read. Three of the browsers write the same
// file, and Apple Passwords writes Safari's.
func readers() map[string]reader {
	return map[string]reader{
		"1password":       readOnePassword,
		"apple-passwords": readSafari,
		"bitwarden":       readBitwarden,
		"brave":           readChromium,
		"chrome":          readChromium,
		"dashlane":        readDashlane,
		"edge":            readChromium,
		"enpass":          readEnpass,
		"firefox":         readFirefox,
		"kaspersky":       readKaspersky,
		"keepass":         readKeePass,
		"keeper":          readKeeper,
		"lastpass":        readLastPass,
		"nordpass":        readNordPass,
		Proton:            readProton,
		"roboform":        readRoboForm,
		"safari":          readSafari,
	}
}

// Formats are the names a file may be read as, for the flag that names one.
func Formats() []string {
	out := make([]string, 0, len(titles))
	for name := range titles {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// Title is what the program behind a format name calls itself.
func Title(name string) string { return titles[name] }

// Open reads path as the named format.
//
// askPassphrase is called only when the file turns out to be encrypted, so a
// plain one is read without asking for anything.
func Open(path, name string, askPassphrase func() (string, error)) (*Document, error) {
	read, ok := readers()[name]
	if !ok {
		return nil, errs.Problemf("%s is not a password manager this can read.", name)
	}
	doc, err := read(source{path: path, passphrase: askPassphrase})
	if err != nil {
		return nil, err
	}
	if doc.Count() == 0 && len(doc.Skipped) == 0 {
		_ = doc.Close()
		return nil, errs.Problemf("%s holds no items.", path)
	}
	return doc, nil
}

// notThisFormat is what a reader says when the file it was handed was written by
// something else.
func notThisFormat(in source, name string) error {
	p := errs.Problemf("%s is not a %s export.", in.path, titles[name])
	if name == Proton {
		return p.Hint("--manager names the password manager that wrote the file.")
	}
	return p
}

// tooLarge is what an archive holding a file this will not read whole says.
func tooLarge(in source) error {
	return errs.Problemf("%s holds a file over %s, which is more than this reads.",
		in.path, units.Size(maxEntry))
}

// notInProtonColumns is what a CSV that was written by something else gets, and
// it is the one refusal that has to point at the flag: every other format was
// named, and this one is what a file falls back to.
func notInProtonColumns(in source, t *table) error {
	missing := make([]string, 0, len(protonCSVColumns))
	for _, column := range protonCSVColumns {
		if !t.has(column) {
			missing = append(missing, column)
		}
	}
	return errs.Problemf("%s is not in Proton Pass's columns.", in.path).
		Hint("a Proton Pass CSV also has the columns " + strings.Join(missing, ", ") + ".").
		Hint("--manager names the password manager that wrote the file.")
}

// notWithThatPassphrase is what an encrypted export that would not open says.
func notWithThatPassphrase() error {
	return errs.Problemf("Could not open the export with that passphrase.")
}
