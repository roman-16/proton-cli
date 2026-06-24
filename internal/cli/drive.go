package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/roman-16/proton-cli/internal/render"
	drivesvc "github.com/roman-16/proton-cli/internal/service/drive"
	"github.com/roman-16/proton-cli/internal/view"
	"github.com/spf13/cobra"
)

func newDriveCmd() *cobra.Command {
	c := &cobra.Command{Use: "drive", Short: "Drive operations"}
	c.AddCommand(driveItemsCmd(), driveFoldersCmd(), driveTrashCmd(), driveShareCmd(), driveInvitationsCmd())
	return c
}

func driveCtx(c *Ctx) (*drivesvc.Context, error) {
	u, err := c.App.Unlock(c.Ctx)
	if err != nil {
		return nil, err
	}
	return c.App.Drive.Resolve(c.Ctx, u)
}

func driveTypeLabel(t int) string {
	if t == 1 {
		return "DIR "
	}
	return "FILE"
}

// ── drive items ──

func driveItemsCmd() *cobra.Command {
	c := &cobra.Command{Use: "items", Short: "Manage files and folders"}
	c.AddCommand(itemsListCmd(), itemsInfoCmd(), itemsUploadCmd(), itemsDownloadCmd(), itemsRenameCmd(), itemsMoveCmd(), itemsDeleteCmd())
	return c
}

func itemsInfoCmd() *cobra.Command {
	return &cobra.Command{
		Use: "info PATH", Short: "Show metadata for a file or folder",
		Args: cobra.ExactArgs(1),
		RunE: run([]Step{stepAuth}, func(c *Ctx) error {
			dc, err := driveCtx(c)
			if err != nil {
				return err
			}
			info, err := c.App.Drive.Info(c.Ctx, dc, c.Args[0])
			if err != nil {
				return err
			}
			if c.R().Format != render.FormatText {
				return c.R().Object(info)
			}
			out := c.R().Stdout
			p := func(k, v string) { _, _ = fmt.Fprintf(out, "%-14s %s\n", k+":", v) }
			p("name", info.Name)
			p("location", info.Location)
			p("type", info.Type)
			if info.MIMEType != "" {
				p("mime_type", info.MIMEType)
			}
			if info.CreatedBy != "" {
				p("created_by", info.CreatedBy)
			}
			p("signature", info.Signature)
			p("uploaded", render.Time(info.Uploaded))
			if info.Modified != 0 {
				p("modified", render.Time(info.Modified))
			}
			p("size", render.Size(info.Size))
			if info.OriginalSize != 0 {
				p("original_size", render.Size(info.OriginalSize))
			}
			if info.SHA1 != "" {
				p("sha1", info.SHA1)
			}
			p("shared", yesNo(info.Shared))
			p("link_id", info.LinkID)
			return nil
		}),
	}
}

func itemsListCmd() *cobra.Command {
	return &cobra.Command{
		Use: "list [PATH]", Short: "List folder contents (decrypted names)",
		Args: cobra.MaximumNArgs(1),
		RunE: run([]Step{stepAuth}, func(c *Ctx) error {
			dc, err := driveCtx(c)
			if err != nil {
				return err
			}
			path := "/"
			if len(c.Args) > 0 {
				path = c.Args[0]
			}
			children, err := c.App.Drive.List(c.Ctx, dc, path)
			if err != nil {
				return err
			}
			return view.Render(c.R(), c.short(), c.App.IDCache, view.List[drivesvc.Child]{
				Columns: []view.Column[drivesvc.Child]{
					{Header: "TYPE", Cell: func(ch drivesvc.Child) string { return driveTypeLabel(ch.Type) }},
					{Header: "SIZE", Cell: func(ch drivesvc.Child) string { return render.Size(ch.Size) }},
					{Header: "NAME", Cell: func(ch drivesvc.Child) string { return ch.Name }},
					{Header: "LINK_ID", ID: true, Cell: func(ch drivesvc.Child) string { return ch.LinkID }},
				},
				CacheIDs: func(ch drivesvc.Child) []string { return []string{ch.LinkID} },
			}, children)
		}),
	}
}

