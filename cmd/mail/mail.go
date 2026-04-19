// Package mail implements the `mail` subcommand tree.
package mail

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/roman-16/proton-cli/cmd/shared"
	"github.com/roman-16/proton-cli/internal/app"
	"github.com/roman-16/proton-cli/internal/render"
	mailsvc "github.com/roman-16/proton-cli/internal/services/mail"
	"github.com/spf13/cobra"
)

// NewCmd returns the root `mail` command.
func NewCmd() *cobra.Command {
	c := &cobra.Command{Use: "mail", Short: "Mail operations"}
	c.AddCommand(messagesCmd(), attachmentsCmd(), labelsCmd(), filtersCmd(), addressesCmd())
	return c
}

// ── mail messages ──

func messagesCmd() *cobra.Command {
	c := &cobra.Command{Use: "messages", Short: "Manage messages"}
	c.AddCommand(msgListCmd(), msgSearchCmd(), msgReadCmd(), msgSendCmd(), msgTrashCmd(), msgDeleteCmd(), msgMoveCmd(), msgMarkCmd(), msgStarCmd(), msgUnstarCmd())
	return c
}

func msgListCmd() *cobra.Command {
	var opts mailsvc.ListOptions
	c := &cobra.Command{
		Use: "list", Short: "List messages",
		RunE: func(cmd *cobra.Command, args []string) error {
			a := app.From(cmd.Context())
			if err := a.Authenticate(cmd.Context()); err != nil {
				return err
			}
			msgs, total, err := a.Mail.List(cmd.Context(), opts)
			if err != nil {
				return err
			}
			return renderMessages(a, msgs, total, opts.Page)
		},
	}
	c.Flags().StringVar(&opts.Folder, "folder", "inbox", "Folder (inbox, sent, drafts, trash, spam, archive, starred, all)")
	c.Flags().IntVar(&opts.Page, "page", 0, "Page number (0-based)")
	c.Flags().IntVar(&opts.PageSize, "page-size", 25, "Messages per page")
	c.Flags().BoolVar(&opts.Unread, "unread", false, "Show only unread messages")
	return c
}

func msgSearchCmd() *cobra.Command {
	var opts mailsvc.SearchOptions
	c := &cobra.Command{
		Use: "search", Short: "Search messages",
		RunE: func(cmd *cobra.Command, args []string) error {
			a := app.From(cmd.Context())
			if err := a.Authenticate(cmd.Context()); err != nil {
				return err
			}
			msgs, total, err := a.Mail.Search(cmd.Context(), opts)
			if err != nil {
				return err
			}
			return renderMessages(a, msgs, total, 0)
		},
	}
	c.Flags().StringVar(&opts.Keyword, "keyword", "", "Search keyword")
	c.Flags().StringVar(&opts.From, "from", "", "Filter by sender")
	c.Flags().StringVar(&opts.To, "to", "", "Filter by recipient")
	c.Flags().StringVar(&opts.Subject, "subject", "", "Filter by subject")
	c.Flags().StringVar(&opts.After, "after", "", "After date (YYYY-MM-DD)")
	c.Flags().StringVar(&opts.Before, "before", "", "Before date (YYYY-MM-DD)")
	c.Flags().StringVar(&opts.Folder, "folder", "all", "Folder to search in")
	c.Flags().IntVar(&opts.Limit, "limit", 25, "Max results")
	return c
}

