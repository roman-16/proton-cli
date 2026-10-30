package pass

import (
	"context"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/roman-16/proton-cli/internal/export"
	"github.com/roman-16/proton-cli/internal/localfile"
	"github.com/roman-16/proton-cli/internal/progress"
)

const (
	copyFile     = "pass/proton-pass-export.zip"
	copyFileKey  = "archive"
	copyVaultKey = "vault:"
	copyItemKey  = "item:"
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
	versions := map[string]string{}
	for shareID, vault := range plan.Doc.Vaults {
		versions[copyVaultKey+shareID] = export.Version(vault.Name, vault.Description, vault.Display, vault.Folders)
		for _, item := range vault.Items {
			versions[copyItemKey+shareID+":"+item.ItemID] = export.Version(item)
		}
	}
	changed := 0
	all := make([]string, 0, len(versions))
	for id, version := range versions {
		all = append(all, id+"="+version)
		if had, ok := ledger.Get(id); !ok || had.Version != version {
			changed++
		}
	}
	var gone []string
	for _, id := range ledger.IDs() {
		_, held := versions[id]
		if !held && (strings.HasPrefix(id, copyVaultKey) || strings.HasPrefix(id, copyItemKey)) {
			gone = append(gone, id)
		}
	}
	slices.Sort(all)
	fileVersion := export.Version(all, p.passphrase != "")
	now, _, err := ledger.Place(copyFileKey, copyFile)
	if err != nil {
		return res, err
	}
	file, _ := ledger.Get(copyFileKey)
	present := localfile.Exists(c.Path(now))
	if present && file.Version == fileVersion {
		return res, nil
	}
	if !present || changed+len(gone) == 0 {
		changed = plan.Items()
	}
	res.Owed = changed + len(gone)
	progress.Counting(sink, "items")
	sink.Start(int64(plan.Items()), "Exporting pass")
	defer sink.Done()
	err = localfile.Stream(c.Path(now), time.Time{}, func(w io.Writer) error {
		return p.s.WriteArchive(ctx, w, plan, p.passphrase, nil)
	})
	if err != nil {
		return res, err
	}
	for id, version := range versions {
		if err := ledger.Put(export.Entry{ID: id, Version: version}); err != nil {
			return res, err
		}
	}
	for _, id := range gone {
		if err := ledger.Forget(id); err != nil {
			return res, err
		}
	}
	sink.Add(int64(plan.Items()))
	res.Exported = res.Owed
	res.Unlocked = p.passphrase == ""
	return res, ledger.Put(export.Entry{ID: copyFileKey, Version: fileVersion, Path: now, Want: copyFile})
}
