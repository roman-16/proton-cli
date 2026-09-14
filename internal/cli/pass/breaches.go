package pass

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/roman-16/proton-cli/internal/cli/kit"
	passsvc "github.com/roman-16/proton-cli/internal/service/pass"
	"github.com/roman-16/proton-cli/internal/ui"
	"github.com/roman-16/proton-cli/internal/units"
)

// Which of your addresses have turned up in somebody else's data breach.
//
// Proton calls this Dark Web Monitoring, the half of Pass Monitor that is about
// addresses rather than passwords. The collection is the addresses it watches,
// because that is what a listing has one row of and what a person names to ask
// for more.

func breachesCmd() *cobra.Command {
	c := &cobra.Command{Use: "breaches", Short: "Addresses that have appeared in a data breach"}
	c.AddCommand(breachesListCmd(), breachesGetCmd(), breachesCreateCmd(),
		breachesVerifyCmd(), breachesResendCmd(), breachesDeleteCmd(),
		breachesToggleCmd("enable", "Have Proton watch an address again", ui.Enabled, true),
		breachesToggleCmd("disable", "Stop Proton watching an address", ui.Disabled, false))
	return c
}

func breachList(c *kit.Invocation) *kit.Lookup[passsvc.MonitoredAddress] {
	return &kit.Lookup[passsvc.MonitoredAddress]{
		Kind: "watched address",
		Load: func(ctx context.Context) ([]passsvc.MonitoredAddress, error) {
			return c.App.Pass.Monitored(ctx)
		},
		ID:     func(a passsvc.MonitoredAddress) string { return a.AddressID },
		Handle: func(a passsvc.MonitoredAddress) string { return a.Email },
	}
}

func breachColumns() []ui.Column[passsvc.MonitoredAddress] {
	return []ui.Column[passsvc.MonitoredAddress]{
		{Header: "ADDRESS", Flex: true, Handle: true, Cell: func(a passsvc.MonitoredAddress) string {
			return a.Email
		}},
		{Header: "TYPE", Cell: func(a passsvc.MonitoredAddress) string { return a.Type }},
		{Header: "BREACHES", Right: true, Cell: func(a passsvc.MonitoredAddress) string {
			return strconv.Itoa(a.Breaches)
		}},
		{Header: "LAST", Cell: func(a passsvc.MonitoredAddress) string {
			return units.Time(a.LastBreach)
		}},
		{Header: "STATE", Cell: func(a passsvc.MonitoredAddress) string { return a.State }},
	}
}

func breachesListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List the addresses Proton watches, and how many breaches each is in",
		Long: "List the addresses Proton watches, and how many breaches each is in.\n\n" +
			"Worst first. Three kinds of address are watched: the ones on your account,\n" +
			"the hide-my-email aliases in your vaults, and the ones you added with\n" +
			"`breaches create`. Listing the aliases reads your vaults, so this costs what\n" +
			"`items list` costs.\n\n" +
			"STATE is what has to happen next: an address you added is unverified until\n" +
			"you hand back the code Proton emailed it, and paused means watching is off.\n\n" +
			"To see which breaches an address is in and what they exposed, run\n" +
			"`breaches get` on it.",
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			rows, err := c.App.Pass.Monitored(c.Ctx)
			if err != nil {
				return err
			}
			return kit.List(c, ui.TableSpec[passsvc.MonitoredAddress]{
				Noun: "watched addresses", Columns: breachColumns(),
				Total: len(rows), Page: ui.Unpaged,
			}, rows)
		}),
	}
}

func breachesGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get REF",
		Short: "Show the breaches one address has appeared in",
		Long: "Show the breaches one address has appeared in.\n\n" +
			"Names each breach, when it happened, what it exposed, and the last few\n" +
			"characters of the password if one leaked in the clear.\n\n" +
			"An address you added is refused until you verify it: Proton is not watching\n" +
			"it until then.\n\n" +
			"The detail needs a plan that includes it. Without one the count is still\n" +
			"right and the breaches are withheld, and the answer says how many.",
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			address, err := breachList(c).Find(c.Ctx, c.Args[0])
			if err != nil {
				return err
			}
			if err := watching(address); err != nil {
				return err
			}
			report, err := c.App.Pass.BreachesFor(c.Ctx, address)
			if err != nil {
				return err
			}
			view := struct {
				passsvc.MonitoredAddress
				Breaches []passsvc.Breach `json:"breach_list"`
				Withheld int              `json:"withheld,omitempty"`
			}{address, report.Breaches, report.Withheld}
			if report.Withheld > 0 {
				c.Warn("Proton is withholding the detail of %s on this plan. "+
					"The addresses and counts are complete; what each breach exposed is not.",
					ui.Quantity(report.Withheld, "breaches"))
			}
			return kit.Show(c, ui.RecordSpec{
				Object: view,
				Fields: append([]ui.Field{
					{Label: "Address", Value: address.Email, Handle: true},
					{Label: "Type", Value: address.Type},
					{Label: "Breaches", Value: strconv.Itoa(address.Breaches)},
					{Label: "State", Value: address.State},
				}, breachFields(report.Breaches)...),
			})
		}),
	}
}

