package mail

import (
	"context"
	"fmt"
	"hash/fnv"
	"log/slog"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/roman-16/proton-cli/internal/accent"
	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/roman-16/proton-cli/internal/proton"
	"github.com/roman-16/proton-cli/internal/units"
)

// Bringing another mailbox in, which Proton calls Easy Switch.
//
// Nothing here speaks IMAP. Proton is handed the host, the port and the
// credentials of the other mailbox and connects outward from its own machines,
// so what this does is start that job, read how far it has got, and stop, pick
// up or take back what it did. The password travels to Proton and is held there
// until the import is over.
//
// An importer is the connection and a report is what one leaves behind, and the
// two are separate listings with separate IDs. Both are imports to the person
// who started them, so both are read and answered as one collection.

// The providers an import can come from, as Proton numbers them. Only the first
// is one this CLI can start: the other two are an OAuth exchange with a browser
// in it, so they arrive here already running, started from Proton's own client.
const (
	providerIMAP    = 0
	providerGoogle  = 1
	providerOutlook = 2
)

// What Proton calls the mail half of an importer, in the two vocabularies it
// uses for it: the product a report is filed under, and the feature a start, a
// cancel or a resume names.
const (
	importProduct = "Mail"
	importFeature = "import_mail"
)

// The two ways an import stops by itself, as Proton numbers them.
const (
	importErrorConnection = 1
	importErrorQuota      = 2
)

// Where a rollback has got to, as Proton numbers it. The first two say whether
// an import can still be taken back; the last two are a state of their own,
// because an import being undone is no longer the import it was.
const (
	rollbackAvailable = 1
	rollbackRunning   = 2
	rollbackDone      = 3
)

// importSystemFolders are the destinations Proton names itself, which is what a
// source folder maps to when Proton recognises it. Nothing may be nested under
// one, which is why a child of one is renamed rather than filed inside it.
var importSystemFolders = []string{
	"Inbox", "All Drafts", "All Sent", "Trash", "Spam", "All Mail",
	"Almost All Mail", "Starred", "Archive", "Sent", "Drafts",
}

// importReserved are the names Proton keeps for itself, refused as the name of
// anything a person makes.
var importReserved = []string{"scheduled", "spam", "trash", "outbox", "snoozed"}

// Proton's own limits on what an import may ask for: how deep its folders go,
// and how long one name may be.
const (
	importMaxDepth    = 3
	importMaxNameSize = 100
)

// gmailIMAP is the one host whose folders are labels rather than folders.
//
// Gmail's IMAP is a view of its labels, so a message in three of them arrives in
// three folders; imported as folders it would land in one of them and lose the
// other two. Proton's own client maps them to labels for that reason, and
// recognises the case by the host, as this does.
const gmailIMAP = "imap.gmail.com"

// Import is one import of mail from another mailbox, running or finished.
type Import struct {
	ID string `json:"id"`
	// Account is the mailbox the mail comes from.
	Account  string `json:"account"`
	Provider string `json:"provider"`
	State    string `json:"state"`
	// Problem is why an import stopped, for the states that stopped for a
	// reason somebody can act on.
	Problem string `json:"problem,omitempty"`
	// Server is where Proton connects, for an import over IMAP.
	Server string `json:"server,omitempty"`
	// Processed and Total are messages, and Total is 0 until Proton has counted
	// them.
	Processed int `json:"processed"`
	Total     int `json:"total"`
	// Size is how much arrived, which Proton totals only once an import is over.
	Size    int64 `json:"size,omitempty"`
	Started int64 `json:"started,omitempty"`
	Ended   int64 `json:"ended,omitempty"`
	// Folders is the mapping an import is working through, folder by folder.
	Folders []ImportFolder `json:"folders"`
	// Finished says the import is over, which is what decides whether it can be
	// stopped or taken back.
	Finished bool `json:"finished"`
	// Undoable says Proton can still remove everything this import created.
	Undoable bool `json:"undoable"`
}

