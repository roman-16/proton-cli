// Package contacts implements the `contacts` subcommand tree.
package contacts

import (
	"fmt"

	"github.com/roman-16/proton-cli/cmd/shared"
	"github.com/roman-16/proton-cli/internal/app"
	"github.com/roman-16/proton-cli/internal/render"
	ctsvc "github.com/roman-16/proton-cli/internal/services/contacts"
	"github.com/spf13/cobra"
)

// NewCmd returns the root `contacts` command.
func NewCmd() *cobra.Command {
	c := &cobra.Command{Use: "contacts", Short: "Contact operations"}
	c.AddCommand(listCmd(), getCmd(), createCmd(), updateCmd(), deleteCmd())
	return c
}

func listCmd() *cobra.Command {
	return &cobra.Command{
		Use: "list", Short: "List contacts (decrypted)",
		RunE: func(cmd *cobra.Command, args []string) error {
			a := app.From(cmd.Context())
			if err := a.Authenticate(cmd.Context()); err != nil {
				return err
			}
			u, err := a.Unlock(cmd.Context())
			if err != nil {
				return err
			}
			contacts, err := a.Contacts.List(cmd.Context(), u)
			if err != nil {
				return err
			}
			if a.R.Format != render.FormatText {
				return a.R.Object(contacts)
			}
			headers := []string{"ID", "NAME", "EMAIL", "PHONE"}
			var rows [][]string
			for _, c := range contacts {
				rows = append(rows, []string{c.ID, c.Name, c.Email, c.Phone})
			}
			render.Table(a.R.Stdout, headers, rows)
			return nil
		},
	}
}

func getCmd() *cobra.Command {
	return &cobra.Command{
		Use: "get REF", Short: "Get a contact (decrypted)",
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
			id, err := a.Contacts.Resolve(cmd.Context(), u, args[0])
			if err != nil {
				return app.Exit(shared.ResolveExit(err), err)
			}
			c, err := a.Contacts.Get(cmd.Context(), u, id)
			if err != nil {
				return err
			}
			if a.R.Format != render.FormatText {
				return a.R.Object(c)
			}
			_, _ = fmt.Fprintf(a.R.Stdout, "ID:    %s\n", c.ID)
			if c.Name != "" {
				_, _ = fmt.Fprintf(a.R.Stdout, "Name:  %s\n", c.Name)
			}
			if c.Email != "" {
				_, _ = fmt.Fprintf(a.R.Stdout, "Email: %s\n", c.Email)
			}
			if c.Phone != "" {
				_, _ = fmt.Fprintf(a.R.Stdout, "Phone: %s\n", c.Phone)
			}
			if c.Org != "" {
				_, _ = fmt.Fprintf(a.R.Stdout, "Org:   %s\n", c.Org)
			}
			if c.Note != "" {
				_, _ = fmt.Fprintf(a.R.Stdout, "Note:  %s\n", c.Note)
			}
			return nil
		},
	}
}

func createCmd() *cobra.Command {
	var nc ctsvc.NewContact
	c := &cobra.Command{
		Use: "create", Short: "Create a contact",
		RunE: func(cmd *cobra.Command, args []string) error {
			a := app.From(cmd.Context())
			if err := a.Authenticate(cmd.Context()); err != nil {
				return err
			}
			u, err := a.Unlock(cmd.Context())
			if err != nil {
				return err
			}
			if a.DryRun {
				a.R.Info(fmt.Sprintf("dry-run: would create contact %q", nc.Name))
				return nil
			}
			body, err := a.Contacts.Create(cmd.Context(), u, nc)
			if err != nil {
				return err
			}
			id := shared.PickID(body, "Responses", 0, "Response", "Contact", "ID")
			a.R.ID(id, fmt.Sprintf("Created contact %q", nc.Name))
			return nil
		},
	}
	c.Flags().StringVar(&nc.Name, "name", "", "Contact name")
	c.Flags().StringVar(&nc.Email, "email", "", "Contact email")
	c.Flags().StringVar(&nc.Phone, "phone", "", "Contact phone")
	c.Flags().StringVar(&nc.Org, "org", "", "Contact organization")
	c.Flags().StringVar(&nc.Note, "note", "", "Contact note")
	return c
}

func updateCmd() *cobra.Command {
	var nc ctsvc.NewContact
	c := &cobra.Command{
		Use: "update REF", Short: "Update a contact",
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
			id, err := a.Contacts.Resolve(cmd.Context(), u, args[0])
			if err != nil {
				return app.Exit(shared.ResolveExit(err), err)
			}
			if a.DryRun {
				a.R.Info(fmt.Sprintf("dry-run: would update contact %s", id))
				return nil
			}
			if err := a.Contacts.Update(cmd.Context(), u, id, nc); err != nil {
				return err
			}
			a.R.Success("Contact updated.")
			return nil
		},
	}
	c.Flags().StringVar(&nc.Name, "name", "", "New name")
	c.Flags().StringVar(&nc.Email, "email", "", "New email")
	c.Flags().StringVar(&nc.Phone, "phone", "", "New phone")
	c.Flags().StringVar(&nc.Org, "org", "", "New organization")
	c.Flags().StringVar(&nc.Note, "note", "", "New note")
	return c
}

func deleteCmd() *cobra.Command {
	return &cobra.Command{
		Use: "delete REF...", Short: "Delete contacts",
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
			ids := make([]string, 0, len(args))
			for _, ref := range args {
				id, err := a.Contacts.Resolve(cmd.Context(), u, ref)
				if err != nil {
					return app.Exit(shared.ResolveExit(err), err)
				}
				ids = append(ids, id)
			}
			if a.DryRun {
				a.R.Info(fmt.Sprintf("dry-run: would delete %d contact(s)", len(ids)))
				return nil
			}
			if err := a.Contacts.Delete(cmd.Context(), ids); err != nil {
				return err
			}
			a.R.Success(fmt.Sprintf("Deleted %d contact(s).", len(ids)))
			return nil
		},
	}
}
