package drive

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/roman-16/proton-cli/internal/cli/kit"
	"github.com/roman-16/proton-cli/internal/errs"
	drivesvc "github.com/roman-16/proton-cli/internal/service/drive"
	"github.com/roman-16/proton-cli/internal/ui"
	"github.com/roman-16/proton-cli/internal/units"
	"github.com/spf13/cobra"
)

func itemsCmd() *cobra.Command {
	c := &cobra.Command{Use: "items", Short: "Files and folders"}
	c.AddCommand(itemsListCmd(), itemsGetCmd(), itemsCreateCmd(), itemsUploadCmd(),
		itemsDownloadCmd(), itemsUpdateCmd(), itemsMoveCmd(), itemsCopyCmd(),
		itemsTrashCmd(), itemsDeleteCmd(), revisionsCmd(), shareCmd())
	return c
}

func childColumns() []ui.Column[drivesvc.Child] {
	return []ui.Column[drivesvc.Child]{
		{Header: "ID", ID: true, Cell: func(ch drivesvc.Child) string { return ch.LinkID }},
		{Header: "TYPE", Cell: func(ch drivesvc.Child) string { return ch.Type }},
		{Header: "SIZE", Right: true, Cell: func(ch drivesvc.Child) string {
			// A folder has no size of its own. Blank is how every other column
			// says "nothing here"; a placeholder glyph would read like a value.
			if ch.Type == drivesvc.TypeFolder {
				return ""
			}
			return units.Size(ch.Size)
		}},
		{Header: "MODIFIED", Cell: func(ch drivesvc.Child) string { return units.Time(ch.ModifyTime) }},
		{Header: "NAME", Flex: true, Cell: func(ch drivesvc.Child) string { return ch.Name }},
	}
}

func itemsListCmd() *cobra.Command {
	var f filters
	var page kit.Page
	var order kit.Order
	c := &cobra.Command{
		Use:   "list [PATH]",
		Short: "List what is in a folder",
		Long: "List what is in a folder.\n\n" +
			"The filters are the same ones move, copy, trash and delete take, so a\n" +
			"selection can be worked out here and then handed to the verb that acts on\n" +
			"it. PATH is what those commands call --scope.",
		Args: cobra.MaximumNArgs(1),
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			dc, err := context(c)
			if err != nil {
				return err
			}
			at := "/"
			if len(c.Args) > 0 {
				at = c.Args[0]
			}
			// A listing with no filter is the folder itself, which is one request;
			// a filtered one is the same walk the bulk verbs do.
			children, err := c.App.Drive.List(c.Ctx, dc, at)
			if f.narrowed() {
				f.scope = at
				children, err = matchItems(c.Ctx, c, dc, &f)
			}
			if err != nil {
				return err
			}
			if err := kit.Sort(order, children, childOrder()); err != nil {
				return err
			}
			rows, total := kit.Slice(page, children)
			return kit.List(c, ui.TableSpec[drivesvc.Child]{
				Noun: "items", Columns: childColumns(),
				Total: total, Page: page.Number, PageSize: page.Size,
				Filtered: f.narrowed(),
			}, rows, func(ch drivesvc.Child) []string { return []string{ch.LinkID} })
		}),
	}
	f.registerNarrowing(c.Flags())
	order.Register(c, "name", "size", "modified")
	page.Register(c, "items")
	return c
}

// childOrder is how a Drive listing may be ordered. A folder's children arrive
// whole, so the ordering is this process's to do.
//
// Folders lead whichever key is chosen, as they do in every file manager: a
// listing sorted by size that scattered the folders through it would be
// answering a question about files with a mix of both.
func childOrder() kit.Comparators[drivesvc.Child] {
	byKind := func(a, b drivesvc.Child) int {
		return kit.Ints(boolInt(a.Type != drivesvc.TypeFolder), boolInt(b.Type != drivesvc.TypeFolder))
	}
	then := func(next func(a, b drivesvc.Child) int) func(a, b drivesvc.Child) int {
		return func(a, b drivesvc.Child) int {
			if c := byKind(a, b); c != 0 {
				return c
			}
			return next(a, b)
		}
	}
	return kit.Comparators[drivesvc.Child]{
		"name": then(func(a, b drivesvc.Child) int { return kit.Fold(a.Name, b.Name) }),
		"size": then(func(a, b drivesvc.Child) int { return kit.Ints(a.Size, b.Size) }),
		"modified": then(func(a, b drivesvc.Child) int {
			return kit.Ints(a.ModifyTime, b.ModifyTime)
		}),
	}
}

