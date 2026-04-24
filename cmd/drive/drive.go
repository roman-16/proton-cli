// Package drive implements the `drive` subcommand tree.
package drive

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/roman-16/proton-cli/cmd/shared"
	"github.com/roman-16/proton-cli/internal/app"
	"github.com/roman-16/proton-cli/internal/render"
	drivesvc "github.com/roman-16/proton-cli/internal/services/drive"
	"github.com/spf13/cobra"
)

// NewCmd returns the root `drive` command.
func NewCmd() *cobra.Command {
	c := &cobra.Command{Use: "drive", Short: "Drive operations"}
	c.AddCommand(itemsCmd(), foldersCmd(), trashCmd())
	return c
}

// ── drive items ──

func itemsCmd() *cobra.Command {
	c := &cobra.Command{Use: "items", Short: "Manage files and folders"}
	c.AddCommand(itemsListCmd(), itemsUploadCmd(), itemsDownloadCmd(), itemsRenameCmd(), itemsMoveCmd(), itemsDeleteCmd())
	return c
}

func itemsListCmd() *cobra.Command {
	return &cobra.Command{
		Use: "list [PATH]", Short: "List folder contents (decrypted names)",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a := app.From(cmd.Context())
			if err := a.Authenticate(cmd.Context()); err != nil {
				return err
			}
			u, err := a.Unlock(cmd.Context())
			if err != nil {
				return err
			}
			dc, err := a.Drive.Resolve(cmd.Context(), u)
			if err != nil {
				return err
			}
			path := "/"
			if len(args) > 0 {
				path = args[0]
			}
			children, err := a.Drive.List(cmd.Context(), dc, path)
			if err != nil {
				return app.Exit(shared.ResolveExit(err), err)
			}
			if a.R.Format != render.FormatText {
				return a.R.Object(children)
			}
			headers := []string{"TYPE", "SIZE", "NAME", "LINK_ID"}
			var rows [][]string
			for _, ch := range children {
				t := "FILE"
				if ch.Type == 1 {
					t = "DIR "
				}
				rows = append(rows, []string{t, render.Size(ch.Size), ch.Name, ch.LinkID})
			}
			render.Table(a.R.Stdout, headers, rows)
			return nil
		},
	}
}

func itemsUploadCmd() *cobra.Command {
	var recursive bool
	c := &cobra.Command{
		Use: "upload SRC [DEST]", Short: "Upload a file (SRC=- reads from stdin)",
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			a := app.From(cmd.Context())
			if err := a.Authenticate(cmd.Context()); err != nil {
				return err
			}
			u, err := a.Unlock(cmd.Context())
			if err != nil {
				return err
			}
			dc, err := a.Drive.Resolve(cmd.Context(), u)
			if err != nil {
				return err
			}
			src := args[0]
			dest := "/"
			if len(args) >= 2 {
				dest = args[1]
			}
			if recursive {
				if src == "-" {
					return fmt.Errorf("--recursive is not supported with stdin")
				}
				return uploadRecursive(cmd, a, dc, src, dest)
			}
			return uploadOne(cmd, a, dc, src, dest)
		},
	}
	c.Flags().BoolVar(&recursive, "recursive", false, "Recursively upload a directory")
	return c
}

func uploadOne(cmd *cobra.Command, a *app.App, dc *drivesvc.Context, src, dest string) error {
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
	if a.DryRun {
		a.R.Info(fmt.Sprintf("dry-run: would upload %s → %s/%s (%s)", src, dest, name, render.Size(size)))
		return nil
	}
	if err := a.Drive.Upload(cmd.Context(), dc, dest, name, r, drivesvc.UploadOptions{
		Label:     fmt.Sprintf("Uploading %s", name),
		Quiet:     a.R.Quiet,
		TotalHint: size,
	}); err != nil {
		return err
	}
	a.R.Success(fmt.Sprintf("Uploaded %s", name))
	return nil
}

func uploadRecursive(cmd *cobra.Command, a *app.App, dc *drivesvc.Context, src, dest string) error {
	srcAbs, err := filepath.Abs(src)
	if err != nil {
		return err
	}
	baseName := filepath.Base(srcAbs)
	// Create the top-level folder under dest.
	top := filepath.ToSlash(filepath.Join(dest, baseName))
	if !a.DryRun {
		if err := a.Drive.CreateFolder(cmd.Context(), dc, top); err != nil {
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
			if a.DryRun {
				a.R.Info("dry-run: would mkdir " + remote)
				return nil
			}
			return a.Drive.CreateFolder(cmd.Context(), dc, remote)
		}
		remoteParent := filepath.ToSlash(filepath.Dir(remote))
		f, err := os.Open(p)
		if err != nil {
			return err
		}
		defer func() { _ = f.Close() }()
		if a.DryRun {
			a.R.Info(fmt.Sprintf("dry-run: would upload %s → %s (%s)", p, remote, render.Size(info.Size())))
			return nil
		}
		return a.Drive.Upload(cmd.Context(), dc, remoteParent, filepath.Base(p), f, drivesvc.UploadOptions{
			Label:     "Uploading " + filepath.Base(p),
			Quiet:     a.R.Quiet,
			TotalHint: info.Size(),
		})
	})
}

