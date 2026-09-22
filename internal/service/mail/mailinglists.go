package mail

import (
	"context"
	"log/slog"
	"net/url"
	"strconv"
	"strings"

	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/roman-16/proton-cli/internal/proton"
)

// The senders that write to you as a list.
//
// Proton reads the List- headers off incoming mail and keeps one record per
// sender that writes as a list: how much it has sent, how much of it is still
// unread, and whatever standing rule the account has put on it. It answers the
// one question a mailbox cannot - who is filling it - which is why the web
// client gives it a view of its own rather than a filter over messages.

const mailingListsPath = "/mail/v4/newsletter-subscriptions"

// mailingListPageSize is how many records one request asks for. Proton answers
// with a cursor for the next page rather than a page number, so this is the only
// number the walk sets.
const mailingListPageSize = 100

// MailingList is one sender that mails you as a list.
type MailingList struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	SenderAddress string `json:"sender_address"`
	// AddressID is the address of yours the list writes to, which is the address
	// an unsubscribe email has to leave from for the list to recognise it.
	AddressID string `json:"-"`

	Unread     int `json:"unread"`
	Received   int `json:"received"`
	Last30Days int `json:"received_30_days"`
	Last90Days int `json:"received_90_days"`
	Trackers   int `json:"trackers,omitempty"`

	FirstReceived int64 `json:"first_received,omitempty"`
	LastReceived  int64 `json:"last_received,omitempty"`
	LastRead      int64 `json:"last_read,omitempty"`

	// MarkAsRead and MoveToFolder are the standing rule: what happens to this
	// list's mail as it arrives. Proton carries the rule out with a filter of its
	// own, which is what FilterID names and what turns the rule off again.
	MarkAsRead   bool   `json:"mark_as_read,omitempty"`
	MoveToFolder string `json:"move_to_folder,omitempty"`
	FilterID     string `json:"filter_id,omitempty"`

	Unsubscribed     bool  `json:"unsubscribed,omitempty"`
	UnsubscribedTime int64 `json:"unsubscribed_time,omitempty"`

	// Way is how this list can be asked to stop, and Link is the page to open
	// when that way is a link.
	Way  UnsubscribeWay `json:"unsubscribe,omitempty"`
	Link string         `json:"unsubscribe_link,omitempty"`

	mailto *mailtoUnsubscribe
}

func (l MailingList) Offer() UnsubscribeOffer {
	return UnsubscribeOffer{Way: l.Way, Link: l.Link, mailto: l.mailto}
}
func (l MailingList) Named() string     { return l.Name }
func (l MailingList) Sender() string    { return l.SenderAddress }
func (l MailingList) addressID() string { return l.AddressID }
func (l MailingList) oneClickPath() string {
	return mailingListsPath + "/" + l.ID + "/unsubscribe"
}

func (l MailingList) recordLeft(ctx context.Context, s *Service) error {
	return s.C.Decode(ctx, proton.Request{
		Method: "POST", Path: mailingListsPath + "/" + l.ID,
		Body: map[string]any{"Unsubscribed": true},
	}, nil)
}

// MessageMailingList is the list one message came from, addressed by the message
// rather than by the list: the banner on an open message, rather than the row in
// the listing of who writes to you.
type MessageMailingList struct {
	ID        string
	From      string
	AddressID string
	offer     UnsubscribeOffer
}

func (m MessageMailingList) Offer() UnsubscribeOffer { return m.offer }
func (m MessageMailingList) Named() string           { return m.From }
func (m MessageMailingList) Sender() string          { return m.From }
func (m MessageMailingList) addressID() string       { return m.AddressID }
func (m MessageMailingList) oneClickPath() string {
	return "/mail/v4/messages/" + m.ID + "/unsubscribe"
}

func (m MessageMailingList) recordLeft(ctx context.Context, s *Service) error {
	return s.C.Decode(ctx, proton.Request{
		Method: "PUT", Path: "/mail/v4/messages/mark/unsubscribed",
		Body: map[string]any{"IDs": []string{m.ID}},
	}, nil)
}