func boolInt(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

func itemsGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get PATH",
		Short: "Show a file or folder's details",
		Args:  cobra.ExactArgs(1),
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			dc, err := context(c)
			if err != nil {
				return err
			}
			info, err := c.App.Drive.Info(c.Ctx, dc, c.Args[0])
			if err != nil {
				return err
			}
			fields := []ui.Field{
				{Label: "Name", Value: info.Name},
				{Label: "Location", Value: info.Location},
				{Label: "Type", Value: info.Type},
				{Label: "MIME Type", Value: info.MIMEType},
				{Label: "Created By", Value: info.CreatedBy},
				kit.SignatureField(info.Signature),
				{Label: "Uploaded", Value: units.Time(info.Uploaded)},
				{Label: "Modified", Value: units.Time(info.Modified)},
				{Label: "Size", Value: units.Size(info.Size)},
			}
			if info.OriginalSize != 0 && info.OriginalSize != info.Size {
				fields = append(fields, ui.Field{Label: "Original Size", Value: units.Size(info.OriginalSize)})
			}
			fields = append(fields,
				ui.Field{Label: "SHA-1", Value: info.SHA1},
				ui.Field{Label: "Shared", Value: yesNo(info.Shared), Always: true},
				ui.Field{Label: "ID", Value: info.LinkID, ID: true},
			)
			return kit.Show(c, ui.RecordSpec{Object: info, Fields: fields})
		}),
	}
}

// ── moving bytes ──

func itemsUploadCmd() *cobra.Command {
	var recursive bool
	ifExists := kit.Enum{
		Name:   "if-exists",
		Usage:  "What to do when the folder already has that name",
		Values: []string{"rename", "replace", "skip"},
	}
	c := &cobra.Command{
		Use:   "upload SRC [DEST]",
		Short: "Upload a file or directory",
		Long: "Upload a file or directory.\n\n" +
			"A name already taken is refused, so nothing is overwritten by accident.\n" +
			"--if-exists answers instead:\n\n" +
			"  replace  a new revision, keeping the file's history\n" +
			"  rename   keep both, numbering the one being uploaded\n" +
			"  skip     leave what is there alone\n\n" +
			"With --recursive that answer is about the folder the tree lands in.\n\n" +
			"SRC of - reads standard input, and then DEST has to name the file.",
		Args: cobra.RangeArgs(1, 2),
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			dc, err := context(c)
			if err != nil {
				return err
			}
			src := c.Args[0]
			dest := "/"
			if len(c.Args) >= 2 {
				dest = c.Args[1]
			}
			choice, err := ifExists.Value()
			if err != nil {
				return err
			}
			onConflict := drivesvc.OnConflict(choice)
			if recursive {
				if src == "-" {
					return kit.Fail("--recursive cannot read from standard input.")
				}
				return uploadTree(c, dc, src, dest, onConflict)
			}
			return uploadOne(c, dc, src, dest, onConflict)
		}),
	}
	c.Flags().BoolVar(&recursive, "recursive", false, "Upload a directory and everything under it")
	ifExists.Register(c)
	return c
}

func uploadOne(c *kit.Invocation, dc *drivesvc.Context, src, dest string, on drivesvc.OnConflict) error {
	var r io.Reader
	var size int64
	var name string

	if src == "-" {
		stdin, err := c.App.Stdin("SRC -")
		if err != nil {
			return err
		}
		r = stdin
		name = fmt.Sprintf("stdin-%d", time.Now().Unix())
		// A stream has no name, so DEST carries it: an existing folder receives
		// the generated name, and any other path is parent plus new file name.
		resolved, err := c.App.Drive.ResolvePath(c.Ctx, dc, dest)
		var notFound *errs.NotFound
		if err != nil && !errors.As(err, &notFound) {
			return err
		}
		if err != nil || !resolved.IsFolder {
			name = path.Base(dest)
			dest = path.Dir(dest)
		}
	} else {
		fi, err := os.Stat(src)
		if err != nil {
			return err
		}
		if fi.IsDir() {
			return kit.Fail("%s is a directory.", src).Hint("--recursive to upload it and its contents.")
		}
		f, err := os.Open(src)
		if err != nil {
			return err
		}
		defer func() { _ = f.Close() }()
		r, size, name = f, fi.Size(), filepath.Base(src)
	}

	plan, err := c.App.Drive.PlanUpload(c.Ctx, dc, dest, name, on)
	if err != nil {
		return sayHowToGoAhead(err, on)
	}
	// What the plan says is what gets reported, so a skip is a count of nothing
	// rather than a claim to have uploaded something, and a dry run promises the
	// name the file will really end up with.
	spec := ui.ResultSpec{
		Action: ui.Uploaded, Count: 1, Name: plan.Name,
		Detail: "to " + dest, Extra: map[string]any{"size": size},
	}
	switch {
	case plan.Nothing:
		spec.Count = 0
		spec.Detail = fmt.Sprintf("- %s already has %s", dest, name)
	case plan.Revision:
		spec.Detail = "to " + dest + " as a new revision"
	}
	return sayHowToGoAhead(kit.Mutate(c, spec, func() error {
		return c.App.Drive.Upload(c.Ctx, dc, plan, r, drivesvc.UploadOptions{
			Label: "Uploading " + plan.Name, Progress: ui.NewProgress(c.UI()), TotalHint: size,
		})
	}), on)
}

