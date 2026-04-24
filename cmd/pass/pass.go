// Package pass implements the `pass` subcommand tree.
package pass

import (
	"fmt"
	"time"

	"github.com/roman-16/proton-cli/cmd/shared"
	"github.com/roman-16/proton-cli/internal/app"
	"github.com/roman-16/proton-cli/internal/keys"
	"github.com/roman-16/proton-cli/internal/render"
	passsvc "github.com/roman-16/proton-cli/internal/services/pass"
	"github.com/spf13/cobra"
)

// NewCmd returns the root `pass` command.
func NewCmd() *cobra.Command {
	c := &cobra.Command{Use: "pass", Short: "Password manager operations"}
	c.AddCommand(itemsCmd(), vaultsCmd(), aliasCmd())
	return c
}

// ── pass items ──

func itemsCmd() *cobra.Command {
	c := &cobra.Command{Use: "items", Short: "Manage Pass items"}

	var listVault string
	list := &cobra.Command{
		Use: "list", Short: "List Pass items across vaults",
		RunE: func(cmd *cobra.Command, args []string) error {
			a := app.From(cmd.Context())
			if err := a.Authenticate(cmd.Context()); err != nil {
				return err
			}
			u, err := a.Unlock(cmd.Context())
			if err != nil {
				return err
			}
			items, err := a.Pass.ItemsList(cmd.Context(), u, listVault)
			if err != nil {
				return err
			}
			if a.R.Format != render.FormatText {
				return a.R.Object(items)
			}
			headers := []string{"TYPE", "NAME", "USERNAME", "SHARE_ID", "ITEM_ID"}
			var rows [][]string
			for _, it := range items {
				uname := it.Username
				if uname == "" {
					uname = it.Email
				}
				rows = append(rows, []string{it.Type, it.Name, uname, it.ShareID, it.ItemID})
			}
			render.Table(a.R.Stdout, headers, rows)
			_, _ = fmt.Fprintf(a.R.Stderr, "\n%d item(s)\n", len(items))
			return nil
		},
	}
	list.Flags().StringVar(&listVault, "vault", "", "Filter by vault name or ID")
	c.AddCommand(list)

	c.AddCommand(&cobra.Command{
		Use: "get {SHARE_ID ITEM_ID | SEARCH}", Short: "Get a Pass item (decrypted)",
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
			shareID, itemID, err := a.Pass.ResolveItem(cmd.Context(), u, args)
			if err != nil {
				return app.Exit(shared.ResolveExit(err), err)
			}
			it, err := a.Pass.ItemGet(cmd.Context(), u, shareID, itemID)
			if err != nil {
				return err
			}
			if a.R.Format != render.FormatText {
				return a.R.Object(it)
			}
			_, _ = fmt.Fprintf(a.R.Stdout, "Type:     %s\n", it.Type)
			_, _ = fmt.Fprintf(a.R.Stdout, "Name:     %s\n", it.Name)
			if it.Username != "" {
				_, _ = fmt.Fprintf(a.R.Stdout, "Username: %s\n", it.Username)
			}
			if it.Email != "" {
				_, _ = fmt.Fprintf(a.R.Stdout, "Email:    %s\n", it.Email)
			}
			if it.Password != "" {
				_, _ = fmt.Fprintf(a.R.Stdout, "Password: %s\n", it.Password)
			}
			if it.TOTP != "" {
				_, _ = fmt.Fprintf(a.R.Stdout, "TOTP:     %s\n", it.TOTP)
			}
			for _, u := range it.URLs {
				_, _ = fmt.Fprintf(a.R.Stdout, "URL:      %s\n", u)
			}
			if it.Holder != "" {
				_, _ = fmt.Fprintf(a.R.Stdout, "Holder:   %s\n", it.Holder)
			}
			if it.Number != "" {
				_, _ = fmt.Fprintf(a.R.Stdout, "Number:   %s\n", it.Number)
			}
			if it.Expiry != "" {
				_, _ = fmt.Fprintf(a.R.Stdout, "Expiry:   %s\n", it.Expiry)
			}
			if it.CVV != "" {
				_, _ = fmt.Fprintf(a.R.Stdout, "CVV:      %s\n", it.CVV)
			}
			if it.PIN != "" {
				_, _ = fmt.Fprintf(a.R.Stdout, "PIN:      %s\n", it.PIN)
			}
			if it.SSID != "" {
				_, _ = fmt.Fprintf(a.R.Stdout, "SSID:     %s\n", it.SSID)
			}
			if it.Note != "" {
				_, _ = fmt.Fprintf(a.R.Stdout, "Note:     %s\n", it.Note)
			}
			_, _ = fmt.Fprintf(a.R.Stdout, "ID:       %s\n", it.ItemID)
			_, _ = fmt.Fprintf(a.R.Stdout, "Share:    %s\n", it.ShareID)
			return nil
		},
	})

	var nc passsvc.NewItem
	var createVault string
	create := &cobra.Command{
		Use: "create", Short: "Create a Pass item (login, note, card)",
		RunE: func(cmd *cobra.Command, args []string) error {
			a := app.From(cmd.Context())
			if err := a.Authenticate(cmd.Context()); err != nil {
				return err
			}
			if nc.Name == "" {
				return fmt.Errorf("--name is required")
			}
			u, err := a.Unlock(cmd.Context())
			if err != nil {
				return err
			}
			shareID, err := a.Pass.ResolveVault(cmd.Context(), u, createVault)
			if err != nil {
				return app.Exit(shared.ResolveExit(err), err)
			}
			if a.DryRun {
				a.R.Info(fmt.Sprintf("dry-run: would create %s %q in vault %s", nc.Type, nc.Name, shareID))
				return nil
			}
			body, err := a.Pass.ItemCreate(cmd.Context(), u, shareID, nc)
			if err != nil {
				return err
			}
			id := shared.PickID(body, "Item", "ItemID")
			a.R.ID(id, fmt.Sprintf("Created %s %q", nc.Type, nc.Name))
			return nil
		},
	}
	create.Flags().StringVar(&nc.Type, "type", "login", "Item type (login, note, card)")
	create.Flags().StringVar(&nc.Name, "name", "", "Item name")
	create.Flags().StringVar(&nc.Username, "username", "", "Username (login)")
	create.Flags().StringVar(&nc.Password, "password", "", "Password (login)")
	create.Flags().StringVar(&nc.Email, "email", "", "Email (login)")
	create.Flags().StringVar(&nc.URL, "url", "", "URL (login)")
	create.Flags().StringVar(&nc.Note, "note", "", "Note")
	create.Flags().StringVar(&nc.Holder, "holder", "", "Cardholder name (card)")
	create.Flags().StringVar(&nc.Number, "number", "", "Card number (card)")
	create.Flags().StringVar(&nc.Expiry, "expiry", "", "Card expiry YYYY-MM (card)")
	create.Flags().StringVar(&nc.CVV, "cvv", "", "Card CVV (card)")
	create.Flags().StringVar(&nc.PIN, "pin", "", "Card PIN (card)")
	create.Flags().StringVar(&createVault, "vault", "", "Vault name or ID (default: first vault)")
	c.AddCommand(create)

	var patch passsvc.Patch
	edit := &cobra.Command{
		Use: "edit {SHARE_ID ITEM_ID | SEARCH}", Short: "Edit a Pass item",
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
			shareID, itemID, err := a.Pass.ResolveItem(cmd.Context(), u, args)
			if err != nil {
				return app.Exit(shared.ResolveExit(err), err)
			}
			if a.DryRun {
				a.R.Info(fmt.Sprintf("dry-run: would edit %s/%s", shareID, itemID))
				return nil
			}
			if err := a.Pass.ItemEdit(cmd.Context(), u, shareID, itemID, patch); err != nil {
				return err
			}
			a.R.Success("Item updated.")
			return nil
		},
	}
	edit.Flags().StringVar(&patch.Name, "name", "", "New name")
	edit.Flags().StringVar(&patch.Username, "username", "", "New username")
	edit.Flags().StringVar(&patch.Password, "password", "", "New password")
	edit.Flags().StringVar(&patch.Email, "email", "", "New email")
	edit.Flags().StringVar(&patch.URL, "url", "", "New URL")
	edit.Flags().StringVar(&patch.Note, "note", "", "New note")
	c.AddCommand(edit)

	c.AddCommand(simpleItemCmd("restore", "Restore an item from trash", func(a *app.App, cmd *cobra.Command, share, item string) error {
		return a.Pass.ItemRestore(cmd.Context(), share, item)
	}, "Item restored."))
	c.AddCommand(bulkItemCmd("trash", "Move items to trash", func(a *app.App, cmd *cobra.Command, share, item string) error {
		return a.Pass.ItemTrash(cmd.Context(), share, item)
	}, "Trashed %d item(s)."))
	c.AddCommand(bulkItemCmd("delete", "Permanently delete items", func(a *app.App, cmd *cobra.Command, share, item string) error {
		return a.Pass.ItemDelete(cmd.Context(), share, item)
	}, "Deleted %d item(s)."))
	return c
}

