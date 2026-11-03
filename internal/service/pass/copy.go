package pass

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"maps"
	"path/filepath"
	"slices"
	"time"

	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/roman-16/proton-cli/internal/export"
	"github.com/roman-16/proton-cli/internal/localfile"
	"github.com/roman-16/proton-cli/internal/passfile"
	"github.com/roman-16/proton-cli/internal/progress"
)

const (
	copyFile    = "pass/proton-pass-export.zip"
	copyFileKey = "archive"
)

type copyPart struct {
	s          *Service
	userID     string
	passphrase string
}

func (s *Service) CopyPart(userID, passphrase string) export.Part {
	return copyPart{s: s, userID: userID, passphrase: passphrase}
}

func (copyPart) App() export.App { return export.AppPass }

func (copyPart) Noun() string { return "items" }

func (p copyPart) Holds(ctx context.Context) ([]export.Held, error) {
	vaults, err := p.s.VaultsList(ctx)
	if err != nil {
		return nil, err
	}
	owned := 0
	for _, v := range vaults {
		if v.Owner {
			owned++
		}
	}
	return []export.Held{{Count: owned, Noun: "vaults"}}, nil
}

func (p copyPart) Run(ctx context.Context, c *export.Copy, sink progress.Sink) (res export.Result, err error) {
	ledger, err := c.Ledger(export.AppPass)
	if err != nil {
		return res, err
	}
	defer func() {
		if closed := ledger.Close(); err == nil {
			err = closed
		}
	}()
	plan, err := p.s.PlanExport(ctx, p.userID, true)
	if err != nil {
		return res, err
	}
	res.Shared = map[string]int{"vaults": plan.SkippedVaults}
	now, _, err := ledger.Place(copyFileKey, copyFile)
	if err != nil {
		return res, err
	}
	if err := ledger.Retain(func(id string) bool { return id == copyFileKey }); err != nil {
		return res, err
	}
	previous, err := p.openPrevious(c.Path(now))
	if err != nil {
		return res, err
	}
	changed := plan.Items()
	if previous != nil {
		defer func() { _ = previous.Close() }()
		changed = changedSince(previous.Doc, plan.Doc)
		plan.Kept = keep(previous, plan.Doc)
		if changed == 0 && previous.Doc.Encrypted == (p.passphrase != "") {
			return res, nil
		}
		if changed == 0 {
			changed = plan.Items()
		}
	}
	res.Owed = changed
	progress.Counting(sink, "items")
	sink.Start(int64(plan.Items()), "Exporting pass")
	defer sink.Done()
	if err := localfile.EnsureDir(filepath.Dir(c.Path(now))); err != nil {
		return res, err
	}
	tmp, err := localfile.Temp(filepath.Dir(c.Path(now)))
	if err != nil {
		return res, err
	}
	err = localfile.CommitTo(tmp, func(w io.Writer) (string, time.Time, error) {
		err := p.s.WriteArchive(ctx, w, plan, p.passphrase, nil)
		if previous != nil {
			_ = previous.Close()
		}
		return c.Path(now), time.Time{}, err
	})
	if err != nil {
		return res, err
	}
	sink.Add(int64(plan.Items()))
	res.Exported = res.Owed
	res.Unlocked = p.passphrase == ""
	return res, ledger.Put(export.Entry{ID: copyFileKey, Path: now, Want: copyFile})
}

func (p copyPart) openPrevious(path string) (*passfile.Backup, error) {
	previous, err := passfile.OpenBackup(path, p.passphrase)
	switch {
	case err == nil:
		return previous, nil
	case errors.Is(err, fs.ErrNotExist):
		return nil, nil
	case errors.Is(err, passfile.ErrBackupLocked):
		return nil, errs.Problemf("The Pass archive at %s is locked.", path).
			Hint("--passphrase-file with the passphrase it was locked with")
	case errors.Is(err, passfile.ErrBackupPassphrase):
		return nil, errs.Problemf("The Pass archive at %s does not open with this passphrase.", path).
			Hint("the passphrase it was locked with, or move " + filepath.Base(path) + " away to start a new one")
	}
	return nil, errs.Problemf("The Pass archive at %s does not read back: %v.", path, err).
		Hint("move " + filepath.Base(path) + " away to start a new one")
}

func changedSince(previous, current *passfile.ExportDocument) int {
	changed := 0
	for shareID, vault := range current.Vaults {
		was, ok := previous.Vaults[shareID]
		if !ok || !same(vaultFrame(was), vaultFrame(vault)) {
			changed++
		}
		items := map[string]passfile.ExportedItem{}
		if ok {
			for _, item := range was.Items {
				items[item.ItemID] = item
			}
		}
		for _, item := range vault.Items {
			if before, held := items[item.ItemID]; !held || !same(before, item) {
				changed++
			}
		}
	}
	return changed
}

func keep(previous *passfile.Backup, current *passfile.ExportDocument) []passfile.ArchiveFile {
	var files []passfile.ArchiveFile
	carry := func(item passfile.ExportedItem) {
		for _, entry := range item.Files {
			if f, ok := previous.File(entry); ok {
				files = append(files, f)
			}
		}
	}
	for _, shareID := range slices.Sorted(maps.Keys(previous.Doc.Vaults)) {
		was := previous.Doc.Vaults[shareID]
		vault, ok := current.Vaults[shareID]
		if !ok {
			current.Vaults[shareID] = was
			for _, item := range was.Items {
				carry(item)
			}
			continue
		}
		held := make(map[string]bool, len(vault.Items))
		for _, item := range vault.Items {
			held[item.ItemID] = true
		}
		for _, item := range was.Items {
			if !held[item.ItemID] {
				vault.Items = append(vault.Items, item)
				carry(item)
			}
		}
	}
	return files
}

func vaultFrame(v *passfile.ExportedVault) any {
	return []any{v.Name, v.Description, v.Display, v.Folders}
}

func same(a, b any) bool {
	x, errA := json.Marshal(a)
	y, errB := json.Marshal(b)
	return errA == nil && errB == nil && bytes.Equal(x, y)
}