// ImportFolder is one folder of the other mailbox, and where its mail lands.
type ImportFolder struct {
	Source      string `json:"source"`
	Destination string `json:"destination"`
	Processed   int    `json:"processed"`
	Total       int    `json:"total"`
	Size        int64  `json:"size,omitempty"`
}

// ImportSpec is an import as somebody asks for one.
type ImportSpec struct {
	// Account, Password, Server and Port are the other mailbox.
	Account  string
	Password string
	Server   string
	Port     int
	// AllowSelfSigned accepts a certificate Proton cannot verify.
	AllowSelfSigned bool
	// Into is the address of this account the mail belongs to afterwards.
	Into Address
	// Label goes on every message that arrives.
	Label string
	// Skip are globs against the other mailbox's folder names. A folder they
	// match is left behind, and so is everything under it.
	Skip []string
	// After and Before bound what is brought over, as unix seconds. Zero is
	// unbounded.
	After, Before int64
}

// ImportTarget is where Proton would connect for an address.
type ImportTarget struct {
	Host string
	Port int
	// OAuthOnly says the provider wants an OAuth exchange rather than a
	// password, which is a browser and not a command line.
	OAuthOnly bool
}

// ImportServer is where Proton would connect for an address, which it knows for
// the providers it has met. A domain it does not know answers with nothing, and
// then the server has to be named.
func (s *Service) ImportServer(ctx context.Context, account string) (ImportTarget, error) {
	var r struct {
		Authentication struct {
			Sasl     string
			ImapHost string
			ImapPort int
		}
	}
	if err := s.C.Decode(ctx, proton.Request{
		Method: "GET", Path: "/importer/v1/mail/importers/authinfo",
		Query: url.Values{"Email": {account}}, Reads: true,
	}, &r); err != nil {
		return ImportTarget{}, err
	}
	a := r.Authentication
	return ImportTarget{Host: a.ImapHost, Port: a.ImapPort, OAuthOnly: strings.EqualFold(a.Sasl, "XOAUTH2")}, nil
}

// rawImporter is a connection to another mailbox, as Proton writes it.
type rawImporter struct {
	ID       string
	Account  string
	Provider int
	ImapHost string
	ImapPort string
	Active   map[string]rawImporterActive
}

type rawImporterActive struct {
	CreateTime int64
	State      int
	ErrorCode  int
	Processed  int
	Total      int
	Mapping    []rawImporterFolder
}

type rawImporterFolder struct {
	SourceFolder      string
	DestinationFolder string
	Processed         int
	Total             int
}

// rawReport is what an import leaves behind, as Proton writes it.
type rawReport struct {
	ID         string
	CreateTime int64
	EndTime    int64
	Provider   int
	Account    string
	TotalSize  int64
	Summary    map[string]rawReportSummary
}

type rawReportSummary struct {
	State         int
	TotalSize     int64
	RollbackState int
	NumMessages   int
}

// Imports is every import of mail this account has, newest first, whether it is
// running or over.
//
// The two come from separate listings, and both are asked for: an import a
// person started an hour ago and one they started last year are the same thing
// to them, and which of Proton's tables holds it is not something they chose.
func (s *Service) Imports(ctx context.Context) ([]Import, error) {
	running, err := s.importersList(ctx)
	if err != nil {
		return nil, err
	}
	finished, err := s.reportsList(ctx)
	if err != nil {
		return nil, err
	}
	out := append(running, finished...)
	sort.SliceStable(out, func(i, j int) bool { return importWhen(out[i]) > importWhen(out[j]) })
	return out, nil
}

// importWhen is the moment a listing orders an import by: when it ended, or
// when it started for one that has not.
func importWhen(i Import) int64 {
	if i.Ended > 0 {
		return i.Ended
	}
	return i.Started
}

