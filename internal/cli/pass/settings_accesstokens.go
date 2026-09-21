package pass

import (
	"context"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/roman-16/proton-cli/internal/cli/kit"
	"github.com/roman-16/proton-cli/internal/errs"
	passsvc "github.com/roman-16/proton-cli/internal/service/pass"
	"github.com/roman-16/proton-cli/internal/ui"
	"github.com/roman-16/proton-cli/internal/units"
)

// A token that lets a program into Pass without the account's password. This
// mirrors Pass's "Access tokens" settings page: what a token may read, how long
// it works, and - for one an AI agent holds - what it did with it.

func accessTokensCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "access-tokens",
		Short: "Tokens that let a program into Pass",
		Long: "Tokens that let a program into Pass without your password.\n\n" +
			"A token is for Proton's pass-cli, or for an AI agent working through it.\n" +
			"It reads the vaults you hand it and nothing else, and it stops working when\n" +
			"it expires. Making one needs a paid Pass plan.\n\n" +
			"The token itself is shown once, when it is made.",
	}
	c.AddCommand(accessTokensListCmd(), accessTokensGetCmd(), accessTokensCreateCmd(),
		accessTokensUpdateCmd(), accessTokensDeleteCmd(), accessTokenActivityCmd())
	return c
}

func accessTokenColumns() []ui.Column[passsvc.AccessToken] {
	return []ui.Column[passsvc.AccessToken]{
		{Header: "ID", ID: true, Cell: func(t passsvc.AccessToken) string { return t.ID }},
		{Header: "NAME", Flex: true, Handle: true, Cell: func(t passsvc.AccessToken) string { return t.Name }},
		{Header: "STATUS", Cell: func(t passsvc.AccessToken) string { return t.Status }},
		{Header: "AGENT", Cell: func(t passsvc.AccessToken) string { return yesNo(t.Agent) }},
		{Header: "EXPIRES", Cell: func(t passsvc.AccessToken) string { return units.Time(t.Expires) }},
		{Header: "CREATED", Cell: func(t passsvc.AccessToken) string { return units.Time(t.Created) }},
	}
}

func accessTokensListCmd() *cobra.Command {
	var held kit.Held[passsvc.AccessToken]
	c := &cobra.Command{
		Use:   "list",
		Short: "List the tokens, expired ones included",
		Long: "List the tokens, expired ones included.\n\n" +
			"STATUS is active, expiring or expired; expiring means within the hour. A\n" +
			"token that expired more than thirty days ago is gone from the list. The\n" +
			"tokens themselves are never shown here.",
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			rows, err := c.App.Pass.AccessTokens(c.Ctx)
			if err != nil {
				return err
			}
			return held.Answer(c, ui.TableSpec[passsvc.AccessToken]{
				Noun: "access tokens", Columns: accessTokenColumns(),
			}, rows)
		}),
	}
	held.Register(c, "access tokens",
		kit.Key[passsvc.AccessToken]{Name: "created", Less: func(a, b passsvc.AccessToken) int {
			return kit.Ints(b.Created, a.Created)
		}},
		kit.Key[passsvc.AccessToken]{Name: "name", Less: func(a, b passsvc.AccessToken) int { return kit.Fold(a.Name, b.Name) }},
		kit.Key[passsvc.AccessToken]{Name: "expires", Less: func(a, b passsvc.AccessToken) int {
			return kit.Ints(a.Expires, b.Expires)
		}},
	)
	return c
}

// tokenWithVaults is a token beside the vaults it reads, which is what `get`
// answers with.
type tokenWithVaults struct {
	passsvc.AccessToken
	Vaults []passsvc.TokenGrant `json:"vaults"`
}

func accessTokensGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get REF",
		Short: "Show a token and the vaults it reads",
		Long: "Show a token and the vaults it reads.\n\n" +
			"The token itself is not here: it was shown once, when it was made, and\n" +
			"nothing brings it back. A token you have lost is deleted and made again.",
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			token, err := accessTokenList(c).Find(c.Ctx, c.Args[0])
			if err != nil {
				return err
			}
			grants, err := c.App.Pass.AccessTokenGrants(c.Ctx, token.ID)
			if err != nil {
				return err
			}
			return kit.Show(c, ui.RecordSpec{
				Object: tokenWithVaults{AccessToken: token, Vaults: grants},
				Fields: []ui.Field{
					{Label: "Name", Value: token.Name, Handle: true},
					{Label: "Status", Value: token.Status},
					{Label: "Agent", Value: yesNo(token.Agent)},
					{Label: "Created", Value: units.Time(token.Created)},
					{Label: "Expires", Value: units.Time(token.Expires)},
					{Label: "Vaults", Value: vaultNames(grants), Always: true},
					{Label: "ID", Value: token.ID, ID: true},
				},
			})
		}),
	}
}

