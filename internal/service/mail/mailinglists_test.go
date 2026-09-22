package mail

import (
	"context"
	"encoding/json"
	"net/url"
	"testing"

	"github.com/roman-16/proton-cli/internal/proton"
)

// One record as Proton writes it, with the fields a listing shows and the
// headers it carries beside the methods it worked out from them.
const rawMailingListJSON = `{
  "ID": "list-1",
  "AddressID": "addr-1",
  "Name": "Trailhead Weekly",
  "SenderAddress": "news@example.com",
  "FirstReceivedTime": 1740000000,
  "LastReceivedTime": 1789000000,
  "LastReadTime": 1789000100,
  "ReceivedMessageCount": 105,
  "UnreadMessageCount": 3,
  "TrackersCount": 12,
  "ReceivedMessages": {"Total": 105, "Last30Days": 6, "Last90Days": 16},
  "MarkAsRead": true,
  "MoveToFolder": "6",
  "FilterID": "filter-1",
  "Unsubscribed": false,
  "UnsubscribedTime": null,
  "UnsubscribeMethods": {"OneClick": "OneClick"},
  "Headers": {"List-Unsubscribe": "<https://example.com/u/1>", "List-Unsubscribe-Post": "List-Unsubscribe=One-Click"}
}`

func decodeMailingList(t *testing.T) MailingList {
	t.Helper()
	var raw rawMailingList
	if err := json.Unmarshal([]byte(rawMailingListJSON), &raw); err != nil {
		t.Fatalf("decode the record: %v", err)
	}
	return toMailingList(raw)
}

func TestAMailingListCarriesWhatTheListingShows(t *testing.T) {
	l := decodeMailingList(t)

	if l.Name != "Trailhead Weekly" || l.SenderAddress != "news@example.com" {
		t.Errorf("name and sender = %q / %q", l.Name, l.SenderAddress)
	}
	if l.Unread != 3 || l.Received != 105 || l.Last30Days != 6 || l.Last90Days != 16 {
		t.Errorf("counts = %d unread, %d total, %d/%d in 30/90 days",
			l.Unread, l.Received, l.Last30Days, l.Last90Days)
	}
	if !l.Filed() {
		t.Error("a record with a destination and a mark-read flag carries a rule")
	}
	if l.FilterID != "filter-1" {
		t.Errorf("the rule names the filter that carries it, got %q", l.FilterID)
	}
	// A null timestamp is a moment nobody has reached, not a zero one to render.
	if l.UnsubscribedTime != 0 {
		t.Errorf("unsubscribed time = %d, want 0 for a list still writing", l.UnsubscribedTime)
	}
}

// The way out is chosen in the order Proton's own client prefers, and falls back
// to the header when Proton declared nothing - which is half the records on a
// real mailbox.
func TestTheWayOutIsChosenInOrder(t *testing.T) {
	oneClickHeaders := map[string]string{
		"List-Unsubscribe":      "<https://example.com/u/1>",
		"List-Unsubscribe-Post": "List-Unsubscribe=One-Click",
	}
	mailto := &mailtoUnsubscribe{ToList: []string{"leave@example.com"}}

	for _, c := range []struct {
		name    string
		methods rawUnsubscribeMethods
		headers map[string]string
		want    UnsubscribeWay
		link    string
	}{
		{"one-click first", rawUnsubscribeMethods{
			OneClick: "OneClick", Mailto: mailto, HttpClient: "https://example.com/u/1",
		}, nil, UnsubscribeOneClick, ""},
		{"then the address", rawUnsubscribeMethods{
			Mailto: mailto, HttpClient: "https://example.com/u/1",
		}, nil, UnsubscribeEmail, ""},
		{"then the page", rawUnsubscribeMethods{
			HttpClient: "https://example.com/u/1",
		}, nil, UnsubscribeLink, "https://example.com/u/1"},
		{"a one-click header where Proton declared nothing", rawUnsubscribeMethods{},
			oneClickHeaders, UnsubscribeOneClick, ""},
		{"an address with nobody to write to is no way at all", rawUnsubscribeMethods{
			Mailto: &mailtoUnsubscribe{},
		}, nil, "", ""},
		{"a header naming no form is no way at all", rawUnsubscribeMethods{},
			map[string]string{"List-Unsubscribe": "<mailto:leave@example.com>"}, "", ""},
		{"nothing offered", rawUnsubscribeMethods{}, nil, "", ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			offer := offerFrom(c.methods, c.headers)
			if offer.Way != c.want {
				t.Errorf("way = %q, want %q", offer.Way, c.want)
			}
			if offer.Link != c.link {
				t.Errorf("link = %q, want %q", offer.Link, c.link)
			}
		})
	}
}

