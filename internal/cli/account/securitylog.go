package account

import (
	"strconv"

	"github.com/roman-16/proton-cli/internal/cli/kit"
	acctsvc "github.com/roman-16/proton-cli/internal/service/account"
	"github.com/roman-16/proton-cli/internal/ui"
	"github.com/roman-16/proton-cli/internal/units"
	"github.com/spf13/cobra"
)

// The log Proton keeps of sign-ins to the account, which mirrors the "Account
// monitor" section of Proton's account settings.
//
// It is a record Proton holds rather than a preference, so it sits beside
// `sessions` at the top of the app: the same screen offers both, and what a
// reader wants from either is the same question about who has been in.
//
// Turning the recording off deletes the events, and `disable` is a verb that
// never stops for a yes - so it refuses while there is anything to lose and
// points at `delete`, which is the verb built to ask.

// eventPage is how many events a listing holds when nothing asked for more.
const eventPage = 25

func securityLogCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "security-log",
		Short: "Sign-ins and credential changes Proton recorded",
		Long: "Sign-ins and credential changes Proton recorded.\n\n" +
			"Nothing is kept until recording is on, and it can keep the IP address of\n" +
			"each event as well. Sign-ins and changes made with " + kit.Program + " are not\n" +
			"recorded.\n\n" +
			"Turning recording off deletes the events with it.",
	}
	c.AddCommand(securityLogListCmd(), securityLogGetCmd(), securityLogEnableCmd(),
		securityLogDisableCmd(), securityLogDeleteCmd())
	return c
}

// securityLogView is what `security-log get` answers: what is being recorded,
// and how much of it there is to read.
type securityLogView struct {
	Status   string `json:"status"`
	Detailed string `json:"detailed"`
	Events   int    `json:"events"`
}

func securityLogListCmd() *cobra.Command {
	page := kit.Page{Default: eventPage}
	c := &cobra.Command{
		Use:   "list",
		Short: "List what Proton recorded, newest first",
		Long: "List what Proton recorded, newest first.\n\n" +
			"An IP column appears once `enable --detailed` is on. Device, location,\n" +
			"provider and protection are filled in for an account Proton Sentinel\n" +
			"watches.\n\n" +
			"--output json carries every field of each event, and --limit 0 answers with\n" +
			"the whole log rather than a page.",
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			events, total, err := c.App.Account.SecurityLogEvents(c.Ctx, page.Number, page.Size)
			if err != nil {
				return err
			}
			if err := kit.List(c, ui.TableSpec[acctsvc.Event]{
				Noun: "events", Columns: eventColumns(events),
				Total: total, Page: page.Number, PageSize: page.Size,
			}, events); err != nil {
				return err
			}
			if total > 0 {
				return nil
			}
			// An empty log is the one answer that needs a second question: nothing
			// recorded and nothing to record read the same on screen.
			level, err := c.App.Account.SecurityLogLevel(c.Ctx)
			if err != nil {
				return err
			}
			if level == acctsvc.LogOff {
				c.Note("Nothing is being recorded: %s account security-log enable", kit.Program)
			}
			return nil
		}),
	}
	page.Register(c, "events")
	return c
}

func securityLogGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get",
		Short: "Show what is being recorded, and how much of it there is",
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			log, err := c.App.Account.SecurityLog(c.Ctx)
			if err != nil {
				return err
			}
			view := securityLogView{
				Status:   kit.OnOffText(boolInt(log.Level != acctsvc.LogOff)),
				Detailed: kit.OnOffText(boolInt(log.Level == acctsvc.LogDetailed)),
				Events:   log.Events,
			}
			return kit.Show(c, ui.RecordSpec{
				Object: view,
				Fields: []ui.Field{
					{Label: "Status", Value: view.Status, Always: true},
					{Label: "Detailed", Value: view.Detailed, Always: true},
					{Label: "Events", Value: strconv.Itoa(view.Events), Always: true},
				},
			})
		}),
	}
}

func securityLogEnableCmd() *cobra.Command {
	var detailed bool
	var reauth kit.Reauth
	c := &cobra.Command{
		Use:   "enable",
		Short: "Record sign-ins and credential changes",
		Long: "Record sign-ins and credential changes.\n\n" +
			"Your password is asked for again.\n\n" +
			"--detailed records the IP address of each event as well, and turns recording\n" +
			"on if it was off.",
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			if err := reauth.Supply(c); err != nil {
				return err
			}
			log, err := c.App.Account.SecurityLog(c.Ctx)
			if err != nil {
				return err
			}
			want := acctsvc.LogOn
			if detailed {
				want = acctsvc.LogDetailed
			}
			if err := alreadyRecording(log.Level, want); err != nil {
				return err
			}
			spec := ui.ResultSpec{
				Action: ui.Enabled, Kind: "settings", Count: 1, Name: "security log",
			}
			switch {
			case detailed && log.Level == acctsvc.LogOn:
				spec.Name = "detailed events"
				spec.Detail = "- the IP address of each event is recorded"
			case detailed:
				spec.Detail = "- with the IP address of each event"
			}
			return kit.Mutate(c, spec, func() error {
				return c.App.Account.SecurityLogSet(c.Ctx, want)
			})
		}),
	}
	c.Flags().BoolVar(&detailed, "detailed", false, "Also record the IP address of each event")
	reauth.Declare(c)
	return c
}