func (s *Service) importersList(ctx context.Context) ([]Import, error) {
	var r struct{ Importers []rawImporter }
	if err := s.C.Decode(ctx, proton.Request{
		Method: "GET", Path: "/importer/v1/importers", Reads: true,
	}, &r); err != nil {
		return nil, err
	}
	var others int
	out := make([]Import, 0, len(r.Importers))
	for _, raw := range r.Importers {
		active, imports := raw.Active[importProduct]
		if !imports {
			others++
			continue
		}
		out = append(out, raw.imported(ctx, active))
	}
	if others > 0 {
		// Recorded and not counted: an importer carrying calendars or contacts
		// and no mail is not a mail import, so leaving it out of a listing of
		// mail imports hides nothing. The count is here because a person whose
		// import is missing from this listing is the one who reports it.
		slog.DebugContext(ctx, "importers left out of a mail listing", "count", others, "kind", "import")
	}
	return out, nil
}

func (r rawImporter) imported(ctx context.Context, active rawImporterActive) Import {
	i := Import{
		ID: r.ID, Account: r.Account, Provider: importProvider(ctx, r.Provider),
		State: importState(ctx, active.State, false), Problem: importProblem(active.ErrorCode),
		Processed: active.Processed, Total: active.Total, Started: active.CreateTime,
	}
	if r.ImapHost != "" {
		i.Server = r.ImapHost
		if r.ImapPort != "" {
			i.Server += ":" + r.ImapPort
		}
	}
	i.Folders = make([]ImportFolder, 0, len(active.Mapping))
	for _, f := range active.Mapping {
		i.Folders = append(i.Folders, ImportFolder{
			Source: f.SourceFolder, Destination: f.DestinationFolder,
			Processed: f.Processed, Total: f.Total,
		})
	}
	return i
}

func (s *Service) reportsList(ctx context.Context) ([]Import, error) {
	var r struct{ Reports []rawReport }
	if err := s.C.Decode(ctx, proton.Request{
		Method: "GET", Path: "/importer/v1/reports", Reads: true,
	}, &r); err != nil {
		return nil, err
	}
	var others int
	out := make([]Import, 0, len(r.Reports))
	for _, raw := range r.Reports {
		summary, imported := raw.Summary[importProduct]
		if !imported {
			others++
			continue
		}
		size := summary.TotalSize
		if size == 0 {
			size = raw.TotalSize
		}
		out = append(out, Import{
			ID: raw.ID, Account: raw.Account, Provider: importProvider(ctx, raw.Provider),
			State:     importFinishedState(ctx, summary),
			Folders:   []ImportFolder{},
			Processed: summary.NumMessages, Total: summary.NumMessages,
			Size: size, Started: raw.CreateTime, Ended: raw.EndTime,
			Finished: true, Undoable: summary.RollbackState == rollbackAvailable,
		})
	}
	if others > 0 {
		// Recorded and not counted, for the reason the importers listing says.
		slog.DebugContext(ctx, "reports left out of a mail listing", "count", others, "kind", "import")
	}
	return out, nil
}

func importProvider(ctx context.Context, provider int) string {
	switch provider {
	case providerIMAP:
		return "imap"
	case providerGoogle:
		return "google"
	case providerOutlook:
		return "outlook"
	}
	// Recorded and not counted: the number stands in its own place on the
	// screen, so the row says for itself that this build has no word for it.
	slog.DebugContext(ctx, "an import comes from a provider this build has no word for", "state", provider)
	return strconv.Itoa(provider)
}

// importState is Proton's numbered state as a word. running is what an importer
// that is still going says; a report says the same numbers about work that has
// stopped, so the two words that differ are told apart by it.
func importState(ctx context.Context, state int, finished bool) string {
	switch state {
	case 0:
		return "queued"
	case 1:
		return "importing"
	case 2:
		return "imported"
	case 3:
		return "failed"
	case 4:
		return "paused"
	case 5:
		if finished {
			return "cancelled"
		}
		return "cancelling"
	case 6:
		return "delayed"
	}
	// Recorded and not counted, for the reason importProvider says.
	slog.DebugContext(ctx, "an import is in a state this build has no word for", "state", state)
	return strconv.Itoa(state)
}

