package drive

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/roman-16/proton-cli/internal/export"
	"github.com/roman-16/proton-cli/internal/localfile"
	"github.com/roman-16/proton-cli/internal/progress"
	"github.com/roman-16/proton-cli/internal/skip"
)

const (
	copyFiles     = "drive/My files"
	copyComputers = "drive/Computers"
	copyPhotos    = "drive/Photos"
)

type copyPart struct{ s *Service }

func (s *Service) CopyPart() export.Part { return copyPart{s: s} }

func (copyPart) App() export.App { return export.AppDrive }

func (copyPart) Noun() string { return "files" }

func (p copyPart) Holds(ctx context.Context) ([]export.Held, error) {
	computers, err := p.s.Computers(ctx)
	if err != nil {
		return nil, err
	}
	held := []export.Held{{Count: export.Uncounted, Noun: "files"}}
	if len(computers) > 0 {
		held = append(held, export.Held{Count: len(computers), Noun: "computers"})
	}
	return append(held, export.Held{Count: export.Uncounted, Noun: "photos"}), nil
}

type copied struct {
	file PlannedFile
	dc   *Context
	want string
}

type copyTree struct {
	dc  *Context
	top string
}

func (p copyPart) Run(ctx context.Context, c *export.Copy, sink progress.Sink) (res export.Result, err error) {
	ledger, err := c.Ledger(export.AppDrive)
	if err != nil {
		return res, err
	}
	defer func() {
		if closed := ledger.Close(); err == nil {
			err = closed
		}
	}()
	trees, err := p.trees(ctx)
	if err != nil {
		return res, err
	}
	var due []copied
	var bytes int64
	for _, t := range trees {
		plan, err := p.s.PlanDownload(ctx, t.dc, "/")
		if err != nil {
			return res, err
		}
		res.Documents += plan.Elsewhere.Documents
		res.Spreadsheets += plan.Elsewhere.Spreadsheets
		for _, folder := range plan.Folders {
			if err := localfile.EnsureDir(c.Path(within(t.top, folder))); err != nil {
				return res, err
			}
		}
		for _, file := range plan.Files {
			want := within(t.top, file.Path)
			moved, err := p.held(c, ledger, file.LinkID(), file.Revision(), want)
			if err != nil {
				return res, err
			}
			switch moved {
			case stays:
			case follows:
				res.Exported++
			default:
				due = append(due, copied{file: file, dc: t.dc, want: want})
				bytes += sizeOf(file)
			}
		}
	}
	photos, err := p.photos(ctx, c, ledger)
	if err != nil {
		return res, err
	}
	res.Owed = res.Exported + len(due) + len(photos.due)
	if len(due) > 0 {
		progress.Counting(sink, "")
		sink.Start(bytes, "Exporting drive files")
		for _, d := range due {
			if err := p.download(ctx, c, ledger, d, sink); err != nil {
				if export.Stops(ctx, err) {
					sink.Done()
					return res, err
				}
				p.passOver(ctx, d.file.LinkID(), err)
				continue
			}
			res.Exported++
		}
		sink.Done()
	}
	exported, err := p.copyPhotos(ctx, c, ledger, photos, sink)
	res.Exported += exported
	return res, err
}

type standing int

const (
	owes standing = iota
	stays
	follows
)

func (p copyPart) held(c *export.Copy, ledger *export.Ledger, id, version, want string) (standing, error) {
	e, ok := ledger.Get(id)
	if !ok || !e.Done() || e.Version != version || !localfile.Exists(c.Path(e.Path)) {
		return owes, nil
	}
	if e.Want == want {
		return stays, nil
	}
	now, was, err := ledger.Place(id, want)
	if err != nil {
		return owes, err
	}
	if err := localfile.EnsureDir(filepath.Dir(c.Path(now))); err != nil {
		return owes, err
	}
	if err := os.Rename(c.Path(was), c.Path(now)); err != nil {
		return owes, err
	}
	return follows, ledger.Put(export.Entry{ID: id, Version: version, Path: now, Want: want})
}

func (p copyPart) trees(ctx context.Context) ([]copyTree, error) {
	var out []copyTree
	dc, err := p.s.Resolve(ctx)
	var none *NoVolume
	switch {
	case errors.As(err, &none):
		// Recorded and not counted: an account that never opened Drive has no
		// files to be short of.
		slog.DebugContext(ctx, "drive: no files volume to copy")
	case err != nil:
		return nil, err
	default:
		out = append(out, copyTree{dc: dc, top: copyFiles})
	}
	computers, err := p.s.Computers(ctx)
	if err != nil {
		return nil, err
	}
	for _, computer := range computers {
		dc, err := p.s.OpenComputer(ctx, computer)
		if err != nil {
			if export.Stops(ctx, err) {
				return nil, err
			}
			skip.Record(ctx, skip.KindShare, computer.ShareID, skip.Unlockable, err)
			continue
		}
		out = append(out, copyTree{dc: dc, top: copyComputers + "/" + localfile.SafeName(computer.Name)})
	}
	return out, nil
}