// vaultNames is the vaults a token reads, as a person knows them, or the fact
// that it reads none.
func vaultNames(grants []passsvc.TokenGrant) string {
	if len(grants) == 0 {
		return "(none)"
	}
	names := make([]string, 0, len(grants))
	for _, g := range grants {
		if g.Vault == "" {
			names = append(names, g.ShareID)
			continue
		}
		names = append(names, g.Vault)
	}
	return strings.Join(names, ", ")
}

func accessTokensCreateCmd() *cobra.Command {
	var name, expires string
	var vaults []string
	var agent bool
	c := &cobra.Command{
		Use:   "create",
		Short: "Make a token for a program to use",
		Long: "Make a token for a program to use.\n\n" +
			"--name, --expires and at least one --vault are required. The token reads\n" +
			"the vaults named and nothing else, and stops working when --expires runs\n" +
			"out, which is between 1h and 1y from now.\n\n" +
			"--agent marks it for an AI agent, which then has to give a reason for every\n" +
			"action it takes; `activity list` shows them.\n\n" +
			"The token is shown once and never again. Under --output json it is the\n" +
			"`secret` field.",
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			if strings.TrimSpace(name) == "" {
				return kit.Fail("A token needs a name.").Hint("--name ci")
			}
			life, err := tokenLife(expires)
			if err != nil {
				return err
			}
			if len(vaults) == 0 {
				return kit.Fail("A token needs at least one vault to read.").Hint("--vault Work")
			}
			shareIDs, names, err := vaultsNamed(c, vaults)
			if err != nil {
				return err
			}
			var token *passsvc.AccessToken
			if err := kit.Mutate(c, ui.ResultSpec{
				Action: ui.Created, Kind: "access tokens", Count: 1, Name: strings.TrimSpace(name),
				Detail:        "lasting " + units.Duration(life) + ", reading " + strings.Join(names, ", "),
				AnswerFollows: true,
			}, func() error {
				token, err = c.App.Pass.AccessTokenCreate(c.Ctx, passsvc.NewAccessToken{
					Name: strings.TrimSpace(name), Life: life, Agent: agent, ShareIDs: shareIDs,
				})
				return err
			}); err != nil {
				return err
			}
			if token == nil || c.App.DryRun {
				return nil
			}
			// The token is the answer, so it goes to stdout: the point of this
			// command is to be able to capture it. The warning goes to stderr,
			// where it does not end up in whatever captured the token.
			c.Warn("Copy the token now. It is shown once and never again.")
			return kit.Show(c, ui.RecordSpec{
				Object: token,
				Fields: []ui.Field{
					{Label: "Token", Value: token.Secret},
					{Label: "Name", Value: token.Name, Handle: true},
					{Label: "Expires", Value: units.Time(token.Expires)},
					{Label: "Vaults", Value: strings.Join(names, ", ")},
					{Label: "ID", Value: token.ID, ID: true},
				},
			})
		}),
	}
	c.Flags().StringVar(&name, "name", "", "Name for the new token")
	c.Flags().StringVar(&expires, "expires", "", "How long the token works (e.g. 30d, 12h); 1h to 1y")
	c.Flags().StringArrayVar(&vaults, "vault", nil, "A vault the token may read, by name or ID (repeatable)")
	c.Flags().BoolVar(&agent, "agent", false, "Make it for an AI agent, which has to give a reason for every action")
	return c
}

// tokenLife reads --expires as a token's life, which has a floor and a ceiling
// and no way of saying never.
func tokenLife(expires string) (time.Duration, error) {
	if expires == "" {
		return 0, kit.Fail("How long should the token work?").Hint("--expires 30d", "--expires 1y")
	}
	if strings.EqualFold(strings.TrimSpace(expires), kit.Never) {
		return 0, kit.Fail("An access token always expires.").Hint("--expires 1y is the longest it can work")
	}
	d, err := units.ParseDuration(expires)
	if err != nil {
		return 0, kit.Fail("--expires: %v", err)
	}
	if d < passsvc.TokenMinLife || d > passsvc.TokenMaxLife {
		return 0, kit.Fail("--expires is between 1h and 1y.")
	}
	return d, nil
}

// vaultsNamed resolves every --vault to the share behind it, and to the name
// the confirmation reads.
func vaultsNamed(c *kit.Invocation, refs []string) (shareIDs, names []string, err error) {
	all, err := c.App.Pass.VaultsList(c.Ctx)
	if err != nil {
		return nil, nil, err
	}
	seen := make(map[string]bool, len(refs))
	for _, ref := range refs {
		shareID, err := resolveVault(c, ref)
		if err != nil {
			return nil, nil, err
		}
		if seen[shareID] {
			continue
		}
		seen[shareID] = true
		shareIDs = append(shareIDs, shareID)
		name := shareID
		for _, v := range all {
			if v.ShareID == shareID && v.Name != "" {
				name = v.Name
			}
		}
		names = append(names, name)
	}
	return shareIDs, names, nil
}