func msgReadCmd() *cobra.Command {
	var format string
	c := &cobra.Command{
		Use: "read REF", Short: "Read a message (decrypted)",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a := app.From(cmd.Context())
			if err := a.Authenticate(cmd.Context()); err != nil {
				return err
			}
			id, err := a.Mail.Resolve(cmd.Context(), args[0])
			if err != nil {
				return app.Exit(resolveExit(err), err)
			}
			u, err := a.Unlock(cmd.Context())
			if err != nil {
				return err
			}
			msg, err := a.Mail.Read(cmd.Context(), u, id)
			if err != nil {
				return err
			}
			if a.R.Format != render.FormatText {
				return a.R.Object(msg)
			}
			fmt.Fprintf(a.R.Stdout, "Subject: %s\n", msg.Subject)
			if s, ok := msg.Sender["Address"].(string); ok {
				fmt.Fprintf(a.R.Stdout, "From:    %s\n", s)
			}
			for _, t := range msg.ToList {
				if s, ok := t["Address"].(string); ok {
					fmt.Fprintf(a.R.Stdout, "To:      %s\n", s)
				}
			}
			fmt.Fprintf(a.R.Stdout, "ID:      %s\n\n", msg.ID)
			body := msg.Body
			switch format {
			case "text":
				if render.IsHTML(msg.MIMEType) {
					body = render.HTMLToText(body)
				}
			case "html", "raw":
				// print body as-is
			default:
				return fmt.Errorf("unknown --format %q (use text, html, raw)", format)
			}
			fmt.Fprintln(a.R.Stdout, body)
			return nil
		},
	}
	c.Flags().StringVar(&format, "format", "text", "Body format: text, html, raw")
	return c
}

func msgSendCmd() *cobra.Command {
	var to, subject, body string
	c := &cobra.Command{
		Use: "send", Short: "Send a message",
		RunE: func(cmd *cobra.Command, args []string) error {
			a := app.From(cmd.Context())
			if err := a.Authenticate(cmd.Context()); err != nil {
				return err
			}
			if to == "" {
				return fmt.Errorf("--to is required")
			}
			if subject == "" {
				return fmt.Errorf("--subject is required")
			}
			if body == "-" {
				b, err := io.ReadAll(os.Stdin)
				if err != nil {
					return err
				}
				body = string(b)
			}
			if body == "" {
				return fmt.Errorf("--body is required (use - for stdin)")
			}
			if a.DryRun {
				a.R.Info(fmt.Sprintf("dry-run: would send to %s subject %q (%d bytes)", to, subject, len(body)))
				return nil
			}
			u, err := a.Unlock(cmd.Context())
			if err != nil {
				return err
			}
			if err := a.Mail.Send(cmd.Context(), u, to, subject, body); err != nil {
				return err
			}
			a.R.Success("Message sent.")
			return nil
		},
	}
	c.Flags().StringVar(&to, "to", "", "Recipient email")
	c.Flags().StringVar(&subject, "subject", "", "Subject")
	c.Flags().StringVar(&body, "body", "", "Message body (use - for stdin)")
	return c
}

// msgFilter collects batch-filter flags accepted by trash/delete/move/mark.
type msgFilter struct {
	unread                             bool
	from, to, subject, keyword, folder string
	olderThan, newerThan               string
	all                                bool
	limit                              int
}

func (f *msgFilter) register(c *cobra.Command) {
	c.Flags().BoolVar(&f.unread, "unread", false, "Match unread messages")
	c.Flags().StringVar(&f.from, "from", "", "Match sender")
	c.Flags().StringVar(&f.to, "to", "", "Match recipient")
	c.Flags().StringVar(&f.subject, "subject", "", "Match subject")
	c.Flags().StringVar(&f.keyword, "keyword", "", "Match keyword")
	c.Flags().StringVar(&f.folder, "folder", "", "Scope to a folder")
	c.Flags().StringVar(&f.olderThan, "older-than", "", "Match messages older than DURATION (e.g. 30d, 2w, 1h)")
	c.Flags().StringVar(&f.newerThan, "newer-than", "", "Match messages newer than DURATION")
	c.Flags().BoolVar(&f.all, "all", false, "Confirm matching every message in the scope (required when no other filter is set)")
	c.Flags().IntVar(&f.limit, "limit", 150, "Maximum messages to affect when using filters (Proton caps at 150 per page)")
}

