package account

import (
	"context"
	"encoding/json"

	"github.com/roman-16/proton-cli/internal/account/keys"
	"github.com/roman-16/proton-cli/internal/cli/kit"
	"github.com/roman-16/proton-cli/internal/fetch"
	"github.com/roman-16/proton-cli/internal/proton"
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
// writing a value, and silently downgrading a security feature does not belong
// behind `set`. Both are still reported by `get`.
var specs = map[string]kit.Setting{
	"crash-reports": {
		Path: settingsPath + "/crashreports", Field: "CrashReports",
		Page: "Security and privacy", Desc: "Send crash reports to Proton",
		Enum: kit.OnOffChoices(),
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
		Enum: kit.OnOffChoices(),
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
			{Name: "locale", N: 0},
			{Name: "monday", N: 1},
			{Name: "saturday", N: 6},
			{Name: "sunday", N: 7},
		},
	},
}

// settingsView is the shape `account settings get` reports.
//
// It is a declared struct rather than Proton's raw envelope so machine output is
// snake_case like every other command's, and so numeric settings arrive as the
// names `set` accepts instead of as bare integers.
type settingsView struct {
	Locale          string `json:"locale"`
	DateFormat      string `json:"date_format"`
	TimeFormat      string `json:"time_format"`
	WeekStart       string `json:"week_start"`
	RecoveryEmail   string `json:"recovery_email,omitempty"`
	RecoveryPhone   string `json:"recovery_phone,omitempty"`
	Telemetry       string `json:"telemetry"`
	CrashReports    string `json:"crash_reports"`
	Sentinel        string `json:"sentinel"`
	TwoPasswordMode string `json:"two_password_mode"`
	TwoFactor       string `json:"two_factor"`
	RecoveryPhrase  string `json:"recovery_phrase"`
}

func settingsCmd() *cobra.Command {
	c := kit.Settings("account", "Account-wide preferences", specs, func(c *kit.Invocation) error {
		var (
			resp *proton.Response
			user struct {
				User struct{ MnemonicStatus int }
			}
		)
		// The recovery phrase is the one credential Proton keeps with the account
		// rather than with the settings, and neither answer is needed to ask for
		// the other.
		if err := fetch.Together(c.Ctx,
			func(ctx context.Context) error {
				var err error
				resp, err = c.App.API.Do(ctx, proton.Request{Method: "GET", Path: settingsPath})
				return err
			},
			func(ctx context.Context) error {
				return c.App.API.Decode(ctx, proton.Request{Method: "GET", Path: "/core/v4/users"}, &user)
			},
		); err != nil {
			return err
		}
		var env struct {
			UserSettings struct {
				Locale       string
				DateFormat   any
				TimeFormat   any
				WeekStart    any
				Email        struct{ Value string }
				Phone        struct{ Value string }
				Telemetry    any
				CrashReports any
				HighSecurity struct{ Value any }
				// Mode is 2 when the account keeps the password that signs it in
				// apart from the one that opens its keys.
				Password struct{ Mode any }
				// Proton still answers with a scalar TwoFactor beside this, and an
				// untagged field would bind to that one and fail to decode.
				TwoFactor struct{ Enabled any } `json:"2FA"`
			}
		}
		if err := json.Unmarshal(resp.Body, &env); err != nil {
			return err
		}
		s := env.UserSettings

		view := settingsView{
			Locale:          s.Locale,
			DateFormat:      specs["date-format"].Name(s.DateFormat),
			TimeFormat:      specs["time-format"].Name(s.TimeFormat),
			WeekStart:       specs["week-start"].Name(s.WeekStart),
			RecoveryEmail:   s.Email.Value,
			RecoveryPhone:   s.Phone.Value,
			Telemetry:       kit.OnOffText(kit.IntOf(s.Telemetry)),
			CrashReports:    kit.OnOffText(kit.IntOf(s.CrashReports)),
			Sentinel:        kit.OnOffText(kit.IntOf(s.HighSecurity.Value)),
			TwoPasswordMode: kit.OnOffText(twoPasswordOn(kit.IntOf(s.Password.Mode))),
			TwoFactor:       twoFactorText(kit.IntOf(s.TwoFactor.Enabled)),
			RecoveryPhrase:  acctsvc.PhraseStatus(user.User.MnemonicStatus),
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
				{Label: "Two-Password Mode", Value: view.TwoPasswordMode, Always: true},
				{Label: "Two-Factor", Value: view.TwoFactor, Always: true},
				{Label: "Recovery Phrase", Value: view.RecoveryPhrase, Always: true},
			},
		})
	})
	c.AddCommand(passwordCmd(), recoveryEmailCmd(), recoveryPhoneCmd(), recoveryPhraseCmd(),
		secondPasswordCmd(), securityKeysCmd(), twoFactorCmd())
	return c
}

// twoPasswordOn turns Proton's numbered password mode into the on and off this
// row is read among. Reporting "one" and "two" would make it the only line in
// the block whose value has to be read twice.
func twoPasswordOn(mode int) int {
	if mode == keys.PasswordModeTwo {
		return 1
	}
	return 0
}

// twoFactorText names the two-factor methods in effect. Proton reports them as a
// bitfield; reporting "on" would hide the distinction that decides whether this
// CLI can sign in at all.
func twoFactorText(enabled int) string {
	switch {
	case enabled == 0:
		return "off"
	case enabled&1 != 0 && enabled&2 != 0:
		return "authenticator app and security key"
	case enabled&1 != 0:
		return "authenticator app"
	case enabled&2 != 0:
		return "security key"
	}
	return "on"
}
