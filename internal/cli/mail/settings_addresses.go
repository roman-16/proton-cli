package mail

import (
	"context"
	"strings"

	"github.com/roman-16/proton-cli/internal/app"
	"github.com/roman-16/proton-cli/internal/cli/kit"
	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/roman-16/proton-cli/internal/mailtext"
	mailsvc "github.com/roman-16/proton-cli/internal/service/mail"
	"github.com/roman-16/proton-cli/internal/ui"
	"github.com/spf13/cobra"
)

func addressesCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "addresses",
		Short: "Your addresses, display names and signatures",
		Long: "Your addresses, display names and signatures.\n\n" +
			"An address sends and receives mail under its own name. Adding, disabling and\n" +
			"deleting one needs a paid Mail plan. Your first Proton address and your\n" +
			"short-domain address stay enabled and cannot be deleted.",
	}
	c.AddCommand(
		addressesListCmd(), addressesGetCmd(), addressesCreateCmd(), addressesUpdateCmd(),
		addressesReorderCmd(), addressesEnableCmd(), addressesDisableCmd(), addressesDeleteCmd(),
	)
	return c
}

func addressesListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List the addresses on the account",
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			addrs, err := c.App.Mail.AddressesList(c.Ctx)
			if err != nil {
				return err
			}
			return kit.List(c, ui.TableSpec[mailsvc.Address]{
				Noun:  "addresses",
				Total: ui.Unknown, Page: ui.Unpaged,
				Columns: addressColumns(),
			}, addrs)
		}),
	}
}

func addressesGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get REF",
		Short: "Show one address, including its signature",
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			a, err := c.App.Mail.ResolveAddress(c.Ctx, c.Args[0])
			if err != nil {
				return err
			}
			signature := "(none)"
			if a.Signature != "" {
				signature = mailtext.HTMLToText(a.Signature)
			}
			return kit.Show(c, ui.RecordSpec{
				Object: a,
				Fields: []ui.Field{
					{Label: "Email", Value: a.Email, Handle: true},
					{Label: "Display Name", Value: a.DisplayName},
					{Label: "Type", Value: addressType(a.Type)},
					{Label: "Status", Value: addressStatus(*a), Always: true},
					{Label: "Keys", Value: addressKeys(*a), Always: true},
					{Label: "Can Send", Value: yesNo(a.CanSend()), Always: true},
					{Label: "End-to-end", Value: yesNo(a.EndToEnd), Always: true},
					{Label: "Signature", Value: signature, Always: true},
					{Label: "ID", Value: a.ID, ID: true},
				},
			})
		}),
	}
}

func addressesUpdateCmd() *cobra.Command {
	var displayName, signature string
	var html, clear bool
	c := &cobra.Command{
		Use:   "update REF",
		Short: "Set an address's display name or signature",
		Long: "Set the display name recipients see and the signature appended to mail sent\n" +
			"from this address.\n\n" +
			"A signature is stored as HTML. Plain text is escaped and its newlines become\n" +
			"line breaks; --html passes markup through untouched.",
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			setName, setSig := c.Changed("display-name"), c.Changed("signature")
			if clear && setSig {
				return kit.Fail("--clear-signature and --signature contradict each other.")
			}
			if !setName && !setSig && !clear {
				return kit.Fail("Nothing to change.").
					Hint("pass --display-name, --signature or --clear-signature.")
			}
			a, err := c.App.Mail.ResolveAddress(c.Ctx, c.Args[0])
			if err != nil {
				return err
			}
			var namePtr, sigPtr *string
			if setName {
				namePtr = &displayName
			}
			switch {
			case clear:
				empty := ""
				sigPtr = &empty
			case setSig:
				text, err := kit.ReadTextArg(c, signature, "--signature")
				if err != nil {
					return err
				}
				if !html {
					text = mailtext.TextToHTML(text)
				}
				sigPtr = &text
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Updated, Kind: "addresses", Count: 1,
				Name: a.Email, IDs: []string{a.ID},
			}, func() error {
				return c.App.Mail.AddressUpdate(c.Ctx, a.ID, namePtr, sigPtr)
			})
		}),
	}
	c.Flags().StringVar(&displayName, "display-name", "", "Name recipients see next to the address")
	c.Flags().StringVar(&signature, "signature", "", "Signature appended to mail from this address (- reads stdin)")
	c.Flags().BoolVar(&html, "html", false, "Treat the signature as HTML rather than escaping it")
	c.Flags().BoolVar(&clear, "clear-signature", false, "Remove the signature")
	return c
}

