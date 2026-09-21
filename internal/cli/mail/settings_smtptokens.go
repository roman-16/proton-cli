package mail

import (
	"context"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/roman-16/proton-cli/internal/app"
	"github.com/roman-16/proton-cli/internal/cli/kit"
	mailsvc "github.com/roman-16/proton-cli/internal/service/mail"
	"github.com/roman-16/proton-cli/internal/ui"
	"github.com/roman-16/proton-cli/internal/units"
)

// A token is what a device sends mail with. This mirrors Proton's "SMTP
// submission" on the IMAP/SMTP settings page: one token per device, sending
// from one custom domain address, shown once.

func smtpTokensCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "smtp-tokens",
		Short: "Tokens that let a device send from your address",
		Long: "Tokens that let a device or a service send mail from your address.\n\n" +
			"A token sends from one custom domain address and nothing else. The address\n" +
			"is the SMTP username and the token is the password, at smtp.protonmail.ch\n" +
			"on port 587 with TLS.\n\n" +
			"The token is shown once, when it is made.",
	}
	c.AddCommand(smtpTokensListCmd(), smtpTokensGetCmd(), smtpTokensCreateCmd(), smtpTokensDeleteCmd())
	return c
}

func smtpTokenColumns() []ui.Column[mailsvc.SMTPToken] {
	return []ui.Column[mailsvc.SMTPToken]{
		{Header: "ID", ID: true, Cell: func(t mailsvc.SMTPToken) string { return t.ID }},
		{Header: "NAME", Flex: true, Handle: true, Cell: func(t mailsvc.SMTPToken) string { return t.Name }},
		{Header: "ADDRESS", Flex: true, Cell: func(t mailsvc.SMTPToken) string { return t.Address }},
		{Header: "CREATED", Cell: func(t mailsvc.SMTPToken) string { return units.Time(t.Created) }},
		{Header: "LAST USED", Cell: func(t mailsvc.SMTPToken) string { return units.Time(t.LastUsed) }},
	}
}

func smtpTokensListCmd() *cobra.Command {
	var held kit.Held[mailsvc.SMTPToken]
	c := &cobra.Command{
		Use:   "list",
		Short: "List the tokens on the account",
		Long: "List the tokens on the account, newest first.\n\n" +
			"LAST USED is when something last sent with a token, and - where nothing\n" +
			"ever has. The tokens themselves are never shown here.",
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			rows, err := c.App.Mail.SMTPTokens(c.Ctx)
			if err != nil {
				return err
			}
			return held.Answer(c, ui.TableSpec[mailsvc.SMTPToken]{
				Noun: "smtp tokens", Columns: smtpTokenColumns(),
			}, rows)
		}),
	}
	held.Register(c, "smtp tokens",
		kit.Key[mailsvc.SMTPToken]{Name: "created", Less: func(a, b mailsvc.SMTPToken) int {
			return kit.Ints(b.Created, a.Created)
		}},
		kit.Key[mailsvc.SMTPToken]{Name: "name", Less: func(a, b mailsvc.SMTPToken) int {
			return kit.Fold(a.Name, b.Name)
		}},
		kit.Key[mailsvc.SMTPToken]{Name: "address", Less: func(a, b mailsvc.SMTPToken) int {
			return kit.Fold(a.Address, b.Address)
		}},
		kit.Key[mailsvc.SMTPToken]{Name: "last-used", Less: func(a, b mailsvc.SMTPToken) int {
			return kit.Ints(b.LastUsed, a.LastUsed)
		}},
	)
	return c
}

// smtpTarget is a token beside where the device using it connects. Neither the
// server nor the port is a fact Proton keeps against a token, and both are what
// somebody types in beside the token itself.
type smtpTarget struct {
	mailsvc.SMTPToken
	Server string `json:"server"`
	Port   int    `json:"port"`
}

func pointedAt(t mailsvc.SMTPToken) smtpTarget {
	return smtpTarget{SMTPToken: t, Server: mailsvc.SMTPServer, Port: mailsvc.SMTPPort}
}

func smtpTokensGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get REF",
		Short: "Show a token and where the device it is for connects",
		Long: "Show a token and where the device it is for connects.\n\n" +
			"The token itself is not here: it was shown once, when it was made. One you\n" +
			"have lost is deleted and made again.",
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			token, err := smtpTokenList(c).Find(c.Ctx, c.Args[0])
			if err != nil {
				return err
			}
			return kit.Show(c, ui.RecordSpec{
				Object: pointedAt(token),
				Fields: []ui.Field{
					{Label: "Name", Value: token.Name, Handle: true},
					{Label: "Address", Value: token.Address},
					{Label: "Server", Value: mailsvc.SMTPServer},
					{Label: "Port", Value: strconv.Itoa(mailsvc.SMTPPort)},
					{Label: "Created", Value: units.Time(token.Created)},
					{Label: "Last Used", Value: units.Time(token.LastUsed), Always: true},
					{Label: "ID", Value: token.ID, ID: true},
				},
			})
		}),
	}
}

