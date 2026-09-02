package pass

import (
	"context"
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
// Proton calls this Pass Monitor. The collection is the addresses it watches,
// because that is what a listing has one row of and what a person names to ask
// for more.

func breachesCmd() *cobra.Command {
	c := &cobra.Command{Use: "breaches", Short: "Addresses that have appeared in a data breach"}
	c.AddCommand(breachesListCmd(), breachesGetCmd())
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
		{Header: "ADDRESS", Cell: func(a passsvc.MonitoredAddress) string { return a.Email }},
		{Header: "BREACHES", Right: true, Cell: func(a passsvc.MonitoredAddress) string {
			return strconv.Itoa(a.Breaches)
		}},
		{Header: "LAST", Cell: func(a passsvc.MonitoredAddress) string {
			return units.Time(a.LastBreach)
		}},
		{Header: "WATCHED", Cell: func(a passsvc.MonitoredAddress) string {
			return yesNo(a.Monitored)
		}},
	}
}

func breachesListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List the addresses Proton watches, and how many breaches each is in",
		Long: "List the addresses Proton watches, and how many breaches each is in.\n\n" +
			"Worst first. To see which breaches an address is in and what they exposed,\n" +
			"run `breaches get` on it.",
		Args: cobra.NoArgs,
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			rows, err := c.App.Pass.Monitored(c.Ctx)
			if err != nil {
				return err
			}
			return kit.List(c, ui.TableSpec[passsvc.MonitoredAddress]{
				Noun: "watched addresses", Columns: breachColumns(),
				Total: len(rows), Page: ui.Unpaged,
			}, rows, func(a passsvc.MonitoredAddress) []string { return []string{a.AddressID} })
		}),
	}
}

func breachesGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get REF",
		Short: "Show the breaches one address has appeared in",
		Args:  cobra.ExactArgs(1),
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			address, err := breachList(c).Find(c.Ctx, c.Args[0])
			if err != nil {
				return err
			}
			breaches, err := c.App.Pass.BreachesFor(c.Ctx, address)
			if err != nil {
				return err
			}
			view := struct {
				passsvc.MonitoredAddress
				Breaches []passsvc.Breach `json:"breach_list"`
			}{address, breaches}
			return kit.Show(c, ui.RecordSpec{
				Object: view,
				Fields: append([]ui.Field{
					{Label: "Address", Value: address.Email},
					{Label: "Breaches", Value: strconv.Itoa(address.Breaches)},
					{Label: "Watched", Value: yesNo(address.Monitored)},
				}, breachFields(breaches)...),
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