func itemsUploadCmd() *cobra.Command {
	var recursive bool
	c := &cobra.Command{
		Use: "upload SRC [DEST]", Short: "Upload a file (SRC=- reads from stdin)",
		Args: cobra.RangeArgs(1, 2),
		RunE: run([]Step{stepAuth}, func(c *Ctx) error {
			dc, err := driveCtx(c)
			if err != nil {
				return err
			}
			src := c.Args[0]
			dest := "/"
			if len(c.Args) >= 2 {
				dest = c.Args[1]
			}
			if recursive {
				if src == "-" {
					return fmt.Errorf("--recursive is not supported with stdin")
				}
				return uploadRecursive(c, dc, src, dest)
			}
			return uploadOne(c, dc, src, dest)
		}),
	}
	c.Flags().BoolVar(&recursive, "recursive", false, "Recursively upload a directory")
	return c
}

func uploadOne(c *Ctx, dc *drivesvc.Context, src, dest string) error {
	var r io.Reader
	var size int64
	var name string
	if src == "-" {
		r = os.Stdin
		name = fmt.Sprintf("stdin-%d", time.Now().Unix())
	} else {
		fi, err := os.Stat(src)
		if err != nil {
			return err
		}
		if fi.IsDir() {
			return fmt.Errorf("%s is a directory (use --recursive)", src)
		}
		f, err := os.Open(src)
		if err != nil {
			return err
		}
		defer func() { _ = f.Close() }()
		r = f
		size = fi.Size()
		name = filepath.Base(src)
	}
	if c.App.DryRun {
		c.R().Info(fmt.Sprintf("dry-run: would upload %s → %s/%s (%s)", src, dest, name, render.Size(size)))
		return nil
	}
	if err := c.App.Drive.Upload(c.Ctx, dc, dest, name, r, drivesvc.UploadOptions{
		Label: fmt.Sprintf("Uploading %s", name), Quiet: c.R().Quiet, TotalHint: size,
	}); err != nil {
		return err
	}
	c.R().Success(fmt.Sprintf("Uploaded %s", name))
	return nil
}

func uploadRecursive(c *Ctx, dc *drivesvc.Context, src, dest string) error {
	srcAbs, err := filepath.Abs(src)
	if err != nil {
		return err
	}
	baseName := filepath.Base(srcAbs)
	top := filepath.ToSlash(filepath.Join(dest, baseName))
	if !c.App.DryRun {
		if err := c.App.Drive.CreateFolder(c.Ctx, dc, top); err != nil {
			return err
		}
	}
	return filepath.Walk(srcAbs, func(p string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if p == srcAbs {
			return nil
		}
		rel, err := filepath.Rel(srcAbs, p)
		if err != nil {
			return err
		}
		remote := filepath.ToSlash(filepath.Join(top, rel))
		if info.IsDir() {
			if c.App.DryRun {
				c.R().Info("dry-run: would mkdir " + remote)
				return nil
			}
			return c.App.Drive.CreateFolder(c.Ctx, dc, remote)
		}
		remoteParent := filepath.ToSlash(filepath.Dir(remote))
		f, err := os.Open(p)
		if err != nil {
			return err
		}
		defer func() { _ = f.Close() }()
		if c.App.DryRun {
			c.R().Info(fmt.Sprintf("dry-run: would upload %s → %s (%s)", p, remote, render.Size(info.Size())))
			return nil
		}
		return c.App.Drive.Upload(c.Ctx, dc, remoteParent, filepath.Base(p), f, drivesvc.UploadOptions{
			Label: "Uploading " + filepath.Base(p), Quiet: c.R().Quiet, TotalHint: info.Size(),
		})
	})
}

