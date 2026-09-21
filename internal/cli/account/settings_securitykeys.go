package account

import (
	"context"
	"strings"

	"github.com/roman-16/proton-cli/internal/app"
	"github.com/roman-16/proton-cli/internal/cli/kit"
	acctsvc "github.com/roman-16/proton-cli/internal/service/account"
	"github.com/roman-16/proton-cli/internal/ui"
	"github.com/spf13/cobra"
)

// The security keys the account signs in with, which is Proton's own list
// beside the authenticator app.
//
// Registering one takes a person: the key has to be plugged in and touched, and
// most keys with a PIN ask for it as well. So `create` is the one command here
// that a job cannot run, however much of the password it is handed.

// What Proton allows an account. Both are judged before anything is sent: a
// fifth key is refused rather than registered and then rejected, and a name too
// long is refused before somebody has touched anything.
const (
	maxSecurityKeys    = 4
	maxSecurityKeyName = 128
)

func securityKeysCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "security-keys",
		Short: "The security keys this account signs in with",
		Long: "The security keys this account signs in with.\n\n" +
			"A key is one you plug in. Registering it asks you to touch it, so it takes\n" +
			"somebody at the machine; a built-in authenticator and a passkey held in a\n" +
			"phone are registered at https://account.proton.me instead.\n\n" +
			"An account may have four. Registering the first one makes a key part of\n" +
			"every sign-in, and removing the last one ends that.",
	}
	c.AddCommand(securityKeysListCmd(), securityKeysCreateCmd(),
		securityKeysUpdateCmd(), securityKeysDeleteCmd())
	return c
}

func securityKeysListCmd() *cobra.Command {
	var held kit.Held[acctsvc.SecurityKey]
	c := &cobra.Command{
		Use:   "list",
		Short: "List the security keys registered with the account",
		Long: "List the security keys registered with the account.\n\n" +
			"The ID is what `update` and `delete` take, and a key's name works in place\n" +
			"of it.",
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			rows, err := securityKeyList(c).Rows(c.Ctx)
			if err != nil {
				return err
			}
			return held.Answer(c, ui.TableSpec[acctsvc.SecurityKey]{
				Noun:    "security keys",
				Columns: securityKeyColumns(),
			}, rows)
		}),
	}
	held.Register(c, "security keys",
		kit.Key[acctsvc.SecurityKey]{Name: "name", Less: func(a, b acctsvc.SecurityKey) int {
			return kit.Fold(a.Name, b.Name)
		}},
	)
	return c
}

func securityKeysCreateCmd() *cobra.Command {
	var name string
	var reauth kit.Reauth
	c := &cobra.Command{
		Use:   "create",
		Short: "Register a security key with the account",
		Long: "Register a security key with the account.\n\n" +
			"Your password is asked for, then the key: plug it in and touch it, and\n" +
			"give its PIN if it has one. A key already registered here is refused by the\n" +
			"key itself.\n\n" +
			"--name is required and is what the key is listed under. An account may have\n" +
			"four keys, and from the first one a key is asked for at every sign-in.\n\n" +
			"With no terminal to ask, pass --password-file, which takes - for stdin.\n" +
			"The touch still needs you there.",
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			if err := reauth.Supply(c); err != nil {
				return err
			}
			chosen, err := securityKeyName(name)
			if err != nil {
				return err
			}
			security, err := c.App.Account.Security(c.Ctx)
			if err != nil {
				return err
			}
			if len(security.TwoFactor.SecurityKeys) >= maxSecurityKeys {
				return kit.Fail("This account already has %s, which is Proton's limit.",
					kit.Quantity(len(security.TwoFactor.SecurityKeys), "security keys")).
					Hint(kit.Program + " account settings security-keys delete REF frees one")
			}
			if !security.TwoFactor.AuthenticatorApp {
				c.Note("Some Proton apps cannot sign in with a security key yet. " +
					"Keep an authenticator app on as well: " +
					kit.Program + " account settings two-factor enable")
			}
			return kit.Create(c, ui.ResultSpec{
				Action: ui.Created, Kind: "security keys", Name: chosen,
				Detail: "- a key is asked for at every sign-in",
			}, func() (string, error) {
				// The password is proved before the key is asked for anything: a
				// touch cannot be taken back, and nobody should spend one to be
				// told afterwards that they mistyped their password.
				relock, err := c.App.Elevate(c.Ctx, "register a security key")
				if err != nil {
					return "", err
				}
				defer relock()
				challenge, err := c.App.Account.SecurityKeyChallenge(c.Ctx)
				if err != nil {
					return "", err
				}
				credential, err := c.App.AttestSecurityKey(c.Ctx, challenge)
				if err != nil {
					return "", err
				}
				credential.Name = chosen
				if err := c.App.Account.SecurityKeyRegister(c.Ctx, challenge, credential); err != nil {
					return "", err
				}
				return credential.ID, nil
			})
		}),
	}
	c.Flags().StringVar(&name, "name", "", "Name for the new security key")
	reauth.Declare(c)
	return c
}

