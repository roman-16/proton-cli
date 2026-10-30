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
	"github.com/roman-16/proton-cli/internal/localfile"
	drivesvc "github.com/roman-16/proton-cli/internal/service/drive"
	"github.com/roman-16/proton-cli/internal/ui"
	"github.com/roman-16/proton-cli/internal/units"
	"github.com/spf13/cobra"
)

func itemsCmd() *cobra.Command {
	c := &cobra.Command{Use: "items", Short: "Files and folders"}
	c.AddCommand(itemsAbuseCmd(), itemsListCmd(), itemsGetCmd(), itemsCreateCmd(), itemsUploadCmd(),
		itemsDownloadCmd(), itemsUpdateCmd(), itemsMoveCmd(), itemsCopyCmd(),
		itemsTrashCmd(), itemsDeleteCmd(), revisionsCmd(), shareCmd(filesShared()))
	return c
}

func childColumns() []ui.Column[drivesvc.Child] {
	return []ui.Column[drivesvc.Child]{
		{Header: "ID", ID: true, Cell: func(ch drivesvc.Child) string { return ch.LinkID }},
		{Header: "TYPE", Cell: func(ch drivesvc.Child) string { return ch.Type }},
		{Header: "SIZE", Right: true, Cell: func(ch drivesvc.Child) string {
			if ch.Type == drivesvc.TypeFolder {
				return ""
			}
			return units.Size(ch.Size)
		}},
		{Header: "MODIFIED", Cell: func(ch drivesvc.Child) string { return units.Time(ch.ModifyTime) }},
		{Header: "NAME", Flex: true, Cell: func(ch drivesvc.Child) string {
			if ch.Path != "" {
				return ch.Path
			}
			return ch.Name
		}},
	}
}

func itemsListCmd() *cobra.Command {
	var f filters
	var t tree
	var page kit.Page
	var order kit.Order
	c := &cobra.Command{
		Use:   "list [PATH]",
		Short: "List what is in a folder",
		Long: "List what is in a folder.\n\n" +
			"Takes the same filters as move, copy, trash and delete, so you can preview a\n" +
			"selection here before acting on it. What PATH is here, those commands call\n" +
			"--scope. A filtered listing shows each item's full path.\n\n" +
			"--keyword matches names, and the contents of your text files once\n" +
			"`" + kit.Program + " index create drive` has downloaded them. Elsewhere - a computer, a\n" +
			"share, a link - it matches names alone, and says so.\n\n" +
			"PATH is in your own files. --computer REF lists inside a computer instead,\n" +
			"--shared REF inside something somebody shared with you, and --link URL inside\n" +
			"a public link somebody sent you. In the last two, / is the item itself.",
		RunE: kit.Run([]kit.Step{t.supply}, func(c *kit.Invocation) error {
			dc, err := t.context(c)
			if err != nil {
				return err
			}
			at := "/"
			if len(c.Args) > 0 {
				at = c.Args[0]
			}
			var cover drivesvc.Coverage
			children, err := c.App.Drive.List(c.Ctx, dc, at)
			if f.narrowed() {
				f.scope = at
				children, cover, err = matchItems(c.Ctx, c, dc, &f)
			}
			if err != nil {
				return err
			}
			if err := kit.Sort(order, children, childOrder()); err != nil {
				return err
			}
			rows, total := kit.Slice(page, children)
			if err := kit.List(c, ui.TableSpec[drivesvc.Child]{
				Noun: "items", Columns: childColumns(),
				Total: total, Page: page.Number, PageSize: page.Size,
				Filtered: f.narrowed(),
			}, rows); err != nil {
				return err
			}
			shortIndex(c, cover, &f)
			return nil
		}),
	}
	f.registerNarrowing(c.Flags())
	t.register(c, reads)
	order.Register(c, "name", "size", "modified")
	page.Default = screenful
	page.Register(c, "items")
	return c
}

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
	var t tree
	c := &cobra.Command{
		Use:   "get PATH",
		Short: "Show a file or folder's details",
		Long: "Show a file or folder's details.\n\n" +
			"Reached through a public link, the details include the link, whether it allows\n" +
			"editing and, for a link saved with `shared add`, the password its owner set on\n" +
			"it. Behind a link, Created By names you on what you uploaded there yourself,\n" +
			"signed in. Anything else names nobody and Signature reads anonymous.",
		RunE: kit.Run([]kit.Step{t.supply}, func(c *kit.Invocation) error {
			dc, err := t.context(c)
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
				ui.Field{Label: "Public Link", Value: info.URL},
				ui.Field{Label: "Link Access", Value: info.LinkAccess},
				ui.Field{Label: "Link Password", Value: info.LinkPassword},
				ui.Field{Label: "Shared", Value: yesNo(info.Shared), Always: true},
				ui.Field{Label: "ID", Value: info.LinkID, ID: true},
			)
			return kit.Show(c, ui.RecordSpec{Object: info, Fields: fields})
		}),
	}
	t.register(c, reads)
	return c
}

