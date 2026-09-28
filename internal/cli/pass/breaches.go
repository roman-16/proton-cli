package pass

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/roman-16/proton-cli/internal/breach"
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
		breachesVerifyCmd(), breachesResendCmd(), breachesDeleteCmd(), breachesResolveCmd(),
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
			if a.Breaches == nil {
				return "?"
			}
			return strconv.Itoa(*a.Breaches)
		}},
		{Header: "LAST", Cell: func(a passsvc.MonitoredAddress) string {
			if a.Breaches == nil {
				return "?"
			}
			return units.Time(a.LastBreach)
		}},
		{Header: "STATE", Cell: func(a passsvc.MonitoredAddress) string { return a.State }},
	}
}

func breachesListCmd() *cobra.Command {
	var held kit.Held[passsvc.MonitoredAddress]
	kind := &kit.Enum{Name: "type", Usage: "List only addresses of this kind", Values: addressKinds}
	c := &cobra.Command{
		Use:   "list",
		Short: "List the addresses Proton watches, and how many breaches each is in",
		Long: "List the addresses Proton watches, and how many breaches each is in.\n\n" +
			"Worst first. Three kinds of address are watched: the ones on your account,\n" +
			"the hide-my-email aliases in your vaults, and the ones you added with\n" +
			"`breaches create`. Listing the aliases reads your vaults, so this costs what\n" +
			"`items list` costs.\n\n" +
			"STATE is what has to happen next: an address you added is unverified until\n" +
			"you hand back the code Proton emailed it, and paused means watching is off,\n" +
			"for the address itself or for every address of its kind.\n\n" +
			"To see which breaches an address is in and what they exposed, run\n" +
			"`breaches get` on it.",
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			rows, err := c.App.Pass.Monitored(c.Ctx)
			if err != nil {
				return err
			}
			if kind.Set() {
				only, _ := kind.Value()
				rows = slices.DeleteFunc(rows, func(a passsvc.MonitoredAddress) bool { return a.Type != only })
			}
			if err := held.Answer(c, ui.TableSpec[passsvc.MonitoredAddress]{
				Noun: "watched addresses", Columns: breachColumns(),
			}, rows); err != nil {
				return err
			}
			uncounted(c, rows)
			pausedGroups(c, rows)
			return nil
		}),
	}
	kind.Register(c)
	held.Register(c, "watched addresses",
		kit.Key[passsvc.MonitoredAddress]{Name: "breaches", Less: passsvc.ByBreaches},
		kit.Key[passsvc.MonitoredAddress]{Name: "email", Less: func(a, b passsvc.MonitoredAddress) int { return kit.Fold(a.Email, b.Email) }},
	)
	return c
}

func uncounted(c *kit.Invocation, rows []passsvc.MonitoredAddress) {
	var missing []string
	for _, row := range rows {
		if row.Breaches == nil {
			missing = append(missing, row.Email)
		}
	}
	switch len(missing) {
	case 0:
	case 1:
		c.Warn("The breach count of %s did not come back. `%s pass breaches get %s` asks again.",
			missing[0], kit.Program, missing[0])
	default:
		c.Warn("The breach counts of %s did not come back. `%s pass breaches get` on each asks again.",
			ui.Listing(missing), kit.Program)
	}
}

var addressKinds = []string{passsvc.AddressProton, passsvc.AddressCustom, passsvc.AddressAlias}

func pausedGroups(c *kit.Invocation, rows []passsvc.MonitoredAddress) {
	for _, kind := range []string{passsvc.AddressProton, passsvc.AddressAlias} {
		if slices.ContainsFunc(rows, func(a passsvc.MonitoredAddress) bool {
			return a.Type == kind && a.GroupPaused()
		}) {
			c.UI().Hint(fmt.Sprintf("Watching for %s is paused. Resume it with `%s pass breaches enable --type %s`.",
				groupName(kind), kit.Program, kind))
		}
	}
}

func groupName(kind string) string {
	if kind == passsvc.AddressAlias {
		return "Hide-my-email aliases"
	}
	return "Proton addresses"
}

func breachesGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get REF",
		Short: "Show the breaches one address has appeared in",
		Long: "Show the breaches one address has appeared in.\n\n" +
			"Names each breach, when it happened, what it exposed, whether it is resolved,\n" +
			"and the last few characters of the password if one leaked in the clear.\n\n" +
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
			counted := len(report.Breaches) + report.Withheld
			address.Breaches = &counted
			view := struct {
				passsvc.MonitoredAddress
				Breaches []breach.Breach `json:"breach_list"`
				Withheld int             `json:"withheld,omitempty"`
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
					{Label: "Breaches", Value: strconv.Itoa(counted)},
					{Label: "State", Value: address.State},
				}, breachFields(report.Breaches)...),
			})
		}),
	}
}

// breachFields lists each breach under the address, since a record is what a
// person reads to decide which password to change.
func breachFields(breaches []breach.Breach) []ui.Field {
	var out []ui.Field
	for _, b := range breaches {
		out = append(out, ui.Field{Label: "Breach", Value: b.Name})
		out = append(out, []ui.Field{
			{Label: "  Severity", Value: b.Severity},
			{Label: "  Happened", Value: happened(b)},
			{Label: "  Source", Value: b.Source},
			{Label: "  Exposed", Value: strings.Join(b.Exposed, ", ")},
			{Label: "  Password ends", Value: b.PasswordTail},
			{Label: "  State", Value: b.State},
		}...)
	}
	return out
}