func itemsDownloadCmd() *cobra.Command {
	return &cobra.Command{
		Use: "download PATH [DEST]", Short: "Download a file (DEST omitted or - writes to stdout)",
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			a := app.From(cmd.Context())
			if err := a.Authenticate(cmd.Context()); err != nil {
				return err
			}
			u, err := a.Unlock(cmd.Context())
			if err != nil {
				return err
			}
			dc, err := a.Drive.Resolve(cmd.Context(), u)
			if err != nil {
				return err
			}
			src := args[0]
			var out io.Writer = os.Stdout
			toStdout := true
			var dest string
			if len(args) >= 2 && args[1] != "-" {
				dest = args[1]
				f, err := os.Create(dest)
				if err != nil {
					return err
				}
				defer func() { _ = f.Close() }()
				out = f
				toStdout = false
			}
			if a.DryRun {
				a.R.Info(fmt.Sprintf("dry-run: would download %s", src))
				return nil
			}
			if err := a.Drive.Download(cmd.Context(), dc, src, out, drivesvc.DownloadOptions{
				Label: "Downloading " + filepath.Base(src),
				Quiet: a.R.Quiet || toStdout,
			}); err != nil {
				return err
			}
			if !toStdout {
				a.R.Success(fmt.Sprintf("Downloaded to %s", dest))
			}
			return nil
		},
	}
}

func itemsRenameCmd() *cobra.Command {
	return &cobra.Command{
		Use: "rename PATH NEW_NAME", Short: "Rename a file or folder",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			a := app.From(cmd.Context())
			if err := a.Authenticate(cmd.Context()); err != nil {
				return err
			}
			u, err := a.Unlock(cmd.Context())
			if err != nil {
				return err
			}
			dc, err := a.Drive.Resolve(cmd.Context(), u)
			if err != nil {
				return err
			}
			if a.DryRun {
				a.R.Info(fmt.Sprintf("dry-run: would rename %s → %s", args[0], args[1]))
				return nil
			}
			if err := a.Drive.Rename(cmd.Context(), dc, args[0], args[1]); err != nil {
				return err
			}
			a.R.Success(fmt.Sprintf("Renamed to %s", args[1]))
			return nil
		},
	}
}

func itemsMoveCmd() *cobra.Command {
	return &cobra.Command{
		Use: "move SRC DEST_FOLDER", Short: "Move a file or folder",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			a := app.From(cmd.Context())
			if err := a.Authenticate(cmd.Context()); err != nil {
				return err
			}
			u, err := a.Unlock(cmd.Context())
			if err != nil {
				return err
			}
			dc, err := a.Drive.Resolve(cmd.Context(), u)
			if err != nil {
				return err
			}
			if a.DryRun {
				a.R.Info(fmt.Sprintf("dry-run: would move %s → %s", args[0], args[1]))
				return nil
			}
			if err := a.Drive.Move(cmd.Context(), dc, args[0], args[1]); err != nil {
				return err
			}
			a.R.Success(fmt.Sprintf("Moved %s → %s", args[0], args[1]))
			return nil
		},
	}
}