func itemsUploadCmd() *cobra.Command {
	var recursive bool
	var t tree
	ifExists := kit.Enum{
		Name:   "if-exists",
		Usage:  "What to do when the folder already has that name",
		Values: []string{"rename", "replace", "skip"},
	}
	c := &cobra.Command{
		Use:   "upload SRC [DEST]",
		Short: "Upload a file or directory",
		Long: "Upload a file or directory.\n\n" +
			"A name already taken is refused. --if-exists answers instead:\n\n" +
			"  replace  a new revision, keeping the file's history\n" +
			"  rename   keep both, numbering the one being uploaded\n" +
			"  skip     leave what is there alone\n\n" +
			"With --recursive that answer is about the folder the tree lands in.\n\n" +
			"SRC of - reads standard input, and then DEST has to name the file.\n\n" +
			"DEST is in your own files. --computer REF uploads into a computer instead,\n" +
			"--shared REF into something somebody shared with you, and --link URL into a\n" +
			"public link. In the last two, / is the item itself. A share or a link has to\n" +
			"allow editing, and a link takes no new revisions: --if-exists replace is\n" +
			"refused there.",
		RunE: kit.Run([]kit.Step{t.supply}, func(c *kit.Invocation) error {
			choice, err := ifExists.Value()
			if err != nil {
				return err
			}
			onConflict := drivesvc.OnConflict(choice)
			if onConflict == drivesvc.ConflictReplace && t.link != "" {
				return noNewRevisions()
			}
			dc, err := t.context(c)
			if err != nil {
				return err
			}
			if onConflict == drivesvc.ConflictReplace && dc.Public() {
				return noNewRevisions()
			}
			src := c.Args[0]
			dest := "/"
			if len(c.Args) >= 2 {
				dest = c.Args[1]
			}
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
	t.register(c, adds)
	return c
}

func noNewRevisions() error {
	return kit.Fail("A public link cannot take a new revision of a file.").
		Hint("--if-exists rename to keep both, or --if-exists skip to leave it alone.")
}

func uploadOne(c *kit.Invocation, dc *drivesvc.Context, src, dest string, on drivesvc.OnConflict) error {
	var r io.Reader
	var size int64
	var name string
	var modified time.Time

	if src == "-" {
		stdin, err := c.App.Stdin("SRC -")
		if err != nil {
			return err
		}
		r = stdin
		name = fmt.Sprintf("stdin-%d", time.Now().Unix())
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
		r, size, name, modified = f, fi.Size(), filepath.Base(src), fi.ModTime()
	}

	plan, err := c.App.Drive.PlanUpload(c.Ctx, dc, dest, name, on)
	if err != nil {
		return sayHowToGoAhead(err, on)
	}
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
			Label: "Uploading " + plan.Name, Progress: ui.NewProgress(c.UI()),
			TotalHint: size, Modified: modified,
		})
	}), on)
}

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
			if err := uploadInto(c, dc, plan, file, srcAbs, i+1, len(plan.Files)); err != nil {
				return err
			}
		}
		return nil
	}), on)
}

func landing(plan *drivesvc.TreePlan) int {
	n := len(plan.Files)
	for _, folder := range plan.Folders {
		if folder != plan.Top {
			n++
		}
	}
	return n
}

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
		TotalHint: info.Size(), Modified: info.ModTime(),
	})
}