// sayHowToGoAhead turns a refusal to write over a name into the question Proton's
// own client asks.
//
// The three answers are offered only when none was given. A refusal that survives
// an answer is one no answer reaches: a file and a folder cannot take each
// other's place, whatever the flag says.
func sayHowToGoAhead(err error, on drivesvc.OnConflict) error {
	var exists *errs.Exists
	if !errors.As(err, &exists) {
		return err
	}
	if on != drivesvc.ConflictRefuse {
		exists.Answers = []string{"Trash what is in the way, or upload somewhere else."}
		return exists
	}
	exists.Answers = []string{
		"--if-exists replace to write the bytes as a new revision of it",
		"--if-exists rename to keep both",
		"--if-exists skip to leave it alone",
	}
	return exists
}

// uploadTree mirrors a local directory into Drive.
//
// The walk is local and the plan is remote: what the tree will do is settled
// before any of it is done, so the count is what will be written, a dry run
// promises the same, and anything in the way is refused while nothing has been.
func uploadTree(c *kit.Invocation, dc *drivesvc.Context, src, dest string, on drivesvc.OnConflict) error {
	srcAbs, err := filepath.Abs(src)
	if err != nil {
		return err
	}
	asked := filepath.ToSlash(filepath.Join(dest, filepath.Base(srcAbs)))

	var items []drivesvc.TreeItem
	if err := filepath.Walk(srcAbs, func(p string, info os.FileInfo, walkErr error) error {
		if walkErr != nil || p == srcAbs {
			return walkErr
		}
		rel, err := filepath.Rel(srcAbs, p)
		if err != nil {
			return err
		}
		items = append(items, drivesvc.TreeItem{
			Path:  filepath.ToSlash(filepath.Join(asked, rel)),
			IsDir: info.IsDir(),
		})
		return nil
	}); err != nil {
		return err
	}

	plan, err := c.App.Drive.PlanTree(c.Ctx, dc, asked, items, on)
	if err != nil {
		return sayHowToGoAhead(err, on)
	}
	spec := ui.ResultSpec{
		Action: ui.Uploaded, Kind: "items", Count: landing(plan),
		Detail: "to " + plan.Top,
	}
	if plan.Nothing {
		spec.Count = 0
		spec.Detail = fmt.Sprintf("- %s already has %s", path.Dir(plan.Top), path.Base(plan.Top))
	}
	return sayHowToGoAhead(kit.Mutate(c, spec, func() error {
		if err := c.App.Drive.CreateFolders(c.Ctx, dc, plan.Folders); err != nil {
			return err
		}
		for i, file := range plan.Files {
			// Each file draws its own bar, so without saying where it sits in the
			// tree a five-hundred-file upload is five hundred identical lines and
			// no sense of how far along it is.
			if err := uploadInto(c, dc, plan, file, srcAbs, i+1, len(plan.Files)); err != nil {
				return err
			}
		}
		return nil
	}), on)
}

// landing is how many things a tree upload puts inside its destination: the
// files, and the folders it has to make to hold them. The tree's own folder is
// the destination rather than something landing in it, so it is not among them.
func landing(plan *drivesvc.TreePlan) int {
	n := len(plan.Files)
	for _, folder := range plan.Folders {
		if folder != plan.Top {
			n++
		}
	}
	return n
}

