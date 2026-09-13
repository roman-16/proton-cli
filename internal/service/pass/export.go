package pass

import (
	"context"
	"io"

	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/roman-16/proton-cli/internal/passfile"
	"github.com/roman-16/proton-cli/internal/progress"
)

// Gathering an account into the document a backup is written from.
//
// The document is Proton's own, so what this writes Proton Pass reads back, and
// the three shapes it can be laid down in - the archive, the document alone, a
// spreadsheet - are all rendered from this one gather.

// ExportPlan is everything a backup will hold, worked out before any of it is
// written: the document, and the attachments that go beside it.
type ExportPlan struct {
	Doc *passfile.ExportDocument
	// Files are the attachments to write, in the order they are written.
	Files []ExportFile
	// SkippedVaults is how many vaults were left out because somebody else owns
	// them, since an archive quietly smaller than the account is worth a word.
	SkippedVaults int
}

// ExportFile is one attachment on its way into an archive.
type ExportFile struct {
	ShareID string
	ItemID  string
	// Entry is where it lands inside the archive.
	Entry string
	File  Attachment
}

// Items is how many items the backup will hold.
func (p *ExportPlan) Items() int { return p.Doc.Count() }

// PlanExport gathers the vaults this account owns, and their items, into the
// document Proton Pass reads, and names the attachments that travel with them.
//
// What somebody else shared is theirs to back up: it stays out, as it does in
// Proton's own export, so restoring this file cannot turn a vault you were let
// into to a second copy of it under your name.
func (s *Service) PlanExport(ctx context.Context, userID string, withFiles bool) (*ExportPlan, error) {
	vaults, err := s.VaultsList(ctx)
	if err != nil {
		return nil, err
	}
	items, err := s.itemsFull(ctx, "", true)
	if err != nil {
		return nil, err
	}

	plan := &ExportPlan{Doc: passfile.NewDocument(userID)}
	for _, v := range vaults {
		if !v.Owner {
			plan.SkippedVaults++
			continue
		}
		plan.Doc.Vaults[v.ShareID] = &passfile.ExportedVault{
			Name: v.Name, Description: v.Description,
			Display: passfile.ExportedDisplay{Color: v.Color, Icon: v.Icon},
			Items:   []passfile.ExportedItem{},
		}
	}
	for _, it := range items {
		vault, ok := plan.Doc.Vaults[it.ShareID]
		if !ok || it.raw == nil {
			continue
		}
		stored := passfile.StoredItem{
			ShareID: it.ShareID, ItemID: it.ItemID, Item: it.raw,
			State: it.State, Alias: it.Alias,
			CreateTime: it.CreateTime, ModifyTime: it.ModifyTime,
		}
		if withFiles && it.hasAttachments {
			files, err := s.Attachments(ctx, it.ShareID, it.ItemID)
			if err != nil {
				return nil, err
			}
			for _, file := range files {
				entry := passfile.ArchiveEntry(it.ShareID, file.ID, file.Name)
				stored.Files = append(stored.Files, entry)
				plan.Files = append(plan.Files, ExportFile{
					ShareID: it.ShareID, ItemID: it.ItemID,
					Entry: entry, File: file,
				})
			}
		}
		exported, err := passfile.ExportItem(stored)
		if err != nil {
			return nil, err
		}
		vault.Items = append(vault.Items, *exported)
	}
	return plan, nil
}

// WriteArchive writes the zip Proton Pass reads.
//
// Each attachment is streamed out of Proton and into the archive rather than
// held whole, so an account with more attachments than memory still has a
// backup. An attachment that will not come stops the export: an archive quietly
// missing a file is worse than no archive.
func (s *Service) WriteArchive(ctx context.Context, w io.Writer, plan *ExportPlan, passphrase string, sink func(index, total int) progress.Sink) error {
	files := make([]passfile.ArchiveFile, 0, len(plan.Files))
	for i, f := range plan.Files {
		files = append(files, passfile.ArchiveFile{
			Entry: f.Entry,
			Write: func(w io.Writer) error {
				var report progress.Sink
				if sink != nil {
					report = sink(i+1, len(plan.Files))
				}
				if err := s.AttachmentDownload(ctx, f.ShareID, f.ItemID, f.File, w, report); err != nil {
					return errs.Problemf("%s could not be read, so the archive would be short of it: %v.", f.File.Name, err)
				}
				return nil
			},
		})
	}
	return passfile.WriteArchive(w, plan.Doc, files, passphrase)
}

// WriteDocument writes the items without the attachments: the document the
// archive holds, on its own, or the spreadsheet Proton Pass also exports.
func (s *Service) WriteDocument(w io.Writer, plan *ExportPlan, format, passphrase string) error {
	if format == "csv" {
		return passfile.WriteCSV(w, plan.Doc)
	}
	return passfile.WriteJSON(w, plan.Doc, passphrase)
}