func itemsDownloadCmd() *cobra.Command {
	var dest kit.Destination
	var recursive bool
	var t tree
	c := &cobra.Command{
		Use:   "download PATH",
		Short: "Download a file or folder",
		Long: "Download a file or folder.\n\n" +
			"A folder is refused without --recursive. With it, the folder lands as a\n" +
			"directory of its own name inside --dest-dir, subfolders and all, and --dest is\n" +
			"refused: one path cannot take a tree.\n\n" +
			"A file already there with the size and modification time it has in Drive is\n" +
			"skipped, so running it again continues a download that stopped. A file there\n" +
			"that differs is refused without --force. Proton Docs and Sheets are not\n" +
			"downloaded.\n\n" +
			"Behind a public link, a file is / when the link points at the file itself, and\n" +
			"a path inside the folder when it points at a folder. A link with a password\n" +
			"takes it from --link-password-file, which takes - for stdin.",
		RunE: kit.Run([]kit.Step{t.supply}, func(c *kit.Invocation) error {
			if err := dest.Validate(!recursive); err != nil {
				return err
			}
			dc, err := t.context(c)
			if err != nil {
				return err
			}
			src := c.Args[0]
			if recursive {
				return downloadTree(c, dc, &dest, src)
			}
			file, err := c.App.Drive.ResolvePath(c.Ctx, dc, src)
			if err != nil {
				return err
			}
			if file.IsFolder {
				return kit.Fail("%s is a folder.", file.Describe(src)).
					Hint("--recursive to download it and its contents.")
			}
			name := file.Name
			if name == "" {
				name = path.Base(src)
			}
			if kind, app := file.OtherProduct(); kind != "" {
				return errs.Naming(name, kit.Fail("%s is a %s.", name, kind).Hint("open it in "+app))
			}

			if dest.Stdout() {
				return c.App.Drive.Download(c.Ctx, file, c.UI().Out, drivesvc.DownloadOptions{
					Label: "Downloading " + name, OnSignatureIssue: signatureIssue(c, name),
				})
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Downloaded, Count: 1, Name: name,
				Detail: "to " + dest.Describe(),
			}, func() error {
				_, err := dest.StreamStamped(c, func(w io.Writer) (string, time.Time, error) {
					err := c.App.Drive.Download(c.Ctx, file, w, drivesvc.DownloadOptions{
						Label: "Downloading " + name, Progress: ui.NewProgress(c.UI()),
						OnSignatureIssue: signatureIssue(c, name),
					})
					return name, file.Modified(), err
				})
				return err
			})
		}),
	}
	c.Flags().BoolVar(&recursive, "recursive", false, "Download a folder and everything under it")
	dest.Register(c)
	t.register(c, reads)
	return c
}

func downloadTree(c *kit.Invocation, dc *drivesvc.Context, dest *kit.Destination, src string) error {
	plan, err := c.App.Drive.PlanDownload(c.Ctx, dc, src)
	if err != nil {
		return err
	}
	dir, err := dest.Dir()
	if err != nil {
		return err
	}
	top := filepath.Join(dir, localfile.SafeName(plan.Top))
	owed, held, err := sortTree(top, plan.Files, dest.Force())
	if err != nil {
		return err
	}
	folders := plan.Folders
	if held > 0 {
		folders = missingFolders(top, plan.Folders)
		c.Note("%s already there and unchanged.", areFiles(held))
	}
	elsewhere(c, plan.Elsewhere)
	return kit.Mutate(c, ui.ResultSpec{
		Action: ui.Downloaded, Kind: "items", Count: len(owed) + len(folders),
		Detail: "to " + top,
	}, func() error {
		if err := localfile.EnsureDir(top); err != nil {
			return err
		}
		for _, folder := range folders {
			if err := localfile.EnsureDir(localPath(top, folder)); err != nil {
				return err
			}
		}
		for i, file := range owed {
			err := localfile.Stream(localPath(top, file.Path), file.Modified, func(w io.Writer) error {
				return c.App.Drive.DownloadPlanned(c.Ctx, dc, file, w, drivesvc.DownloadOptions{
					Label:            "Downloading " + file.Name,
					Progress:         ui.Batch(ui.NewProgress(c.UI()), i+1, len(owed)),
					OnSignatureIssue: signatureIssue(c, file.Name),
				})
			})
			if err != nil {
				return err
			}
		}
		return nil
	})
}

func sortTree(top string, files []drivesvc.PlannedFile, force bool) ([]drivesvc.PlannedFile, int, error) {
	owed := make([]drivesvc.PlannedFile, 0, len(files))
	held := 0
	var differ []string
	for _, file := range files {
		at := localPath(top, file.Path)
		switch {
		case force || !localfile.Exists(at):
			owed = append(owed, file)
		case localfile.Holds(at, file.Plain, file.Modified):
			held++
		default:
			differ = append(differ, strings.TrimPrefix(file.Path, "/"))
		}
	}
	if len(differ) > 0 {
		names := ui.Listing(differ)
		return nil, 0, errs.Naming(names, kit.Fail("%s in %s differ from the ones in Drive: %s.",
			ui.Quantity(len(differ), "files"), top, names).Hint("--force to overwrite "+them(len(differ))+"."))
	}
	return owed, held, nil
}