func (f *msgFilter) set() bool {
	return f.unread || f.from != "" || f.to != "" || f.subject != "" || f.keyword != "" ||
		f.folder != "" || f.olderThan != "" || f.newerThan != "" || f.all
}

func (f *msgFilter) toSearch() (mailsvc.SearchOptions, error) {
	opts := mailsvc.SearchOptions{
		Keyword: f.keyword, From: f.from, To: f.to, Subject: f.subject,
		Folder: f.folder, Limit: f.limit, Unread: f.unread,
	}
	if opts.Folder == "" {
		opts.Folder = "all"
	}
	if f.olderThan != "" {
		d, err := render.ParseDuration(f.olderThan)
		if err != nil {
			return opts, fmt.Errorf("invalid --older-than: %w", err)
		}
		opts.Before = time.Now().Add(-d).Format("2006-01-02")
	}
	if f.newerThan != "" {
		d, err := render.ParseDuration(f.newerThan)
		if err != nil {
			return opts, fmt.Errorf("invalid --newer-than: %w", err)
		}
		opts.After = time.Now().Add(-d).Format("2006-01-02")
	}
	return opts, nil
}

// collectMessageIDs unions explicit REFs with messages matched by filters.
func collectMessageIDs(cmd *cobra.Command, args []string, f *msgFilter) ([]string, error) {
	a := app.From(cmd.Context())
	var ids []string

	for _, arg := range args {
		id, err := a.Mail.Resolve(cmd.Context(), arg)
		if err != nil {
			return nil, app.Exit(resolveExit(err), err)
		}
		ids = append(ids, id)
	}

	if f.set() {
		// --all without any actual filter needs at least a scope (folder) or
		// operates on everything — make the user be explicit.
		if f.all && !f.unread && f.from == "" && f.to == "" && f.subject == "" &&
			f.keyword == "" && f.folder == "" && f.olderThan == "" && f.newerThan == "" {
			a.R.Info("--all with no other filter will affect every message in the account. Add --folder to scope it.")
		}
		search, err := f.toSearch()
		if err != nil {
			return nil, err
		}
		msgs, _, err := a.Mail.Search(cmd.Context(), search)
		if err != nil {
			return nil, err
		}
		for _, m := range msgs {
			ids = append(ids, m.ID)
		}
	}

	if len(args) == 0 && !f.set() {
		return nil, fmt.Errorf("no messages selected: pass REF(s) or a filter (e.g. --unread, --from, --older-than); use --all to target an entire folder")
	}

	return shared.Dedupe(ids), nil
}

func msgTrashCmd() *cobra.Command {
	var f msgFilter
	c := &cobra.Command{
		Use: "trash [REF...]", Short: "Move messages to trash",
		RunE: bulkMessageAction(&f, "Moved %d message(s) to trash.", func(cmd *cobra.Command, ids []string) error {
			a := app.From(cmd.Context())
			return a.Mail.Trash(cmd.Context(), ids)
		}),
	}
	f.register(c)
	return c
}

func msgDeleteCmd() *cobra.Command {
	var f msgFilter
	c := &cobra.Command{
		Use: "delete [REF...]", Short: "Permanently delete messages",
		RunE: bulkMessageAction(&f, "Permanently deleted %d message(s).", func(cmd *cobra.Command, ids []string) error {
			a := app.From(cmd.Context())
			return a.Mail.Delete(cmd.Context(), ids)
		}),
	}
	f.register(c)
	return c
}

