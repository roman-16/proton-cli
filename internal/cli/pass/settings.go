package pass

import (
	"strconv"

	"github.com/spf13/cobra"

	"github.com/roman-16/proton-cli/internal/cli/kit"
	passsvc "github.com/roman-16/proton-cli/internal/service/pass"
	"github.com/roman-16/proton-cli/internal/ui"
)

// What Pass keeps on the account rather than in a vault.
//
// Both of these are what an alias is made of: the mailboxes it can arrive in and
// the domains it can be made on. Pass only offers them in its settings, which is
// where they hang here.

func settingsCmd() *cobra.Command {
	c := &cobra.Command{Use: "settings", Short: "Pass settings"}
	c.AddCommand(mailboxesCmd(), domainsCmd())
	return c
}

func mailboxesCmd() *cobra.Command {
	c := &cobra.Command{Use: "mailboxes", Short: "The addresses your aliases forward to"}
	c.AddCommand(mailboxesListCmd(), mailboxesCreateCmd(), mailboxesVerifyCmd(),
		mailboxesResendCmd(), mailboxesUpdateCmd(), mailboxesDeleteCmd())
	return c
}

func mailboxesCreateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "create EMAIL",
		Short: "Add an address for aliases to forward to",
		Long: "Add an address for aliases to forward to.\n\n" +
			"Proton emails the address a code. The mailbox receives nothing until you\n" +
			"pass that code to `mailboxes verify`.",
		Args: cobra.ExactArgs(1),
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			return kit.Create(c, ui.ResultSpec{
				Action: ui.Created, Kind: "mailboxes", Name: c.Args[0],
			}, func() (string, error) {
				box, err := c.App.Pass.MailboxAdd(c.Ctx, c.Args[0])
				if err != nil {
					return "", err
				}
				if !box.Verified {
					c.Note("Proton has emailed %s a code. Hand it back with "+
						"`proton pass settings mailboxes verify %s --code CODE`.", box.Email, box.Email)
				}
				return strconv.Itoa(box.ID), nil
			})
		}),
	}
}

func mailboxesVerifyCmd() *cobra.Command {
	var code string
	c := &cobra.Command{
		Use:   "verify REF",
		Short: "Confirm an address with the code Proton emailed it",
		Args:  cobra.ExactArgs(1),
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			box, err := c.App.Pass.MailboxByEmail(c.Ctx, c.Args[0])
			if err != nil {
				return err
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Verified, Kind: "mailboxes", Count: 1, Name: box.Email,
				IDs: []string{strconv.Itoa(box.ID)},
			}, func() error {
				return c.App.Pass.MailboxVerify(c.Ctx, box.ID, code)
			})
		}),
	}
	c.Flags().StringVar(&code, "code", "", "The code Proton emailed the address")
	_ = c.MarkFlagRequired("code")
	return c
}

func mailboxesResendCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "resend REF",
		Short: "Send the confirmation code again",
		Args:  cobra.ExactArgs(1),
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			box, err := c.App.Pass.MailboxByEmail(c.Ctx, c.Args[0])
			if err != nil {
				return err
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Resent, Kind: "mailboxes", Count: 1, Name: box.Email,
				IDs: []string{strconv.Itoa(box.ID)},
			}, func() error {
				return c.App.Pass.MailboxResend(c.Ctx, box.ID)
			})
		}),
	}
}

func mailboxesDeleteCmd() *cobra.Command {
	var transfer string
	c := &cobra.Command{
		Use:   "delete REF",
		Short: "Remove an address aliases forward to",
		Long: "Remove an address aliases forward to.\n\n" +
			"--transfer-to names the mailbox that its aliases move to. It is required:\n" +
			"without a new mailbox, those aliases would stop receiving mail.",
		Args: cobra.ExactArgs(1),
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			box, err := c.App.Pass.MailboxByEmail(c.Ctx, c.Args[0])
			if err != nil {
				return err
			}
			to := 0
			detail := ""
			if transfer != "" {
				target, err := c.App.Pass.MailboxByEmail(c.Ctx, transfer)
				if err != nil {
					return err
				}
				if target.ID == box.ID {
					return kit.Fail("%s cannot take over from itself.", box.Email)
				}
				to, detail = target.ID, "- its aliases move to "+target.Email
			} else if box.Aliases > 0 {
				return kit.Fail("%d aliases arrive in %s.", box.Aliases, box.Email).
					Hint("--transfer-to ADDRESS says where they should arrive instead.",
						"`proton pass settings mailboxes list` shows the others.")
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Deleted, Kind: "mailboxes", Count: 1, Name: box.Email,
				Detail: detail, IDs: []string{strconv.Itoa(box.ID)},
			}, func() error {
				return c.App.Pass.MailboxDelete(c.Ctx, box.ID, to)
			})
		}),
	}
	c.Flags().StringVar(&transfer, "transfer-to", "", "Move the aliases arriving here to this mailbox")
	return c
}