func missingFolders(top string, folders []string) []string {
	out := make([]string, 0, len(folders))
	for _, folder := range folders {
		if !localfile.Exists(localPath(top, folder)) {
			out = append(out, folder)
		}
	}
	return out
}

func areFiles(n int) string {
	if n == 1 {
		return "1 file is"
	}
	return fmt.Sprintf("%d files are", n)
}

func them(n int) string {
	if n == 1 {
		return "it"
	}
	return "them"
}

func elsewhere(c *kit.Invocation, e drivesvc.Elsewhere) {
	if e.Documents > 0 {
		c.Note("%s not downloaded; open %s in Proton Docs.", wasNot(e.Documents, "Proton Docs documents"), them(e.Documents))
	}
	if e.Spreadsheets > 0 {
		c.Note("%s not downloaded; open %s in Proton Sheets.", wasNot(e.Spreadsheets, "Proton Sheets spreadsheets"), them(e.Spreadsheets))
	}
}

func wasNot(n int, noun string) string {
	if n == 1 {
		return ui.Quantity(1, noun) + " was"
	}
	return ui.Quantity(n, noun) + " were"
}

func localPath(top, inTree string) string {
	parts := strings.Split(strings.Trim(inTree, "/"), "/")
	for i, part := range parts {
		parts[i] = localfile.SafeName(part)
	}
	return filepath.Join(append([]string{top}, parts...)...)
}

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

