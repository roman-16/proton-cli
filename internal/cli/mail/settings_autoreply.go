package mail

import (
	"strings"

	"github.com/roman-16/proton-cli/internal/app"
	"github.com/roman-16/proton-cli/internal/cli/kit"
	"github.com/roman-16/proton-cli/internal/mailtext"
	mailsvc "github.com/roman-16/proton-cli/internal/service/mail"
	"github.com/roman-16/proton-cli/internal/ui"
	"github.com/spf13/cobra"
)

func autoreplyCmd() *cobra.Command {
	c := &cobra.Command{Use: "autoreply", Short: "The automatic reply and its schedule"}
	c.AddCommand(
		autoreplyGetCmd(), autoreplySetCmd(),
		autoreplyToggleCmd("enable", "Turn the auto-reply on, keeping its schedule", ui.Enabled, true),
		autoreplyToggleCmd("disable", "Turn the auto-reply off, keeping its schedule", ui.Disabled, false),
	)
	return c
}

func autoreplyGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get",
		Short: "Show the auto-reply and its schedule",
		Args:  cobra.NoArgs,
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			ar, err := c.App.Mail.AutoReplyGet(c.Ctx)
			if err != nil {
				return err
			}
			fields := []ui.Field{
				{Label: "Status", Value: kit.OnOffText(boolInt(ar.Enabled)), Always: true},
				{Label: "Schedule", Value: ar.ScheduleSummary(), Always: true},
			}
			if ar.Zone != "" && ar.Repeat != "permanent" {
				fields = append(fields, ui.Field{Label: "Time Zone", Value: ar.Zone})
			}
			if ar.Message != "" {
				fields = append(fields, ui.Field{Label: "Message", Value: mailtext.HTMLToText(ar.Message)})
			}
			return kit.Show(c, ui.RecordSpec{Object: ar, Fields: fields})
		}),
	}
}

func autoreplySetCmd() *cobra.Command {
	var ar mailsvc.AutoReply
	var html bool
	var reauth kit.Reauth
	repeat := &kit.Enum{
		Name: "repeat", Usage: "How the schedule repeats", Default: "fixed",
		Values: mailsvc.RepeatNames(),
	}
	c := &cobra.Command{
		Use:   "set",
		Short: "Configure the auto-reply and turn it on",
		Long: "Configure the auto-reply and turn it on.\n\n" +
			"--start and --end are written in the grammar the repeat mode dictates:\n" +
			"  fixed      2026-07-01T09:00   a date and time in your zone\n" +
			"  daily      09:00              a time of day, with --days\n" +
			"  weekly     mon:09:00          a weekday and time\n" +
			"  monthly    1:09:00            a day of the month and time\n" +
			"  permanent  -                  no bounds\n\n" +
			"Proton sends every auto-reply with the subject \"Auto\" and offers no way to\n" +
			"change it. Auto-reply is a paid feature.",
		Args: cobra.NoArgs,
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			if err := reauth.Supply(c); err != nil {
				return err
			}
			mode, err := repeat.Value()
			if err != nil {
				return err
			}
			ar.Repeat = mode
			if ar.Zone, err = c.App.Zone(c.Ctx); err != nil {
				return err
			}
			msg, err := kit.ReadTextArg(c, ar.Message, "--message")
			if err != nil {
				return err
			}
			if strings.TrimSpace(msg) == "" {
				return kit.Fail("A message is required.").
					Hint(`--message "I'm away until the 14th.", or --message - to read stdin.`)
			}
			if !html {
				msg = mailtext.TextToHTML(msg)
			}
			ar.Message = msg
			req, err := ar.Request()
			if err != nil {
				return err
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Enabled, Kind: "settings", Count: 1, Name: "auto-reply",
				Detail: "on a " + mode + " schedule",
			}, func() error {
				// Nothing here arranges the elevation: the client does it when the
				// server asks. All this owes the user is a reason for the prompt.
				return c.App.Mail.AutoReplySet(app.WithScopeReason(c.Ctx, "change your auto-reply"), req)
			})
		}),
	}
	repeat.Register(c)
	c.Flags().StringVar(&ar.Start, "start", "", "Start of the window (grammar depends on --repeat)")
	c.Flags().StringVar(&ar.End, "end", "", "End of the window (grammar depends on --repeat)")
	c.Flags().StringSliceVar(&ar.Days, "days", nil, "Days it is active, for a daily schedule, e.g. mon,tue,wed")
	c.Flags().StringVar(&ar.Message, "message", "", "Reply body (- reads stdin)")
	c.Flags().BoolVar(&html, "html", false, "Treat the message as HTML rather than escaping it")
	reauth.Declare(c)
	return c
}

func autoreplyToggleCmd(use, short string, action ui.Action, enabled bool) *cobra.Command {
	var reauth kit.Reauth
	c := &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.NoArgs,
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			if err := reauth.Supply(c); err != nil {
				return err
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: action, Kind: "settings", Count: 1, Name: "auto-reply",
			}, func() error {
				// Proton guards the autoresponder whichever way the switch goes,
				// so turning one off asks for the password exactly as setting one
				// up does. Nothing here arranges the elevation: the client does it
				// when the server asks, and all this owes the user is a reason.
				return c.App.Mail.AutoReplyToggle(app.WithScopeReason(c.Ctx, "change your auto-reply"), enabled)
			})
		}),
	}
	reauth.Declare(c)
	return c
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
