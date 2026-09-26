package pass

import (
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"

	"google.golang.org/protobuf/proto"

	"github.com/roman-16/proton-cli/internal/crypto/aead"
	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/roman-16/proton-cli/internal/passfile"
	"github.com/roman-16/proton-cli/internal/progress"
	"github.com/roman-16/proton-cli/internal/proton"
	pb "github.com/roman-16/proton-cli/internal/service/pass/proto"
	"github.com/roman-16/proton-cli/internal/units"
)

// Landing a file's worth of items in the account.
//
// Everything about where an item goes is settled here, before anything is sent:
// which vault it lands in, whether that vault has to be made and whether the
// plan has room for it, and which of its attachments will travel. Reading the
// file is passfile's, and this does not care which program wrote it.
//
// Items are added rather than matched: an item carries no identity a second
// account would recognise, so nothing here can tell a re-import from a file
// somebody edited, and inventing a match would be the one mistake that loses
// data instead of duplicating it.

// importBatch is how many items go up in one request. Proton takes a hundred;
// the app sends fifty, and a batch that is refused is refused whole, so the
// smaller number is the one worth losing.
const importBatch = 50

// ImportResult is what a read-back did and what it could not.
type ImportResult struct {
	Imported []string               `json:"imported"`
	Skipped  []passfile.Skip        `json:"skipped"`
	Files    []passfile.SkippedFile `json:"skipped_files"`
}

// ImportPlan is what an import would do, worked out before any of it is done.
type ImportPlan struct {
	// Vaults are the vaults the file names, in the order they appear.
	Vaults []PlannedVault
	// Skipped are the items that will not land, and why.
	Skipped []passfile.Skip
	// SkippedFiles are the attachments that will not land, and why.
	SkippedFiles []passfile.SkippedFile
}

// PlannedVault is one vault's worth of the file: where it will land, whether
// that vault has to be made, and what will go into it.
type PlannedVault struct {
	Name    string
	ShareID string
	New     bool
	Items   []PlannedItem
}

// PlannedItem is one item on its way in, with the attachments that travel with
// it.
type PlannedItem struct {
	passfile.Entry
	Files []Upload
}

// Count is how many items the whole plan would write.
func (p ImportPlan) Count() int {
	var n int
	for _, v := range p.Vaults {
		n += len(v.Items)
	}
	return n
}

// Attachments is how many files the whole plan would put back.
func (p ImportPlan) Attachments() int {
	var n int
	for _, v := range p.Vaults {
		for _, item := range v.Items {
			n += len(item.Files)
		}
	}
	return n
}

// NewVaults is how many vaults the import would create, since making one is the
// part a person cannot undo by deleting a few items.
func (p ImportPlan) NewVaults() int {
	var n int
	for _, v := range p.Vaults {
		if v.New && len(v.Items) > 0 {
			n++
		}
	}
	return n
}

// PlanImport works out what a document would land, without sending anything.
//
// target names one vault to put everything in; without it the file's own vaults
// are followed, and one the account does not have yet is made. Everything that
// can go wrong before the network does go wrong here - a kind of item this
// version cannot read, an alias that cannot be recreated, a vault the plan has
// no room for, an attachment that will not be taken - so a dry run says the same
// things a real one does.
func (s *Service) PlanImport(ctx context.Context, doc *passfile.Document, userID, target string) (*ImportPlan, error) {
	existing, err := s.VaultsList(ctx)
	if err != nil {
		return nil, err
	}
	if len(existing) == 0 {
		return nil, errs.Problemf("This account has no vault to import into.")
	}
	limits, err := s.Limits(ctx)
	if err != nil {
		return nil, err
	}

	plan := &ImportPlan{Skipped: doc.Skipped, SkippedFiles: doc.SkippedFiles}
	placed, err := s.placeVaults(ctx, doc, existing, target)
	if err != nil {
		return nil, err
	}
	held, err := s.heldAliases(ctx, doc, userID)
	if err != nil {
		return nil, err
	}

	room := vaultRoom(existing, limits)
	for _, vault := range placed {
		if vault.New {
			if room <= 0 {
				plan.Skipped = append(plan.Skipped, vaultOverLimit(vault, existing, limits)...)
				continue
			}
			room--
		}
		planned := PlannedVault{Name: vault.Name, ShareID: vault.ShareID, New: vault.New}
		for _, entry := range vault.Items {
			if reason := refusedEntry(entry, doc.UserID, userID, held); reason != "" {
				plan.Skipped = append(plan.Skipped, passfile.Skip{
					Name: entry.Name, Vault: vault.Name, Reason: reason,
				})
				continue
			}
			files, skipped := plannedFiles(entry, limits.Storage)
			plan.SkippedFiles = append(plan.SkippedFiles, skipped...)
			planned.Items = append(planned.Items, PlannedItem{Entry: entry, Files: files})
		}
		plan.Vaults = append(plan.Vaults, planned)
	}
	return plan, nil
}

