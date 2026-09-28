package account

import (
	"github.com/roman-16/proton-cli/internal/cli/kit"
	"github.com/roman-16/proton-cli/internal/ui"
	"github.com/spf13/cobra"
)

type sentinelView struct {
	Status   string `json:"status"`
	Emails   string `json:"emails"`
	Eligible bool   `json:"eligible"`
	Enforced bool   `json:"enforced"`
}

func sentinelCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "sentinel",
		Short: "Heightened protection for your account",
		Long: "Proton Sentinel, the heightened protection for your account.\n\n" +
			"It needs a plan that includes it. While it is on, Proton chooses which\n" +
			"recovery methods the account may use. An organization that turns it on for\n" +
			"its members decides it for them.",
	}
	c.AddCommand(sentinelGetCmd(),
		switchCmd("enable", "Turn Proton Sentinel on", true, sentinel),
		switchCmd("disable", "Turn Proton Sentinel off", false, sentinel))
	return c
}

func sentinelGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get",
		Short: "Show whether Proton Sentinel is on",
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			f, err := sentinel(c)
			if err != nil {
				return err
			}
			view := sentinelView{
				Status:   kit.OnOffText(boolInt(f.on)),
				Emails:   kit.OnOffText(boolInt(f.emails)),
				Eligible: f.eligible,
				Enforced: f.enforced,
			}
			var setBy string
			if f.enforced {
				setBy = "your organization"
			}
			if err := kit.Show(c, ui.RecordSpec{
				Object: view,
				Fields: []ui.Field{
					{Label: "Status", Value: view.Status, Always: true},
					{Label: "Emails", Value: view.Emails, Always: true},
					{Label: "Set by", Value: setBy},
				},
			}); err != nil {
				return err
			}
			if !f.eligible {
				c.UI().Hint("Your plan does not include Proton Sentinel.")
			}
			return nil
		}),
	}
}