// importFinishedState is the state of an import that is over, where being taken
// back outranks how it ended: an import whose messages are being removed is no
// longer described by how it went.
func importFinishedState(ctx context.Context, summary rawReportSummary) string {
	switch summary.RollbackState {
	case rollbackRunning:
		return "undoing"
	case rollbackDone:
		return "undone"
	}
	return importState(ctx, summary.State, true)
}

// importProblem is why an import stopped, said to the person who can do
// something about it.
func importProblem(code int) string {
	switch code {
	case importErrorConnection:
		return "the other mailbox refused the connection"
	case importErrorQuota:
		return "this account is nearly out of storage"
	}
	return ""
}

// ImportCreate connects to the other mailbox and sets the import going.
//
// Three requests in order, which is what Proton's own client does and the only
// order that works: the credentials are handed over and validated, the folders
// they open are read back, and the work is started over the mapping they make.
// Nothing is imported by the first two, and nothing can list the folders without
// the first, which is why a dry run of this stops before all three.
func (s *Service) ImportCreate(ctx context.Context, spec ImportSpec) (Import, error) {
	var created struct{ ImporterID string }
	if err := s.C.Decode(ctx, proton.Request{
		Method: "POST", Path: "/importer/v1/importers",
		Body: map[string]any{
			importProduct: map[string]any{
				"Account":         spec.Account,
				"ImapHost":        spec.Server,
				"ImapPort":        spec.Port,
				"Sasl":            "PLAIN",
				"Code":            spec.Password,
				"AllowSelfSigned": boolToInt(spec.AllowSelfSigned),
			},
		},
	}, &created); err != nil {
		return Import{}, err
	}

	folders, err := s.importFolders(ctx, created.ImporterID, spec.Password)
	if err != nil {
		return Import{}, connectedButNotStarted(ctx, err)
	}
	mapping, err := importMapping(ctx, folders, spec)
	if err != nil {
		return Import{}, connectedButNotStarted(ctx, err)
	}

	payload := map[string]any{
		"AddressID":   spec.Into.ID,
		"Code":        spec.Password,
		"Mapping":     mapping.payload,
		"ImportLabel": map[string]any{"Name": spec.Label, "Color": accentFor(spec.Label), "Type": 1},
	}
	if spec.After > 0 {
		payload["StartTime"] = spec.After
	}
	if spec.Before > 0 {
		payload["EndTime"] = spec.Before
	}
	if err := s.C.Decode(ctx, proton.Request{
		Method: "POST", Path: "/importer/v1/importers/start",
		Body: map[string]any{"ImporterID": created.ImporterID, importProduct: payload},
	}, nil); err != nil {
		return Import{}, connectedButNotStarted(ctx, err)
	}

	return Import{
		ID: created.ImporterID, Account: spec.Account, Provider: "imap",
		State:   "queued",
		Server:  spec.Server + ":" + strconv.Itoa(spec.Port),
		Size:    mapping.size,
		Folders: mapping.folders,
		Started: time.Now().Unix(),
	}, nil
}

// connectedButNotStarted says what a failure after the connection leaves behind.
//
// Proton holds the other mailbox's password from the moment it connects, and an
// import that never started is on no list to be taken off. Nothing here can undo
// that, so the person is told rather than offered a remedy that does not exist.
func connectedButNotStarted(ctx context.Context, err error) error {
	slog.WarnContext(ctx, "The import did not start, and Proton holds the password it connected with. Change it at the other provider if that matters.")
	return err
}

