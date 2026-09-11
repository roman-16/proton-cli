package account

import (
	"fmt"

	"github.com/roman-16/proton-cli/internal/account/keys"
	"github.com/roman-16/proton-cli/internal/app"
	"github.com/roman-16/proton-cli/internal/cli/kit"
	"github.com/roman-16/proton-cli/internal/ui"
	"github.com/spf13/cobra"
)

// Keys a password reset locked, and bringing them back.
//
// Proton's account settings say it twice - a banner on the Recovery page that
// some encrypted data is locked, and a Reactivate keys button beside the keys -
// and both open one dialog with three ways in: the password from before the
// reset, the recovery phrase, or a recovery file. This is that dialog.

func keysCmd() *cobra.Command {
	c := &cobra.Command{Use: "keys", Short: "The keys your data is encrypted with"}
	c.AddCommand(keysReactivateCmd())
	return c
}

// leftLocked is one key a reactivation did not bring back, phrased for the
// warning that names it.
type leftLocked struct {
	key keys.LockedKey
	why string
}

func (l leftLocked) String() string {
	if l.key.Email != "" {
		return fmt.Sprintf("A key of %s %s.", l.key.Email, l.why)
	}
	return fmt.Sprintf("Key %s %s.", l.key.Key.ID, l.why)
}

// leftOut names every locked key the reactivation did not bring back, and why:
// a user key the secret did not open, an address key whose user key stayed
// shut, or a key held in a form only a Proton client brings back.
func leftOut(locked []keys.LockedKey, out *keys.Outcome) []leftLocked {
	reactivated := map[string]bool{}
	for _, k := range out.Reactivated {
		reactivated[k.Key.ID] = true
	}
	unsupported := map[string]bool{}
	for _, k := range out.Unsupported {
		unsupported[k.Key.ID] = true
	}
	var left []leftLocked
	for _, k := range locked {
		switch {
		case reactivated[k.Key.ID]:
		case unsupported[k.Key.ID]:
			left = append(left, leftLocked{key: k, why: "is held in a form only a Proton client brings back"})
		case k.AddressID == "":
			left = append(left, leftLocked{key: k, why: "did not open and stays locked; a key from an earlier reset opens with the secret from then"})
		default:
			left = append(left, leftLocked{key: k, why: "stays locked with the account key that holds it"})
		}
	}
	return left
}

func lockedColumns() []ui.Column[keys.LockedKey] {
	return []ui.Column[keys.LockedKey]{
		{Header: "ID", ID: true, Cell: func(k keys.LockedKey) string { return k.Key.ID }},
		{Header: "KIND", Cell: func(k keys.LockedKey) string {
			if k.AddressID == "" {
				return "account"
			}
			return "address"
		}},
		{Header: "ADDRESS", Flex: true, Cell: func(k keys.LockedKey) string { return k.Email }},
	}
}

func keysReactivateCmd() *cobra.Command {
	var (
		reauth   kit.Reauth
		recovery kit.Recovery
	)
	c := &cobra.Command{
		Use:   "reactivate",
		Short: "Bring back the keys a password reset locked",
		Long: "Bring back the keys a password reset locked.\n\n" +
			"Give one secret from before the reset - the password, the recovery phrase or\n" +
			"a recovery file - and your current password to confirm. With no flag, the\n" +
			"previous password is asked for; in two-password mode that is the second one.\n" +
			"Keys the secret does not open stay locked and are named. Drive files come back\n" +
			"with `" + kit.Program + " drive volumes restore`.",
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			if err := reauth.Supply(c); err != nil {
				return err
			}
			if err := recovery.Supply(c); err != nil {
				return err
			}
			// A secret that arrived on the command line is judged before anything
			// is asked of Proton. A typed one is asked for once the change is going
			// ahead, and not before: a prompt for a secret this command is about to
			// refuse is a prompt nobody should have answered.
			var secret keys.Secret
			if recovery.Offered() {
				s, err := recovery.Secret(c)
				if err != nil {
					return err
				}
				secret = s
			}
			u, err := c.App.Unlock(c.Ctx)
			if err != nil {
				return err
			}
			locked := u.Locked()
			if len(locked) == 0 {
				return kit.Fail("No keys are locked.")
			}
			if len(u.LockedAccountKeys()) == 0 {
				return kit.Fail("The locked keys are address keys, which come back with the account key that holds them.").
					Hint("reactivate them at https://account.proton.me")
			}
			if recovery.UsesPhrase() && !u.HasRecoveryPhrase() {
				return kit.Fail("This account has no recovery phrase to recover with.").
					Hint("use the password from before the reset, or a recovery file")
			}
			ids := make([]string, 0, len(locked))
			for _, k := range locked {
				ids = append(ids, k.Key.ID)
			}
			ctx := app.WithScopeReason(c.Ctx, "reactivate your keys")
			return kit.Attempt(c, ui.ResultSpec{
				Action: ui.Reactivated, Kind: "keys", Count: len(locked), IDs: ids,
				Preview: kit.Preview("keys", lockedColumns(), locked),
			}, func() ([]leftLocked, error) {
				if secret == nil {
					s, err := recovery.Secret(c)
					if err != nil {
						return nil, err
					}
					secret = s
				}
				out, err := u.Reactivate(ctx, c.App.API, secret)
				if err != nil {
					return nil, err
				}
				return leftOut(locked, out), nil
			})
		}),
	}
	reauth.Declare(c)
	recovery.Declare(c)
	return c
}
