package account

import (
	"github.com/roman-16/proton-cli/internal/cli/kit"
	acctsvc "github.com/roman-16/proton-cli/internal/service/account"
	"github.com/roman-16/proton-cli/internal/ui"
	"github.com/roman-16/proton-cli/internal/units"
	"github.com/spf13/cobra"
)

// Data-recovery contacts: the people who may help the account back in if its
// owner is locked out. Each is given a copy of the account's keys, sealed to
// them, and can hand back the part that reactivates locked keys.

func recoveryContactsCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "recovery-contacts",
		Short: "People who can help you back into your account",
		Long: "People who can help you back into your account.\n\n" +
			"A contact you add is handed a copy of your keys, sealed so only they can\n" +
			"open it. If you are ever locked out, they can hand back the part that\n" +
			"reactivates your keys. A contact has to have a Proton account of their own.\n\n" +
			"`list` shows the people you chose; `list --incoming` shows accounts that\n" +
			"chose you.",
	}
	c.AddCommand(recoveryContactsListCmd(), recoveryContactsGetCmd(),
		recoveryContactsAddCmd(), recoveryContactsRemoveCmd())
	return c
}

func recoveryContactColumns() []ui.Column[acctsvc.DelegatedAccess] {
	return []ui.Column[acctsvc.DelegatedAccess]{
		{Header: "ID", ID: true, Cell: func(d acctsvc.DelegatedAccess) string { return d.ID }},
		{Header: "CONTACT", Flex: true, Handle: true, Cell: func(d acctsvc.DelegatedAccess) string { return d.Contact }},
		{Header: "STATUS", Cell: func(d acctsvc.DelegatedAccess) string { return d.Status }},
		{Header: "CREATED", Cell: func(d acctsvc.DelegatedAccess) string { return units.Time(d.Created) }},
	}
}

func recoveryContactsListCmd() *cobra.Command {
	var incoming bool
	var held kit.Held[acctsvc.DelegatedAccess]
	c := &cobra.Command{
		Use:   "list",
		Short: "List the people who can help you recover",
		Long: "List the people who can help you recover.\n\n" +
			"--incoming lists accounts you can help recover instead.",
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			rows, err := delegatedRows(c, acctsvc.KindRecovery, incoming)
			if err != nil {
				return err
			}
			return held.Answer(c, ui.TableSpec[acctsvc.DelegatedAccess]{
				Noun: "recovery contacts", Columns: recoveryContactColumns(),
			}, rows)
		}),
	}
	kit.Incoming(c.Flags(), &incoming)
	held.Register(c, "recovery contacts",
		kit.Key[acctsvc.DelegatedAccess]{Name: "created", Less: func(a, b acctsvc.DelegatedAccess) int {
			return kit.Ints(b.Created, a.Created)
		}},
		kit.Key[acctsvc.DelegatedAccess]{Name: "contact", Less: func(a, b acctsvc.DelegatedAccess) int {
			return kit.Fold(a.Contact, b.Contact)
		}},
	)
	return c
}

func recoveryContactsGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get REF",
		Short: "Show one recovery contact",
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			da, err := delegatedLookup(c, acctsvc.KindRecovery).Find(c.Ctx, c.Args[0])
			if err != nil {
				return err
			}
			return kit.Show(c, ui.RecordSpec{
				Object: da,
				Fields: []ui.Field{
					{Label: "Contact", Value: da.Contact, Handle: true},
					{Label: "Direction", Value: string(da.Direction)},
					{Label: "Status", Value: da.Status, Always: true},
					{Label: "Created", Value: units.Time(da.Created)},
					{Label: "ID", Value: da.ID, ID: true},
				},
			})
		}),
	}
}

func recoveryContactsAddCmd() *cobra.Command {
	var reauth kit.Reauth
	c := &cobra.Command{
		Use:   "add EMAIL",
		Short: "Let somebody help you recover your account",
		Long: "Let somebody help you recover your account.\n\n" +
			"Your password is asked for. The contact has to have a Proton account.",
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			email, err := contactAddress(c.Args[0])
			if err != nil {
				return err
			}
			if err := reauth.Supply(c); err != nil {
				return err
			}
			var da acctsvc.DelegatedAccess
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Added, Kind: "recovery contacts", Count: 1, Name: email,
			}, func() error {
				da, err = c.App.Account.AddDelegatedAccess(c.Ctx, acctsvc.KindRecovery, email, 0)
				if err == nil {
					c.Note("ID %s", da.ID)
				}
				return err
			})
		}),
	}
	reauth.Declare(c)
	return c
}

func recoveryContactsRemoveCmd() *cobra.Command {
	var reauth kit.Reauth
	c := &cobra.Command{
		Use:   "remove REF...",
		Short: "Remove a recovery contact",
		Long: "Remove a recovery contact.\n\n" +
			"Your password is asked for. Their copy of your keys stops working. A contact\n" +
			"who is also an emergency contact keeps that.",
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			sel, err := kit.SelectFrom(c, "recovery contacts", recoveryContactColumns(), delegatedLookup(c, acctsvc.KindRecovery))
			if err != nil {
				return err
			}
			if err := reauth.Supply(c); err != nil {
				return err
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Removed, Kind: "recovery contacts", Count: sel.Len(), IDs: sel.IDs,
			}, func() error {
				for _, id := range sel.IDs {
					if err := c.App.Account.DeleteDelegatedAccess(c.Ctx, id, acctsvc.KindRecovery); err != nil {
						return err
					}
				}
				return nil
			})
		}),
	}
	reauth.Declare(c)
	return c
}