// importFolders is what the other mailbox holds, as Proton found it.
func (s *Service) importFolders(ctx context.Context, id, password string) ([]rawImportFolder, error) {
	var r struct{ Folders []rawImportFolder }
	if err := s.C.Decode(ctx, proton.Request{
		Method: "GET", Path: "/importer/v1/mail/importers/" + id,
		Query: url.Values{"Code": {password}}, Reads: true,
	}, &r); err != nil {
		return nil, err
	}
	return r.Folders, nil
}

// ImportCancel stops an import. What it has already brought over stays.
func (s *Service) ImportCancel(ctx context.Context, id string) error {
	return s.C.Decode(ctx, proton.Request{
		Method: "PUT", Path: "/importer/v1/importers/cancel",
		Body: map[string]any{"ImporterID": id, "Features": []string{importFeature}},
	}, nil)
}

// ImportResume sets a paused import going again, with a password first where one
// was given: an import Proton paused because the other mailbox turned it away is
// one whose credentials have to be right before there is any point resuming.
func (s *Service) ImportResume(ctx context.Context, id, password string) error {
	if password != "" {
		if err := s.C.Decode(ctx, proton.Request{
			Method: "PUT", Path: "/importer/v1/importers/" + id,
			Body: map[string]any{importProduct: map[string]any{"Code": password, "Sasl": "PLAIN"}},
		}, nil); err != nil {
			return err
		}
	}
	return s.C.Decode(ctx, proton.Request{
		Method: "PUT", Path: "/importer/v1/importers/resume",
		Body: map[string]any{"ImporterID": id, "Features": []string{importFeature}},
	}, nil)
}

// ImportUndo has Proton remove every message, folder and label an import
// created.
func (s *Service) ImportUndo(ctx context.Context, id string) error {
	return s.C.Decode(ctx, proton.Request{
		Method: "POST", Path: "/importer/v1/reports/" + id + "/undo",
		Body: map[string]any{"Features": []string{importFeature}},
	}, nil)
}

// ImportForget removes the record of a finished import. The mail it brought over
// stays where it is.
func (s *Service) ImportForget(ctx context.Context, id string) error {
	return s.C.Decode(ctx, proton.Request{
		Method: "DELETE", Path: "/importer/v1/mail/importers/reports/" + id,
	}, nil)
}

// rawImportFolder is one folder of the other mailbox, as Proton found it.
type rawImportFolder struct {
	Source    string
	Separator string
	Size      int64
	Total     int
	Hierarchy []string
	// DestinationFolder is Proton's own answer for a folder it recognises: the
	// inbox of any mailbox is the inbox of this one.
	DestinationFolder string
	// DestinationCategory is a Gmail tab, which is a destination inside the
	// inbox rather than a folder beside it.
	DestinationCategory string
}

// mapped is an import's folders, ready to be sent and ready to be shown.
type mapped struct {
	payload []map[string]any
	folders []ImportFolder
	size    int64
}

// importMapping works out where each folder of the other mailbox lands.
//
// Proton answers for the folders it recognises, and its answer is taken: a
// mailbox's inbox is this mailbox's inbox whatever it is called over there. What
// is left is a folder of the same path, flattened to the three levels Proton
// allows and with any slash inside a name escaped, or - for Gmail, whose folders
// are labels - a label named after the path.
func importMapping(ctx context.Context, folders []rawImportFolder, spec ImportSpec) (mapped, error) {
	if len(folders) == 0 {
		return mapped{}, errs.Problemf("That mailbox holds no folders to import.")
	}
	labelling := strings.EqualFold(spec.Server, gmailIMAP)
	sources := make([]string, 0, len(folders))
	for _, f := range folders {
		sources = append(sources, f.Source)
	}

	var out mapped
	var left int
	for _, f := range folders {
		if f.Source == "" || skipped(f.Source, f.separator(), spec.Skip) {
			left++
			continue
		}
		p := protonPath(f, sources, labelling)
		if err := admissible(p, f.Source, labelling); err != nil {
			return mapped{}, err
		}
		destination, entry := importDestination(f, p, labelling)
		out.payload = append(out.payload, map[string]any{
			"Source": f.Source, "Destinations": destination,
		})
		out.folders = append(out.folders, ImportFolder{
			Source: f.Source, Destination: entry, Total: f.Total, Size: f.Size,
		})
		out.size += f.Size
	}
	if left > 0 {
		// Recorded and not counted: a folder is left out because the person
		// running this asked for it to be, and what is being imported is on the
		// screen for them to check it against.
		slog.DebugContext(ctx, "folders left out of an import", "count", left, "kind", "folder")
	}
	if len(out.payload) == 0 {
		return mapped{}, errs.Problemf("Nothing is left to import: every folder was skipped.").
			Hint("run it without --skip to see what is there")
	}
	return out, nil
}