// MailingListOf is the list a message came from, read off the message's own
// headers.
func (s *Service) MailingListOf(ctx context.Context, id string) (MessageMailingList, error) {
	m, err := s.fetchMessageRaw(ctx, id)
	if err != nil {
		return MessageMailingList{}, err
	}
	return MessageMailingList{
		ID: m.ID, From: senderAddress(m.Sender), AddressID: m.AddressID,
		offer: offerFrom(m.UnsubscribeMethods, listHeaders(m.ParsedHeaders)),
	}, nil
}

// listHeaders are the List- headers as the record carries them, out of the
// parsed header block a message arrives with.
func listHeaders(parsed map[string]any) map[string]string {
	out := make(map[string]string, 2)
	for _, name := range []string{"List-Unsubscribe", "List-Unsubscribe-Post"} {
		if v, ok := parsed[name].(string); ok {
			out[name] = v
		}
	}
	return out
}

// UnsubscribeOffer is what a list offers as a way out: which way, and what that
// way needs to be carried out.
type UnsubscribeOffer struct {
	Way    UnsubscribeWay
	Link   string
	mailto *mailtoUnsubscribe
}

// UnsubscribeTarget is something a mailing list writes: the list itself, or one
// message from it. Proton's own client offers the same three ways in both
// places, and only where the answer is recorded differs - which is the whole of
// what an implementation says beyond its own name.
type UnsubscribeTarget interface {
	// Offer is the way out this thing carries.
	Offer() UnsubscribeOffer
	// Named is what to call it on screen, and Sender is the address to keep out
	// when there is no way to leave at all.
	Named() string
	Sender() string

	// addressID is the address of yours the list writes to, which an unsubscribe
	// email has to leave from for the list to recognise it.
	addressID() string
	// oneClickPath is the endpoint that asks Proton to submit the list's form.
	oneClickPath() string
	// recordLeft writes down that the list has been left, for the two ways Proton
	// does not carry out itself.
	recordLeft(context.Context, *Service) error
}

// UnsubscribeWay is how a list may be asked to stop. A list offers one of three,
// or none at all, and the three are not interchangeable: one is a request Proton
// makes on your behalf, one is an email that has to leave your account, and one
// is a page somebody has to open.
type UnsubscribeWay string

const (
	// UnsubscribeOneClick is the List-Unsubscribe-Post form, which Proton
	// submits for you.
	UnsubscribeOneClick UnsubscribeWay = "one-click"
	// UnsubscribeEmail is an address the list wants a message at.
	UnsubscribeEmail UnsubscribeWay = "email"
	// UnsubscribeLink is a page, and nothing but a browser can visit it.
	UnsubscribeLink UnsubscribeWay = "link"
)

// MailingListScope is which of the two sets a listing asks for. Proton keeps
// them apart, and the web client shows them as two tabs.
type MailingListScope string

const (
	// MailingListsActive are the ones still writing to you.
	MailingListsActive MailingListScope = "active"
	// MailingListsLeft are the ones you have unsubscribed from.
	MailingListsLeft MailingListScope = "unsubscribed"
	// MailingListsAll is both, which is what a reference resolves against: a
	// list you left is still a thing to look at and to forget.
	MailingListsAll MailingListScope = "all"
)

type mailtoUnsubscribe struct {
	Subject string
	Body    string
	ToList  []string
}

// rawUnsubscribeMethods is what Proton worked out about a list's own headers,
// on a subscription record and on a message alike.
type rawUnsubscribeMethods struct {
	Mailto     *mailtoUnsubscribe
	HttpClient string
	OneClick   string
}

type rawMailingList struct {
	ID            string
	AddressID     string
	Name          string
	SenderAddress string

	FirstReceivedTime    int64
	LastReceivedTime     int64
	LastReadTime         int64
	ReceivedMessageCount int
	UnreadMessageCount   int
	TrackersCount        int
	ReceivedMessages     struct {
		Total, Last30Days, Last90Days int
	}

	MarkAsRead   bool
	MoveToFolder string
	FilterID     string

	Unsubscribed     bool
	UnsubscribedTime int64

	UnsubscribeMethods rawUnsubscribeMethods
	// Headers are the List- headers of the last message, which Proton passes
	// through beside the methods it worked out from them.
	Headers map[string]string
}

