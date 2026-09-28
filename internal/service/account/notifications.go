package account

import (
	"context"

	"github.com/roman-16/proton-cli/internal/proton"
)

type Notification struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Detail      string `json:"detail"`
	On          bool   `json:"on"`

	field string
	bit   int
}

const (
	newsFeatures      = 1 << 1
	newsDailyMail     = 1 << 6
	newsInbox         = 1 << 9
	newsVPN           = 1 << 10
	newsDrive         = 1 << 11
	newsPass          = 1 << 12
	newsProductEmails = newsInbox | newsVPN | newsDrive | newsPass
)

var notifications = []Notification{
	{Name: "announcements", field: "Announcements", bit: 1 << 0,
		Description: "Proton important announcements", Detail: "2-4 emails per year"},
	{Name: "business", field: "Business", bit: 1 << 4,
		Description: "Proton for Business communications", Detail: "1-2 emails per month"},
	{Name: "newsletter", field: "Newsletter", bit: 1 << 2,
		Description: "Proton newsletter",
		Detail:      "1 email per month. Get the latest privacy news and what is going on in the Proton universe."},
	{Name: "offers", field: "Offers", bit: 1 << 5,
		Description: "Proton offers and promotions", Detail: "1 email per quarter"},
	{Name: "welcome", field: "Onboarding", bit: 1 << 7,
		Description: "Proton welcome emails", Detail: "During your first month of using a Proton product"},
	{Name: "surveys", field: "UserSurveys", bit: 1 << 8,
		Description: "Proton user survey", Detail: "Participate in surveys to improve Proton services"},
	{Name: "mail", field: "InboxNews", bit: newsInbox,
		Description: "Proton Mail and Calendar product updates", Detail: "1 email per month"},
	{Name: "drive", field: "DriveNews", bit: newsDrive,
		Description: "Proton Drive product updates", Detail: "4-6 emails per year"},
	{Name: "pass", field: "PassNews", bit: newsPass,
		Description: "Proton Pass product updates", Detail: "4-6 emails per year"},
	{Name: "wallet", field: "WalletNews", bit: 1 << 13,
		Description: "Proton Wallet product updates", Detail: "4-6 emails per year"},
	{Name: "vpn", field: "VpnNews", bit: newsVPN,
		Description: "Proton VPN product updates", Detail: "4-6 emails per year"},
	{Name: "lumo", field: "LumoNews", bit: 1 << 15,
		Description: "Lumo product updates", Detail: "4-6 emails per year"},
	{Name: "meet", field: "MeetNews", bit: 1 << 16,
		Description: "Proton Meet product updates", Detail: "4-6 emails per year"},
	{Name: "recovery-checklist", field: "AccountRecovery", bit: 1 << 17,
		Description: "Recovery checklist",
		Detail:      "1-4 emails per year. Reminders to keep your recovery methods up to date."},
	{Name: "in-app", field: "InAppNotifications", bit: 1 << 14,
		Description: "In-app notifications",
		Detail: "Disabling this means you won't get in-app notifications about promotions, " +
			"new features, new Proton services, and reminders. Critical account notifications are still delivered."},
}

type News struct {
	Bits          int
	RecoveryEmail string
}

func (n News) Notifications() []Notification {
	out := make([]Notification, len(notifications))
	for i, row := range notifications {
		row.On = n.Bits&row.bit != 0
		out[i] = row
	}
	return out
}

func (n News) DailyMail() bool { return n.RecoveryEmail != "" && n.Bits&newsDailyMail != 0 }

func (s *Service) News(ctx context.Context) (News, error) {
	var r struct {
		UserSettings struct {
			News  int
			Email struct{ Value string }
		}
	}
	if err := s.C.Decode(ctx, proton.Request{Method: "GET", Path: settingsPath}, &r); err != nil {
		return News{}, err
	}
	return News{Bits: r.UserSettings.News, RecoveryEmail: r.UserSettings.Email.Value}, nil
}

func (s *Service) SetNotifications(ctx context.Context, current News, rows []Notification, on bool) error {
	body := map[string]bool{}
	bits := current.Bits
	for _, row := range rows {
		body[row.field] = on
		if on {
			bits |= row.bit
		} else {
			bits &^= row.bit
		}
	}
	if products := bits&newsProductEmails != 0; products != (current.Bits&newsFeatures != 0) {
		body["Features"] = products
	}
	_, err := s.C.Do(ctx, proton.Request{Method: "PATCH", Path: "/core/v4/settings/news", Body: body})
	return err
}