// importDestination is where a folder's mail lands, as Proton is told it and as
// a person reads it.
func importDestination(f rawImportFolder, p []string, labelling bool) (map[string]any, string) {
	switch {
	case f.DestinationCategory != "":
		// A Gmail tab is a place inside the inbox, so the folder it belongs to
		// is the one Proton named, and the tab rides along with it.
		into := f.DestinationFolder
		if into == "" {
			into = "Inbox"
		}
		return map[string]any{"FolderPath": into, "Category": f.DestinationCategory}, into
	case f.DestinationFolder != "":
		return map[string]any{"FolderPath": f.DestinationFolder}, f.DestinationFolder
	case labelling:
		name := p[0]
		return map[string]any{
			"Labels": []map[string]any{{"Name": name, "Color": accentFor(name)}},
		}, name
	}
	escaped := make([]string, 0, len(p))
	for _, part := range p {
		escaped = append(escaped, strings.ReplaceAll(part, "/", `\/`))
	}
	into := strings.Join(escaped, "/")
	return map[string]any{"FolderPath": into}, into
}

// separator is what stands between the levels of this folder's name, which the
// other server decides and Proton passes on.
func (f rawImportFolder) separator() string {
	if f.Separator == "" {
		return "/"
	}
	return f.Separator
}

// skipped reports whether a folder was left out, which takes everything under it
// too: somebody who skips a folder means the folder, not its name.
func skipped(source, separator string, globs []string) bool {
	names := append([]string{source}, ancestors(source, separator)...)
	for _, glob := range globs {
		if glob = strings.TrimSpace(glob); glob == "" {
			continue
		}
		if matches(glob, names...) {
			return true
		}
	}
	return false
}

// ancestors is every folder a folder sits inside, so skipping a parent skips it.
func ancestors(source, separator string) []string {
	parts := strings.Split(source, separator)
	out := make([]string, 0, len(parts))
	for i := 1; i < len(parts); i++ {
		out = append(out, strings.Join(parts[:i], separator))
	}
	return out
}

// matches reports whether a glob picks out any of the names, exactly or as a
// pattern, and without caring about case.
func matches(glob string, names ...string) bool {
	for _, name := range names {
		if strings.EqualFold(glob, name) {
			return true
		}
		if ok, err := path.Match(strings.ToLower(glob), strings.ToLower(name)); err == nil && ok {
			return true
		}
	}
	return false
}