// uploadInto writes one of a tree's files, reading it from where the tree came
// from: the plan names it by where it lands, which is the same path with the
// tree's own folder swapped for the local one.
func uploadInto(c *kit.Invocation, dc *drivesvc.Context, plan *drivesvc.TreePlan, file drivesvc.TreeFile, srcAbs string, index, count int) error {
	local := filepath.Join(srcAbs, filepath.FromSlash(strings.TrimPrefix(file.Path, plan.Top+"/")))
	f, err := os.Open(local)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return err
	}

	answer := drivesvc.ConflictRefuse
	if file.Replaces {
		answer = drivesvc.ConflictReplace
	}
	name := path.Base(file.Path)
	filePlan, err := c.App.Drive.PlanUpload(c.Ctx, dc, path.Dir(file.Path), name, answer)
	if err != nil {
		return err
	}
	return c.App.Drive.Upload(c.Ctx, dc, filePlan, f, drivesvc.UploadOptions{
		Label:     "Uploading " + name,
		Progress:  ui.Batch(ui.NewProgress(c.UI()), index, count),
		TotalHint: info.Size(),
	})
}

func itemsDownloadCmd() *cobra.Command {
	var dest kit.Destination
	c := &cobra.Command{
		Use:   "download PATH",
		Short: "Download a file",
		Args:  cobra.ExactArgs(1),
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			if err := dest.Validate(true); err != nil {
				return err
			}
			dc, err := context(c)
			if err != nil {
				return err
			}
			src := c.Args[0]
			name := path.Base(src)

			if dest.Stdout() {
				// Streaming to stdout means the bar would compete with the
				// payload's own consumer for the terminal, so it stays off.
				return c.App.Drive.Download(c.Ctx, dc, src, c.UI().Out, drivesvc.DownloadOptions{
					Label: "Downloading " + name, OnSignatureIssue: signatureIssue(c, name),
				})
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Downloaded, Count: 1, Name: name,
				Detail: "to " + dest.Describe(),
			}, func() error {
				// A download that fails its integrity checks part way through must
				// not leave a plausible-looking file behind, so the bytes land
				// beside the destination and are moved into place at the end.
				_, err := dest.Stream(c, name, func(w io.Writer) error {
					return c.App.Drive.Download(c.Ctx, dc, src, w, drivesvc.DownloadOptions{
						Label: "Downloading " + name, Progress: ui.NewProgress(c.UI()),
						OnSignatureIssue: signatureIssue(c, name),
					})
				})
				return err
			})
		}),
	}
	dest.Register(c)
	return c
}

// signatureIssue reports a block whose author signature does not check out.
//
// The content is already known to be what the revision was signed for, so this is
// not a reason to refuse the file; it is a reason to say who cannot be confirmed as
// having written it. Once said, it is not said again for every remaining block.
func signatureIssue(c *kit.Invocation, name string) func(int, string) {
	reported := false
	return func(index int, verdict string) {
		if reported {
			return
		}
		reported = true
		c.Warn("%s downloaded, but the signature on block %d is %s, so who wrote it cannot be confirmed.",
			name, index, verdict)
	}
}

// ── organising ──

func itemsUpdateCmd() *cobra.Command {
	var name string
	c := &cobra.Command{
		Use:   "update PATH",
		Short: "Rename a file or folder",
		Long: "Rename a file or folder.\n\n" +
			"A name is a field like any other, so changing it is `update --name` rather\n" +
			"than a verb of its own. To put something somewhere else, use `move`.",
		Args: cobra.ExactArgs(1),
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			dc, err := context(c)
			if err != nil {
				return err
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Updated, Count: 1, Name: path.Base(c.Args[0]),
				Detail: "to " + name,
			}, func() error {
				return c.App.Drive.Rename(c.Ctx, dc, c.Args[0], name)
			})
		}),
	}
	c.Flags().StringVar(&name, "name", "", "New name, without a path")
	_ = c.MarkFlagRequired("name")
	return c
}

func itemsMoveCmd() *cobra.Command {
	return relocateCmd("move", "Move files or folders into another folder", ui.Moved,
		func(c *kit.Invocation, dc *drivesvc.Context, src string, into *drivesvc.Resolved) error {
			return c.App.Drive.Move(c.Ctx, dc, src, into)
		})
}

func itemsCopyCmd() *cobra.Command {
	return relocateCmd("copy", "Copy files into another folder", ui.Copied,
		func(c *kit.Invocation, dc *drivesvc.Context, src string, into *drivesvc.Resolved) error {
			return c.App.Drive.Copy(c.Ctx, dc, src, into)
		})
}

