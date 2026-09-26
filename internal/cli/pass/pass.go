// Package pass is the `proton pass` tree: vaults, the items in them, aliases,
// and the trash.
//
// An item lives inside a share, so it takes two IDs to address. They are written
// as one slash-separated token, keeping every command to a single REF - and a name
// or URL still works, which is how anyone actually reaches an item.
package pass

import (
	"errors"
	"log/slog"

	"github.com/roman-16/proton-cli/internal/cli/kit"
	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/roman-16/proton-cli/internal/secret"
	passsvc "github.com/roman-16/proton-cli/internal/service/pass"
	"github.com/roman-16/proton-cli/internal/ui"
	"github.com/spf13/cobra"
)

func New() *cobra.Command {
	c := &cobra.Command{
		Use:   "pass",
		Short: "Vaults, logins and secrets",
	}
	c.AddCommand(itemsCmd(), vaultsCmd(), aliasesCmd(), trashCmd(), generateCmd(), breachesCmd(), linksCmd(),
		invitationsCmd(), sharedCmd(), sharingCmd(), settingsCmd(),
		exportCmd(), importCmd())
	return c
}

// itemRef is the single token that addresses an item.
func itemRef(it passsvc.Item) string { return kit.JoinPair(it.ShareID, it.ItemID) }

// resolveItem turns a reference into the share and item IDs the service needs.
//
// A name is looked for in the vaults that are not hidden, so a name that finds
// nothing says so when there are hidden ones it did not look in.
func resolveItem(c *kit.Invocation, ref string) (shareID, itemID string, err error) {
	if first, second, err := kit.ExpandPair(c.App, ref); err != nil || first != "" {
		return first, second, err
	}
	shareID, itemID, err = c.App.Pass.ResolveItem(c.Ctx, []string{ref})
	var missing *errs.NotFound
	if !errors.As(err, &missing) {
		return shareID, itemID, err
	}
	vaults, listErr := c.App.Pass.VaultsList(c.Ctx)
	if listErr != nil {
		// Recorded and not counted. The refusal is on the screen either way; this
		// only costs it the hint about hidden vaults.
		slog.DebugContext(c.Ctx, "pass: whether hidden vaults were left out of a lookup is unknown",
			"error", listErr)
		return "", "", err
	}
	for _, v := range vaults {
		if v.Hidden {
			missing.Try = append(missing.Try, "hidden vaults are not searched: "+kit.Program+" pass vaults list")
			break
		}
	}
	return "", "", err
}

// resolveVault accepts a vault name or ID, defaulting to the first vault that is
// not hidden.
func resolveVault(c *kit.Invocation, ref string) (string, error) {
	expanded, err := kit.Expand(c.App, ref)
	if err != nil {
		return "", err
	}
	return c.App.Pass.ResolveVault(c.Ctx, expanded)
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

// ── making a password ──

// generator is how a password is shaped, wherever one is made: on its own with
// `pass generate`, or into an item with --generate-password. One declaration, so
// the two cannot come to mean different things.
type generator struct {
	o                         secret.Options
	noDigits, noSymbols, noUp bool
	separator                 *kit.Enum
}

func (g *generator) register(c *cobra.Command) {
	f := c.Flags()
	f.IntVar(&g.o.Length, "length", secret.DefaultLength, "How many characters")
	f.IntVar(&g.o.Words, "words", 0, "Make a passphrase of this many words instead")
	f.BoolVar(&g.noDigits, "no-digits", false, "Leave the digits out")
	f.BoolVar(&g.noSymbols, "no-symbols", false, "Leave the symbols out")
	f.BoolVar(&g.noUp, "no-uppercase", false, "Leave the capitals out")
	g.separator = &kit.Enum{
		Name: "separator", Usage: "What stands between the words of a passphrase",
		Values: secret.SeparatorNames(), Default: secret.DefaultSeparator,
	}
	g.separator.Register(c)
}

func (g *generator) make() (string, error) {
	separator, err := g.separator.Value()
	if err != nil {
		return "", err
	}
	o := g.o
	o.Separator = separator
	o.Digits, o.Symbols, o.Upper = !g.noDigits, !g.noSymbols, !g.noUp
	pw, err := secret.Make(o)
	if err != nil {
		return "", kit.Fail("%v", err)
	}
	return pw, nil
}

// Generate makes a password without storing it anywhere.
//
// It reaches no account and needs no session: a password is made on this machine
// and may never leave it. That is also the point - a generator you already have
// beats reaching for whatever is on the path.
//
// The wordlist and the separators are Proton's own, so a passphrase made here is
// one Pass could have made. A password of characters is drawn from a wider set of
// symbols than Pass draws from.
func generateCmd() *cobra.Command {
	var g generator
	c := &cobra.Command{
		Use:   "generate",
		Short: "Make a password",
		Long: "Make a password, without storing it anywhere.\n\n" +
			"Runs locally. It reaches no account and needs no session.\n\n" +
			"The alphabet leaves out i, o, l and their capitals, which are easily\n" +
			"misread. They are used only when letters are all the password may contain.\n\n" +
			"Every character kind you ask for is guaranteed to appear at least once.\n\n" +
			"--words makes a passphrase instead, from Proton's own wordlist. Each word is\n" +
			"capitalised and followed by a digit, unless --no-uppercase or --no-digits\n" +
			"says otherwise.",
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			pw, err := g.make()
			if err != nil {
				return err
			}
			return kit.Show(c, ui.RecordSpec{
				Object: struct {
					Password string `json:"password"`
				}{pw},
				Fields: []ui.Field{{Label: "Password", Value: pw}},
			})
		}),
	}
	g.register(c)
	return c
}
