package pass

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/roman-16/proton-cli/internal/progress"
	pb "github.com/roman-16/proton-cli/internal/service/pass/proto"
	"github.com/roman-16/proton-cli/internal/units"
)

// Reading a Proton Pass backup back in.
//
// A vault in the file lands in the vault of that name, and one that does not
// exist yet is made. Items are added rather than matched: an item carries no
// identity a second account would recognise, so nothing here can tell a re-import
// from a file somebody edited, and inventing a match would be the one mistake
// that loses data instead of duplicating it.

// ImportResult is what a read-back did and what it could not.
type ImportResult struct {
	Imported []string       `json:"imported"`
	Skipped  []SkippedEntry `json:"skipped"`
	// SkippedFiles are the attachments that did not land. They are apart from
	// the items because an item whose file would not go up is still an item that
	// landed, and counting it as lost would be a wrong count.
	SkippedFiles []SkippedFile `json:"skipped_files"`
}

// SkippedFile is one attachment a read-back could not put back, and why.
type SkippedFile struct {
	Name   string `json:"name"`
	Item   string `json:"item"`
	Reason string `json:"reason"`
}

// String names the file and the item it belonged to.
func (s SkippedFile) String() string {
	return fmt.Sprintf("Skipped attachment %q of %q: %s.", s.Name, s.Item, s.Reason)
}

// SkippedEntry is one item a read-back could not take, and why.
type SkippedEntry struct {
	Name   string `json:"name,omitempty"`
	Vault  string `json:"vault,omitempty"`
	Reason string `json:"reason"`
}

// String names the item and the vault it was headed for.
func (s SkippedEntry) String() string {
	if s.Name == "" {
		return fmt.Sprintf("Skipped an item: %s.", s.Reason)
	}
	return fmt.Sprintf("Skipped %q: %s.", s.Name, s.Reason)
}

// ImportPlan is what a read-back would do, worked out before any of it is done.
type ImportPlan struct {
	// Vaults are the vaults the file names, in the order they appear.
	Vaults []PlannedVault
	// Skipped are the items that will not be read back, and why.
	Skipped []SkippedEntry
	// SkippedFiles are the attachments that will not be read back, and why.
	SkippedFiles []SkippedFile
}

// PlannedVault is one vault's worth of the file: where it will land, whether
// that vault has to be made, and what will go into it.
type PlannedVault struct {
	Name  string
	New   bool
	Items []*pb.Item
	Names []string
	// Files are the attachments of each item, in step with Items.
	Files [][]Upload
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
		for _, files := range v.Files {
			n += len(files)
		}
	}
	return n
}

// PlanImport works out what a document would land, without sending anything.
//
// Everything that can go wrong with the file itself goes wrong here - a kind of
// item this version cannot read, content that will not parse, an attachment the
// plan will not take - so a run that reaches the network is one that has already
// been understood, and a dry run says the same things a real one does.
func (s *Service) PlanImport(ctx context.Context, archive *Archive) (*ImportPlan, error) {
	existing, err := s.VaultsList(ctx)
	if err != nil {
		return nil, err
	}
	have := make(map[string]bool, len(existing))
	for _, v := range existing {
		have[v.Name] = true
	}
	doc := archive.Document()

	plan := &ImportPlan{}
	limits, err := s.attachmentLimits(ctx, doc)
	if err != nil {
		return nil, err
	}
	for _, v := range sortedVaults(doc) {
		planned := PlannedVault{Name: v.Name, New: !have[v.Name]}
		if planned.Name == "" {
			planned.Name = "Personal"
		}
		for _, item := range v.Items {
			built, err := importItem(item)
			if err != nil {
				plan.Skipped = append(plan.Skipped, SkippedEntry{
					Name: item.Data.Metadata.Name, Vault: v.Name, Reason: err.Error(),
				})
				continue
			}
			files, skipped := plannedFiles(archive, item, limits)
			plan.SkippedFiles = append(plan.SkippedFiles, skipped...)
			planned.Items = append(planned.Items, built)
			planned.Names = append(planned.Names, item.Data.Metadata.Name)
			planned.Files = append(planned.Files, files)
		}
		plan.Vaults = append(plan.Vaults, planned)
	}
	return plan, nil
}

// attachmentLimits is what the plan allows, asked for only when the document
// carries a file at all.
func (s *Service) attachmentLimits(ctx context.Context, doc *ExportDocument) (*StorageLimits, error) {
	for _, v := range doc.Vaults {
		for _, item := range v.Items {
			if len(item.Files) > 0 {
				return s.StorageLimits(ctx)
			}
		}
	}
	return nil, nil
}

// plannedFiles are one item's attachments, and the ones that will not go back.
//
// Each of the three ways a file is refused is known before anything is sent, so
// a dry run names them too: the archive does not hold it, the plan takes no
// attachments at all, or the file is larger than one may be.
func plannedFiles(archive *Archive, item ExportedItem, limits *StorageLimits) ([]Upload, []SkippedFile) {
	var out []Upload
	var skipped []SkippedFile
	for _, entry := range item.Files {
		name := ImportName(entry)
		up, ok := archive.Attachment(entry)
		switch {
		case !ok:
			skipped = append(skipped, SkippedFile{
				Name: name, Item: item.Data.Metadata.Name,
				Reason: "the archive does not hold it",
			})
		case limits == nil || !limits.Allowed:
			skipped = append(skipped, SkippedFile{
				Name: name, Item: item.Data.Metadata.Name,
				Reason: "attachments need a paid Pass plan",
			})
		case up.Size == 0:
			skipped = append(skipped, SkippedFile{
				Name: name, Item: item.Data.Metadata.Name, Reason: "it is empty",
			})
		case limits.MaxFileSize > 0 && up.Size > limits.MaxFileSize:
			skipped = append(skipped, SkippedFile{
				Name: name, Item: item.Data.Metadata.Name,
				Reason: fmt.Sprintf("it is %s, and an attachment may be at most %s",
					units.Size(up.Size), units.Size(limits.MaxFileSize)),
			})
		default:
			out = append(out, up)
		}
	}
	return out, skipped
}

