package mail

import (
	"github.com/roman-16/proton-cli/internal/cli/kit"
	"github.com/roman-16/proton-cli/internal/mailtext"
	mailsvc "github.com/roman-16/proton-cli/internal/service/mail"
	"github.com/roman-16/proton-cli/internal/ui"
	"github.com/spf13/cobra"
)

func addressesCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "addresses",
		Short: "Your addresses, display names and signatures",
	}
	c.AddCommand(addressesListCmd(), addressesGetCmd(), addressesUpdateCmd())
	return c
}

func addressesListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List the addresses on the account",
		Args:  cobra.NoArgs,
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			addrs, err := c.App.Mail.AddressesList(c.Ctx)
			if err != nil {
				return err
			}
			return kit.List(c, ui.TableSpec[mailsvc.Address]{
				Noun:  "addresses",
				Total: ui.Unknown, Page: ui.Unpaged,
				Columns: []ui.Column[mailsvc.Address]{
					{Header: "ID", ID: true, Cell: func(a mailsvc.Address) string { return a.ID }},
					{Header: "EMAIL", Flex: true, Handle: true, Cell: func(a mailsvc.Address) string { return a.Email }},
					{Header: "DISPLAY_NAME", Flex: true, Cell: func(a mailsvc.Address) string { return a.DisplayName }},
					{Header: "STATUS", Cell: func(a mailsvc.Address) string { return addressStatus(a) }},
					{Header: "TYPE", Cell: func(a mailsvc.Address) string { return addressType(a.Type) }},
					{Header: "SIGNATURE", Cell: func(a mailsvc.Address) string {
						return yesNo(a.Signature != "")
					}},
				},
			}, addrs)
		}),
	}
}

func addressesGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get REF",
		Short: "Show one address, including its signature",
		Args:  cobra.ExactArgs(1),
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
					{Label: "Can Send", Value: yesNo(a.CanSend()), Always: true},
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
			"Proton stores signatures as HTML. Plain text is escaped and its newlines become\n" +
			"line breaks; --html passes markup through untouched.",
		Args: cobra.ExactArgs(1),
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

func addressStatus(a mailsvc.Address) string {
	if a.Status == 1 {
		return "active"
	}
	return "disabled"
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

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}