// placedVault is one of the file's vaults with the account's answer to it:
// which share it lands in, or that it has to be made.
type placedVault struct {
	Name    string
	ShareID string
	New     bool
	Items   []passfile.Entry
}

// placeVaults decides where each of the file's vaults lands.
//
// A file that names no vault - which is most of them, since only Proton Pass and
// the managers with folders do - lands in the account's first vault, the one the
// app opens on. --vault overrides all of it.
func (s *Service) placeVaults(ctx context.Context, doc *passfile.Document, existing []Vault, target string) ([]placedVault, error) {
	byName := make(map[string]Vault, len(existing))
	for _, v := range existing {
		byName[v.Name] = v
	}

	if target != "" {
		shareID, err := s.ResolveVault(ctx, target)
		if err != nil {
			return nil, err
		}
		into := placedVault{ShareID: shareID}
		for _, v := range existing {
			if v.ShareID == shareID {
				into.Name = v.Name
			}
		}
		for _, vault := range doc.Vaults {
			into.Items = append(into.Items, vault.Items...)
		}
		return []placedVault{into}, nil
	}

	var out []placedVault
	seen := map[string]int{}
	for _, vault := range doc.Vaults {
		name := vault.Name
		if name == "" {
			name = existing[0].Name
		}
		if at, ok := seen[name]; ok {
			out[at].Items = append(out[at].Items, vault.Items...)
			continue
		}
		seen[name] = len(out)
		placed := placedVault{Name: name, Items: vault.Items}
		if v, ok := byName[name]; ok {
			placed.ShareID = v.ShareID
		} else {
			placed.New = true
		}
		out = append(out, placed)
	}
	return out, nil
}

// vaultRoom is how many more vaults the plan allows.
func vaultRoom(existing []Vault, limits *Limits) int {
	if limits.Vaults == nil {
		return len(existing) + 1000
	}
	return *limits.Vaults - len(existing)
}

// vaultOverLimit is what the items of a vault the plan has no room for are told.
func vaultOverLimit(vault placedVault, existing []Vault, limits *Limits) []passfile.Skip {
	allowed := fmt.Sprintf("%d vaults", *limits.Vaults)
	if *limits.Vaults == 1 {
		allowed = "1 vault"
	}
	reason := fmt.Sprintf("your plan allows %s and you have %d", allowed, len(existing))
	out := make([]passfile.Skip, 0, len(vault.Items))
	for _, entry := range vault.Items {
		out = append(out, passfile.Skip{Name: entry.Name, Vault: vault.Name, Reason: reason})
	}
	return out
}

// refusedEntry is why an item cannot land, and empty when it can.
//
// An alias is an address Proton owns and hands out. The account that made it
// still has it, and a second account cannot be given the same one, so the only
// alias that can come back is one this account made and has since deleted.
func refusedEntry(entry passfile.Entry, fileUser, accountUser string, held map[string]bool) string {
	if entry.Kind != "alias" {
		return ""
	}
	switch {
	case entry.AliasEmail == "":
		return "the file does not say which address this alias stands for"
	case fileUser == "" || fileUser != accountUser:
		return "an alias belongs to the account that made it, so it cannot be read into another one"
	case held[entry.AliasEmail]:
		return "this account already holds that alias"
	}
	return ""
}