func itemsDownloadCmd() *cobra.Command {
	var force bool
	c := &cobra.Command{
		Use: "download PATH [DEST]", Short: "Download a file (DEST omitted or - writes to stdout)",
		Args: cobra.RangeArgs(1, 2),
		RunE: run([]Step{stepAuth}, func(c *Ctx) error {
			dc, err := driveCtx(c)
			if err != nil {
				return err
			}
			src := c.Args[0]
			var out io.Writer = os.Stdout
			toStdout := true
			var dest string
			if len(c.Args) >= 2 && c.Args[1] != "-" {
				dest = c.Args[1]
				if !force {
					if _, err := os.Stat(dest); err == nil {
						return fmt.Errorf("destination %s exists; use --force to overwrite", dest)
					}
				}
				f, err := os.Create(dest)
				if err != nil {
					return err
				}
				defer func() { _ = f.Close() }()
				out = f
				toStdout = false
			}
			if c.App.DryRun {
				c.R().Info(fmt.Sprintf("dry-run: would download %s", src))
				return nil
			}
			if err := c.App.Drive.Download(c.Ctx, dc, src, out, drivesvc.DownloadOptions{
				Label: "Downloading " + filepath.Base(src), Quiet: c.R().Quiet || toStdout,
			}); err != nil {
				return err
			}
			if !toStdout {
				c.R().Success(fmt.Sprintf("Downloaded to %s", dest))
			}
			return nil
		}),
	}
	c.Flags().BoolVar(&force, "force", false, "Overwrite existing destination")
	return c
}

func itemsRenameCmd() *cobra.Command {
	return &cobra.Command{
		Use: "rename PATH NEW_NAME", Short: "Rename a file or folder",
		Args: cobra.ExactArgs(2),
		RunE: run([]Step{stepAuth}, func(c *Ctx) error {
			dc, err := driveCtx(c)
			if err != nil {
				return err
			}
			if c.App.DryRun {
				c.R().Info(fmt.Sprintf("dry-run: would rename %s → %s", c.Args[0], c.Args[1]))
				return nil
			}
			if err := c.App.Drive.Rename(c.Ctx, dc, c.Args[0], c.Args[1]); err != nil {
				return err
			}
			c.R().Success(fmt.Sprintf("Renamed to %s", c.Args[1]))
			return nil
		}),
	}
}

func itemsMoveCmd() *cobra.Command {
	return &cobra.Command{
		Use: "move SRC DEST_FOLDER", Short: "Move a file or folder",
		Args: cobra.ExactArgs(2),
		RunE: run([]Step{stepAuth}, func(c *Ctx) error {
			dc, err := driveCtx(c)
			if err != nil {
				return err
			}
			if c.App.DryRun {
				c.R().Info(fmt.Sprintf("dry-run: would move %s → %s", c.Args[0], c.Args[1]))
				return nil
			}
			if err := c.App.Drive.Move(c.Ctx, dc, c.Args[0], c.Args[1]); err != nil {
				return err
			}
			c.R().Success(fmt.Sprintf("Moved %s → %s", c.Args[0], c.Args[1]))
			return nil
		}),
	}
}