// addressesCreateCmd adds an address, key and all: Proton lets nothing through
// an address that has none, so the two are one command.
func addressesCreateCmd() *cobra.Command {
	var displayName string
	var reauth kit.Reauth
	c := &cobra.Command{
		Use:   "create EMAIL",
		Short: "Add an address to the account",
		Long: "Add an address to the account.\n\n" +
			"EMAIL is the address to add. Its domain has to be one the account can use: a\n" +
			"Proton domain, or a custom domain already set up. Adding an address needs a\n" +
			"paid Mail plan.\n\n" +
			"Your short-domain address is your username at pm.me. It takes the signature\n" +
			"of your default address, and its display name unless you pass --display-name.\n" +
			"Once it exists it cannot be disabled or deleted.\n\n" +
			"The address sends and receives as soon as it exists. An account that creates\n" +
			"post-quantum keys is refused: add the address in a Proton client.\n\n" +
			"Asks for your password even when you are signed in. With no terminal to ask,\n" +
			"pass --password-file or --password-stdin.",
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			if err := reauth.Supply(c); err != nil {
				return err
			}
			local, domain, err := splitAddress(c.Args[0])
			if err != nil {
				return err
			}
			return kit.Create(c, ui.ResultSpec{
				Action: ui.Created, Kind: "addresses", Name: local + "@" + domain,
			}, func() (string, error) {
				// Nothing here arranges the elevation: the client does it when the
				// server asks, and drops the scope again afterwards. All this owes
				// the user is a reason for the prompt.
				ctx := app.WithScopeReason(c.Ctx, "add an address")
				return c.App.Mail.AddressCreate(ctx, local, domain, displayName)
			})
		}),
	}
	c.Flags().StringVar(&displayName, "display-name", "", "Name recipients see next to the address")
	reauth.Declare(c)
	return c
}

// splitAddress reads EMAIL as Proton files it, which is a local part and a
// domain. It is judged from the command line, so a malformed one costs nothing.
func splitAddress(email string) (local, domain string, err error) {
	whole := strings.TrimSpace(email)
	local, domain, found := strings.Cut(whole, "@")
	if !found || local == "" || domain == "" ||
		strings.Contains(domain, "@") || strings.ContainsAny(whole, " \t") {
		return "", "", kit.Fail("%q is not an email address.", email).
			Hint("write it in full, as work@example.com")
	}
	return local, domain, nil
}

func addressesEnableCmd() *cobra.Command {
	return addressesVerbCmd(addressesVerb{
		use:   "enable",
		short: "Let a disabled address send and receive again",
		long: "REF is an address of yours that is disabled.\n\n" +
			"Asks for your password even when you are signed in. With no terminal to ask,\n" +
			"pass --password-file or --password-stdin.",
		action: ui.Enabled,
		reason: "enable an address",
		takes:  enableTakes,
		apply: func(ctx context.Context, m *mailsvc.Service, a mailsvc.Address) error {
			return m.AddressEnable(ctx, a.ID)
		},
	})
}

func addressesDisableCmd() *cobra.Command {
	return addressesVerbCmd(addressesVerb{
		use:   "disable",
		short: "Stop an address sending and receiving",
		long: "REF is an address of yours that is enabled. Everything it already holds\n" +
			"stays, and enabling it again needs nothing else.\n\n" +
			"Asks for your password even when you are signed in. With no terminal to ask,\n" +
			"pass --password-file or --password-stdin.",
		action: ui.Disabled,
		reason: "disable an address",
		takes:  disableTakes,
		apply: func(ctx context.Context, m *mailsvc.Service, a mailsvc.Address) error {
			return m.AddressDisable(ctx, a.ID)
		},
	})
}

func addressesDeleteCmd() *cobra.Command {
	return addressesVerbCmd(addressesVerb{
		use:   "delete",
		short: "Delete addresses",
		long: "REF is an address of yours. Proton allows one address deletion a year unless\n" +
			"the address is on a custom domain. Your first Proton address, your\n" +
			"short-domain address and the account's default address cannot be deleted.\n\n" +
			"A deleted address cannot be used again, by you or by anybody else.\n\n" +
			"Asks for your password even when you are signed in. With no terminal to ask,\n" +
			"pass --password-file or --password-stdin.",
		reason: "delete an address",
		action: ui.Deleted,
		takes:  deleteTakes,
		// Proton grants one deletion a year for an address on one of its own
		// domains, and says so for the account rather than for the address. Asking
		// before the question is put is what keeps a refusal from arriving after
		// somebody has already agreed to lose the address.
		allowed: func(c *kit.Invocation, rows []mailsvc.Address) error {
			var counted []mailsvc.Address
			for _, a := range rows {
				if a.Type != mailsvc.TypeCustomDomain {
					counted = append(counted, a)
				}
			}
			if len(counted) == 0 {
				return nil
			}
			allowed, err := c.App.Mail.AddressDeletionAllowed(c.Ctx)
			if err != nil {
				return err
			}
			if !allowed {
				return errs.Problemf(
					"Proton allows one address deletion a year, and this account has used it.").
					Hint("`" + kit.Program + " mail settings addresses disable " + counted[0].Email +
						"` stops mail reaching it")
			}
			if len(counted) > 1 {
				return errs.Problemf(
					"Only one of these addresses can be deleted: Proton allows one deletion a year.")
			}
			return nil
		},
		apply: func(ctx context.Context, m *mailsvc.Service, a mailsvc.Address) error {
			return m.AddressDelete(ctx, a)
		},
	})
}