// relocateCmd builds move and copy, which differ only in whether the original
// stays. Both take the selection model, so a filtered move is as available as a
// filtered trash.
func relocateCmd(use, short string, action ui.Action,
	apply func(*kit.Invocation, *drivesvc.Context, string, *drivesvc.Resolved) error) *cobra.Command {
	var f filters
	var into string
	c := &cobra.Command{
		Use:   use + " [PATH...]",
		Short: short,
		RunE: kit.Run([]kit.Step{kit.StepSelection(f.set, filterHint, itemScope)}, func(c *kit.Invocation) error {
			dc, err := context(c)
			if err != nil {
				return err
			}
			sel, err := selectItems(c, dc, &f)
			if err != nil {
				return err
			}
			// The destination is looked for before anything is promised about it, and
			// once for the whole selection rather than once per item.
			dest, err := c.App.Drive.ResolveFolder(c.Ctx, dc, into)
			if err != nil {
				return err
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: action, Kind: "items", Count: sel.Len(), IDs: sel.IDs,
				Detail: "into " + into, Preview: sel.Preview(),
			}, func() error {
				for _, row := range sel.Rows {
					if err := apply(c, dc, row.Path, dest); err != nil {
						return err
					}
				}
				return nil
			})
		}),
	}
	c.Flags().StringVar(&into, "into", "", "Destination folder")
	_ = c.MarkFlagRequired("into")
	f.register(c)
	return c
}

func itemsTrashCmd() *cobra.Command {
	return removeCmd("trash", "Move files or folders to the trash", ui.Trashed, false)
}

func itemsDeleteCmd() *cobra.Command {
	return removeCmd("delete", "Delete files or folders permanently", ui.Deleted, true)
}

func removeCmd(use, short string, action ui.Action, permanent bool) *cobra.Command {
	var f filters
	c := &cobra.Command{
		Use:   use + " [PATH...]",
		Short: short,
		RunE: kit.Run([]kit.Step{kit.StepSelection(f.set, filterHint, itemScope)}, func(c *kit.Invocation) error {
			dc, err := context(c)
			if err != nil {
				return err
			}
			sel, err := selectItems(c, dc, &f)
			if err != nil {
				return err
			}
			detail := ""
			if !permanent {
				detail = "to trash"
			}
			return kit.Attempt(c, ui.ResultSpec{
				Action: action, Kind: "items", Count: sel.Len(), IDs: sel.IDs,
				Detail: detail, Preview: sel.Preview(),
			}, func() ([]drivesvc.Refused, error) {
				if permanent {
					return c.App.Drive.Delete(c.Ctx, dc, sel.IDs)
				}
				return c.App.Drive.Trash(c.Ctx, dc, sel.IDs)
			})
		}),
	}
	f.register(c)
	return c
}

// ── revisions ──

func revisionsCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "revisions",
		Short: "Earlier versions of a file",
		Long: "Earlier versions of a file.\n\n" +
			"Uploading over a file with `--if-exists replace` keeps what was there as a\n" +
			"revision. Any of them can be read back without disturbing the file, put back\n" +
			"in place, or dropped from the history.",
	}
	c.AddCommand(revisionsListCmd(), revisionsDownloadCmd(), revisionsRestoreCmd(), revisionsDeleteCmd())
	return c
}

func revisionState(state int) string {
	switch state {
	case 0:
		return "draft"
	case 1:
		return "active"
	case 2:
		return "inactive"
	}
	return "unknown"
}

func revisionsListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list PATH",
		Short: "List a file's earlier versions",
		Args:  cobra.ExactArgs(1),
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			dc, err := context(c)
			if err != nil {
				return err
			}
			revs, err := c.App.Drive.RevisionsList(c.Ctx, dc, c.Args[0])
			if err != nil {
				return err
			}
			return kit.List(c, ui.TableSpec[drivesvc.Revision]{
				Noun:  "revisions",
				Total: ui.Unknown, Page: ui.Unpaged,
				Columns: []ui.Column[drivesvc.Revision]{
					{Header: "ID", ID: true, Cell: func(r drivesvc.Revision) string { return r.ID }},
					{Header: "STATE", Cell: func(r drivesvc.Revision) string { return revisionState(r.State) }},
					{Header: "SIZE", Right: true, Cell: func(r drivesvc.Revision) string { return units.Size(r.Size) }},
					{Header: "CREATED", Cell: func(r drivesvc.Revision) string { return units.Time(r.CreateTime) }},
					{Header: "AUTHOR", Flex: true, Cell: func(r drivesvc.Revision) string { return r.Author }},
				},
			}, revs, func(r drivesvc.Revision) []string { return []string{r.ID} })
		}),
	}
}