func smtpTokensCreateCmd() *cobra.Command {
	var name string
	var reauth kit.Reauth
	c := &cobra.Command{
		Use:   "create REF",
		Short: "Make a token for a device to send with",
		Long: "Make a token for a device or a service to send with.\n\n" +
			"REF is one of your custom domain addresses, and --name is required. The\n" +
			"token sends from that address and nothing else.\n\n" +
			"The token is shown once and never again. Under --output json it is the\n" +
			"`token` field.\n\n" +
			"Asks for your password even when you are signed in. With no terminal to ask,\n" +
			"pass --password-file, which takes - for stdin.",
		Annotations: map[string]string{kit.Addresses: "mail settings addresses"},
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			if err := reauth.Supply(c); err != nil {
				return err
			}
			name = strings.TrimSpace(name)
			if name == "" {
				return kit.Fail("A token needs a name.").Hint("--name 'Office printer'")
			}
			address, err := c.App.Mail.ResolveAddress(c.Ctx, c.Args[0])
			if err != nil {
				return err
			}
			if err := sendsWithAToken(*address); err != nil {
				return err
			}
			var token mailsvc.SMTPToken
			if err := kit.Mutate(c, ui.ResultSpec{
				Action: ui.Created, Kind: "smtp tokens", Count: 1, Name: name,
				Detail: "sending from " + address.Email, AnswerFollows: true,
			}, func() error {
				// Nothing here arranges the elevation: the client does it when the
				// server asks, and drops the scope again afterwards. All this owes
				// the user is a reason for the prompt.
				ctx := app.WithScopeReason(c.Ctx, "make an SMTP token")
				token, err = c.App.Mail.SMTPTokenCreate(ctx, *address, name)
				return err
			}); err != nil {
				return err
			}
			if c.App.DryRun {
				return nil
			}
			// The token is the answer, so it goes to stdout: the point of this
			// command is to be able to capture it. The warning goes to stderr,
			// where it does not end up in whatever captured the token.
			c.Warn("Copy the token now. It is shown once and never again.")
			return kit.Show(c, ui.RecordSpec{
				Object: pointedAt(token),
				Fields: []ui.Field{
					{Label: "Token", Value: token.Secret},
					{Label: "Address", Value: token.Address},
					{Label: "Server", Value: mailsvc.SMTPServer},
					{Label: "Port", Value: strconv.Itoa(mailsvc.SMTPPort)},
					{Label: "Name", Value: token.Name, Handle: true},
				},
			})
		}),
	}
	c.Flags().StringVar(&name, "name", "", "Name for the new token")
	reauth.Declare(c)
	return c
}

// sendsWithAToken admits an address a token can be made for, and tells an
// address that cannot hold one what is wrong with it.
func sendsWithAToken(a mailsvc.Address) error {
	switch {
	case a.Type != mailsvc.TypeCustomDomain:
		return kit.Fail("A token sends from a custom domain address, and %s is not one.", a.Email).
			Hint(kit.Program + " mail settings domains list shows the domains on this account")
	case a.Status != mailsvc.StatusEnabled:
		return kit.Fail("%s is turned off, so nothing can send from it.", a.Email).
			Hint(kit.Program + " mail settings addresses enable " + a.Email)
	}
	return nil
}

func smtpTokensDeleteCmd() *cobra.Command {
	var reauth kit.Reauth
	c := &cobra.Command{
		Use:   "delete REF...",
		Short: "Stop a token working",
		Long: "Stop a token working, for good.\n\n" +
			"The device or the service holding it cannot send from then on. Nothing\n" +
			"brings a token back; make another and set the device up again.\n\n" +
			"Asks for your password even when you are signed in. With no terminal to ask,\n" +
			"pass --password-file, which takes - for stdin.",
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			if err := reauth.Supply(c); err != nil {
				return err
			}
			sel, err := kit.SelectFrom(c, "smtp tokens", smtpTokenColumns(), smtpTokenList(c))
			if err != nil {
				return err
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Deleted, Kind: "smtp tokens", Count: sel.Len(), IDs: sel.IDs,
				Name:    kit.Sole(sel.Rows, func(t mailsvc.SMTPToken) string { return t.Name }),
				Preview: sel.Preview(),
			}, func() error {
				// Nothing here arranges the elevation: the client does it when the
				// server asks, and drops the scope again afterwards. All this owes
				// the user is a reason for the prompt.
				ctx := app.WithScopeReason(c.Ctx, "delete an SMTP token")
				for _, id := range sel.IDs {
					if err := c.App.Mail.SMTPTokenDelete(ctx, id); err != nil {
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

func smtpTokenList(c *kit.Invocation) *kit.Lookup[mailsvc.SMTPToken] {
	return &kit.Lookup[mailsvc.SMTPToken]{
		Kind: "smtp token",
		Load: func(ctx context.Context) ([]mailsvc.SMTPToken, error) {
			return c.App.Mail.SMTPTokens(ctx)
		},
		ID:     func(t mailsvc.SMTPToken) string { return t.ID },
		Handle: func(t mailsvc.SMTPToken) string { return t.Name },
	}
}
