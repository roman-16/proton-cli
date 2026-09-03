package mail

import (
	"context"
	"fmt"
	"net/url"
	"time"

	pgp "github.com/ProtonMail/gopenpgp/v2/crypto"
	"github.com/roman-16/proton-cli/internal/account/keys"
	pgphelper "github.com/roman-16/proton-cli/internal/crypto/pgp"
	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/roman-16/proton-cli/internal/proton"
)

type rawListMessage struct {
	ID             string
	ConversationID string
	Subject        string
	Unread         int
	Time           int64
	Sender         struct{ Name, Address string }
	NumAttachments int
	LabelIDs       []string
}

func toMessage(m rawListMessage) Message {
	return Message{
		ID: m.ID, ConversationID: m.ConversationID,
		Subject: m.Subject, Unread: m.Unread, Time: m.Time,
		FromName: m.Sender.Name, FromAddress: m.Sender.Address,
		NumAttachments: m.NumAttachments, Labels: m.LabelIDs,
	}
}

func (s *Service) List(ctx context.Context, opts ListOptions) ([]Message, int, error) {
	q, err := listQuery(opts, false)
	if err != nil {
		return nil, 0, err
	}
	var r struct {
		Total    int
		Messages []rawListMessage
	}
	if err := s.C.Decode(ctx, proton.Request{Method: "GET", Path: "/mail/v4/messages", Query: q}, &r); err != nil {
		return nil, 0, err
	}
	out := make([]Message, 0, len(r.Messages))
	for _, m := range r.Messages {
		out = append(out, toMessage(m))
	}
	return out, r.Total, nil
}

// listQuery builds the one request both a listing and a filtered selection make.
// Its recipients flag renames the To field to "Recipients", as the conversations
// endpoint expects.
func listQuery(opts ListOptions, recipients bool) (url.Values, error) {
	if opts.PageSize <= 0 {
		opts.PageSize = 25
	}
	folder := opts.Folder
	if folder == "" {
		folder = "inbox"
	}
	q := url.Values{}
	q.Set("LabelID", ResolveFolder(folder))
	if opts.ID != "" {
		q.Set("ID", opts.ID)
	}
	q.Set("Sort", "Time")
	q.Set("Desc", "1")
	q.Set("Page", fmt.Sprintf("%d", opts.Page))
	q.Set("PageSize", fmt.Sprintf("%d", opts.PageSize))
	if opts.Unread {
		q.Set("Unread", "1")
	}
	if opts.Keyword != "" {
		q.Set("Keyword", opts.Keyword)
	}
	if opts.From != "" {
		q.Set("From", opts.From)
	}
	if opts.To != "" {
		if recipients {
			q.Set("Recipients", opts.To)
		} else {
			q.Set("To", opts.To)
		}
	}
	if opts.Subject != "" {
		q.Set("Subject", opts.Subject)
	}
	if opts.After != "" {
		t, err := time.Parse("2006-01-02", opts.After)
		if err != nil {
			return nil, errs.Problemf("invalid --after: %v", err)
		}
		q.Set("Begin", fmt.Sprintf("%d", t.Unix()))
	}
	if opts.Before != "" {
		t, err := time.Parse("2006-01-02", opts.Before)
		if err != nil {
			return nil, errs.Problemf("invalid --before: %v", err)
		}
		q.Set("End", fmt.Sprintf("%d", t.Unix()))
	}
	return q, nil
}

type rawMessage struct {
	ID             string
	ConversationID string
	Subject        string
	Sender         map[string]any
	ToList         []map[string]any
	CCList         []map[string]any
	BCCList        []map[string]any
	ReplyTos       []map[string]any
	Time           int64
	Body           string
	MIMEType       string
	AddressID      string
	ExternalID     string
	Flags          int64
	// Header is the original message's raw RFC 822 header block, and
	// ParsedHeaders the same thing keyed. Export reuses Header verbatim; reply
	// reads X-Original-To out of ParsedHeaders to pick the sending address.
	Header        string
	ParsedHeaders map[string]any
	Attachments   []rawAttachment
}

type rawAttachment struct {
	ID, Name, MIMEType, KeyPackets, Disposition, ContentID string
	Size                                                   int64
}

// Message flags the CLI acts on, from Proton's MESSAGE_FLAGS.
const (
	flagReceived = 1 << 0
	flagSent     = 1 << 1
)

// isSent reports whether we sent this message, which flips how a reply derives
// its recipients: answering our own sent mail addresses its original recipients.
func (m rawMessage) isSent() bool { return m.Flags&flagSent != 0 }

func (m rawMessage) parsedHeader(name string) string {
	v, ok := m.ParsedHeaders[name]
	if !ok {
		return ""
	}
	switch x := v.(type) {
	case string:
		return x
	case []any:
		if len(x) > 0 {
			if s, ok := x[0].(string); ok {
				return s
			}
		}
	}
	return ""
}