func within(top, inTree string) string {
	parts := strings.Split(strings.Trim(inTree, "/"), "/")
	for i, part := range parts {
		parts[i] = localfile.SafeName(part)
	}
	return top + "/" + strings.Join(parts, "/")
}

func sizeOf(f PlannedFile) int64 {
	if f.Plain >= 0 {
		return f.Plain
	}
	return f.Size
}

func (p copyPart) download(ctx context.Context, c *export.Copy, ledger *export.Ledger, d copied, sink progress.Sink) error {
	now, _, err := ledger.Place(d.file.LinkID(), d.want)
	if err != nil {
		return err
	}
	err = localfile.Stream(c.Path(now), d.file.Modified, func(w io.Writer) error {
		return p.s.DownloadPlanned(ctx, d.dc, d.file, w, DownloadOptions{
			Label: "Exporting drive files", Progress: progress.Within(sink),
		})
	})
	if err != nil {
		return err
	}
	return ledger.Put(export.Entry{ID: d.file.LinkID(), Version: d.file.Revision(), Path: now, Want: d.want})
}

func (copyPart) passOver(ctx context.Context, id string, err error) {
	if export.Gone(err) {
		// Recorded and not counted: the file left the account while the run was
		// under way, so there is nothing of it to be short of.
		slog.DebugContext(ctx, "drive: a file went before it was copied", "link", id)
		return
	}
	skip.Record(ctx, skip.KindItem, id, skip.Undecryptable, err)
}

type library struct {
	dc  *Context
	due []Photo
}

func (p copyPart) photos(ctx context.Context, c *export.Copy, ledger *export.Ledger) (library, error) {
	dc, err := p.s.ResolvePhotos(ctx)
	var missing *errs.NotFound
	if errors.As(err, &missing) {
		// Recorded and not counted: an account with no photo library has no
		// photos to be short of.
		slog.DebugContext(ctx, "drive: no photo library to copy")
		return library{}, nil
	}
	if err != nil {
		return library{}, err
	}
	photos, err := p.s.PhotosList(ctx, dc, 0, false)
	if err != nil {
		return library{}, err
	}
	out := library{dc: dc}
	for _, photo := range photos {
		e, ok := ledger.Get(photo.LinkID)
		if ok && e.Done() && localfile.Exists(c.Path(e.Path)) {
			continue
		}
		out.due = append(out.due, photo)
	}
	return out, nil
}

func (p copyPart) copyPhotos(ctx context.Context, c *export.Copy, ledger *export.Ledger, lib library, sink progress.Sink) (int, error) {
	if len(lib.due) == 0 {
		return 0, nil
	}
	progress.Counting(sink, "photos")
	sink.Start(int64(len(lib.due)), "Exporting drive photos")
	defer sink.Done()
	exported := 0
	for _, photo := range lib.due {
		err := p.copyPhoto(ctx, c, ledger, lib.dc, photo)
		switch {
		case err == nil:
			exported++
		case export.Stops(ctx, err):
			return exported, err
		default:
			p.passOver(ctx, photo.LinkID, err)
		}
		sink.Add(1)
	}
	return exported, nil
}

func (p copyPart) copyPhoto(ctx context.Context, c *export.Copy, ledger *export.Ledger, dc *Context, photo Photo) error {
	res, err := p.s.ResolveRef(ctx, dc, photo.LinkID)
	if err != nil {
		return err
	}
	name := res.Name
	if name == "" {
		name = photo.LinkID
	}
	year := time.Unix(photo.CaptureTime, 0).Year()
	want := fmt.Sprintf("%s/%d/%s", copyPhotos, year, localfile.SafeName(name))
	now, _, err := ledger.Place(photo.LinkID, want)
	if err != nil {
		return err
	}
	link := res.Link
	revision := activeRevisionID(link)
	err = localfile.Stream(c.Path(now), res.Modified(), func(w io.Writer) error {
		return p.s.downloadFile(ctx, dc, link, res.NodeKR, revision, link.Size, w, DownloadOptions{})
	})
	if err != nil {
		return err
	}
	return ledger.Put(export.Entry{ID: photo.LinkID, Version: revision, Path: now, Want: want})
}
