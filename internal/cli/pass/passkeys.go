package pass

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/roman-16/proton-cli/internal/cli/kit"
	"github.com/roman-16/proton-cli/internal/errs"
	passsvc "github.com/roman-16/proton-cli/internal/service/pass"
	"github.com/roman-16/proton-cli/internal/ui"
	"github.com/roman-16/proton-cli/internal/units"
)

// Signing in with no password at all.
//
// A passkey is made between the site and the browser, so nothing here can add
// one. What a terminal can do is what Proton Pass itself offers: say which
// passkeys a login carries, and take one off.

func passkeysCmd() *cobra.Command {
	c := &cobra.Command{Use: "passkeys", Short: "Passkeys stored against a login"}
	c.AddCommand(passkeysListCmd(), passkeysRemoveCmd())
	return c
}

func passkeyColumns() []ui.Column[passsvc.Passkey] {
	return []ui.Column[passsvc.Passkey]{
		{Header: "ID", ID: true, Cell: func(p passsvc.Passkey) string { return p.KeyID }},
		{Header: "DOMAIN", Flex: true, Cell: func(p passsvc.Passkey) string { return p.Domain }},
		{Header: "USERNAME", Flex: true, Handle: true, Cell: func(p passsvc.Passkey) string { return p.Username }},
		{Header: "CREATED", Cell: func(p passsvc.Passkey) string { return units.Time(p.Created) }},
		{Header: "DEVICE", Flex: true, Cell: func(p passsvc.Passkey) string { return p.Device }},
	}
}

func passkeysListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list REF",
		Short: "List the passkeys stored against a login",
		Long: "List the passkeys stored against a login.\n\n" +
			"A passkey signs in without a password, so a login that carries one may hold\n" +
			"no password at all.\n\n" +
			"--output json carries what a row leaves out: the site's own name, the name\n" +
			"it shows you as, the passkey's note, and the build of Pass that made it.",
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			login, err := loginOf(c, c.Args[0])
			if err != nil {
				return err
			}
			return kit.List(c, ui.TableSpec[passsvc.Passkey]{
				Noun: "passkeys", Columns: passkeyColumns(),
				Total: ui.Unknown, Page: ui.Unpaged,
			}, login.Passkeys)
		}),
	}
}

func passkeysRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remove REF PASSKEY_REF...",
		Short: "Take a passkey off a login",
		Long: "Take a passkey off a login.\n\n" +
			"PASSKEY_REF is the username or the ID `passkeys list` shows.\n\n" +
			"The site keeps its half of the credential, so it will still offer to sign\n" +
			"you in with a passkey this account no longer holds.\n\n" +
			"Taking one off is a new version of the item, so `items revisions restore`\n" +
			"puts it back.",
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			login, err := loginOf(c, c.Args[0])
			if err != nil {
				return err
			}
			held := &kit.Lookup[passsvc.Passkey]{
				Kind: "passkey",
				Load: func(context.Context) ([]passsvc.Passkey, error) {
					return login.Passkeys, nil
				},
				ID:     func(p passsvc.Passkey) string { return p.KeyID },
				Handle: func(p passsvc.Passkey) string { return p.Username },
			}
			// The first argument named the login, so the rest are the passkeys.
			sel, err := kit.Select(c, kit.Selector[passsvc.Passkey]{
				Noun: "passkeys", Columns: passkeyColumns(), Refs: c.Args[1:],
				IDOf: held.ID, ByRef: held.Find,
			})
			if err != nil {
				return err
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Removed, Kind: "passkeys", Count: sel.Len(), IDs: sel.IDs,
				Name:    kit.Sole(sel.Rows, func(p passsvc.Passkey) string { return p.Username }),
				Preview: sel.Preview(),
			}, func() error {
				return c.App.Pass.ItemEdit(c.Ctx, login.ShareID, login.ItemID,
					passsvc.Patch{RemovePasskeys: sel.IDs})
			})
		}),
	}
}

// loginOf resolves the reference to a login, refusing anything that is not one:
// only a login can carry a passkey.
func loginOf(c *kit.Invocation, ref string) (*passsvc.FullItem, error) {
	shareID, itemID, err := resolveItem(c, ref)
	if err != nil {
		return nil, err
	}
	it, err := c.App.Pass.ItemGet(c.Ctx, shareID, itemID)
	if err != nil {
		return nil, err
	}
	if it.Type != "login" {
		return nil, errs.Naming(it.Name, kit.Fail("%s is a %s, not a login.", it.Name, it.Type).
			Hint("only a login carries passkeys; `proton pass items list --type login` shows yours."))
	}
	return it, nil
}

// passkeyFields are the passkeys a login carries, as a record shows them.
func passkeyFields(keys []passsvc.Passkey) []ui.Field {
	out := make([]ui.Field, 0, len(keys))
	for _, p := range keys {
		out = append(out, ui.Field{Label: "Passkey", Value: passkeyLabel(p)})
	}
	return out
}

// passkeyLabel is the one line a record gives a passkey: who it signs in as and
// where, which is what tells two on the same item apart.
func passkeyLabel(p passsvc.Passkey) string {
	switch {
	case p.Username != "" && p.Domain != "":
		return p.Username + " (" + p.Domain + ")"
	case p.Username != "":
		return p.Username
	case p.Domain != "":
		return p.Domain
	}
	return p.KeyID
}