// Import carries out a plan.
//
// report is handed each attachment as it goes up, numbered within the run.
func (s *Service) Import(ctx context.Context, plan *ImportPlan, report func(index, total int) progress.Sink) (*ImportResult, error) {
	existing, err := s.VaultsList(ctx)
	if err != nil {
		return nil, err
	}
	shareOf := make(map[string]string, len(existing))
	for _, v := range existing {
		shareOf[v.Name] = v.ShareID
	}

	res := &ImportResult{Skipped: plan.Skipped, SkippedFiles: plan.SkippedFiles}
	totalFiles, sentFiles := plan.Attachments(), 0
	for _, v := range plan.Vaults {
		if len(v.Items) == 0 {
			continue
		}
		shareID, ok := shareOf[v.Name]
		if !ok {
			shareID, err = s.VaultCreate(ctx, v.Name)
			if err != nil {
				// A vault that cannot be made takes its items with it, and the
				// free plan's vault limit is the usual reason.
				for _, name := range v.Names {
					res.Skipped = append(res.Skipped, SkippedEntry{
						Name: name, Vault: v.Name, Reason: err.Error(),
					})
				}
				continue
			}
			shareOf[v.Name] = shareID
		}
		sk, err := s.decryptShareKeys(ctx, shareID)
		if err != nil {
			return nil, err
		}
		shareKey, rotation := sk.latest()
		for i, item := range v.Items {
			id, err := s.putItem(ctx, shareID, shareKey, rotation, item)
			if err != nil {
				res.Skipped = append(res.Skipped, SkippedEntry{
					Name: v.Names[i], Vault: v.Name, Reason: err.Error(),
				})
				continue
			}
			res.Imported = append(res.Imported, id)
			// The item has landed, so a file that will not go up costs the file
			// and not the item: it is named afterwards rather than counted as a
			// loss.
			var pending []pendingFile
			for _, up := range v.Files[i] {
				sentFiles++
				if report != nil {
					up.Progress = report(sentFiles, totalFiles)
				}
				file, err := s.uploadPending(ctx, up)
				if err != nil {
					res.SkippedFiles = append(res.SkippedFiles, SkippedFile{
						Name: up.Name, Item: v.Names[i], Reason: err.Error(),
					})
					continue
				}
				pending = append(pending, file)
			}
			if _, err := s.linkFiles(ctx, shareID, id, 1, pending, nil); err != nil {
				for _, up := range v.Files[i] {
					res.SkippedFiles = append(res.SkippedFiles, SkippedFile{
						Name: up.Name, Item: v.Names[i], Reason: err.Error(),
					})
				}
			}
		}
	}
	return res, nil
}

// importItem rebuilds the stored protobuf from one entry of the document.
func importItem(in ExportedItem) (*pb.Item, error) {
	// An alias is an address Proton owns and hands out; a second account cannot
	// be given the same one, and the account that exported it still has it. So
	// there is nothing to recreate.
	kind := importTypeName(in.Data.Type)
	if kind == "alias" {
		return nil, errs.Problemf("An alias belongs to the account that made it, so it cannot be read back.")
	}

	content, err := importContent(kind, in.Data.Content)
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
	return item, nil
}

// importContent parses the content of whichever kind of item this is.
func importContent(kind string, raw json.RawMessage) (*pb.Content, error) {
	out := &pb.Content{}
	var msg proto.Message
	switch kind {
	case "login":
		m := &pb.ItemLogin{}
		out.Content, msg = &pb.Content_Login{Login: m}, m
	case "note":
		m := &pb.ItemNote{}
		out.Content, msg = &pb.Content_Note{Note: m}, m
	case "credit-card":
		m := &pb.ItemCreditCard{}
		out.Content, msg = &pb.Content_CreditCard{CreditCard: m}, m
	case "identity":
		m := &pb.ItemIdentity{}
		out.Content, msg = &pb.Content_Identity{Identity: m}, m
	case "ssh-key":
		m := &pb.ItemSSHKey{}
		out.Content, msg = &pb.Content_SshKey{SshKey: m}, m
	case "wifi":
		m := &pb.ItemWifi{}
		out.Content, msg = &pb.Content_Wifi{Wifi: m}, m
	case "custom":
		m := &pb.ItemCustom{}
		out.Content, msg = &pb.Content_Custom{Custom: m}, m
	default:
		return nil, fmt.Errorf("%q is a kind of item this version does not know how to read", kind)
	}
	// Proton writes a field it has no value for as an empty one rather than
	// leaving it out, and a newer app may write fields this one has never heard
	// of. Neither is a reason to refuse the item.
	opts := protojson.UnmarshalOptions{DiscardUnknown: true}
	if err := opts.Unmarshal(raw, msg); err != nil {
		return nil, fmt.Errorf("its contents could not be read")
	}
	return out, nil
}

// sortedVaults returns the document's vaults in a settled order, so a dry run and
// the run itself say the same thing.
func sortedVaults(doc *ExportDocument) []*ExportedVault {
	keys := make([]string, 0, len(doc.Vaults))
	for k := range doc.Vaults {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	out := make([]*ExportedVault, 0, len(keys))
	for _, k := range keys {
		out = append(out, doc.Vaults[k])
	}
	return out
}
