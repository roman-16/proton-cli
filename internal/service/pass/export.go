package pass

import (
	"context"
	"io"

	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/roman-16/proton-cli/internal/passfile"
	"github.com/roman-16/proton-cli/internal/progress"
)

type ExportPlan struct {
	Doc           *passfile.ExportDocument
	Files         []ExportFile
	Kept          []passfile.ArchiveFile
	SkippedVaults int
}

type ExportFile struct {
	ShareID string
	ItemID  string
	Entry   string
	File    Attachment
}

func (p *ExportPlan) Items() int { return p.Doc.Count() }

func (s *Service) PlanExport(ctx context.Context, userID string, withFiles bool) (*ExportPlan, error) {
	vaults, err := s.VaultsList(ctx)
	if err != nil {
		return nil, err
	}
	items, err := s.itemsFull(ctx, "", everything, true)
	if err != nil {
		return nil, err
	}

	plan := &ExportPlan{Doc: passfile.NewDocument(userID)}
	for _, v := range vaults {
		if !v.Owner {
			plan.SkippedVaults++
			continue
		}
		tree, err := s.folders(ctx, v.ShareID)
		if err != nil {
			return nil, err
		}
		plan.Doc.Vaults[v.ShareID] = &passfile.ExportedVault{
			Name: v.Name, Description: v.Description,
			Display: passfile.ExportedDisplay{Color: ColorValue(v.Color), Icon: IconValue(v.Icon)},
			Folders: exportedFolders(tree),
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
			FolderID: it.FolderID,
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

func exportedFolders(tree *folderTree) []passfile.ExportedFolder {
	out := []passfile.ExportedFolder{}
	for _, f := range tree.list() {
		folder := passfile.ExportedFolder{FolderID: f.FolderID, Name: f.Name}
		if f.ParentID != "" {
			parent := f.ParentID
			folder.ParentFolderID = &parent
		}
		out = append(out, folder)
	}
	return out
}

func (s *Service) WriteArchive(ctx context.Context, w io.Writer, plan *ExportPlan, passphrase string, sink func(index, total int) progress.Sink) error {
	files := make([]passfile.ArchiveFile, 0, len(plan.Files)+len(plan.Kept))
	for i, f := range plan.Files {
		files = append(files, passfile.ArchiveFile{
			Entry: f.Entry,
			Write: func(w io.Writer) error {
				var report progress.Sink
				if sink != nil {
					report = sink(i+1, len(plan.Files))
				}
				if err := s.AttachmentDownload(ctx, f.ShareID, f.ItemID, f.File, w, report); err != nil {
					return errs.Naming(f.File.Name, errs.Problemf(
						"%s could not be read, so the archive would be short of it: %v.", f.File.Name, err))
				}
				return nil
			},
		})
	}
	return passfile.WriteArchive(w, plan.Doc, append(files, plan.Kept...), passphrase)
}

func (s *Service) WriteDocument(w io.Writer, plan *ExportPlan, format, passphrase string) error {
	if format == "csv" {
		return passfile.WriteCSV(w, plan.Doc)
	}
	return passfile.WriteJSON(w, plan.Doc, passphrase)
}