func simpleItemCmd(use, short string, fn func(a *app.App, cmd *cobra.Command, share, item string) error, success string) *cobra.Command {
	return &cobra.Command{
		Use: use + " {SHARE_ID ITEM_ID | SEARCH}", Short: short,
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
			shareID, itemID, err := a.Pass.ResolveItem(cmd.Context(), u, args)
			if err != nil {
				return app.Exit(shared.ResolveExit(err), err)
			}
			if a.DryRun {
				a.R.Info(fmt.Sprintf("dry-run: would %s %s/%s", use, shareID, itemID))
				return nil
			}
			if err := fn(a, cmd, shareID, itemID); err != nil {
				return err
			}
			a.R.Success(success)
			return nil
		},
	}
}

// itemFilter holds the batch-filter flags for pass items.
type itemFilter struct {
	vault, itemType      string
	olderThan, newerThan string
	all                  bool
}

func (f *itemFilter) register(c *cobra.Command) {
	c.Flags().StringVar(&f.vault, "vault", "", "Filter by vault name or ID")
	c.Flags().StringVar(&f.itemType, "type", "", "Filter by item type (login, note, credit_card, alias, identity, ssh_key, wifi, custom)")
	c.Flags().StringVar(&f.olderThan, "older-than", "", "Match items not modified within DURATION (e.g. 30d, 2w, 1h)")
	c.Flags().StringVar(&f.newerThan, "newer-than", "", "Match items modified within DURATION")
	c.Flags().BoolVar(&f.all, "all", false, "Confirm matching every item in the scope (required when no other filter is set)")
}

