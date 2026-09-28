package account

import (
	"github.com/roman-16/proton-cli/internal/cli/kit"
	acctsvc "github.com/roman-16/proton-cli/internal/service/account"
	"github.com/roman-16/proton-cli/internal/ui"
	"github.com/spf13/cobra"
)

const settingsPath = "/core/v4/settings"

// specs covers the account-level settings that are one value written once:
// "Language and time", and the privacy half of "Security and privacy".
//
// The credentials are not among them, and not because they are too dangerous
// to script: they are collections beside this table, with verbs of their own.
// None of them is a value. Changing a password proves the old one and re-locks
// the keys, a second factor is minted and then confirmed, a recovery address is
// set and then verified later - so `set KEY VALUE`, which sends one field in
// one request, is the one shape none of them has.
//
// Proton Sentinel and Dark Web Monitoring are absent for a different reason:
// Proton stores them by calling enable and disable endpoints rather than
// writing a value, so each is switched by the collection it belongs to, and
// `get` reports both.
var specs = map[string]kit.Setting{
	"crash-reports": {
		Path: settingsPath + "/crashreports", Field: "CrashReports",
		Page: "Security and privacy", Desc: "Send crash reports to Proton",
		Enum: kit.OnOffNumbers(),
	},
	"date-format": {
		Path: settingsPath + "/dateformat", Field: "DateFormat",
		Page: "Language and time", Desc: "How dates are written",
		Enum: kit.Ordered("locale", "dd/mm/yyyy", "mm/dd/yyyy", "yyyy-mm-dd"),
	},
	"locale": {
		Path: settingsPath + "/locale", Field: "Locale",
		Page: "Language and time", Desc: "Interface language, e.g. en_US or de_AT",
	},
	"telemetry": {
		Path: settingsPath + "/telemetry", Field: "Telemetry",
		Page: "Security and privacy", Desc: "Send anonymous usage data to Proton",
		Enum: kit.OnOffNumbers(),
	},
	"time-format": {
		Path: settingsPath + "/timeformat", Field: "TimeFormat",
		Page: "Language and time", Desc: "Clock format",
		Enum: kit.Ordered("locale", "24h", "12h"),
	},
	// Proton numbers all seven days but accepts only these four, which is also
	// the set its own week-start selector offers.
	"week-start": {
		Path: settingsPath + "/weekstart", Field: "WeekStart",
		Page: "Language and time", Desc: "First day of the week",
		Enum: []kit.Choice{
			{Name: "locale", Value: 0},
			{Name: "monday", Value: 1},
			{Name: "saturday", Value: 6},
			{Name: "sunday", Value: 7},
		},
	},
}

// settingsView is the shape `account settings get` reports.
//
// It is a declared struct rather than Proton's raw envelope so machine output is
// snake_case like every other command's, and so numeric settings arrive as the
// names `set` accepts instead of as bare integers.
type settingsView struct {
	Locale            string `json:"locale"`
	DateFormat        string `json:"date_format"`
	TimeFormat        string `json:"time_format"`
	WeekStart         string `json:"week_start"`
	RecoveryEmail     string `json:"recovery_email,omitempty"`
	RecoveryPhone     string `json:"recovery_phone,omitempty"`
	Telemetry         string `json:"telemetry"`
	CrashReports      string `json:"crash_reports"`
	Sentinel          string `json:"sentinel"`
	DarkWebMonitoring string `json:"dark_web_monitoring"`
	BreachEmails      string `json:"breach_emails"`
	TwoPasswordMode   string `json:"two_password_mode"`
	TwoFactor         string `json:"two_factor"`
	RecoveryPhrase    string `json:"recovery_phrase"`
}

func settingsCmd() *cobra.Command {
	c := kit.Settings("account", "Account-wide preferences", specs, settingsView{}, func(c *kit.Invocation) error {
		prefs, err := c.App.Account.Preferences(c.Ctx)
		if err != nil {
			return err
		}
		view := settingsView{
			Locale:            prefs.Locale,
			DateFormat:        specs["date-format"].Name(prefs.DateFormat),
			TimeFormat:        specs["time-format"].Name(prefs.TimeFormat),
			WeekStart:         specs["week-start"].Name(prefs.WeekStart),
			RecoveryEmail:     prefs.RecoveryEmail.Address,
			RecoveryPhone:     prefs.RecoveryPhone.Number,
			Telemetry:         kit.OnOffText(boolInt(prefs.Telemetry)),
			CrashReports:      kit.OnOffText(boolInt(prefs.CrashReports)),
			Sentinel:          kit.OnOffText(boolInt(prefs.Sentinel.On)),
			DarkWebMonitoring: kit.OnOffText(boolInt(prefs.DarkWebMonitoring.On)),
			BreachEmails:      kit.OnOffText(boolInt(prefs.DarkWebMonitoring.Emails)),
			TwoPasswordMode:   kit.OnOffText(boolInt(prefs.TwoPasswordMode)),
			TwoFactor:         twoFactorText(prefs.TwoFactor),
			RecoveryPhrase:    prefs.RecoveryPhrase.Status,
		}

		return kit.Show(c, ui.RecordSpec{
			Object: view,
			Fields: []ui.Field{
				{Label: "Locale", Value: view.Locale},
				{Label: "Date Format", Value: view.DateFormat},
				{Label: "Time Format", Value: view.TimeFormat},
				{Label: "Week Start", Value: view.WeekStart},
				{Label: "Recovery Email", Value: view.RecoveryEmail},
				{Label: "Recovery Phone", Value: view.RecoveryPhone},
				{Label: "Telemetry", Value: view.Telemetry, Always: true},
				{Label: "Crash Reports", Value: view.CrashReports, Always: true},
				{Label: "Sentinel", Value: view.Sentinel, Always: true},
				{Label: "Dark Web Monitoring", Value: view.DarkWebMonitoring, Always: true},
				{Label: "Breach Emails", Value: view.BreachEmails, Always: true},
				{Label: "Two-Password Mode", Value: view.TwoPasswordMode, Always: true},
				{Label: "Two-Factor", Value: view.TwoFactor, Always: true},
				{Label: "Recovery Phrase", Value: view.RecoveryPhrase, Always: true},
			},
		})
	})
	c.AddCommand(emergencyAccessCmd(), notificationsCmd(), passwordCmd(), recoveryContactsCmd(),
		recoveryEmailCmd(), recoveryPhoneCmd(), recoveryPhraseCmd(), secondPasswordCmd(),
		securityKeysCmd(), sentinelCmd(), twoFactorCmd())
	return c
}

// twoFactorText names the two-factor methods in effect. Reporting "on" would
// hide the distinction that decides whether this CLI can sign in at all.
func twoFactorText(t acctsvc.TwoFactor) string {
	keys := len(t.SecurityKeys) > 0
	switch {
	case t.AuthenticatorApp && keys:
		return "authenticator app and security key"
	case t.AuthenticatorApp:
		return "authenticator app"
	case keys:
		return "security key"
	}
	return "off"
}