// providerPath is a folder's name split into the levels the other server keeps
// it in.
//
// A name breaks into levels only as far as the folders above it exist, and from
// the first one that does not it is a single name again. Servers hand back names
// with the separator inside them - an "a/b" on a server whose separator is "/",
// with no "a" anywhere - and splitting one of those would file its mail under a
// parent nobody has.
//
// The one thing that starts a new level after a break is a folder Proton owns,
// because nothing can be filed inside one anyway.
func providerPath(f rawImportFolder, sources []string) []string {
	if len(f.Hierarchy) > 0 {
		return f.Hierarchy
	}
	separator := f.separator()
	var chunks []string
	if separator == "/" {
		// An escaped separator is part of a name rather than a break in it.
		for _, part := range strings.Split(strings.ReplaceAll(f.Source, `\/`, "\x00"), "/") {
			chunks = append(chunks, strings.ReplaceAll(part, "\x00", `\/`))
		}
	} else {
		chunks = strings.Split(f.Source, separator)
	}

	var out []string
	var broken bool
	for i, chunk := range chunks {
		sofar := chunk
		if i > 0 {
			sofar = strings.Join(out, separator) + separator + chunk
		}
		switch {
		case !broken && exists(sources, sofar):
			out = append(out, chunk)
		case i == 0 || (!broken && exists(importSystemFolders, out[len(out)-1])):
			broken = true
			out = append(out, chunk)
		default:
			broken = true
			out[len(out)-1] += separator + chunk
		}
	}
	return out
}

func exists(sources []string, source string) bool {
	for _, s := range sources {
		if strings.EqualFold(s, source) {
			return true
		}
	}
	return false
}

// protonPath is where a folder lands, level by level.
//
// Two of Proton's rules shape it. Nothing may be nested under a folder Proton
// owns, so a folder inside one is renamed to carry it - "[Inbox]Receipts" - and
// nothing may be more than three levels deep, so what is deeper is folded into
// its third level. A label has neither rule and no levels at all, so the whole
// path becomes one name.
func protonPath(f rawImportFolder, sources []string, labelling bool) []string {
	p := providerPath(f, sources)
	if len(p) > 1 && exists(importSystemFolders, p[0]) {
		p = append([]string{"[" + p[0] + "]" + p[1]}, p[2:]...)
	}
	if labelling {
		return []string{strings.Join(p, "-")}
	}
	if len(p) <= importMaxDepth {
		return p
	}
	return append(p[:importMaxDepth-1:importMaxDepth-1], strings.Join(p[importMaxDepth-1:], f.separator()))
}

// admissible refuses a destination Proton would refuse, before the import is
// started rather than after some of it has already moved.
func admissible(p []string, source string, labelling bool) error {
	name := p[len(p)-1]
	kind := "folder"
	if labelling {
		kind = "label"
	}
	switch {
	case strings.TrimSpace(name) == "":
		return errs.Naming(source, errs.Problemf("That folder has no name to import it under.").
			Hint("--skip to leave it behind"))
	case len(name) >= importMaxNameSize:
		return errs.Naming(source, errs.Problemf(
			"That folder's name is %d bytes, and a Proton %s holds fewer than %d.",
			len(name), kind, importMaxNameSize).Hint("--skip to leave it behind"))
	case exists(importReserved, name):
		return errs.Naming(source, errs.Problemf("Proton keeps %q for itself, so no %s can be called that.",
			strings.ToLower(name), kind).Hint("--skip to leave it behind"))
	}
	return nil
}

// ImportLabel is the name every message of an import carries, when nobody names
// one: where the mail came from, and when it was fetched.
func ImportLabel(account string, now time.Time) string {
	domain := account
	if _, after, ok := strings.Cut(account, "@"); ok && after != "" {
		domain = after
	}
	return domain + " " + units.Time(now.Unix())
}

// accentFor is the colour a label made by an import is given.
//
// Proton's own client picks one at random. This picks by name instead, so the
// same import run twice looks the same both times and a folder keeps its colour
// wherever it is read back.
func accentFor(name string) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(strings.ToLower(name)))
	return accent.Palette[int(h.Sum32())%len(accent.Palette)].Hex
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// Progress is how far an import has got, as a person reads it.
func (i Import) Progress() string {
	switch {
	case i.Total == 0 && i.Processed == 0:
		return ""
	case i.Finished || i.Total == i.Processed:
		return fmt.Sprintf("%d", i.Processed)
	}
	return fmt.Sprintf("%d/%d", i.Processed, i.Total)
}