func (s *Service) decryptMessage(ctx context.Context, u *keys.Unlocked, m rawMessage) Full {
	addrKR, ok := u.AddrKR(m.AddressID)
	if !ok {
		if kr, _, err := u.FirstAddr(); err == nil {
			addrKR = kr
		}
	}
	var body string
	sig := pgphelper.Unverified
	if addrKR == nil {
		body = "(decryption failed: no address key available)"
	} else {
		// Verify the body signature against the sender's public key (their
		// own key for sent mail). No key available -> Unverified, never Invalid.
		verKR := s.senderKeyRing(ctx, senderAddress(m.Sender))
		b, v, err := decryptBody(m.Body, addrKR, verKR)
		if err != nil {
			body = "(decryption failed: " + err.Error() + ")"
		} else {
			body, sig = b, v
		}
	}
	atts := make([]Attachment, 0, len(m.Attachments))
	for _, a := range m.Attachments {
		atts = append(atts, Attachment{
			ID: a.ID, Name: a.Name, Size: a.Size, MIMEType: a.MIMEType,
			Disposition: a.Disposition, KeyPackets: a.KeyPackets,
		})
	}
	return Full{
		ID: m.ID, ConversationID: m.ConversationID, Subject: m.Subject, Sender: m.Sender,
		ToList: m.ToList, CCList: m.CCList, BCCList: m.BCCList,
		Time: m.Time, Body: body, MIMEType: m.MIMEType, AddressID: m.AddressID,
		Attachments: atts, Signature: sig,
	}
}

// senderAddress extracts the sender email from a raw message Sender map.
func senderAddress(sender map[string]any) string {
	if s, ok := sender["Address"].(string); ok {
		return s
	}
	return ""
}

// senderKeyRing fetches and caches the public key ring for an email address,
// used to verify message-body signatures. Returns nil (cached) when the
// address is empty or has no published keys (e.g. external senders).
func (s *Service) senderKeyRing(ctx context.Context, email string) *pgp.KeyRing {
	if email == "" {
		return nil
	}
	s.keyMu.Lock()
	defer s.keyMu.Unlock()
	if s.senderKeys == nil {
		s.senderKeys = map[string]*pgp.KeyRing{}
	}
	if kr, ok := s.senderKeys[email]; ok {
		return kr
	}
	var r struct {
		Address struct {
			Keys []struct{ PublicKey string }
		}
	}
	var kr *pgp.KeyRing
	if err := s.C.Decode(ctx, proton.Request{Method: "GET", Path: "/core/v4/keys/all", Query: proton.Query("Email", email)}, &r); err == nil {
		if ring, err := pgp.NewKeyRing(nil); err == nil {
			for _, k := range r.Address.Keys {
				if key, err := pgp.NewKeyFromArmored(k.PublicKey); err == nil {
					_ = ring.AddKey(key)
				}
			}
			if ring.CountEntities() > 0 {
				kr = ring
			}
		}
	}
	s.senderKeys[email] = kr
	return kr
}

func (s *Service) Read(ctx context.Context, id string) (*Full, error) {
	raw, u, err := s.messageAndKeys(ctx, id)
	if err != nil {
		return nil, err
	}
	full := s.decryptMessage(ctx, u, *raw)
	return &full, nil
}

// messageAndKeys reads a message and the keys it will be decrypted with at the
// same time, which is every read this service does: the body is encrypted, so
// one is useless without the other and neither is needed to ask for it.
func (s *Service) messageAndKeys(ctx context.Context, id string) (*rawMessage, *keys.Unlocked, error) {
	var raw *rawMessage
	var fetchErr error
	u, err := s.keys.Alongside(ctx, func(ctx context.Context) error {
		raw, fetchErr = s.fetchMessageRaw(ctx, id)
		return fetchErr
	})
	if fetchErr != nil {
		return nil, nil, s.crossTableProbe(ctx, id, fetchErr, "messages")
	}
	if err != nil {
		return nil, nil, err
	}
	return raw, u, nil
}

func (s *Service) fetchMessageRaw(ctx context.Context, id string) (*rawMessage, error) {
	var r struct{ Message rawMessage }
	if err := s.C.Decode(ctx, proton.Request{Method: "GET", Path: "/mail/v4/messages/" + id}, &r); err != nil {
		return nil, err
	}
	return &r.Message, nil
}

// AssertMessageKind probes the messages endpoint for an ID-shaped string and
// returns *WrongTableError when the ID belongs to the conversations table.
func (s *Service) AssertMessageKind(ctx context.Context, id string) error {
	err := s.C.Decode(ctx, proton.Request{Method: "GET", Path: "/mail/v4/messages/" + id}, nil)
	if err == nil {
		return nil
	}
	return s.crossTableProbe(ctx, id, err, "messages")
}
