package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/roman-16/proton-cli/internal/api"
	"github.com/roman-16/proton-cli/internal/app"
	"github.com/spf13/cobra"
)

func newSettingsCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "settings",
		Short: "Account and mail settings",
	}
	c.AddCommand(&cobra.Command{
		Use:   "get",
		Short: "Show current account settings",
		RunE: func(cmd *cobra.Command, args []string) error {
			a := app.From(cmd.Context())
			if err := a.Authenticate(cmd.Context()); err != nil {
				return err
			}
			resp, err := a.API.Do(cmd.Context(), api.Request{Method: "GET", Path: "/core/v4/settings"})
			if err != nil {
				return err
			}
			if a.R.Format != "text" {
				return a.R.JSON(resp.Body)
			}
			return printSettingsText(a, resp.Body, renderAccountSettings)
		},
	})
	c.AddCommand(&cobra.Command{
		Use:   "mail",
		Short: "Show mail settings",
		RunE: func(cmd *cobra.Command, args []string) error {
			a := app.From(cmd.Context())
			if err := a.Authenticate(cmd.Context()); err != nil {
				return err
			}
			resp, err := a.API.Do(cmd.Context(), api.Request{Method: "GET", Path: "/mail/v4/settings"})
			if err != nil {
				return err
			}
			if a.R.Format != "text" {
				return a.R.JSON(resp.Body)
			}
			return printSettingsText(a, resp.Body, renderMailSettings)
		},
	})
	return c
}

func printSettingsText(a *app.App, body []byte, renderer func(*app.App, map[string]any)) error {
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		return a.R.JSON(body)
	}
	renderer(a, m)
	return nil
}

func renderAccountSettings(a *app.App, m map[string]any) {
	u, _ := m["UserSettings"].(map[string]any)
	if u == nil {
		_ = a.R.Object(m)
		return
	}
	print := func(k, v string) { fmt.Fprintf(a.R.Stdout, "%-16s %s\n", k+":", v) }
	print("Locale", str(u["Locale"]))
	if e, ok := u["Email"].(map[string]any); ok {
		print("Recovery Email", str(e["Value"]))
	}
	if p, ok := u["Phone"].(map[string]any); ok {
		print("Recovery Phone", str(p["Value"]))
	}
	print("Telemetry", intStr(u["Telemetry"]))
	print("CrashReports", intStr(u["CrashReports"]))
	if hs, ok := u["HighSecurity"].(map[string]any); ok {
		v, _ := hs["Value"].(float64)
		if int(v) == 1 {
			print("High Security", "on")
		} else {
			print("High Security", "off")
		}
	}
}

func renderMailSettings(a *app.App, m map[string]any) {
	ms, _ := m["MailSettings"].(map[string]any)
	if ms == nil {
		_ = a.R.Object(m)
		return
	}
	print := func(k, v string) { fmt.Fprintf(a.R.Stdout, "%-20s %s\n", k+":", v) }
	print("Display Name", str(ms["DisplayName"]))
	print("Page Size", intStr(ms["PageSize"]))
	print("View Mode", viewMode(intOf(ms["ViewMode"])))
	print("Draft MIME Type", str(ms["DraftMIMEType"]))
	print("PM Signature", onOff(intOf(ms["PMSignature"])))
	print("Auto Save Contacts", onOff(intOf(ms["AutoSaveContacts"])))
	print("Hide Remote Images", onOff(intOf(ms["HideRemoteImages"])))
	print("Sign Outgoing", onOff(intOf(ms["Sign"])))
	print("Attach Public Key", onOff(intOf(ms["AttachPublicKey"])))
	print("Shortcuts", onOff(intOf(ms["Shortcuts"])))
	print("Delay Send", fmt.Sprintf("%ds", intOf(ms["DelaySendSeconds"])))
}

func str(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func intStr(v any) string {
	if f, ok := v.(float64); ok {
		return fmt.Sprintf("%d", int(f))
	}
	return ""
}

func intOf(v any) int {
	if f, ok := v.(float64); ok {
		return int(f)
	}
	return 0
}

func onOff(i int) string {
	if i == 1 {
		return "on"
	}
	return "off"
}

func viewMode(i int) string {
	if i == 0 {
		return "conversations"
	}
	return "messages"
}