func (f *itemFilter) set() bool {
	return f.vault != "" || f.itemType != "" || f.olderThan != "" || f.newerThan != "" || f.all
}

func collectItemIDs(cmd *cobra.Command, u *keys.Unlocked, args []string, f *itemFilter) ([][2]string, error) {
	a := app.From(cmd.Context())
	var pairs [][2]string

	// Explicit arg(s): either [SHARE ITEM] or a single search term.
	if len(args) == 2 {
		pairs = append(pairs, [2]string{args[0], args[1]})
	} else if len(args) == 1 {
		shareID, itemID, err := a.Pass.ResolveItem(cmd.Context(), u, args)
		if err != nil {
			return nil, app.Exit(shared.ResolveExit(err), err)
		}
		pairs = append(pairs, [2]string{shareID, itemID})
	}

	if f.set() {
		var olderCutoff, newerCutoff int64
		if f.olderThan != "" {
			d, err := render.ParseDuration(f.olderThan)
			if err != nil {
				return nil, fmt.Errorf("invalid --older-than: %w", err)
			}
			olderCutoff = time.Now().Add(-d).Unix()
		}
		if f.newerThan != "" {
			d, err := render.ParseDuration(f.newerThan)
			if err != nil {
				return nil, fmt.Errorf("invalid --newer-than: %w", err)
			}
			newerCutoff = time.Now().Add(-d).Unix()
		}
		items, err := a.Pass.ItemsList(cmd.Context(), u, f.vault)
		if err != nil {
			return nil, err
		}
		for _, it := range items {
			if f.itemType != "" && it.Type != f.itemType {
				continue
			}
			if olderCutoff != 0 && it.ModifyTime > olderCutoff {
				continue
			}
			if newerCutoff != 0 && it.ModifyTime < newerCutoff {
				continue
			}
			pairs = append(pairs, [2]string{it.ShareID, it.ItemID})
		}
	}

	if len(args) == 0 && !f.set() {
		return nil, fmt.Errorf("no items selected: pass an item argument or a filter (--vault, --type); use --all to target an entire vault")
	}

	seen := make(map[string]struct{}, len(pairs))
	out := make([][2]string, 0, len(pairs))
	for _, p := range pairs {
		key := p[0] + "/" + p[1]
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, p)
	}
	return out, nil
}