// breachFields lists each breach under the address, since a record is what a
// person reads to decide which password to change.
func breachFields(breaches []passsvc.Breach) []ui.Field {
	var out []ui.Field
	for _, b := range breaches {
		out = append(out, ui.Field{Label: "Breach", Value: b.Name})
		out = append(out, []ui.Field{
			{Label: "  Severity", Value: b.Severity},
			{Label: "  Happened", Value: units.Time(b.Published)},
			{Label: "  Source", Value: b.Source},
			{Label: "  Exposed", Value: strings.Join(b.Exposed, ", ")},
			{Label: "  Password ends", Value: b.PasswordTail},
		}...)
	}
	return out
}

// ── the addresses you add yourself ──

func breachesCreateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "create EMAIL",
		Short: "Have Proton watch an address you own elsewhere",
		Long: "Have Proton watch an address you own elsewhere.\n\n" +
			"Proton emails the address a code and watches nothing until you hand that\n" +
			"code back with `breaches verify`.\n\n" +
			"The addresses on your account and the aliases in your vaults are watched\n" +
			"already; this is for the ones somewhere else.",
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			return kit.Create(c, ui.ResultSpec{
				Action: ui.Created, Kind: "watched addresses", Name: c.Args[0],
			}, func() (string, error) {
				address, err := c.App.Pass.WatchAddress(c.Ctx, c.Args[0])
				if err != nil {
					return "", err
				}
				if !address.Verified {
					c.Note("Proton has emailed %s a code. Hand it back with "+
						"`%s pass breaches verify %s --code CODE`.",
						address.Email, kit.Program, address.Email)
				}
				return address.AddressID, nil
			})
		}),
	}
}

func breachesVerifyCmd() *cobra.Command {
	var code string
	c := &cobra.Command{
		Use:   "verify REF",
		Short: "Confirm an address with the code Proton emailed it",
		RunE: kit.Run([]kit.Step{kit.StepExpand}, breachAction(ui.Verified, anyAddress,
			func(c *kit.Invocation, a passsvc.MonitoredAddress) error {
				return c.App.Pass.VerifyAddress(c.Ctx, a, code)
			})),
	}
	c.Flags().StringVar(&code, "code", "", "The code Proton emailed the address")
	_ = c.MarkFlagRequired("code")
	return c
}

func breachesResendCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "resend REF",
		Short: "Send the confirmation code again",
		RunE: kit.Run([]kit.Step{kit.StepExpand}, breachAction(ui.Resent, anyAddress,
			func(c *kit.Invocation, a passsvc.MonitoredAddress) error {
				return c.App.Pass.ResendVerification(c.Ctx, a)
			})),
	}
}

func breachesDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete REF",
		Short: "Stop Proton watching an address you added",
		Long: "Stop Proton watching an address you added, and forget its breach history.\n\n" +
			"Only an address you added yourself can be removed. To stop Proton watching\n" +
			"one of your own addresses or an alias, use `breaches disable`.",
		RunE: kit.Run([]kit.Step{kit.StepExpand}, breachAction(ui.Deleted, anyAddress,
			func(c *kit.Invocation, a passsvc.MonitoredAddress) error {
				return c.App.Pass.StopWatching(c.Ctx, a)
			})),
	}
}

func breachesToggleCmd(use, short string, action ui.Action, on bool) *cobra.Command {
	return &cobra.Command{
		Use:   use + " REF",
		Short: short,
		Long: short + ".\n\n" +
			"Works on an address on your account, on an alias in one of your vaults, and\n" +
			"on an address you added once you have verified it.\n\n" +
			"Pausing an alias also leaves it out of `items list --risk`, which is the\n" +
			"same switch.",
		RunE: kit.Run([]kit.Step{kit.StepExpand}, breachAction(action, onlyWatched,
			func(c *kit.Invocation, a passsvc.MonitoredAddress) error {
				return c.App.Pass.SetMonitored(c.Ctx, a, on)
			})),
	}
}

// Whether a command needs Proton to be watching the address already.
//
// An address you add is a record of an intention until its code comes back:
// Proton is not watching it, so there is nothing to read, pause or resume. The
// commands that act on the watching need it; the three that act on the record
// itself are what gets it there.
const (
	anyAddress  = false
	onlyWatched = true
)

// watching refuses a command that needs Proton to be watching an address it is
// not watching yet.
func watching(address passsvc.MonitoredAddress) error {
	if address.Verified {
		return nil
	}
	return kit.Fail("%s is unverified, so Proton is not watching it.", address.Email).
		Hint(fmt.Sprintf("Hand the code Proton emailed it back: %s pass breaches verify %s --code CODE",
			kit.Program, address.Email))
}

// breachAction is the shape every command that changes one watched address has:
// find it by whatever was typed, then report the change it made to that one.
func breachAction(action ui.Action, needsWatching bool,
	apply func(*kit.Invocation, passsvc.MonitoredAddress) error,
) kit.Handler {
	return func(c *kit.Invocation) error {
		address, err := breachList(c).Find(c.Ctx, c.Args[0])
		if err != nil {
			return err
		}
		if needsWatching {
			if err := watching(address); err != nil {
				return err
			}
		}
		return kit.Mutate(c, ui.ResultSpec{
			Action: action, Kind: "watched addresses", Count: 1,
			Name: address.Email, IDs: []string{address.AddressID},
		}, func() error { return apply(c, address) })
	}
}