func itemsDeleteCmd() *cobra.Command {
	var permanent, recursive, all bool
	var pattern, largerThan, scope, olderThan, newerThan string
	c := &cobra.Command{
		Use:   "delete [PATH...]",
		Short: "Delete files or folders (move to trash)",
		RunE: run([]Step{stepAuth}, func(c *Ctx) error {
			dc, err := driveCtx(c)
			if err != nil {
				return err
			}

			filtersSet := pattern != "" || largerThan != "" || all || scope != "" || olderThan != "" || newerThan != ""
			if len(c.Args) == 0 && !filtersSet {
				return fmt.Errorf("no paths selected: pass PATH(s) or a filter (--pattern, --larger-than, --older-than, --newer-than, --scope); use --all with --scope to target an entire subtree")
			}

			targets := append([]string{}, c.Args...)

			if filtersSet {
				if all && scope == "" && pattern == "" && largerThan == "" && olderThan == "" && newerThan == "" {
					return fmt.Errorf("--all requires --scope or a filter (e.g. --scope / to target the whole drive)")
				}
				root := scope
				if root == "" {
					root = "/"
				}
				var minSize int64
				if largerThan != "" {
					n, err := parseSize(largerThan)
					if err != nil {
						return err
					}
					minSize = n
				}
				var olderCutoff, newerCutoff int64
				if olderThan != "" {
					d, err := render.ParseDuration(olderThan)
					if err != nil {
						return fmt.Errorf("invalid --older-than: %w", err)
					}
					olderCutoff = time.Now().Add(-d).Unix()
				}
				if newerThan != "" {
					d, err := render.ParseDuration(newerThan)
					if err != nil {
						return fmt.Errorf("invalid --newer-than: %w", err)
					}
					newerCutoff = time.Now().Add(-d).Unix()
				}
				children, err := c.App.Drive.Walk(c.Ctx, dc, root)
				if err != nil {
					return err
				}
				for _, ch := range children {
					if !recursive && strings.Count(strings.TrimPrefix(ch.Path, root), "/") > 1 {
						continue
					}
					if ch.Type != 2 && (minSize > 0 || olderCutoff != 0 || newerCutoff != 0) {
						continue
					}
					if pattern != "" && !matchGlob(pattern, ch.Name) {
						continue
					}
					if minSize > 0 && ch.Size < minSize {
						continue
					}
					if olderCutoff != 0 && ch.ModifyTime > olderCutoff {
						continue
					}
					if newerCutoff != 0 && ch.ModifyTime < newerCutoff {
						continue
					}
					targets = append(targets, ch.Path)
				}
			}

			targets = dedupe(targets)
			if len(targets) == 0 {
				c.R().Info("Nothing to delete.")
				return nil
			}

			if c.App.DryRun {
				label := "dry-run: would delete"
				if permanent {
					label = "dry-run: would permanently delete"
				}
				c.R().Info(fmt.Sprintf("%s %d item(s):", label, len(targets)))
				for _, t := range targets {
					_, _ = fmt.Fprintln(c.R().Stderr, "  "+t)
				}
				return nil
			}
			for _, p := range targets {
				if err := c.App.Drive.Delete(c.Ctx, dc, p, permanent); err != nil {
					return err
				}
			}
			verb := "Moved to trash"
			if permanent {
				verb = "Permanently deleted"
			}
			c.R().Success(fmt.Sprintf("%s %d item(s)", verb, len(targets)))
			return nil
		}),
	}
	c.Flags().BoolVar(&permanent, "permanent", false, "Permanently delete instead of moving to trash")
	c.Flags().StringVar(&pattern, "pattern", "", "Match items by glob pattern (shell-style, e.g. *.tmp)")
	c.Flags().StringVar(&largerThan, "larger-than", "", "Match files larger than SIZE (e.g. 100MB, 2GB)")
	c.Flags().StringVar(&olderThan, "older-than", "", "Match files not modified within DURATION (e.g. 30d, 2w)")
	c.Flags().StringVar(&newerThan, "newer-than", "", "Match files modified within DURATION")
	c.Flags().StringVar(&scope, "scope", "", "Limit filtered deletion to this subtree (default: /)")
	c.Flags().BoolVar(&recursive, "recursive", false, "Descend into subfolders when applying filters")
	c.Flags().BoolVar(&all, "all", false, "Confirm matching every item in the scope (requires --scope or a filter)")
	return c
}

// ── drive folders ──

func driveFoldersCmd() *cobra.Command {
	c := &cobra.Command{Use: "folders", Short: "Manage folders"}
	c.AddCommand(&cobra.Command{
		Use: "create PATH", Short: "Create a folder",
		Args: cobra.ExactArgs(1),
		RunE: run([]Step{stepAuth}, func(c *Ctx) error {
			dc, err := driveCtx(c)
			if err != nil {
				return err
			}
			if c.App.DryRun {
				c.R().Info(fmt.Sprintf("dry-run: would create folder %s", c.Args[0]))
				return nil
			}
			if err := c.App.Drive.CreateFolder(c.Ctx, dc, c.Args[0]); err != nil {
				return err
			}
			c.R().Success(fmt.Sprintf("Created folder %s", c.Args[0]))
			return nil
		}),
	})
	return c
}

// ── drive share ──