// heldAliases are the addresses the account has now, which is asked for only
// when the file carries an alias this account could recreate.
func (s *Service) heldAliases(ctx context.Context, doc *passfile.Document, userID string) (map[string]bool, error) {
	if doc.UserID == "" || doc.UserID != userID {
		return nil, nil
	}
	var any bool
	for _, vault := range doc.Vaults {
		for _, entry := range vault.Items {
			any = any || entry.Kind == "alias"
		}
	}
	if !any {
		return nil, nil
	}
	items, err := s.itemsFull(ctx, "", everything, true)
	if err != nil {
		return nil, err
	}
	held := map[string]bool{}
	for _, it := range items {
		if it.Alias != "" {
			held[it.Alias] = true
		}
	}
	return held, nil
}

// plannedFiles are one item's attachments, and the ones that will not go.
//
// Each way a file is refused is known before anything is sent, so a dry run
// names them too: the plan takes no attachments at all, the file is empty, or it
// is larger than one may be.
func plannedFiles(entry passfile.Entry, limits StorageLimits) ([]Upload, []passfile.SkippedFile) {
	var out []Upload
	var skipped []passfile.SkippedFile
	for _, file := range entry.Files {
		refused := ""
		switch {
		case !limits.Allowed:
			refused = "attachments need a paid Pass plan"
		case file.Size == 0:
			refused = "it is empty"
		case limits.MaxFileSize > 0 && file.Size > limits.MaxFileSize:
			refused = fmt.Sprintf("it is %s, and an attachment may be at most %s",
				units.Size(file.Size), units.Size(limits.MaxFileSize))
		}
		if refused != "" {
			skipped = append(skipped, passfile.SkippedFile{
				Name: file.Name, Item: entry.Name, Reason: refused,
			})
			continue
		}
		out = append(out, Upload{Name: file.Name, Size: file.Size, Open: file.Open})
	}
	return out, skipped
}

// Import carries out a plan.
//
// report is handed each attachment as it goes up, numbered within the run.
func (s *Service) Import(ctx context.Context, plan *ImportPlan, report func(index, total int) progress.Sink) (*ImportResult, error) {
	res := &ImportResult{Skipped: plan.Skipped, Files: plan.SkippedFiles}
	totalFiles, sentFiles := plan.Attachments(), 0

	for _, vault := range plan.Vaults {
		if len(vault.Items) == 0 {
			continue
		}
		shareID := vault.ShareID
		if shareID == "" {
			created, err := s.VaultCreate(ctx, vault.Name)
			if err != nil {
				// A vault that cannot be made takes its items with it.
				slog.WarnContext(ctx, "import vault refused",
					"count", len(vault.Items), "error", err)
				for _, item := range vault.Items {
					res.Skipped = append(res.Skipped, passfile.Skip{
						Name: item.Name, Vault: vault.Name, Reason: err.Error(),
					})
				}
				continue
			}
			shareID = created
		}
		sk, err := s.decryptShareKeys(ctx, shareID)
		if err != nil {
			return nil, err
		}
		shareKey, rotation := sk.latest()

		for start := 0; start < len(vault.Items); start += importBatch {
			batch := vault.Items[start:min(start+importBatch, len(vault.Items))]
			ids, err := s.importItems(ctx, shareID, shareKey, rotation, batch)
			if err != nil {
				// Proton refuses a batch whole, so every item in it is lost and
				// the run carries on with the next one.
				slog.WarnContext(ctx, "import batch refused",
					"count", len(batch), "error", err)
				for _, item := range batch {
					res.Skipped = append(res.Skipped, passfile.Skip{
						Name: item.Name, Vault: vault.Name, Reason: err.Error(),
					})
				}
				continue
			}
			res.Imported = append(res.Imported, ids...)
			for i, item := range batch {
				// The item has landed, so a file that will not go up costs the
				// file and not the item: it is named afterwards rather than
				// counted as a loss.
				sentFiles = s.attach(ctx, shareID, ids[i], item, res, report, sentFiles, totalFiles)
			}
		}
	}
	return res, nil
}