func toMailingList(r rawMailingList) MailingList {
	offer := offerFrom(r.UnsubscribeMethods, r.Headers)
	l := MailingList{
		ID: r.ID, Name: r.Name, SenderAddress: r.SenderAddress, AddressID: r.AddressID,
		Unread: r.UnreadMessageCount, Received: r.ReceivedMessages.Total,
		Last30Days: r.ReceivedMessages.Last30Days, Last90Days: r.ReceivedMessages.Last90Days,
		Trackers:      r.TrackersCount,
		FirstReceived: r.FirstReceivedTime, LastReceived: r.LastReceivedTime,
		LastRead:   r.LastReadTime,
		MarkAsRead: r.MarkAsRead, MoveToFolder: r.MoveToFolder, FilterID: r.FilterID,
		Unsubscribed: r.Unsubscribed, UnsubscribedTime: r.UnsubscribedTime,
		Way: offer.Way, Link: offer.Link, mailto: offer.mailto,
	}
	if l.Received == 0 {
		l.Received = r.ReceivedMessageCount
	}
	return l
}

// offerFrom is the way out, chosen in the order Proton's own client prefers: the
// request it makes for you, then the message you send, then the page you open.
//
// The last case reads the header the record carries rather than the method
// Proton declared. On a real mailbox half the records arrive with no method at
// all and a perfectly good one-click header beside them, so refusing on the
// declared set alone would decline lists that can be left. Asking and being
// refused costs one request and says which it was.
func offerFrom(m rawUnsubscribeMethods, headers map[string]string) UnsubscribeOffer {
	switch {
	case m.OneClick != "":
		return UnsubscribeOffer{Way: UnsubscribeOneClick}
	case m.Mailto != nil && len(m.Mailto.ToList) > 0:
		return UnsubscribeOffer{Way: UnsubscribeEmail, mailto: m.Mailto}
	case m.HttpClient != "":
		return UnsubscribeOffer{Way: UnsubscribeLink, Link: m.HttpClient}
	case oneClickHeader(headers):
		return UnsubscribeOffer{Way: UnsubscribeOneClick}
	}
	return UnsubscribeOffer{}
}

func oneClickHeader(headers map[string]string) bool {
	if !strings.Contains(headers["List-Unsubscribe-Post"], "One-Click") {
		return false
	}
	return strings.Contains(headers["List-Unsubscribe"], "http")
}

// Filed reports whether the list carries a standing rule.
func (l MailingList) Filed() bool { return l.MarkAsRead || l.MoveToFolder != "" }