// The default address is a position rather than a field: it is whichever
// address comes first, so making one the default and sorting them all are the
// same change.
func addressesReorderCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "reorder REF...",
		Short: "Make an address the default, or set their order",
		Long: "Make an address the default, or set their order.\n\n" +
			"The first address is the default: mail leaves from it when no --from is\n" +
			"given. Name the addresses that should come first, in order; the rest keep\n" +
			"the order they are in.\n\n" +
			"A disabled or external address, or one that cannot send or receive, cannot\n" +
			"be the default.",
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			list := addressList(c)
			all, err := list.Rows(c.Ctx)
			if err != nil {
				return err
			}
			sel, err := kit.SelectFrom(c, "addresses", addressColumns(), list)
			if err != nil {
				return err
			}
			if err := defaultTakes(sel.Rows[0]); err != nil {
				return err
			}
			order := reordered(all, sel.Rows)
			if err := reorderTakes(all, order, sel.Rows); err != nil {
				return err
			}
			ids := addressIDs(order)
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Reordered, Kind: "addresses", Count: len(ids), IDs: ids,
				Detail:  "with " + order[0].Email + " as the default",
				Preview: kit.Preview("addresses", addressColumns(), order),
			}, func() error {
				return c.App.Mail.AddressesReorder(c.Ctx, ids)
			})
		}),
	}
}

// reordered puts the named addresses first, in the order they were named, and
// leaves the rest where they are.
func reordered(all, named []mailsvc.Address) []mailsvc.Address {
	first := make(map[string]bool, len(named))
	for _, a := range named {
		first[a.ID] = true
	}
	order := make([]mailsvc.Address, 0, len(all))
	order = append(order, named...)
	for _, a := range all {
		if !first[a.ID] {
			order = append(order, a)
		}
	}
	return order
}

// reorderTakes admits an order that is a change, and refuses one the account is
// already in rather than reporting a change nobody made.
func reorderTakes(all, order, named []mailsvc.Address) error {
	for i := range order {
		if order[i].ID != all[i].ID {
			return nil
		}
	}
	if len(named) == 1 {
		return errs.Problemf("%s is already the default address.", named[0].Email)
	}
	return errs.Problemf("The addresses are already in that order.")
}

// defaultTakes admits the address a reorder would make the default, or says why
// Proton would not send from it.
func defaultTakes(a mailsvc.Address) error {
	switch {
	case a.Status == mailsvc.StatusDisabled:
		return errs.Problemf("%s is disabled, and a disabled address cannot be the default.", a.Email)
	case a.Type == mailsvc.TypeExternal:
		return errs.Problemf("%s is an external address, which cannot be the default.", a.Email)
	case a.Receive == 0:
		return errs.Problemf("%s cannot receive mail, so it cannot be the default.", a.Email)
	case a.Send == 0:
		return errs.Problemf("%s is receive-only, so it cannot be the default.", a.Email)
	}
	return nil
}

func addressIDs(addrs []mailsvc.Address) []string {
	ids := make([]string, 0, len(addrs))
	for _, a := range addrs {
		ids = append(ids, a.ID)
	}
	return ids
}

func enableTakes(a mailsvc.Address) error {
	if a.Status == mailsvc.StatusEnabled {
		return errs.Problemf("%s is already enabled.", a.Email)
	}
	return nil
}

func disableTakes(a mailsvc.Address) error {
	if err := notKept(a, "disabled"); err != nil {
		return err
	}
	if a.Status == mailsvc.StatusDisabled {
		return errs.Problemf("%s is already disabled.", a.Email)
	}
	return nil
}

func deleteTakes(a mailsvc.Address) error {
	if err := notKept(a, "deleted"); err != nil {
		return err
	}
	if a.Order == 1 {
		return errs.Problemf("%s is the account's default address, which cannot be deleted.", a.Email)
	}
	return nil
}

