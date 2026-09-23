package mail

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/roman-16/proton-cli/internal/proton"
)

type askedFor struct {
	answers
	asked []proton.Request
}

func (c *askedFor) Decode(ctx context.Context, r proton.Request, out any) error {
	c.asked = append(c.asked, r)
	return c.answers.Decode(ctx, r, out)
}

func TestCountsFollowTheRowsAndAPlaceProtonLeavesOutCountsZero(t *testing.T) {
	counts, _ := json.Marshal(map[string]any{"Counts": []map[string]any{
		{"LabelID": labelInbox, "Total": 312, "Unread": 12},
		{"LabelID": "f3", "Total": 143, "Unread": 0},
		{"LabelID": "l1", "Total": 31, "Unread": 2},
	}})
	s := New(answers{
		"GET /mail/v4/settings":                                  `{"MailSettings":{"MailCategoryView":false,"AlmostAllMail":0}}`,
		"GET /core/v4/labels?Type=1":                             ownLabels,
		"GET /core/v4/labels?Type=3":                             ownFolders,
		"GET /mail/v4/messages/count?OnlyInInboxForCategories=1": string(counts),
	}, testKeys(nil))
	rows, err := s.Counts(context.Background(), false, "")
	if err != nil {
		t.Fatalf("Counts: %v", err)
	}
	got := map[string][2]int{}
	for _, r := range rows {
		got[r.Name] = [2]int{r.Unread, r.Total}
	}
	for name, want := range map[string][2]int{
		"inbox": {12, 312}, "Receipts": {0, 143}, "Work": {2, 31}, "drafts": {0, 0}, "Clients": {0, 0},
	} {
		if got[name] != want {
			t.Errorf("%s = %v, want %v", name, got[name], want)
		}
	}
	if len(rows) != 14 {
		t.Errorf("%d rows, want every built-in folder and every folder and label of yours", len(rows))
	}
}

func TestThreadsAreCountedByTheConversationsEndpoint(t *testing.T) {
	c := &askedFor{answers: answers{
		"GET /mail/v4/conversations/count?OnlyInInboxForCategories=1": `{"Counts":[{"LabelID":"0","Total":140,"Unread":9}]}`,
	}}
	rows, err := New(c, testKeys(nil)).Counts(context.Background(), true, "inbox")
	if err != nil {
		t.Fatalf("Counts: %v", err)
	}
	if want := []MailboxCount{{ID: labelInbox, Name: "inbox", Unread: 9, Total: 140}}; !reflect.DeepEqual(rows, want) {
		t.Errorf("rows = %+v, want %+v", rows, want)
	}
	if len(c.asked) != 1 {
		t.Errorf("a built-in folder cost %d requests, want the count alone", len(c.asked))
	}
}