func securityLogDisableCmd() *cobra.Command {
	var detailed bool
	var reauth kit.Reauth
	c := &cobra.Command{
		Use:   "disable",
		Short: "Stop recording sign-ins and credential changes",
		Long: "Stop recording sign-ins and credential changes.\n\n" +
			"Your password is asked for again.\n\n" +
			"Turning recording off deletes the events it holds, and this refuses while\n" +
			"there are any. `security-log delete` removes them.\n\n" +
			"--detailed stops the IP addresses being recorded and leaves everything else\n" +
			"on, the events included.",
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			if err := reauth.Supply(c); err != nil {
				return err
			}
			log, err := c.App.Account.SecurityLog(c.Ctx)
			if err != nil {
				return err
			}
			if detailed {
				if log.Level != acctsvc.LogDetailed {
					return kit.Fail("The security log is not recording IP addresses.")
				}
				return kit.Mutate(c, ui.ResultSpec{
					Action: ui.Disabled, Kind: "settings", Count: 1, Name: "detailed events",
					Detail: "- IP addresses are no longer recorded",
				}, func() error {
					return c.App.Account.SecurityLogSet(c.Ctx, acctsvc.LogOn)
				})
			}
			if log.Level == acctsvc.LogOff {
				return kit.Fail("The security log is already off.")
			}
			if log.Events > 0 {
				return kit.Fail("Turning the security log off deletes the %s it holds.",
					ui.Quantity(log.Events, "events")).
					Hint(kit.Program + " account security-log delete, then run this again")
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Disabled, Kind: "settings", Count: 1, Name: "security log",
			}, func() error {
				return c.App.Account.SecurityLogSet(c.Ctx, acctsvc.LogOff)
			})
		}),
	}
	c.Flags().BoolVar(&detailed, "detailed", false, "Stop recording the IP address of each event")
	reauth.Declare(c)
	return c
}

func securityLogDeleteCmd() *cobra.Command {
	var reauth kit.Reauth
	c := &cobra.Command{
		Use:   "delete",
		Short: "Remove every event the log holds",
		Long: "Remove every event the log holds.\n\n" +
			"Your password is asked for again.\n\n" +
			"What is recorded from now on is unchanged, and the log fills up again from\n" +
			"the next sign-in.",
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			if err := reauth.Supply(c); err != nil {
				return err
			}
			log, err := c.App.Account.SecurityLog(c.Ctx)
			if err != nil {
				return err
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Deleted, Kind: "events", Count: log.Events,
			}, func() error {
				return c.App.Account.SecurityLogDelete(c.Ctx)
			})
		}),
	}
	reauth.Declare(c)
	return c
}

// alreadyRecording refuses a level the account is already at, which is the one
// thing `enable` can be asked for that would change nothing.
func alreadyRecording(level, want string) error {
	if level != want {
		return nil
	}
	if level == acctsvc.LogDetailed {
		return kit.Fail("The security log is already on and recording IP addresses.")
	}
	return kit.Fail("The security log is already on.").
		Hint(kit.Program + " account security-log enable --detailed to record IP addresses as well")
}

// eventColumns is the table for the events in hand.
//
// What Proton fills in depends on the recording level and on whether Sentinel
// watches the account, and a column of blanks says nothing to anybody - so the
// optional ones are drawn when something on the page has them.
func eventColumns(events []acctsvc.Event) []ui.Column[acctsvc.Event] {
	columns := []ui.Column[acctsvc.Event]{
		{Header: "TIME", Cell: func(e acctsvc.Event) string { return units.Time(e.Time) }},
		{
			Header: "EVENT", Flex: true, Handle: true,
			Cell: func(e acctsvc.Event) string { return e.Event },
			Role: func(e acctsvc.Event) ui.Role {
				switch e.Status {
				case acctsvc.EventFailure:
					return ui.Danger
				case acctsvc.EventAttempt:
					return ui.Caution
				}
				return ui.Plain
			},
		},
		{Header: "APP", Cell: func(e acctsvc.Event) string { return e.App }},
	}
	for _, optional := range []ui.Column[acctsvc.Event]{
		{Header: "IP", Cell: func(e acctsvc.Event) string { return e.IP }},
		{Header: "DEVICE", Cell: func(e acctsvc.Event) string { return e.Device }},
		{Header: "LOCATION", Cell: func(e acctsvc.Event) string { return e.Location }},
		{Header: "ISP", Cell: func(e acctsvc.Event) string { return e.ISP }},
		{Header: "PROTECTION", Cell: func(e acctsvc.Event) string { return e.Protection }},
	} {
		if filled(events, optional.Cell) {
			columns = append(columns, optional)
		}
	}
	return columns
}

// filled reports whether any event on the page has something to say in a
// column.
func filled(events []acctsvc.Event, cell func(acctsvc.Event) string) bool {
	for _, e := range events {
		if cell(e) != "" {
			return true
		}
	}
	return false
}