func happened(b breach.Breach) string {
	if b.Published == 0 {
		return ""
	}
	return units.Time(b.Published)
}

func breachesResolveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "resolve REF...",
		Short: "Mark an address's current breaches as resolved",
		Long: "Mark an address's current breaches as resolved.\n\n" +
			"Pass has no way to reopen them. A breach found later is reported as new.\n\n" +
			"Resolving needs a plan that includes the detail of each breach.",
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			lookup := breachList(c)
			var (
				addresses []passsvc.MonitoredAddress
				open      int
			)
			for _, ref := range c.Args {
				address, err := lookup.Find(c.Ctx, ref)
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
				if !report.Eligible {
					return kit.Fail("Resolving breaches needs a plan that includes their detail.")
				}
				current := slices.DeleteFunc(report.Breaches, breach.Breach.Resolved)
				if len(current) == 0 {
					return kit.Fail("%s has no open breaches.", address.Email)
				}
				addresses = append(addresses, address)
				open += len(current)
			}
			detail := "of " + ui.Quantity(len(addresses), "addresses")
			if len(addresses) == 1 {
				detail = "of " + addresses[0].Email
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Resolved, Kind: "breaches", Count: open, Detail: detail,
			}, func() error {
				for _, address := range addresses {
					if err := c.App.Pass.ResolveBreaches(c.Ctx, address); err != nil {
						return err
					}
				}
				return nil
			})
		}),
	}
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
		RunE: kit.Run([]kit.Step{kit.StepExpand}, breachAction(ui.Verified,
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
		RunE: kit.Run([]kit.Step{kit.StepExpand}, breachAction(ui.Resent,
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
		RunE: kit.Run([]kit.Step{kit.StepExpand}, breachAction(ui.Deleted,
			func(c *kit.Invocation, a passsvc.MonitoredAddress) error {
				return c.App.Pass.StopWatching(c.Ctx, a)
			})),
	}
}

func breachesToggleCmd(use, short string, action ui.Action, on bool) *cobra.Command {
	kind := &kit.Enum{
		Name: "type", Usage: "Every address of this kind at once, instead of one", Values: addressKinds,
	}
	c := &cobra.Command{
		Use:   use + " [REF]",
		Short: short,
		Long: short + ".\n\n" +
			"Works on an address on your account, on an alias in one of your vaults, and\n" +
			"on an address you added once you have verified it.\n\n" +
			"Pausing an alias also leaves it out of `items list --risk`, which is the\n" +
			"same switch.\n\n" +
			"--type proton or --type alias switches watching for every address of that\n" +
			"kind at once, on a paid Pass plan. While it is off, no single one can be\n" +
			"switched, and turning it back on leaves the ones you paused one at a time\n" +
			"paused.",
		Args: func(_ *cobra.Command, args []string) error {
			if (len(args) == 1) == kind.Set() {
				return kit.Fail("Name one address, or pass --type for every address of one kind.")
			}
			if kind.Is(passsvc.AddressCustom) {
				return kit.Fail("Addresses you added are paused one at a time.")
			}
			return nil
		},
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			if kind.Set() {
				group, _ := kind.Value()
				return toggleGroup(c, action, group, on)
			}
			address, err := breachList(c).Find(c.Ctx, c.Args[0])
			if err != nil {
				return err
			}
			if err := watching(address); err != nil {
				return err
			}
			if address.GroupPaused() {
				verb, noun := "resumed", "address"
				if !on {
					verb = "paused"
				}
				if address.Type == passsvc.AddressAlias {
					noun = "alias"
				}
				return kit.Fail("Watching for %s is paused, so no single %s can be %s.",
					groupName(address.Type), noun, verb).
					Hint(fmt.Sprintf("%s pass breaches enable --type %s", kit.Program, address.Type))
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: action, Kind: "watched addresses", Count: 1,
				Name: address.Email, IDs: []string{address.AddressID},
			}, func() error { return c.App.Pass.SetMonitored(c.Ctx, address, on) })
		}),
	}
	kind.Register(c)
	return c
}

func toggleGroup(c *kit.Invocation, action ui.Action, group string, on bool) error {
	groups, err := c.App.Pass.WatchedGroups(c.Ctx)
	if err != nil {
		return err
	}
	if !groups.Paid {
		return kit.Fail("Switching watching for every address of a kind needs a paid Pass plan.")
	}
	if groups.Watched(group) == on {
		state := "on"
		if !on {
			state = "paused"
		}
		return kit.Fail("Watching for %s is already %s.", groupName(group), state)
	}
	return kit.Mutate(c, ui.ResultSpec{
		Action: action, Count: 1, Name: "watching for " + groupName(group),
	}, func() error { return c.App.Pass.WatchGroup(c.Ctx, group, on) })
}

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

// breachAction is the shape every command that acts on the record of one
// address you added has: find it by whatever was typed, then report the change
// it made to that one.
func breachAction(action ui.Action,
	apply func(*kit.Invocation, passsvc.MonitoredAddress) error,
) kit.Handler {
	return func(c *kit.Invocation) error {
		address, err := breachList(c).Find(c.Ctx, c.Args[0])
		if err != nil {
			return err
		}
		return kit.Mutate(c, ui.ResultSpec{
			Action: action, Kind: "watched addresses", Count: 1,
			Name: address.Email, IDs: []string{address.AddressID},
		}, func() error { return apply(c, address) })
	}
}