func itemsUpdateCmd() *cobra.Command {
	var name string
	var t tree
	c := &cobra.Command{
		Use:   "update PATH",
		Short: "Rename a file or folder",
		Long: "Rename a file or folder.\n\n" +
			"Renaming is `update --name`; there is no `rename` verb. To put something\n" +
			"somewhere else, use `move`.\n\n" +
			"PATH names something inside a tree, never the tree itself: to rename a\n" +
			"computer, use `computers update`.\n\n" +
			"PATH is in your own files. --computer REF renames inside a computer instead,\n" +
			"--shared REF inside something somebody shared with you, and --link URL inside\n" +
			"a public link. In a link, you can rename only what you uploaded yourself,\n" +
			"signed in, within the last hour.",
		RunE: kit.Run([]kit.Step{t.supply}, func(c *kit.Invocation) error {
			if strings.Trim(c.Args[0], "/") == "" {
				return kit.Fail("A tree has no name of its own to change here.").
					Hint("`" + kit.Program + " drive computers update REF --name NEW_NAME` renames a computer.")
			}
			dc, err := t.context(c)
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
	t.register(c, edits)
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

func relocateCmd(use, short string, action ui.Action,
	apply func(*kit.Invocation, *drivesvc.Context, string, *drivesvc.Resolved) error) *cobra.Command {
	var f filters
	var t tree
	var into string
	c := &cobra.Command{
		Use:   use + " [PATH...]",
		Short: short,
		RunE: kit.Run([]kit.Step{kit.StepSelection(f.set, filterHint, itemScope)}, func(c *kit.Invocation) error {
			dc, err := t.context(c)
			if err != nil {
				return err
			}
			sel, err := selectItems(c, dc, &f)
			if err != nil {
				return err
			}
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
	t.register(c, manages)
	return c
}

func itemsTrashCmd() *cobra.Command {
	var f filters
	var t tree
	c := &cobra.Command{
		Use:   "trash [PATH...]",
		Short: "Move files or folders to the trash",
		Long: "Move files or folders to the trash.\n\n" +
			"A public link has no trash. What you uploaded into one is removed with\n" +
			"`delete`.",
		RunE: kit.Run([]kit.Step{kit.StepSelection(f.set, filterHint, itemScope)}, func(c *kit.Invocation) error {
			dc, err := t.context(c)
			if err != nil {
				return err
			}
			sel, err := selectItems(c, dc, &f)
			if err != nil {
				return err
			}
			return kit.Attempt(c, ui.ResultSpec{
				Action: ui.Trashed, Kind: "items", Count: sel.Len(), IDs: sel.IDs,
				Detail: "to trash", Preview: sel.Preview(),
			}, func() ([]drivesvc.Refused, error) {
				return c.App.Drive.Trash(c.Ctx, dc, sel.IDs)
			})
		}),
	}
	f.register(c)
	t.register(c, manages)
	return c
}

func itemsDeleteCmd() *cobra.Command {
	var f filters
	var t tree
	c := &cobra.Command{
		Use:   "delete [PATH...]",
		Short: "Delete files or folders permanently",
		Long: "Delete files or folders permanently.\n\n" +
			"PATH is in your own files. --computer REF deletes inside a computer instead,\n" +
			"--shared REF inside something somebody shared with you, and --link URL inside\n" +
			"a public link. In a link, you can delete only what you uploaded yourself,\n" +
			"signed in, within the last hour. A link has no trash.",
		RunE: kit.Run([]kit.Step{t.supply, kit.StepSelection(f.set, filterHint, itemScope)}, func(c *kit.Invocation) error {
			dc, err := t.context(c)
			if err != nil {
				return err
			}
			sel, err := selectItems(c, dc, &f)
			if err != nil {
				return err
			}
			plan, err := c.App.Drive.PlanDelete(c.Ctx, dc, sel.Rows)
			if err != nil {
				return err
			}
			for _, refused := range plan.Refused {
				c.Warn("%v", refused)
			}
			sel = withoutRefusedItems(sel, plan.Refused)
			return kit.Attempt(c, ui.ResultSpec{
				Action: ui.Deleted, Kind: "items", Count: sel.Len(), IDs: sel.IDs,
				Preview: sel.Preview(),
			}, func() ([]drivesvc.Refused, error) {
				return c.App.Drive.Delete(c.Ctx, dc, plan)
			})
		}),
	}
	f.register(c)
	t.register(c, edits)
	return c
}

func revisionsCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "revisions",
		Short: "Earlier versions of a file",
		Long: "Earlier versions of a file.\n\n" +
			"Uploading over a file with `--if-exists replace` keeps what was there as a\n" +
			"revision. You can read any revision back, restore it, or delete it.",
	}
	c.AddCommand(revisionsAbuseCmd(), revisionsListCmd(), revisionsDownloadCmd(), revisionsRestoreCmd(), revisionsDeleteCmd())
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
	var t tree
	c := &cobra.Command{
		Use:   "list PATH",
		Short: "List a file's earlier versions",
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			dc, err := t.context(c)
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
			}, revs)
		}),
	}
	t.register(c, manages)
	return c
}

func findRevision(c *kit.Invocation, t *tree) (*drivesvc.FileRevision, error) {
	dc, err := t.context(c)
	if err != nil {
		return nil, err
	}
	return c.App.Drive.FindRevision(c.Ctx, dc, c.Args[0], c.Args[1])
}

func revisionsDownloadCmd() *cobra.Command {
	var dest kit.Destination
	var t tree
	c := &cobra.Command{
		Use:   "download PATH REVISION_REF",
		Short: "Download an earlier version of a file",
		Long: "Download an earlier version of a file.\n\n" +
			"The file itself is not changed. To put an old version back in place, use\n" +
			"`revisions restore`.",
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			if err := dest.Validate(true); err != nil {
				return err
			}
			rev, err := findRevision(c, &t)
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
	t.register(c, manages)
	return c
}

func revisionsRestoreCmd() *cobra.Command {
	var t tree
	c := &cobra.Command{
		Use:   "restore PATH REVISION_REF",
		Short: "Restore a file to an earlier version",
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			rev, err := findRevision(c, &t)
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
	t.register(c, manages)
	return c
}

func revisionsDeleteCmd() *cobra.Command {
	var t tree
	c := &cobra.Command{
		Use:   "delete PATH REVISION_REF",
		Short: "Delete an earlier version permanently",
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			rev, err := findRevision(c, &t)
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
	t.register(c, manages)
	return c
}

func itemsCreateCmd() *cobra.Command {
	var t tree
	c := &cobra.Command{
		Use:   "create PATH",
		Short: "Create a folder, and any missing folder above it",
		Long: "Create a folder, and any missing folder above it.\n\n" +
			"PATH is in your own files. --computer REF creates inside a computer instead,\n" +
			"--shared REF inside something somebody shared with you, and --link URL inside\n" +
			"a public link. A share or a link has to allow editing.",
		RunE: kit.Run([]kit.Step{t.supply}, func(c *kit.Invocation) error {
			dc, err := t.context(c)
			if err != nil {
				return err
			}
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
	t.register(c, adds)
	return c
}