func driveShareCmd() *cobra.Command {
	c := &cobra.Command{Use: "share", Short: "Manage sharing (public links and members)"}
	c.AddCommand(shareStatusCmd(), shareLinkCmd(), shareUnlinkCmd(), shareAddCmd(), shareRemoveCmd())
	return c
}

func shareStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use: "status PATH", Short: "Show how a file or folder is shared",
		Args: cobra.ExactArgs(1),
		RunE: run([]Step{stepAuth}, func(c *Ctx) error {
			dc, err := driveCtx(c)
			if err != nil {
				return err
			}
			st, err := c.App.Drive.ShareStatusOf(c.Ctx, dc, c.Args[0])
			if err != nil {
				return err
			}
			if c.R().Format != render.FormatText {
				return c.R().Object(st)
			}
			if len(st.Links) == 0 && len(st.Members) == 0 && len(st.Invitees) == 0 {
				c.R().Info("Not shared.")
				return nil
			}
			out := c.R().Stdout
			if len(st.Links) > 0 {
				_, _ = fmt.Fprintln(out, "Public links:")
				for _, l := range st.Links {
					perm := "view"
					if l.CanEdit {
						perm = "edit"
					}
					exp := "never"
					if l.ExpireTime != nil {
						exp = render.Time(*l.ExpireTime)
					}
					_, _ = fmt.Fprintf(out, "  %s  (%s, expires %s, %d accesses)\n", l.URL, perm, exp, l.NumAccesses)
				}
			}
			if len(st.Members) > 0 {
				_, _ = fmt.Fprintln(out, "Members:")
				for _, m := range st.Members {
					_, _ = fmt.Fprintf(out, "  %-32s %s\n", m.Email, m.Role)
				}
			}
			if len(st.Invitees) > 0 {
				_, _ = fmt.Fprintln(out, "Pending invitations:")
				for _, p := range st.Invitees {
					_, _ = fmt.Fprintf(out, "  %-32s %s (pending)\n", p.Email, p.Role)
				}
			}
			return nil
		}),
	}
}

func shareLinkCmd() *cobra.Command {
	var edit bool
	var expires, password string
	c := &cobra.Command{
		Use:   "link PATH",
		Short: "Create or show the public link for a file or folder",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts := drivesvc.LinkOptions{}
			if cmd.Flags().Changed("edit") {
				opts.SetEdit, opts.CanEdit = true, edit
			}
			if cmd.Flags().Changed("expires") {
				d, err := render.ParseDuration(expires)
				if err != nil {
					return fmt.Errorf("invalid --expires: %w", err)
				}
				opts.SetExpiry, opts.ExpireSeconds = true, int(d.Seconds())
			}
			if cmd.Flags().Changed("password") {
				opts.SetPassword, opts.CustomPassword = true, password
			}
			return run([]Step{stepAuth}, func(c *Ctx) error {
				dc, err := driveCtx(c)
				if err != nil {
					return err
				}
				if c.App.DryRun {
					c.R().Info(fmt.Sprintf("dry-run: would create/update public link for %s", args[0]))
					return nil
				}
				link, err := c.App.Drive.EnsureLink(c.Ctx, dc, args[0], opts)
				if err != nil {
					return err
				}
				if c.R().Format != render.FormatText {
					return c.R().Object(link)
				}
				_, _ = fmt.Fprintln(c.R().Stdout, link.URL)
				if link.CustomPassword != "" {
					c.R().Info("Password recipients must enter: " + link.CustomPassword)
				}
				return nil
			})(cmd, args)
		},
	}
	c.Flags().BoolVar(&edit, "edit", false, "Allow editing (default: view only)")
	c.Flags().StringVar(&expires, "expires", "", "Link expiration (e.g. 7d, 2w, 6mo)")
	c.Flags().StringVar(&password, "password", "", "Require a custom password to open the link")
	return c
}