// notKept refuses the two addresses Proton keeps for the account.
func notKept(a mailsvc.Address, past string) error {
	switch a.Type {
	case mailsvc.TypeOriginal:
		return errs.Problemf("%s is your first Proton address, which cannot be %s.", a.Email, past)
	case mailsvc.TypePremium:
		return errs.Problemf("%s is your short-domain address, which cannot be %s.", a.Email, past)
	}
	return nil
}

// addressesVerb is one of the things that can be done to an address that is
// already there. They differ in what they do to each one and in which ones they
// take, so they are built from one declaration.
type addressesVerb struct {
	use    string
	short  string
	long   string
	action ui.Action
	// takes admits one address or says why the verb has nothing to do with it,
	// judged from the row before anything is sent.
	takes func(mailsvc.Address) error
	// allowed asks Proton what the whole selection would need, for the one verb
	// that spends something the account has a limited number of.
	allowed func(*kit.Invocation, []mailsvc.Address) error
	apply   func(context.Context, *mailsvc.Service, mailsvc.Address) error
	// reason completes "Your password is required to ..." for the verb Proton
	// guards behind an elevated session. Empty for the ones it does not.
	reason string
}

func addressesVerbCmd(v addressesVerb) *cobra.Command {
	var reauth kit.Reauth
	c := &cobra.Command{
		Use:   v.use + " REF...",
		Short: v.short,
		Long:  v.short + ".\n\n" + v.long,
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			if v.reason != "" {
				if err := reauth.Supply(c); err != nil {
					return err
				}
			}
			sel, err := kit.SelectFrom(c, "addresses", addressColumns(), addressList(c))
			if err != nil {
				return err
			}
			for _, a := range sel.Rows {
				if err := v.takes(a); err != nil {
					return err
				}
			}
			if v.allowed != nil {
				if err := v.allowed(c, sel.Rows); err != nil {
					return err
				}
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: v.action, Kind: "addresses", Count: sel.Len(), IDs: sel.IDs,
				Name:    kit.Sole(sel.Rows, func(a mailsvc.Address) string { return a.Email }),
				Preview: sel.Preview(),
			}, func() error {
				// Nothing here arranges the elevation: the client does it when the
				// server asks, and drops the scope again afterwards. All this owes
				// the user is a reason for the prompt.
				ctx := c.Ctx
				if v.reason != "" {
					ctx = app.WithScopeReason(ctx, v.reason)
				}
				for _, a := range sel.Rows {
					if err := v.apply(ctx, c.App.Mail, a); err != nil {
						return err
					}
				}
				return nil
			})
		}),
	}
	if v.reason != "" {
		reauth.Declare(c)
	}
	return c
}

func addressColumns() []ui.Column[mailsvc.Address] {
	return []ui.Column[mailsvc.Address]{
		{Header: "ID", ID: true, Cell: func(a mailsvc.Address) string { return a.ID }},
		{Header: "EMAIL", Flex: true, Handle: true, Cell: func(a mailsvc.Address) string { return a.Email }},
		{Header: "DISPLAY_NAME", Flex: true, Cell: func(a mailsvc.Address) string { return a.DisplayName }},
		{Header: "STATUS", Cell: func(a mailsvc.Address) string { return addressStatus(a) }},
		{Header: "TYPE", Cell: func(a mailsvc.Address) string { return addressType(a.Type) }},
		{Header: "SIGNATURE", Cell: func(a mailsvc.Address) string { return yesNo(a.Signature != "") }},
	}
}

func addressList(c *kit.Invocation) *kit.Lookup[mailsvc.Address] {
	return &kit.Lookup[mailsvc.Address]{
		Kind: "address",
		Load: func(ctx context.Context) ([]mailsvc.Address, error) {
			return c.App.Mail.AddressesList(ctx)
		},
		ID:     func(a mailsvc.Address) string { return a.ID },
		Handle: func(a mailsvc.Address) string { return a.Email },
	}
}

func addressStatus(a mailsvc.Address) string {
	if a.Status == mailsvc.StatusEnabled {
		return "active"
	}
	return "disabled"
}

// addressKeys says whether the address can carry mail at all. An address whose
// key was never published reads as active and does nothing.
func addressKeys(a mailsvc.Address) string {
	if a.HasKeys {
		return "published"
	}
	return "missing"
}

func addressType(t int) string {
	switch t {
	case mailsvc.TypeOriginal:
		return "original"
	case mailsvc.TypeAlias:
		return "alias"
	case mailsvc.TypeCustomDomain:
		return "custom"
	case mailsvc.TypePremium:
		return "premium"
	case mailsvc.TypeExternal:
		return "external"
	}
	return "unknown"
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}