// findRevision resolves the file and the version every PATH REVISION_REF command
// addresses, so each of them says which version it is about to act on rather than
// the reference it was handed.
func findRevision(c *kit.Invocation) (*drivesvc.FileRevision, error) {
	dc, err := context(c)
	if err != nil {
		return nil, err
	}
	return c.App.Drive.FindRevision(c.Ctx, dc, c.Args[0], c.Args[1])
}

func revisionsDownloadCmd() *cobra.Command {
	var dest kit.Destination
	c := &cobra.Command{
		Use:   "download PATH REVISION_REF",
		Short: "Download an earlier version of a file",
		Long: "Download an earlier version of a file.\n\n" +
			"The file keeps whatever it holds now: this reads an old version out, where\n" +
			"`revisions restore` puts one back in place.",
		Args: cobra.ExactArgs(2),
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			if err := dest.Validate(true); err != nil {
				return err
			}
			rev, err := findRevision(c)
			if err != nil {
				return err
			}
			label := fmt.Sprintf("Downloading %s of %s", units.Time(rev.CreateTime), rev.File)

			if dest.Stdout() {
				return c.App.Drive.DownloadRevision(c.Ctx, rev, c.UI().Out, drivesvc.DownloadOptions{
					Label: label, OnSignatureIssue: signatureIssue(c, rev.File),
				})
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Downloaded, Count: 1, Name: rev.File,
				Detail: fmt.Sprintf("as it was on %s to %s", units.Time(rev.CreateTime), dest.Describe()),
				IDs:    []string{rev.ID},
			}, func() error {
				_, err := dest.Stream(c, rev.File, func(w io.Writer) error {
					return c.App.Drive.DownloadRevision(c.Ctx, rev, w, drivesvc.DownloadOptions{
						Label: label, Progress: ui.NewProgress(c.UI()),
						OnSignatureIssue: signatureIssue(c, rev.File),
					})
				})
				return err
			})
		}),
	}
	dest.Register(c)
	return c
}

func revisionsRestoreCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "restore PATH REVISION_REF",
		Short: "Restore a file to an earlier version",
		Args:  cobra.ExactArgs(2),
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			rev, err := findRevision(c)
			if err != nil {
				return err
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Restored, Count: 1, Name: rev.File,
				Detail: "to the version from " + units.Time(rev.CreateTime), IDs: []string{rev.ID},
			}, func() error {
				return c.App.Drive.RevisionRestore(c.Ctx, rev)
			})
		}),
	}
}

func revisionsDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete PATH REVISION_REF",
		Short: "Delete an earlier version permanently",
		Args:  cobra.ExactArgs(2),
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			rev, err := findRevision(c)
			if err != nil {
				return err
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Deleted, Kind: "revisions", Count: 1,
				Name: units.Time(rev.CreateTime), Detail: "of " + rev.File,
				IDs: []string{rev.ID},
			}, func() error {
				return c.App.Drive.RevisionDelete(c.Ctx, rev)
			})
		}),
	}
}

// ── creating a folder ──

// A folder is the one thing in Drive you make rather than upload, so it is
// `items create`: Drive addresses one collection, and rename, move, trash and
// delete already treat files and folders alike.
func itemsCreateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "create PATH",
		Short: "Create a folder, and any missing folder above it",
		Args:  cobra.ExactArgs(1),
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			dc, err := context(c)
			if err != nil {
				return err
			}
			// What the folders above it are is worked out before any of them is
			// made, so the count is the answer to what happened rather than a
			// claim, and a dry run promises the same number.
			paths, err := c.App.Drive.PlanFolders(c.Ctx, dc, c.Args[0])
			if err != nil {
				return err
			}
			spec := ui.ResultSpec{
				Action: ui.Created, Kind: "folders", Count: len(paths), Name: c.Args[0],
				Extra: map[string]any{"paths": paths},
			}
			if len(paths) > 1 {
				spec.Detail = "down to " + c.Args[0]
			}
			return kit.Mutate(c, spec, func() error {
				return c.App.Drive.CreateFolders(c.Ctx, dc, paths)
			})
		}),
	}
}