func shareUnlinkCmd() *cobra.Command {
	return &cobra.Command{
		Use: "unlink PATH", Short: "Remove the public link(s) for a file or folder",
		Args: cobra.ExactArgs(1),
		RunE: run([]Step{stepAuth}, func(c *Ctx) error {
			dc, err := driveCtx(c)
			if err != nil {
				return err
			}
			if c.App.DryRun {
				n, err := c.App.Drive.CountLinks(c.Ctx, dc, c.Args[0])
				if err != nil {
					return err
				}
				c.R().Info(fmt.Sprintf("dry-run: would remove %d public link(s)", n))
				return nil
			}
			n, err := c.App.Drive.RemoveLinks(c.Ctx, dc, c.Args[0])
			if err != nil {
				return err
			}
			if n == 0 {
				c.R().Info("No public links to remove.")
				return nil
			}
			c.R().Success(fmt.Sprintf("Removed %d public link(s)", n))
			return nil
		}),
	}
}

func shareAddCmd() *cobra.Command {
	var edit bool
	var message string
	c := &cobra.Command{
		Use: "add PATH EMAIL", Short: "Invite a Proton user to a file or folder",
		Args: cobra.ExactArgs(2),
		RunE: run([]Step{stepAuth}, func(c *Ctx) error {
			dc, err := driveCtx(c)
			if err != nil {
				return err
			}
			if c.App.DryRun {
				c.R().Info(fmt.Sprintf("dry-run: would invite %s to %s", c.Args[1], c.Args[0]))
				return nil
			}
			if err := c.App.Drive.InviteMember(c.Ctx, dc, c.Args[0], c.Args[1], edit, message); err != nil {
				return err
			}
			c.R().Success(fmt.Sprintf("Invited %s", c.Args[1]))
			return nil
		}),
	}
	c.Flags().BoolVar(&edit, "edit", false, "Allow editing (default: view only)")
	c.Flags().StringVar(&message, "message", "", "Optional note included in the invitation email")
	return c
}

func shareRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use: "remove PATH EMAIL", Short: "Revoke a member or cancel a pending invitation",
		Args: cobra.ExactArgs(2),
		RunE: run([]Step{stepAuth}, func(c *Ctx) error {
			dc, err := driveCtx(c)
			if err != nil {
				return err
			}
			if c.App.DryRun {
				c.R().Info(fmt.Sprintf("dry-run: would revoke access for %s on %s", c.Args[1], c.Args[0]))
				return nil
			}
			if err := c.App.Drive.RemoveMember(c.Ctx, dc, c.Args[0], c.Args[1]); err != nil {
				return err
			}
			c.R().Success(fmt.Sprintf("Revoked access for %s", c.Args[1]))
			return nil
		}),
	}
}

// ── drive invitations ──

func driveInvitationsCmd() *cobra.Command {
	c := &cobra.Command{Use: "invitations", Short: "Manage incoming share invitations"}
	c.AddCommand(invitationsListCmd(), invitationsAcceptCmd(), invitationsRejectCmd())
	return c
}

func invitationsListCmd() *cobra.Command {
	return &cobra.Command{
		Use: "list", Short: "List pending incoming share invitations",
		RunE: run([]Step{stepAuth}, func(c *Ctx) error {
			invitations, err := c.App.Drive.ListInvitations(c.Ctx)
			if err != nil {
				return err
			}
			if c.R().Format == render.FormatText && len(invitations) == 0 {
				c.R().Info("No pending invitations.")
				return nil
			}
			return view.Render(c.R(), c.short(), c.App.IDCache, view.List[drivesvc.Invitation]{
				Columns: []view.Column[drivesvc.Invitation]{
					{Header: "INVITATION_ID", ID: true, Cell: func(i drivesvc.Invitation) string { return i.InvitationID }},
					{Header: "FROM", Cell: func(i drivesvc.Invitation) string { return i.InviterEmail }},
					{Header: "ROLE", Cell: func(i drivesvc.Invitation) string { return i.Role }},
					{Header: "CREATED", Cell: func(i drivesvc.Invitation) string { return render.Time(i.CreateTime) }},
				},
				CacheIDs: func(i drivesvc.Invitation) []string { return []string{i.InvitationID} },
			}, invitations)
		}),
	}
}