func msgMoveCmd() *cobra.Command {
	var dest string
	var f msgFilter
	c := &cobra.Command{
		Use: "move [REF...]", Short: "Move messages to a folder",
		RunE: func(cmd *cobra.Command, args []string) error {
			a := app.From(cmd.Context())
			if err := a.Authenticate(cmd.Context()); err != nil {
				return err
			}
			ids, err := collectMessageIDs(cmd, args, &f)
			if err != nil {
				return err
			}
			if a.DryRun {
				a.R.Info(fmt.Sprintf("dry-run: would move %d message(s) to %s", len(ids), dest))
				return nil
			}
			if err := a.Mail.Move(cmd.Context(), ids, dest); err != nil {
				return err
			}
			a.R.Success(fmt.Sprintf("Moved %d message(s) to %s.", len(ids), dest))
			return nil
		},
	}
	// --dest keeps --folder available as a scope filter.
	c.Flags().StringVar(&dest, "dest", "", "Destination folder (inbox, sent, drafts, trash, spam, archive, starred, or a label ID)")
	_ = c.MarkFlagRequired("dest")
	f.register(c)
	return c
}

// msgMarkCmd accepts a positional ACTION (read|unread) so the --unread flag
// stays unambiguous as a filter.
func msgMarkCmd() *cobra.Command {
	var f msgFilter
	c := &cobra.Command{
		Use:       "mark ACTION [REF...]",
		Short:     "Mark messages (ACTION: read|unread)",
		Args:      cobra.MinimumNArgs(1),
		ValidArgs: []string{"read", "unread"},
		RunE: func(cmd *cobra.Command, args []string) error {
			action := strings.ToLower(args[0])
			rest := args[1:]
			if action != "read" && action != "unread" {
				return fmt.Errorf("unknown action %q (use: read, unread)", action)
			}
			a := app.From(cmd.Context())
			if err := a.Authenticate(cmd.Context()); err != nil {
				return err
			}
			ids, err := collectMessageIDs(cmd, rest, &f)
			if err != nil {
				return err
			}
			if a.DryRun {
				a.R.Info(fmt.Sprintf("dry-run: would mark %d message(s) as %s", len(ids), action))
				return nil
			}
			if err := a.Mail.Mark(cmd.Context(), ids, action == "read", action == "unread", false, false); err != nil {
				return err
			}
			a.R.Success(fmt.Sprintf("Marked %d message(s) as %s.", len(ids), action))
			return nil
		},
	}
	f.register(c)
	return c
}

// msgStarCmd / msgUnstarCmd are separate commands so the star action never
// collides with the --starred filter on other commands.
func msgStarCmd() *cobra.Command {
	var f msgFilter
	c := &cobra.Command{
		Use: "star [REF...]", Short: "Add a star to messages",
		RunE: bulkMessageAction(&f, "Starred %d message(s).", func(cmd *cobra.Command, ids []string) error {
			a := app.From(cmd.Context())
			return a.Mail.Mark(cmd.Context(), ids, false, false, true, false)
		}),
	}
	f.register(c)
	return c
}

func msgUnstarCmd() *cobra.Command {
	var f msgFilter
	c := &cobra.Command{
		Use: "unstar [REF...]", Short: "Remove a star from messages",
		RunE: bulkMessageAction(&f, "Unstarred %d message(s).", func(cmd *cobra.Command, ids []string) error {
			a := app.From(cmd.Context())
			return a.Mail.Mark(cmd.Context(), ids, false, false, false, true)
		}),
	}
	f.register(c)
	return c
}

func bulkMessageAction(f *msgFilter, successFmt string, do func(cmd *cobra.Command, ids []string) error) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		a := app.From(cmd.Context())
		if err := a.Authenticate(cmd.Context()); err != nil {
			return err
		}
		ids, err := collectMessageIDs(cmd, args, f)
		if err != nil {
			return err
		}
		if a.DryRun {
			a.R.Info(fmt.Sprintf("dry-run: would affect %d message(s)", len(ids)))
			for _, id := range ids {
				fmt.Fprintln(a.R.Stderr, "  "+id)
			}
			return nil
		}
		if err := do(cmd, ids); err != nil {
			return err
		}
		a.R.Success(fmt.Sprintf(successFmt, len(ids)))
		return nil
	}
}