func accessTokensUpdateCmd() *cobra.Command {
	var vaults []string
	c := &cobra.Command{
		Use:   "update REF",
		Short: "Change which vaults a token reads",
		Long: "Change which vaults a token reads.\n\n" +
			"--vault names the whole set, one flag per vault: a vault not named is taken\n" +
			"away, and one the token did not have is handed to it. An expired token\n" +
			"cannot be changed; delete it and make another.",
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			if len(vaults) == 0 {
				return kit.Fail("Nothing to change.").Hint("--vault Work --vault Personal")
			}
			token, err := accessTokenList(c).Find(c.Ctx, c.Args[0])
			if err != nil {
				return err
			}
			if token.Status == passsvc.TokenExpired {
				return errs.Naming(token.Name, kit.Fail("%s expired on %s, and cannot be changed.",
					token.Name, units.Time(token.Expires)).
					Hint(kit.Program+" pass settings access-tokens create makes another"))
			}
			shareIDs, names, err := vaultsNamed(c, vaults)
			if err != nil {
				return err
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Updated, Kind: "access tokens", Count: 1, Name: token.Name,
				IDs: []string{token.ID}, Detail: "- reading " + strings.Join(names, ", "),
			}, func() error {
				_, _, err := c.App.Pass.AccessTokenSetVaults(c.Ctx, token, shareIDs)
				return err
			})
		}),
	}
	c.Flags().StringArrayVar(&vaults, "vault", nil, "A vault the token may read, by name or ID (repeatable)")
	return c
}

func accessTokensDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete REF...",
		Short: "Stop a token working",
		Long: "Stop a token working, for good.\n\n" +
			"A program holding the token is refused from then on. Deleting one that\n" +
			"has already expired only takes it off the list.",
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			sel, err := kit.SelectFrom(c, "access tokens", accessTokenColumns(), accessTokenList(c))
			if err != nil {
				return err
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Deleted, Kind: "access tokens", Count: sel.Len(), IDs: sel.IDs,
			}, func() error {
				for _, id := range sel.IDs {
					if err := c.App.Pass.AccessTokenDelete(c.Ctx, id); err != nil {
						return err
					}
				}
				return nil
			})
		}),
	}
}

func accessTokenActivityCmd() *cobra.Command {
	c := &cobra.Command{Use: "activity", Short: "What a token has been used for"}
	c.AddCommand(accessTokenActivityListCmd())
	return c
}

func accessTokenActivityListCmd() *cobra.Command {
	var held kit.Held[passsvc.TokenAction]
	c := &cobra.Command{
		Use:   "list REF",
		Short: "List what a token has done",
		Long: "List what a token has done, newest first.\n\n" +
			"Each action is Proton's record of it. The item, vault and reason are what\n" +
			"the program wrote down, which a token made with `access-tokens create\n" +
			"--agent` has to do for every action; one made without has actions and no\n" +
			"reasons.",
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			token, err := accessTokenList(c).Find(c.Ctx, c.Args[0])
			if err != nil {
				return err
			}
			rows, err := c.App.Pass.AccessTokenActivity(c.Ctx, token)
			if err != nil {
				return err
			}
			return held.Answer(c, ui.TableSpec[passsvc.TokenAction]{
				Noun: "actions",
				Columns: []ui.Column[passsvc.TokenAction]{
					{Header: "TIME", Cell: func(a passsvc.TokenAction) string { return units.Time(a.Time) }},
					{Header: "ACTION", Cell: func(a passsvc.TokenAction) string { return a.Action }},
					{Header: "ITEM", Flex: true, Cell: func(a passsvc.TokenAction) string { return a.Item }},
					{Header: "VAULT", Cell: func(a passsvc.TokenAction) string { return a.Vault }},
					{Header: "REASON", Flex: true, Cell: func(a passsvc.TokenAction) string {
						if a.Sealed {
							return "(could not be decrypted)"
						}
						return a.Reason
					}},
				},
			}, rows)
		}),
	}
	held.Register(c, "actions")
	return c
}

func accessTokenList(c *kit.Invocation) *kit.Lookup[passsvc.AccessToken] {
	return &kit.Lookup[passsvc.AccessToken]{
		Kind: "access token",
		Load: func(ctx context.Context) ([]passsvc.AccessToken, error) {
			return c.App.Pass.AccessTokens(ctx)
		},
		ID:     func(t passsvc.AccessToken) string { return t.ID },
		Handle: func(t passsvc.AccessToken) string { return t.Name },
	}
}