func mailboxesUpdateCmd() *cobra.Command {
	var makeDefault bool
	c := &cobra.Command{
		Use:   "update REF",
		Short: "Change a mailbox",
		Args:  cobra.ExactArgs(1),
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			if !makeDefault {
				return kit.Fail("Nothing to change.").Hint("--default makes new aliases arrive here.")
			}
			box, err := c.App.Pass.MailboxByEmail(c.Ctx, c.Args[0])
			if err != nil {
				return err
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Updated, Kind: "mailboxes", Count: 1, Name: box.Email,
				Detail: "- new aliases arrive here", IDs: []string{strconv.Itoa(box.ID)},
			}, func() error {
				return c.App.Pass.MailboxSetDefault(c.Ctx, box.ID)
			})
		}),
	}
	c.Flags().BoolVar(&makeDefault, "default", false, "Make new aliases arrive here")
	return c
}

func mailboxesListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List the addresses your aliases forward to",
		Long: "List the addresses your aliases forward to.\n\n" +
			"An alias is a route, not a mailbox: mail sent to it arrives in one of\n" +
			"these. To point an alias at one, run `proton pass items update REF\n" +
			"--mailbox`.",
		Args: cobra.NoArgs,
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			rows, err := c.App.Pass.Mailboxes(c.Ctx)
			if err != nil {
				return err
			}
			return kit.List(c, ui.TableSpec[passsvc.Mailbox]{
				Noun: "mailboxes", Total: ui.Unknown, Page: ui.Unpaged,
				Columns: []ui.Column[passsvc.Mailbox]{
					{Header: "EMAIL", Flex: true, Cell: func(m passsvc.Mailbox) string { return m.Email }},
					{Header: "VERIFIED", Cell: func(m passsvc.Mailbox) string { return yesNo(m.Verified) }},
					{Header: "DEFAULT", Cell: func(m passsvc.Mailbox) string { return yesNo(m.Default) }},
					{Header: "ALIASES", Right: true, Cell: func(m passsvc.Mailbox) string {
						return strconv.Itoa(m.Aliases)
					}},
					{Header: "PENDING", Cell: func(m passsvc.Mailbox) string { return m.Pending }},
				},
			}, rows)
		}),
	}
}

func domainsCmd() *cobra.Command {
	c := &cobra.Command{Use: "domains", Short: "The domains an alias can be made on"}
	c.AddCommand(domainsListCmd())
	return c
}

func domainsListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List the domains an alias can be made on",
		Long: "List the domains an alias can be made on.\n\n" +
			"These are the values `proton pass aliases create --suffix` accepts: the\n" +
			"part of an alias after the @.",
		Args: cobra.NoArgs,
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			rows, err := c.App.Pass.Domains(c.Ctx)
			if err != nil {
				return err
			}
			return kit.List(c, ui.TableSpec[passsvc.Domain]{
				Noun: "domains", Total: ui.Unknown, Page: ui.Unpaged,
				Columns: []ui.Column[passsvc.Domain]{
					{Header: "DOMAIN", Flex: true, Cell: func(d passsvc.Domain) string { return d.Domain }},
					{Header: "DEFAULT", Cell: func(d passsvc.Domain) string { return yesNo(d.Default) }},
					{Header: "CUSTOM", Cell: func(d passsvc.Domain) string { return yesNo(d.Custom) }},
					{Header: "MX", Cell: func(d passsvc.Domain) string {
						// Only a custom domain has MX records of yours to get wrong.
						if !d.Custom {
							return ""
						}
						return yesNo(d.MXVerified)
					}},
				},
			}, rows)
		}),
	}
}