func renderMessages(a *app.App, msgs []mailsvc.Message, total, page int) error {
	if a.R.Format != render.FormatText {
		return a.R.Object(struct {
			Total    int               `json:"total"`
			Messages []mailsvc.Message `json:"messages"`
		}{Total: total, Messages: msgs})
	}
	headers := []string{"ID", "FROM", "SUBJECT", "DATE", "⚑"}
	rows := make([][]string, 0, len(msgs))
	for _, m := range msgs {
		from := m.FromAddress
		if m.FromName != "" {
			from = m.FromName
		}
		flags := ""
		if m.Unread == 1 {
			flags += "●"
		}
		if m.NumAttachments > 0 {
			flags += "📎"
		}
		rows = append(rows, []string{m.ID, from, m.Subject, time.Unix(m.Time, 0).Local().Format("2006-01-02 15:04"), flags})
	}
	render.Table(a.R.Stdout, headers, rows)
	fmt.Fprintf(a.R.Stderr, "\n%d messages total (page %d)\n", total, page)
	return nil
}

// ── mail attachments ──

func attachmentsCmd() *cobra.Command {
	c := &cobra.Command{Use: "attachments", Short: "Manage message attachments"}
	c.AddCommand(&cobra.Command{
		Use: "list MESSAGE_ID", Short: "List attachments of a message",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a := app.From(cmd.Context())
			if err := a.Authenticate(cmd.Context()); err != nil {
				return err
			}
			atts, err := a.Mail.AttachmentsList(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			if a.R.Format != render.FormatText {
				return a.R.Object(atts)
			}
			headers := []string{"ID", "NAME", "SIZE", "TYPE"}
			var rows [][]string
			for _, at := range atts {
				rows = append(rows, []string{at.ID, at.Name, render.Size(at.Size), at.MIMEType})
			}
			render.Table(a.R.Stdout, headers, rows)
			return nil
		},
	})
	c.AddCommand(&cobra.Command{
		Use:   "download MESSAGE_ID ATTACHMENT_ID [OUTPUT_PATH]",
		Short: "Download and decrypt an attachment (- for stdout)",
		Args:  cobra.RangeArgs(2, 3),
		RunE: func(cmd *cobra.Command, args []string) error {
			a := app.From(cmd.Context())
			if err := a.Authenticate(cmd.Context()); err != nil {
				return err
			}
			u, err := a.Unlock(cmd.Context())
			if err != nil {
				return err
			}
			bin, name, err := a.Mail.AttachmentDownload(cmd.Context(), u, args[0], args[1])
			if err != nil {
				return err
			}
			out := name
			if len(args) >= 3 {
				out = args[2]
			}
			if out == "-" {
				_, err := a.R.Stdout.Write(bin)
				return err
			}
			if err := os.WriteFile(out, bin, 0644); err != nil {
				return err
			}
			a.R.Success(fmt.Sprintf("Downloaded %s (%d bytes)", out, len(bin)))
			return nil
		},
	})
	return c
}

// ── mail labels ──