// pagingDoer answers a listing in two pages, handing back the cursor Proton
// sends rather than a page number.
type pagingDoer struct {
	asked []url.Values
}

func (p *pagingDoer) Do(context.Context, proton.Request) (*proton.Response, error) {
	return nil, nil
}

func (p *pagingDoer) Decode(_ context.Context, r proton.Request, out any) error {
	p.asked = append(p.asked, r.Query)
	body := `{"NewsletterSubscriptions":[{"ID":"list-2","Name":"Second"}],"PageInfo":{"Total":2}}`
	if len(p.asked) == 1 {
		body = `{"NewsletterSubscriptions":[{"ID":"list-1","Name":"First"}],
		  "PageInfo":{"Total":2,"NextPage":{"QueryString":"Active=1&AnchorID=list-1&PageSize=100"}}}`
	}
	return json.Unmarshal([]byte(body), out)
}

// The walk follows the cursor the answer carries, filters and order included,
// instead of rebuilding a query of its own.
func TestTheListingFollowsProtonsCursor(t *testing.T) {
	d := &pagingDoer{}
	s := &Service{C: d}

	lists, err := s.MailingLists(context.Background(), MailingListsActive)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(lists) != 2 || lists[0].ID != "list-1" || lists[1].ID != "list-2" {
		t.Fatalf("got %d lists, in the order %v", len(lists), lists)
	}
	if len(d.asked) != 2 {
		t.Fatalf("asked %d times, want two pages", len(d.asked))
	}
	if got := d.asked[0].Get("Active"); got != "1" {
		t.Errorf("the first page asked for Active=%q, want the ones still writing", got)
	}
	if got := d.asked[1].Get("AnchorID"); got != "list-1" {
		t.Errorf("the second page asked from anchor %q, want the cursor Proton sent", got)
	}
}

// The two sets are one endpoint under one flag, and the unsubscribed one is not
// the absence of a question.
func TestTheLeftListingAsksForTheOtherSet(t *testing.T) {
	d := &pagingDoer{}
	s := &Service{C: d}
	if _, err := s.MailingLists(context.Background(), MailingListsLeft); err != nil {
		t.Fatalf("list: %v", err)
	}
	if got := d.asked[0].Get("Active"); got != "0" {
		t.Errorf("Active=%q, want the ones already left", got)
	}
}

// A rule covers the mail already here as well as what arrives, which is what
// setting one means to the person who asked for it.
func TestARuleAppliesToWhatIsAlreadyHere(t *testing.T) {
	f := &fakeDoer{}
	s := &Service{C: f}
	l := decodeMailingList(t)

	if err := s.MailingListRule(context.Background(), l, "6", true); err != nil {
		t.Fatalf("rule: %v", err)
	}
	if f.last.Path != mailingListsPath+"/list-1/filter" {
		t.Errorf("path = %s", f.last.Path)
	}
	body, ok := f.last.Body.(map[string]any)
	if !ok {
		t.Fatalf("body is %T", f.last.Body)
	}
	if body["ApplyTo"] != "All" {
		t.Errorf("ApplyTo = %v, want All", body["ApplyTo"])
	}
	if body["DestinationFolder"] != "6" || body["MarkAsRead"] != true {
		t.Errorf("rule = %v", body)
	}
}

// A message and a mailing list are the same three ways out, and differ only in
// where the answer is recorded.
func TestAMessageAndAListRecordLeavingInTheirOwnPlaces(t *testing.T) {
	f := &fakeDoer{}
	s := &Service{C: f}

	if err := s.MarkLeft(context.Background(), decodeMailingList(t)); err != nil {
		t.Fatalf("mark a list left: %v", err)
	}
	if f.last.Path != mailingListsPath+"/list-1" {
		t.Errorf("a list is recorded at %s", f.last.Path)
	}

	m := MessageMailingList{ID: "msg-1", From: "news@example.com"}
	if err := s.MarkLeft(context.Background(), m); err != nil {
		t.Fatalf("mark a message left: %v", err)
	}
	if f.last.Path != "/mail/v4/messages/mark/unsubscribed" {
		t.Errorf("a message is recorded at %s", f.last.Path)
	}
	if got := m.oneClickPath(); got != "/mail/v4/messages/msg-1/unsubscribe" {
		t.Errorf("one-click path = %s", got)
	}
}
