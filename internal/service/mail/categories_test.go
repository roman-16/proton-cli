package mail

import (
	"context"
	"reflect"
	"testing"

	"github.com/roman-16/proton-cli/internal/proton"
)

const systemFolderAnswer = `{"Labels":[` +
	`{"ID":"0","Name":"Inbox","Display":1,"Notify":1},` +
	`{"ID":"26","Name":"Transactions","Color":"#e8","Display":0,"Notify":1},` +
	`{"ID":"20","Name":"Social","Color":"#0b","Display":1,"Notify":0},` +
	`{"ID":"24","Name":"Primary","Color":"#6d","Display":1,"Notify":1}]}`

func TestCategoriesComeInTheWebsOrderWithAHiddenOneQuiet(t *testing.T) {
	s := New(answers{"GET /core/v4/labels?Type=4": systemFolderAnswer}, testKeys(nil))
	got, err := s.Categories(context.Background())
	if err != nil {
		t.Fatalf("Categories: %v", err)
	}
	var rows [][3]any
	for _, c := range got {
		rows = append(rows, [3]any{c.Name, c.Shown, c.Notify})
	}
	want := [][3]any{{"primary", true, true}, {"social", true, false}, {"transactions", false, false}}
	if !reflect.DeepEqual(rows, want) {
		t.Errorf("categories = %v, want %v", rows, want)
	}
}

type recorder struct {
	answers
	sent []proton.Request
}

func (r *recorder) Decode(ctx context.Context, req proton.Request, out any) error {
	if req.Method != "GET" {
		r.sent = append(r.sent, req)
		return nil
	}
	return r.answers.Decode(ctx, req, out)
}

func TestACategoryChangeKeepsWhatItDoesNotChange(t *testing.T) {
	r := &recorder{answers: answers{"GET /core/v4/labels?Type=4": systemFolderAnswer}}
	s := New(r, testKeys(nil))
	categories, err := s.Categories(context.Background())
	if err != nil {
		t.Fatalf("Categories: %v", err)
	}
	transactions := categories[2]
	if err := s.ShowCategory(context.Background(), transactions, true); err != nil {
		t.Fatalf("ShowCategory: %v", err)
	}
	if err := s.NotifyCategory(context.Background(), categories[1], true); err != nil {
		t.Fatalf("NotifyCategory: %v", err)
	}
	want := []proton.Request{
		{Method: "PUT", Path: "/core/v4/labels/26", Body: map[string]any{
			"Name": "Transactions", "Color": "#e8", "Display": 1, "Notify": 1,
		}},
		{Method: "PUT", Path: "/core/v4/labels/20", Body: map[string]any{
			"Name": "Social", "Color": "#0b", "Display": 1, "Notify": 1,
		}},
	}
	if !reflect.DeepEqual(r.sent, want) {
		t.Errorf("sent %#v, want %#v", r.sent, want)
	}
}

func TestPrimaryIsNamedByItsNameOrItsID(t *testing.T) {
	for ref, want := range map[string]bool{"primary": true, "Primary": true, "24": true, "social": false, "20": false} {
		if got := IsPrimary(ref); got != want {
			t.Errorf("IsPrimary(%q) = %v, want %v", ref, got, want)
		}
	}
}