func labelsCmd() *cobra.Command {
	c := &cobra.Command{Use: "labels", Short: "Manage labels and folders"}
	c.AddCommand(&cobra.Command{
		Use: "list", Short: "List labels and folders",
		RunE: func(cmd *cobra.Command, args []string) error {
			a := app.From(cmd.Context())
			if err := a.Authenticate(cmd.Context()); err != nil {
				return err
			}
			labels, folders, err := a.Mail.LabelsList(cmd.Context())
			if err != nil {
				return err
			}
			if a.R.Format != render.FormatText {
				return a.R.Object(map[string]any{"Labels": labels, "Folders": folders})
			}
			headers := []string{"ID", "TYPE", "NAME", "COLOR", "PATH"}
			var rows [][]string
			for _, l := range folders {
				rows = append(rows, []string{l.ID, "FOLDER", l.Name, l.Color, l.Path})
			}
			for _, l := range labels {
				rows = append(rows, []string{l.ID, "LABEL", l.Name, l.Color, ""})
			}
			render.Table(a.R.Stdout, headers, rows)
			return nil
		},
	})
	var createName, createColor string
	var createFolder bool
	createCmd := &cobra.Command{
		Use: "create", Short: "Create a label or folder",
		RunE: func(cmd *cobra.Command, args []string) error {
			a := app.From(cmd.Context())
			if err := a.Authenticate(cmd.Context()); err != nil {
				return err
			}
			if createName == "" {
				return fmt.Errorf("--name is required")
			}
			if a.DryRun {
				a.R.Info(fmt.Sprintf("dry-run: would create label %q", createName))
				return nil
			}
			body, err := a.Mail.LabelCreate(cmd.Context(), createName, createColor, createFolder)
			if err != nil {
				return err
			}
			id := pickID(body, "Label", "ID")
			kind := "Label"
			if createFolder {
				kind = "Folder"
			}
			a.R.ID(id, fmt.Sprintf("Created %s %q", kind, createName))
			return nil
		},
	}
	createCmd.Flags().StringVar(&createName, "name", "", "Label name")
	createCmd.Flags().StringVar(&createColor, "color", "#7272a7", "Label color (hex)")
	createCmd.Flags().BoolVar(&createFolder, "folder", false, "Create a folder instead of a label")
	c.AddCommand(createCmd)

	c.AddCommand(&cobra.Command{
		Use: "delete LABEL_ID...", Short: "Delete labels or folders",
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a := app.From(cmd.Context())
			if err := a.Authenticate(cmd.Context()); err != nil {
				return err
			}
			if a.DryRun {
				a.R.Info(fmt.Sprintf("dry-run: would delete %d label(s)", len(args)))
				return nil
			}
			if err := a.Mail.LabelDelete(cmd.Context(), args); err != nil {
				return err
			}
			a.R.Success(fmt.Sprintf("Deleted %d label(s)/folder(s).", len(args)))
			return nil
		},
	})
	return c
}

// ── mail filters ──

func filtersCmd() *cobra.Command {
	c := &cobra.Command{Use: "filters", Short: "Manage Sieve filters"}
	c.AddCommand(&cobra.Command{
		Use: "list", Short: "List filters",
		RunE: func(cmd *cobra.Command, args []string) error {
			a := app.From(cmd.Context())
			if err := a.Authenticate(cmd.Context()); err != nil {
				return err
			}
			body, err := a.Mail.FiltersList(cmd.Context())
			if err != nil {
				return err
			}
			if a.R.Format != render.FormatText {
				return a.R.JSON(body)
			}
			var r struct {
				Filters []struct {
					ID, Name string
					Status   int
					Version  int
				}
			}
			if err := json.Unmarshal(body, &r); err != nil {
				return err
			}
			headers := []string{"ID", "STATUS", "NAME", "VERSION"}
			var rows [][]string
			for _, f := range r.Filters {
				st := "disabled"
				if f.Status == 1 {
					st = "enabled"
				}
				rows = append(rows, []string{f.ID, st, f.Name, fmt.Sprintf("%d", f.Version)})
			}
			render.Table(a.R.Stdout, headers, rows)
			return nil
		},
	})
	var fName, fSieve string
	var fStatus int
	createCmd := &cobra.Command{
		Use: "create", Short: "Create a sieve filter",
		RunE: func(cmd *cobra.Command, args []string) error {
			a := app.From(cmd.Context())
			if err := a.Authenticate(cmd.Context()); err != nil {
				return err
			}
			if fName == "" || fSieve == "" {
				return fmt.Errorf("--name and --sieve are required")
			}
			if a.DryRun {
				a.R.Info(fmt.Sprintf("dry-run: would create filter %q", fName))
				return nil
			}
			body, err := a.Mail.FilterCreate(cmd.Context(), fName, fSieve, fStatus)
			if err != nil {
				return err
			}
			id := pickID(body, "Filter", "ID")
			a.R.ID(id, fmt.Sprintf("Created filter %q", fName))
			return nil
		},
	}
	createCmd.Flags().StringVar(&fName, "name", "", "Filter name")
	createCmd.Flags().StringVar(&fSieve, "sieve", "", "Sieve script")
	createCmd.Flags().IntVar(&fStatus, "status", 1, "Status (1=enabled, 0=disabled)")
	c.AddCommand(createCmd)

	c.AddCommand(filterSingleArg("delete", "Delete a filter", func(a *app.App, ctx context.Context, id string) error { return a.Mail.FilterDelete(ctx, id) }, "Deleted filter %s."))
	c.AddCommand(filterSingleArg("enable", "Enable a filter", func(a *app.App, ctx context.Context, id string) error { return a.Mail.FilterEnable(ctx, id) }, "Enabled filter %s."))
	c.AddCommand(filterSingleArg("disable", "Disable a filter", func(a *app.App, ctx context.Context, id string) error { return a.Mail.FilterDisable(ctx, id) }, "Disabled filter %s."))
	return c
}