// MailingLists reads the whole set, following Proton's cursor.
//
// The endpoint pages by anchor rather than by number: each answer carries the
// query string for the next one, filters and sort included, so the walk hands
// back what it was given instead of rebuilding it.
func (s *Service) MailingLists(ctx context.Context, scope MailingListScope) ([]MailingList, error) {
	if scope == MailingListsAll {
		active, err := s.MailingLists(ctx, MailingListsActive)
		if err != nil {
			return nil, err
		}
		left, err := s.MailingLists(ctx, MailingListsLeft)
		if err != nil {
			return nil, err
		}
		return append(active, left...), nil
	}

	q := url.Values{}
	q.Set("PageSize", strconv.Itoa(mailingListPageSize))
	q.Set("Active", map[bool]string{true: "1", false: "0"}[scope == MailingListsActive])
	q.Set("Spam", "0")
	q.Set("Sort[LastReceivedTime]", "DESC")
	q.Set("Sort[ID]", "DESC")

	var out []MailingList
	err := proton.Pages(ctx, func(ctx context.Context, _ int) (bool, error) {
		var r struct {
			NewsletterSubscriptions []rawMailingList
			PageInfo                struct {
				Total    int
				NextPage *struct{ QueryString string }
			}
		}
		if err := s.C.Decode(ctx, proton.Request{
			Method: "GET", Path: mailingListsPath, Query: q,
		}, &r); err != nil {
			return false, err
		}
		for _, raw := range r.NewsletterSubscriptions {
			out = append(out, toMailingList(raw))
		}
		if r.PageInfo.NextPage == nil || r.PageInfo.NextPage.QueryString == "" {
			return false, nil
		}
		next, err := url.ParseQuery(r.PageInfo.NextPage.QueryString)
		if err != nil {
			return false, err
		}
		q = next
		return true, nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// UnsubscribeOneClick asks Proton to submit the list's own form, which is the
// one way of the three that ends here: Proton is the party the list already
// knows, and it needs nothing from this machine.
func (s *Service) UnsubscribeOneClick(ctx context.Context, t UnsubscribeTarget) error {
	slog.DebugContext(ctx, "mail: asking a mailing list to stop",
		"unsubscribe", string(UnsubscribeOneClick))
	if err := s.C.Decode(ctx, proton.Request{
		Method: "POST", Path: t.oneClickPath(),
	}, nil); err != nil {
		return errs.Naming(t.Named(), err)
	}
	return nil
}

// UnsubscribeByEmail sends the message the list asked for, from the address the
// list writes to.
//
// The subject and body are the list's own where it named them, and the wording
// every client falls back to where it did not - which is what the robots on the
// other end are written to read.
func (s *Service) UnsubscribeByEmail(ctx context.Context, t UnsubscribeTarget) error {
	m := t.Offer().mailto
	if m == nil || len(m.ToList) == 0 {
		return errs.Naming(t.Named(), errs.Problemf("%s named no address to unsubscribe at.", t.Named()))
	}
	slog.DebugContext(ctx, "mail: asking a mailing list to stop",
		"unsubscribe", string(UnsubscribeEmail))
	sender, err := s.ResolveSender(ctx, SenderRequest{ParentAddressID: t.addressID()})
	if err != nil {
		return err
	}
	to := make([]Recipient, 0, len(m.ToList))
	for _, addr := range m.ToList {
		to = append(to, Recipient{Address: addr})
	}
	c := Content{
		From: sender, To: to,
		Subject: firstNonEmpty(m.Subject, "Unsubscribe"),
		Body:    firstNonEmpty(m.Body, "Please, unsubscribe me"),
	}
	if _, err := s.Send(ctx, c, Delivery{}); err != nil {
		return errs.Naming(t.Named(), err)
	}
	if err := t.recordLeft(ctx, s); err != nil {
		return errs.Naming(t.Named(), err)
	}
	return nil
}

// MarkLeft records that a list has been left, which is what the two ways Proton
// does not carry out itself end with.
func (s *Service) MarkLeft(ctx context.Context, t UnsubscribeTarget) error {
	slog.DebugContext(ctx, "mail: asking a mailing list to stop",
		"unsubscribe", string(t.Offer().Way))
	if err := t.recordLeft(ctx, s); err != nil {
		return errs.Naming(t.Named(), err)
	}
	return nil
}

// MailingListRule sets what happens to a list's mail: where it goes, and whether
// it arrives read.
//
// It applies to the mail already here as well as to what comes next, which is
// what "this list goes to Archive" means to the person saying it. Proton keeps
// the standing half as a filter of its own, so turning it off again is that
// filter, which the list carries the ID of.
func (s *Service) MailingListRule(ctx context.Context, l MailingList, folderID string, markRead bool) error {
	body := map[string]any{"ApplyTo": "All", "MarkAsRead": markRead}
	if folderID != "" {
		body["DestinationFolder"] = folderID
	}
	if err := s.C.Decode(ctx, proton.Request{
		Method: "POST", Path: mailingListsPath + "/" + l.ID + "/filter", Body: body,
	}, nil); err != nil {
		return errs.Naming(l.Name, err)
	}
	return nil
}

// MailingListForget drops the entry. It unsubscribes from nothing and the list
// comes back the next time it writes, which is what makes it the tidy-up rather
// than a removal.
func (s *Service) MailingListForget(ctx context.Context, l MailingList) error {
	if err := s.C.Decode(ctx, proton.Request{
		Method: "DELETE", Path: mailingListsPath + "/" + l.ID,
	}, nil); err != nil {
		return errs.Naming(l.Name, err)
	}
	return nil
}

func firstNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}