func securityKeysUpdateCmd() *cobra.Command {
	var name string
	c := &cobra.Command{
		Use:   "update REF",
		Short: "Rename a security key",
		Long: "Rename a security key.\n\n" +
			"The name is how you tell your keys apart and is all that changes: the key\n" +
			"signs in exactly as it did.",
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			if !c.Changed("name") {
				return kit.Fail("Nothing to change.").
					Hint(`--name "Key in the safe"`)
			}
			chosen, err := securityKeyName(name)
			if err != nil {
				return err
			}
			key, err := securityKeyList(c).Find(c.Ctx, c.Args[0])
			if err != nil {
				return err
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Updated, Kind: "security keys", Count: 1,
				Name: chosen, IDs: []string{key.ID},
			}, func() error {
				return c.App.Account.SecurityKeyRename(c.Ctx, key.ID, chosen)
			})
		}),
	}
	c.Flags().StringVar(&name, "name", "", "New name for the security key")
	return c
}

func securityKeysDeleteCmd() *cobra.Command {
	var reauth kit.Reauth
	c := &cobra.Command{
		Use:   "delete REF",
		Short: "Remove a security key from the account",
		Long: "Remove a security key from the account.\n\n" +
			"The key stops signing in here and keeps the credential it made, which only\n" +
			"its own manufacturer's tool clears. Removing the last one stops a key being\n" +
			"asked for at all.\n\n" +
			"Asks for your password even when you are signed in. With no terminal to ask,\n" +
			"pass --password-file, which takes - for stdin.",
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			if err := reauth.Supply(c); err != nil {
				return err
			}
			list := securityKeyList(c)
			key, err := list.Find(c.Ctx, c.Args[0])
			if err != nil {
				return err
			}
			rows, err := list.Rows(c.Ctx)
			if err != nil {
				return err
			}
			detail := ""
			if len(rows) == 1 {
				detail = "- no security key is asked for at sign-in any more"
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Deleted, Kind: "security keys", Count: 1,
				Name: key.Name, IDs: []string{key.ID}, Detail: detail,
			}, func() error {
				// Nothing here arranges the elevation: the client does it when the
				// server asks, and drops the scope again afterwards. All this owes
				// the user is a reason for the prompt.
				ctx := app.WithScopeReason(c.Ctx, "remove a security key")
				return c.App.Account.SecurityKeyRemove(ctx, key.ID)
			})
		}),
	}
	reauth.Declare(c)
	return c
}

// securityKeyName reads what the key is to be called. Both refusals are judged
// from the command line, so neither costs a request or a touch.
func securityKeyName(flag string) (string, error) {
	name := strings.TrimSpace(flag)
	switch {
	case name == "":
		return "", kit.Fail("A security key needs a name.").
			Hint(`--name "YubiKey 5C"`)
	case len(name) > maxSecurityKeyName:
		return "", kit.Fail("A security key's name can be %d characters at most.", maxSecurityKeyName)
	}
	return name, nil
}

func securityKeyColumns() []ui.Column[acctsvc.SecurityKey] {
	return []ui.Column[acctsvc.SecurityKey]{
		{Header: "ID", ID: true, Cell: func(k acctsvc.SecurityKey) string { return k.ID }},
		{Header: "NAME", Flex: true, Handle: true, Cell: func(k acctsvc.SecurityKey) string { return k.Name }},
	}
}

func securityKeyList(c *kit.Invocation) *kit.Lookup[acctsvc.SecurityKey] {
	return &kit.Lookup[acctsvc.SecurityKey]{
		Kind: "security key",
		Load: func(ctx context.Context) ([]acctsvc.SecurityKey, error) {
			security, err := c.App.Account.Security(ctx)
			if err != nil {
				return nil, err
			}
			return security.TwoFactor.SecurityKeys, nil
		},
		ID:     func(k acctsvc.SecurityKey) string { return k.ID },
		Handle: func(k acctsvc.SecurityKey) string { return k.Name },
	}
}