func bulkItemCmd(use, short string, fn func(a *app.App, cmd *cobra.Command, share, item string) error, successFmt string) *cobra.Command {
	var f itemFilter
	c := &cobra.Command{
		Use:   use + " [SHARE_ID ITEM_ID | SEARCH]",
		Short: short,
		Args:  cobra.RangeArgs(0, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			a := app.From(cmd.Context())
			if err := a.Authenticate(cmd.Context()); err != nil {
				return err
			}
			u, err := a.Unlock(cmd.Context())
			if err != nil {
				return err
			}
			pairs, err := collectItemIDs(cmd, u, args, &f)
			if err != nil {
				return err
			}
			if a.DryRun {
				a.R.Info(fmt.Sprintf("dry-run: would %s %d item(s)", use, len(pairs)))
				for _, p := range pairs {
					_, _ = fmt.Fprintf(a.R.Stderr, "  %s/%s\n", p[0], p[1])
				}
				return nil
			}
			for _, p := range pairs {
				if err := fn(a, cmd, p[0], p[1]); err != nil {
					return err
				}
			}
			a.R.Success(fmt.Sprintf(successFmt, len(pairs)))
			return nil
		},
	}
	f.register(c)
	return c
}

// ── pass vaults ──

func vaultsCmd() *cobra.Command {
	c := &cobra.Command{Use: "vaults", Short: "Manage Pass vaults"}
	c.AddCommand(&cobra.Command{
		Use: "list", Short: "List vaults",
		RunE: func(cmd *cobra.Command, args []string) error {
			a := app.From(cmd.Context())
			if err := a.Authenticate(cmd.Context()); err != nil {
				return err
			}
			u, err := a.Unlock(cmd.Context())
			if err != nil {
				return err
			}
			vaults, err := a.Pass.VaultsList(cmd.Context(), u)
			if err != nil {
				return err
			}
			if a.R.Format != render.FormatText {
				return a.R.Object(vaults)
			}
			headers := []string{"SHARE_ID", "NAME", "MEMBERS", "OWNER", "SHARED"}
			var rows [][]string
			for _, v := range vaults {
				owner := "no"
				if v.Owner {
					owner = "yes"
				}
				shared := "no"
				if v.Shared {
					shared = "yes"
				}
				name := v.Name
				if name == "" {
					name = "(encrypted)"
				}
				rows = append(rows, []string{v.ShareID, name, fmt.Sprintf("%d", v.Members), owner, shared})
			}
			render.Table(a.R.Stdout, headers, rows)
			return nil
		},
	})
	var name string
	create := &cobra.Command{
		Use: "create", Short: "Create a vault",
		RunE: func(cmd *cobra.Command, args []string) error {
			a := app.From(cmd.Context())
			if err := a.Authenticate(cmd.Context()); err != nil {
				return err
			}
			if name == "" {
				return fmt.Errorf("--name is required")
			}
			u, err := a.Unlock(cmd.Context())
			if err != nil {
				return err
			}
			if a.DryRun {
				a.R.Info(fmt.Sprintf("dry-run: would create vault %q", name))
				return nil
			}
			body, err := a.Pass.VaultCreate(cmd.Context(), u, name)
			if err != nil {
				return err
			}
			id := shared.PickID(body, "Share", "ShareID")
			a.R.ID(id, fmt.Sprintf("Created vault %q", name))
			return nil
		},
	}
	create.Flags().StringVar(&name, "name", "", "Vault name")
	c.AddCommand(create)

	c.AddCommand(&cobra.Command{
		Use: "delete SHARE_ID", Short: "Delete a vault",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a := app.From(cmd.Context())
			if err := a.Authenticate(cmd.Context()); err != nil {
				return err
			}
			if a.DryRun {
				a.R.Info(fmt.Sprintf("dry-run: would delete vault %s", args[0]))
				return nil
			}
			if err := a.Pass.VaultDelete(cmd.Context(), args[0]); err != nil {
				return err
			}
			a.R.Success("Vault deleted.")
			return nil
		},
	})
	return c
}

