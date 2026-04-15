package cmd

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
)

var settingsCmd = &cobra.Command{
	Use:   "settings",
	Short: "Account and mail settings",
}

var settingsGetCmd = &cobra.Command{
	Use:   "get",
	Short: "Show current settings",
	RunE:  runSettingsGet,
}

var settingsMailCmd = &cobra.Command{
	Use:   "mail",
	Short: "Show mail settings",
	RunE:  runSettingsMail,
}

func init() {
	settingsCmd.AddCommand(settingsGetCmd, settingsMailCmd)
	rootCmd.AddCommand(settingsCmd)
}

func runSettingsGet(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	c, err := getAuthenticatedClient(ctx)
	if err != nil {
		return err
	}

	body, _, err := c.Do(ctx, "GET", "/core/v4/settings", nil, "", "", "")
	if err != nil {
		return err
	}

	if flagJSON {
		printJSON(body)
		return nil
	}

	var res struct {
		UserSettings struct {
			Email struct {
				Value  string
				Status int
				Notify int
				Reset  int
			}
			Phone struct {
				Value  string
				Status int
				Notify int
				Reset  int
			}
			Locale        string
			LogAuth       int
			Density       int
			WeekStart     int
			DateFormat    int
			TimeFormat    int
			EarlyAccess   int
			Telemetry     int
			CrashReports  int
			HideSidePanel int
			HighSecurity  struct {
				Eligible int
				Value    int
			}
		}
	}
	if err := json.Unmarshal(body, &res); err != nil {
		// Fall back to raw JSON if structure doesn't match
		printJSON(body)
		return nil
	}

	s := res.UserSettings
	fmt.Printf("Locale:         %s\n", s.Locale)
	fmt.Printf("Recovery Email: %s\n", s.Email.Value)
	fmt.Printf("Recovery Phone: %s\n", s.Phone.Value)

	weekDays := map[int]string{0: "Sunday", 1: "Monday", 6: "Saturday", 7: "Sunday"}
	fmt.Printf("Week Start:     %s\n", weekDays[s.WeekStart])

	dateFmts := map[int]string{0: "default", 1: "DD/MM/YYYY", 2: "MM/DD/YYYY", 3: "YYYY-MM-DD"}
	fmt.Printf("Date Format:    %s\n", dateFmts[s.DateFormat])

	timeFmts := map[int]string{0: "default", 1: "12h", 2: "24h"}
	fmt.Printf("Time Format:    %s\n", timeFmts[s.TimeFormat])

	densities := map[int]string{0: "comfortable", 1: "compact"}
	fmt.Printf("Density:        %s\n", densities[s.Density])

	fmt.Printf("Log Auth:       %d\n", s.LogAuth)
	fmt.Printf("Telemetry:      %d\n", s.Telemetry)
	fmt.Printf("Crash Reports:  %d\n", s.CrashReports)
	fmt.Printf("Early Access:   %d\n", s.EarlyAccess)

	highSec := "off"
	if s.HighSecurity.Value == 1 {
		highSec = "on"
	}
	fmt.Printf("High Security:  %s\n", highSec)

	return nil
}

func runSettingsMail(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	c, err := getAuthenticatedClient(ctx)
	if err != nil {
		return err
	}

	body, _, err := c.Do(ctx, "GET", "/mail/v4/settings", nil, "", "", "")
	if err != nil {
		return err
	}

	if flagJSON {
		printJSON(body)
		return nil
	}

	var res struct {
		MailSettings struct {
			DisplayName                string
			PMSignature                int
			AutoSaveContacts           int
			ComposerMode               int
			ViewMode                   int
			ViewLayout                 int
			SwipeLeft                  int
			SwipeRight                 int
			PageSize                   int
			HideRemoteImages           int
			HideEmbeddedImages         int
			Shortcuts                  int
			DraftMIMEType              string
			Sign                       int
			AttachPublicKey            int
			ConfirmLink                int
			DelaySendSeconds           int
			StickyLabels               int
			FontFace                   string
			FontSize                   int
			SpamAction                 *int
			AutoDeleteSpamAndTrashDays int
		}
	}
	if err := json.Unmarshal(body, &res); err != nil {
		printJSON(body)
		return nil
	}

	s := res.MailSettings

	composerModes := map[int]string{0: "popup", 1: "maximized"}
	viewModes := map[int]string{0: "conversations", 1: "messages"}
	viewLayouts := map[int]string{0: "no split", 1: "horizontal", 2: "vertical"}

	fmt.Printf("Display Name:       %s\n", s.DisplayName)
	fmt.Printf("Page Size:          %d\n", s.PageSize)
	fmt.Printf("Composer Mode:      %s\n", composerModes[s.ComposerMode])
	fmt.Printf("View Mode:          %s\n", viewModes[s.ViewMode])
	fmt.Printf("View Layout:        %s\n", viewLayouts[s.ViewLayout])
	fmt.Printf("Draft MIME Type:    %s\n", s.DraftMIMEType)
	fmt.Printf("Font:               %s %dpx\n", s.FontFace, s.FontSize)
	fmt.Printf("PM Signature:       %s\n", onOff(s.PMSignature))
	fmt.Printf("Auto Save Contacts: %s\n", onOff(s.AutoSaveContacts))
	fmt.Printf("Hide Remote Images: %s\n", onOff(s.HideRemoteImages))
	fmt.Printf("Confirm Links:      %s\n", onOff(s.ConfirmLink))
	fmt.Printf("Sign Outgoing:      %s\n", onOff(s.Sign))
	fmt.Printf("Attach Public Key:  %s\n", onOff(s.AttachPublicKey))
	fmt.Printf("Sticky Labels:      %s\n", onOff(s.StickyLabels))
	fmt.Printf("Shortcuts:          %s\n", onOff(s.Shortcuts))
	fmt.Printf("Delay Send:         %ds\n", s.DelaySendSeconds)
	fmt.Printf("Auto Delete Trash:  %d days\n", s.AutoDeleteSpamAndTrashDays)

	return nil
}

func onOff(v int) string {
	if v == 1 {
		return "on"
	}
	return "off"
}