// ── mail addresses ──

func addressesCmd() *cobra.Command {
	c := &cobra.Command{Use: "addresses", Short: "Manage email addresses"}
	c.AddCommand(&cobra.Command{
		Use: "list", Short: "List email addresses on the account",
		RunE: func(cmd *cobra.Command, args []string) error {
			a := app.From(cmd.Context())
			if err := a.Authenticate(cmd.Context()); err != nil {
				return err
			}
			body, err := a.Mail.AddressesList(cmd.Context())
			if err != nil {
				return err
			}
			if a.R.Format != render.FormatText {
				return a.R.JSON(body)
			}
			var r struct {
				Addresses []struct {
					ID, Email, DisplayName string
					Type, Status           int
				}
			}
			if err := json.Unmarshal(body, &r); err != nil {
				return err
			}
			headers := []string{"ID", "EMAIL", "DISPLAY_NAME", "STATUS", "TYPE"}
			var rows [][]string
			for _, ad := range r.Addresses {
				st := "disabled"
				if ad.Status == 1 {
					st = "active"
				}
				rows = append(rows, []string{ad.ID, ad.Email, ad.DisplayName, st, addressType(ad.Type)})
			}
			render.Table(a.R.Stdout, headers, rows)
			return nil
		},
	})
	return c
}

func addressType(t int) string {
	switch t {
	case 1:
		return "original"
	case 2:
		return "alias"
	case 3:
		return "custom"
	case 4:
		return "premium"
	case 5:
		return "external"
	}
	return "unknown"
}

// Helpers ─────────────────────────────────────────────────────────────

func filterSingleArg(use, short string, fn func(a *app.App, ctx context.Context, id string) error, successFmt string) *cobra.Command {
	return &cobra.Command{
		Use: use + " FILTER_ID", Short: short,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a := app.From(cmd.Context())
			if err := a.Authenticate(cmd.Context()); err != nil {
				return err
			}
			if a.DryRun {
				a.R.Info(fmt.Sprintf("dry-run: would %s filter %s", use, args[0]))
				return nil
			}
			if err := fn(a, cmd.Context(), args[0]); err != nil {
				return err
			}
			a.R.Success(fmt.Sprintf(successFmt, args[0]))
			return nil
		},
	}
}

func resolveExit(err error) int {
	if err == nil {
		return 0
	}
	s := strings.ToLower(err.Error())
	switch {
	case strings.Contains(s, "ambiguous"):
		return 4
	case strings.Contains(s, "not found"), strings.Contains(s, "no "):
		return 3
	}
	return 1
}

func pickID(body []byte, keys ...string) string {
	var v any
	if err := json.Unmarshal(body, &v); err != nil {
		return ""
	}
	cur := v
	for _, k := range keys {
		m, ok := cur.(map[string]any)
		if !ok {
			return ""
		}
		cur = m[k]
	}
	if s, ok := cur.(string); ok {
		return s
	}
	return ""
}