func itemsDeleteCmd() *cobra.Command {
	var permanent, recursive, all bool
	var pattern, largerThan, scope, olderThan, newerThan string
	c := &cobra.Command{
		Use:   "delete [PATH...]",
		Short: "Delete files or folders (move to trash)",
		RunE: func(cmd *cobra.Command, args []string) error {
			a := app.From(cmd.Context())
			if err := a.Authenticate(cmd.Context()); err != nil {
				return err
			}
			u, err := a.Unlock(cmd.Context())
			if err != nil {
				return err
			}
			dc, err := a.Drive.Resolve(cmd.Context(), u)
			if err != nil {
				return err
			}

			filtersSet := pattern != "" || largerThan != "" || all || scope != "" || olderThan != "" || newerThan != ""
			if len(args) == 0 && !filtersSet {
				return fmt.Errorf("no paths selected: pass PATH(s) or a filter (--pattern, --larger-than, --older-than, --newer-than, --scope); use --all with --scope to target an entire subtree")
			}

			targets := make([]string, 0, len(args))
			targets = append(targets, args...)

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
					n, err := shared.ParseSize(largerThan)
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
				children, err := a.Drive.Walk(cmd.Context(), dc, root)
				if err != nil {
					return err
				}
				for _, ch := range children {
					if !recursive && strings.Count(strings.TrimPrefix(ch.Path, root), "/") > 1 {
						continue
					}
					if ch.Type != 2 && (minSize > 0 || olderCutoff != 0 || newerCutoff != 0) {
						// size/time filters only apply to files
						continue
					}
					if pattern != "" && !shared.MatchGlob(pattern, ch.Name) {
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

			targets = shared.Dedupe(targets)
			if len(targets) == 0 {
				a.R.Info("Nothing to delete.")
				return nil
			}

			if a.DryRun {
				label := "dry-run: would delete"
				if permanent {
					label = "dry-run: would permanently delete"
				}
				a.R.Info(fmt.Sprintf("%s %d item(s):", label, len(targets)))
				for _, t := range targets {
					_, _ = fmt.Fprintln(a.R.Stderr, "  "+t)
				}
				return nil
			}
			for _, p := range targets {
				if err := a.Drive.Delete(cmd.Context(), dc, p, permanent); err != nil {
					return err
				}
			}
			verb := "Moved to trash"
			if permanent {
				verb = "Permanently deleted"
			}
			a.R.Success(fmt.Sprintf("%s %d item(s)", verb, len(targets)))
			return nil
		},
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

func foldersCmd() *cobra.Command {
	c := &cobra.Command{Use: "folders", Short: "Manage folders"}
	c.AddCommand(&cobra.Command{
		Use: "create PATH", Short: "Create a folder",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a := app.From(cmd.Context())
			if err := a.Authenticate(cmd.Context()); err != nil {
				return err
			}
			u, err := a.Unlock(cmd.Context())
			if err != nil {
				return err
			}
			dc, err := a.Drive.Resolve(cmd.Context(), u)
			if err != nil {
				return err
			}
			if a.DryRun {
				a.R.Info(fmt.Sprintf("dry-run: would create folder %s", args[0]))
				return nil
			}
			if err := a.Drive.CreateFolder(cmd.Context(), dc, args[0]); err != nil {
				return err
			}
			a.R.Success(fmt.Sprintf("Created folder %s", args[0]))
			return nil
		},
	})
	return c
}

// ── drive trash ──

func trashCmd() *cobra.Command {
	c := &cobra.Command{Use: "trash", Short: "Manage the drive trash"}
	c.AddCommand(&cobra.Command{
		Use: "list", Short: "List trashed items",
		RunE: func(cmd *cobra.Command, args []string) error {
			a := app.From(cmd.Context())
			if err := a.Authenticate(cmd.Context()); err != nil {
				return err
			}
			u, err := a.Unlock(cmd.Context())
			if err != nil {
				return err
			}
			dc, err := a.Drive.Resolve(cmd.Context(), u)
			if err != nil {
				return err
			}
			entries, err := a.Drive.TrashList(cmd.Context(), dc)
			if err != nil {
				return err
			}
			if a.R.Format != render.FormatText {
				return a.R.Object(entries)
			}
			if len(entries) == 0 {
				a.R.Info("(trash is empty)")
				return nil
			}
			headers := []string{"LINK_ID", "TYPE", "SIZE"}
			var rows [][]string
			for _, e := range entries {
				t := "FILE"
				if e.Type == 1 {
					t = "DIR "
				}
				rows = append(rows, []string{e.LinkID, t, render.Size(e.Size)})
			}
			render.Table(a.R.Stdout, headers, rows)
			return nil
		},
	})
	c.AddCommand(&cobra.Command{
		Use: "restore LINK_ID...", Short: "Restore items from trash (IDs only)",
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a := app.From(cmd.Context())
			if err := a.Authenticate(cmd.Context()); err != nil {
				return err
			}
			u, err := a.Unlock(cmd.Context())
			if err != nil {
				return err
			}
			dc, err := a.Drive.Resolve(cmd.Context(), u)
			if err != nil {
				return err
			}
			if a.DryRun {
				a.R.Info(fmt.Sprintf("dry-run: would restore %d item(s)", len(args)))
				return nil
			}
			if err := a.Drive.TrashRestore(cmd.Context(), dc, args); err != nil {
				return err
			}
			a.R.Success(fmt.Sprintf("Restored %d item(s)", len(args)))
			return nil
		},
	})
	c.AddCommand(&cobra.Command{
		Use: "empty", Short: "Empty the trash",
		RunE: func(cmd *cobra.Command, args []string) error {
			a := app.From(cmd.Context())
			if err := a.Authenticate(cmd.Context()); err != nil {
				return err
			}
			u, err := a.Unlock(cmd.Context())
			if err != nil {
				return err
			}
			dc, err := a.Drive.Resolve(cmd.Context(), u)
			if err != nil {
				return err
			}
			if a.DryRun {
				a.R.Info("dry-run: would empty trash")
				return nil
			}
			if err := a.Drive.TrashEmpty(cmd.Context(), dc); err != nil {
				return err
			}
			a.R.Success("Trash emptied.")
			return nil
		},
	})
	return c
}
