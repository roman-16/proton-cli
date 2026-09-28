package account

import (
	"context"
	"fmt"
	"testing"
)

func TestEveryNotificationIsItsOwnBit(t *testing.T) {
	seen := map[int]string{}
	for _, n := range notifications {
		if other, taken := seen[n.bit]; taken {
			t.Errorf("%s and %s share a bit", n.Name, other)
		}
		seen[n.bit] = n.Name
		if n.bit == newsFeatures || n.bit == newsDailyMail {
			t.Errorf("%s is a bit the web keeps off this page", n.Name)
		}
	}
	if len(notifications) != 15 {
		t.Errorf("%d notifications, want the 15 the web's page offers", len(notifications))
	}
}

func TestProductAnnouncementsFollowTheProductUpdates(t *testing.T) {
	byName := map[string]Notification{}
	for _, n := range notifications {
		byName[n.Name] = n
	}
	for _, tc := range []struct {
		news int
		rows []string
		on   bool
		want string
	}{
		{news: newsFeatures | newsDrive, rows: []string{"drive"}, on: false, want: "map[DriveNews:false Features:false]"},
		{news: newsFeatures | newsDrive | newsPass, rows: []string{"drive"}, on: false, want: "map[DriveNews:false]"},
		{news: 0, rows: []string{"mail"}, on: true, want: "map[Features:true InboxNews:true]"},
		{news: 0, rows: []string{"wallet"}, on: true, want: "map[WalletNews:true]"},
		{news: newsFeatures | newsVPN, rows: []string{"offers", "vpn"}, on: false, want: "map[Features:false Offers:false VpnNews:false]"},
	} {
		a := &answers{}
		var rows []Notification
		for _, name := range tc.rows {
			rows = append(rows, byName[name])
		}
		if err := New(a, nil).SetNotifications(context.Background(), News{Bits: tc.news}, rows, tc.on); err != nil {
			t.Fatal(err)
		}
		sent := a.sent[0]
		if sent.Method != "PATCH" || sent.Path != "/core/v4/settings/news" {
			t.Errorf("sent %s %s", sent.Method, sent.Path)
		}
		if got := fmt.Sprint(sent.Body); got != tc.want {
			t.Errorf("%v %v from %b: sent %s, want %s", tc.rows, tc.on, tc.news, got, tc.want)
		}
	}
}

func TestDailyMailNeedsARecoveryAddress(t *testing.T) {
	if (News{Bits: newsDailyMail}).DailyMail() {
		t.Error("daily mail reads as on with nowhere to send it")
	}
	if !(News{Bits: newsDailyMail, RecoveryEmail: "jane.roe@example.com"}).DailyMail() {
		t.Error("daily mail reads as off with the bit set and an address to send it to")
	}
}