func invitationsAcceptCmd() *cobra.Command {
	return &cobra.Command{
		Use: "accept INVITATION_ID", Short: "Accept a pending share invitation",
		Args: cobra.ExactArgs(1),
		RunE: run([]Step{stepAuth}, func(c *Ctx) error {
			if c.App.DryRun {
				c.R().Info(fmt.Sprintf("dry-run: would accept invitation %s", c.Args[0]))
				return nil
			}
			u, err := c.App.Unlock(c.Ctx)
			if err != nil {
				return err
			}
			if err := c.App.Drive.AcceptInvitation(c.Ctx, u, c.Args[0]); err != nil {
				return err
			}
			c.R().Success(fmt.Sprintf("Accepted invitation %s", c.Args[0]))
			return nil
		}),
	}
}

func invitationsRejectCmd() *cobra.Command {
	return &cobra.Command{
		Use: "reject INVITATION_ID", Short: "Reject a pending share invitation",
		Args: cobra.ExactArgs(1),
		RunE: run([]Step{stepAuth}, func(c *Ctx) error {
			if c.App.DryRun {
				c.R().Info(fmt.Sprintf("dry-run: would reject invitation %s", c.Args[0]))
				return nil
			}
			if err := c.App.Drive.RejectInvitation(c.Ctx, c.Args[0]); err != nil {
				return err
			}
			c.R().Success(fmt.Sprintf("Rejected invitation %s", c.Args[0]))
			return nil
		}),
	}
}

// ── drive trash ──

func driveTrashCmd() *cobra.Command {
	c := &cobra.Command{Use: "trash", Short: "Manage the drive trash"}
	c.AddCommand(&cobra.Command{
		Use: "list", Short: "List trashed items",
		RunE: run([]Step{stepAuth}, func(c *Ctx) error {
			dc, err := driveCtx(c)
			if err != nil {
				return err
			}
			entries, err := c.App.Drive.TrashList(c.Ctx, dc)
			if err != nil {
				return err
			}
			if c.R().Format == render.FormatText && len(entries) == 0 {
				c.R().Info("(trash is empty)")
				return nil
			}
			return view.Render(c.R(), c.short(), c.App.IDCache, view.List[drivesvc.TrashEntry]{
				Columns: []view.Column[drivesvc.TrashEntry]{
					{Header: "LINK_ID", ID: true, Cell: func(e drivesvc.TrashEntry) string { return e.LinkID }},
					{Header: "TYPE", Cell: func(e drivesvc.TrashEntry) string { return driveTypeLabel(e.Type) }},
					{Header: "SIZE", Cell: func(e drivesvc.TrashEntry) string { return render.Size(e.Size) }},
				},
				CacheIDs: func(e drivesvc.TrashEntry) []string { return []string{e.LinkID} },
			}, entries)
		}),
	})
	c.AddCommand(&cobra.Command{
		Use: "restore LINK_ID...", Short: "Restore items from trash (IDs only)",
		Args: cobra.MinimumNArgs(1),
		RunE: run([]Step{stepAuth, stepResolve}, func(c *Ctx) error {
			dc, err := driveCtx(c)
			if err != nil {
				return err
			}
			if c.App.DryRun {
				c.R().Info(fmt.Sprintf("dry-run: would restore %d item(s)", len(c.Args)))
				return nil
			}
			if err := c.App.Drive.TrashRestore(c.Ctx, dc, c.Args); err != nil {
				return err
			}
			c.R().Success(fmt.Sprintf("Restored %d item(s)", len(c.Args)))
			return nil
		}),
	})
	c.AddCommand(&cobra.Command{
		Use: "empty", Short: "Empty the trash",
		RunE: run([]Step{stepAuth}, func(c *Ctx) error {
			dc, err := driveCtx(c)
			if err != nil {
				return err
			}
			if c.App.DryRun {
				c.R().Info("dry-run: would empty trash")
				return nil
			}
			if err := c.App.Drive.TrashEmpty(c.Ctx, dc); err != nil {
				return err
			}
			c.R().Success("Trash emptied.")
			return nil
		}),
	})
	return c
}