// importItems seals one batch and sends it.
//
// The import endpoint is what the app uses, and it is the only one that carries
// what a backup knows beyond the item's content: when it was made, when it last
// changed, whether it was in the trash, and the address an alias stands for.
func (s *Service) importItems(ctx context.Context, shareID string, shareKey []byte, rotation int, items []PlannedItem) ([]string, error) {
	body := make([]map[string]any, 0, len(items))
	for _, item := range items {
		sealed, err := sealItem(shareKey, rotation, item.Item)
		if err != nil {
			return nil, err
		}
		one := map[string]any{"Item": sealed}
		if item.Trashed {
			one["Trashed"] = true
		}
		if item.CreateTime > 0 {
			one["CreateTime"] = item.CreateTime
		}
		if item.ModifyTime > 0 {
			one["ModifyTime"] = item.ModifyTime
		}
		if item.AliasEmail != "" {
			one["AliasEmail"] = item.AliasEmail
		}
		body = append(body, one)
	}

	var r struct {
		Revisions struct {
			RevisionsData []struct{ ItemID string }
		}
	}
	if err := s.C.Decode(ctx, proton.Request{
		Method: "POST", Path: "/pass/v1/share/" + shareID + "/item/import/batch",
		Body: map[string]any{"Items": body},
	}, &r); err != nil {
		return nil, err
	}
	if len(r.Revisions.RevisionsData) != len(items) {
		return nil, fmt.Errorf("only %d of the %d items sent came back", len(r.Revisions.RevisionsData), len(items))
	}
	out := make([]string, 0, len(items))
	for _, rev := range r.Revisions.RevisionsData {
		out = append(out, rev.ItemID)
	}
	return out, nil
}

// sealItem locks one item under a fresh key of its own, which is sealed in turn
// under the vault's.
func sealItem(shareKey []byte, rotation int, item *pb.Item) (map[string]any, error) {
	itemKey, err := aead.NewKey()
	if err != nil {
		return nil, err
	}
	pbBytes, err := proto.Marshal(item)
	if err != nil {
		return nil, err
	}
	ct, err := aead.Encrypt(itemKey, pbBytes, []byte(aead.TagItemContent))
	if err != nil {
		return nil, err
	}
	ek, err := aead.Encrypt(shareKey, itemKey, []byte(aead.TagItemKey))
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"Content":              base64.StdEncoding.EncodeToString(ct),
		"ContentFormatVersion": contentFormatVersion,
		"ItemKey":              base64.StdEncoding.EncodeToString(ek),
		"KeyRotation":          rotation,
	}, nil
}

// attach puts one item's files back on it, and returns how many files the run
// has sent.
func (s *Service) attach(ctx context.Context, shareID, itemID string, item PlannedItem, res *ImportResult, report func(int, int) progress.Sink, sent, total int) int {
	if len(item.Files) == 0 {
		return sent
	}
	var pending []pendingFile
	for _, up := range item.Files {
		sent++
		if report != nil {
			up.Progress = report(sent, total)
		}
		file, err := s.uploadPending(ctx, up)
		if err != nil {
			res.Files = append(res.Files, passfile.SkippedFile{
				Name: up.Name, Item: item.Name, Reason: err.Error(),
			})
			continue
		}
		pending = append(pending, file)
	}
	if _, err := s.linkFiles(ctx, shareID, itemID, 1, pending, nil); err != nil {
		for _, up := range item.Files {
			res.Files = append(res.Files, passfile.SkippedFile{
				Name: up.Name, Item: item.Name, Reason: err.Error(),
			})
		}
	}
	return sent
}