// ── pass alias ──

func aliasCmd() *cobra.Command {
	c := &cobra.Command{Use: "alias", Short: "Manage aliases"}
	c.AddCommand(&cobra.Command{
		Use: "options", Short: "List available alias suffixes and mailboxes",
		RunE: func(cmd *cobra.Command, args []string) error {
			a := app.From(cmd.Context())
			if err := a.Authenticate(cmd.Context()); err != nil {
				return err
			}
			u, err := a.Unlock(cmd.Context())
			if err != nil {
				return err
			}
			shareID, err := a.Pass.ResolveVault(cmd.Context(), u, "")
			if err != nil {
				return err
			}
			sx, mx, err := a.Pass.AliasOptions(cmd.Context(), shareID)
			if err != nil {
				return err
			}
			if a.R.Format != render.FormatText {
				return a.R.Object(map[string]any{"Suffixes": sx, "Mailboxes": mx})
			}
			_, _ = fmt.Fprintln(a.R.Stdout, "Suffixes:")
			for _, s := range sx {
				_, _ = fmt.Fprintf(a.R.Stdout, "  %s\n", s.Suffix)
			}
			_, _ = fmt.Fprintln(a.R.Stdout, "\nMailboxes:")
			for _, m := range mx {
				_, _ = fmt.Fprintf(a.R.Stdout, "  %s (ID: %d)\n", m.Email, m.ID)
			}
			return nil
		},
	})
	var prefix, suffix, mailbox, name, vault string
	create := &cobra.Command{
		Use: "create", Short: "Create an alias",
		RunE: func(cmd *cobra.Command, args []string) error {
			a := app.From(cmd.Context())
			if err := a.Authenticate(cmd.Context()); err != nil {
				return err
			}
			if prefix == "" {
				return fmt.Errorf("--prefix is required")
			}
			u, err := a.Unlock(cmd.Context())
			if err != nil {
				return err
			}
			shareID, err := a.Pass.ResolveVault(cmd.Context(), u, vault)
			if err != nil {
				return app.Exit(shared.ResolveExit(err), err)
			}
			if a.DryRun {
				a.R.Info(fmt.Sprintf("dry-run: would create alias %s@%s", prefix, suffix))
				return nil
			}
			body, err := a.Pass.AliasCreate(cmd.Context(), u, shareID, prefix, suffix, mailbox, name)
			if err != nil {
				return err
			}
			id := shared.PickID(body, "Item", "ItemID")
			a.R.ID(id, "Alias created.")
			return nil
		},
	}
	create.Flags().StringVar(&prefix, "prefix", "", "Alias prefix (before @)")
	create.Flags().StringVar(&suffix, "suffix", "", "Alias suffix (e.g. @passmail.net)")
	create.Flags().StringVar(&mailbox, "mailbox", "", "Mailbox email to forward to")
	create.Flags().StringVar(&name, "name", "", "Display name for the alias item")
	create.Flags().StringVar(&vault, "vault", "", "Vault name or ID (default: first vault)")
	c.AddCommand(create)
	return c
}
