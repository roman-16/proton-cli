// Package mail provides Proton Mail operations.
package mail

import (
	"context"
	"errors"
	"fmt"
	"sync"

	pgp "github.com/ProtonMail/gopenpgp/v2/crypto"
	"github.com/roman-16/proton-cli/internal/account/keys"
	pgphelper "github.com/roman-16/proton-cli/internal/crypto/pgp"
	"github.com/roman-16/proton-cli/internal/proton"
	"github.com/roman-16/proton-cli/internal/ref"
)

// WrongTableError signals that an ID-shaped REF was passed to the wrong
// endpoint family (a conversation ID into the messages tree, or vice versa).
// The cli layer catches this to emit a redirect hint and exit 3.
type WrongTableError struct {
	// Kind is what the ID actually is ("message" or "conversation") - the
	// OTHER table from the one the user invoked.
	Kind string
	ID   string
}

func (e *WrongTableError) Error() string {
	return fmt.Sprintf("that ID is a %s, not a %s", e.Kind, OppositeKind(e.Kind))
}
func (e *WrongTableError) ExitCode() int { return 3 }

func OppositeKind(k string) string {
	if k == "conversation" {
		return "message"
	}
	return "conversation"
}

// Built-in Proton system-label IDs. The mutation endpoints reference some of
// these directly; the mailbox lookup in mailboxes.go is built from the same
// constants so the two can never drift.
const (
	labelInbox     = "0"
	labelTrash     = "3"
	labelSpam      = "4"
	labelAllMail   = "5"
	labelArchive   = "6"
	labelSent      = "7"
	labelDrafts    = "8"
	labelStarred   = "10"
	labelScheduled = "12"
	labelSnoozed   = "16"
	// Proton's inbox categories, which the web shows as tabs across the top.
	labelSocial       = "20"
	labelPromotions   = "21"
	labelUpdates      = "22"
	labelNewsletters  = "25"
	labelTransactions = "26"
)

type Service struct {
	C    proton.Doer
	keys keys.Get

	// senderKeys caches fetched sender public key rings (per email) for body
	// signature verification. A nil entry means "no key available" - cached so
	// we don't refetch on every message in a conversation.
	keyMu      sync.Mutex
	senderKeys map[string]*pgp.KeyRing

	// Mail settings are read once per run, for the outgoing signature's Proton
	// footer. See signature.go.
	settingsOnce  sync.Once
	settingsCache mailSettings
	settingsErr   error
}

func New(c proton.Doer, k keys.Get) *Service { return &Service{C: c, keys: k} }

type Message struct {
	ID             string   `json:"id"`
	ConversationID string   `json:"conversation_id,omitempty"`
	Subject        string   `json:"subject"`
	FromName       string   `json:"from_name,omitempty"`
	FromAddress    string   `json:"from_address"`
	Time           int64    `json:"time"`
	Unread         int      `json:"unread"`
	NumAttachments int      `json:"num_attachments"`
	Labels         []string `json:"labels"`
}

// Starred reports whether the message carries the Starred label. A star is a
// label like any other, so this is a lookup rather than a field of its own.
func (m Message) Starred() bool { return hasLabel(m.Labels, labelStarred) }

// Full carries a decrypted body, unlike the raw API envelope.
type Full struct {
	ID             string                 `json:"id"`
	ConversationID string                 `json:"conversation_id,omitempty"`
	Subject        string                 `json:"subject"`
	Sender         map[string]any         `json:"from"`
	ToList         []map[string]any       `json:"to"`
	CCList         []map[string]any       `json:"cc"`
	BCCList        []map[string]any       `json:"bcc"`
	Time           int64                  `json:"time,omitempty"`
	Body           string                 `json:"body"`
	MIMEType       string                 `json:"mime_type"`
	AddressID      string                 `json:"address_id"`
	Attachments    []Attachment           `json:"attachments"`
	Signature      pgphelper.VerifyResult `json:"signature,omitempty"`
}

type Conversation struct {
	ID             string           `json:"id"`
	Subject        string           `json:"subject"`
	NumMessages    int              `json:"num_messages"`
	NumUnread      int              `json:"num_unread"`
	NumAttachments int              `json:"num_attachments"`
	Time           int64            `json:"time"`
	Senders        []map[string]any `json:"senders"`
	Recipients     []map[string]any `json:"recipients"`
	Labels         []string         `json:"labels"`
}

// Starred reports whether the thread carries the Starred label.
func (c Conversation) Starred() bool { return hasLabel(c.Labels, labelStarred) }

func hasLabel(labels []string, want string) bool {
	for _, l := range labels {
		if l == want {
			return true
		}
	}
	return false
}

type ConversationFull struct {
	Conversation Conversation `json:"conversation"`
	Messages     []Full       `json:"messages"`
}

// Attachment's Disposition is "inline" for HTML-referenced parts (e.g.
// signature graphics); empty or any other value counts as a real attachment.
type Attachment struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Size        int64  `json:"size"`
	MIMEType    string `json:"mime_type"`
	Disposition string `json:"disposition"`
	KeyPackets  string `json:"-"`
}

func (a Attachment) IsInline() bool { return a.Disposition == "inline" }

func FilterInline(atts []Attachment) []Attachment {
	out := make([]Attachment, 0, len(atts))
	for _, a := range atts {
		if !a.IsInline() {
			out = append(out, a)
		}
	}
	return out
}

// ListOptions is every way of asking Proton for a set of messages or threads.
//
// There is one of these rather than a listing shape and a searching shape
// because Proton has one endpoint: `/mail/v4/messages` takes a label, a page and
// a handful of text predicates, and which of them are set is the only thing that
// separated the two. Two option types meant two commands that could not hand a
// selection to one another.
type ListOptions struct {
	Folder string
	// Keyword, From, To and Subject are the server's text predicates.
	Keyword, From, To, Subject string
	// After and Before bound the range by date, as YYYY-MM-DD.
	After, Before string
	Unread        bool

	// Page and PageSize are the caller's page of the result, counting from zero.
	// A size of zero is the whole result. A bulk selection asks for page zero the
	// size of its own cap, which is what --limit sets.
	Page, PageSize int

	// ID narrows the query to one message or thread, which is how a reference
	// that is already an ID is turned back into a row.
	ID string
}

// pageMax is how many rows Proton returns for one listing request, whatever
// larger number is asked for.
const pageMax = 150

// window reads the page a caller asked for out of however many of Proton's it
// spans, and reports how many rows the whole result has.
//
// A page no wider than Proton's own is one request, which is every ordinary
// listing. A wider one, and the whole result asked for with a size of zero, are
// read at Proton's width and cut down to what was asked for. That is what keeps
// --page-size the reader's number: how many requests it costs is this layer's
// business, and 150 never reaches a screen.
func window[T any](ctx context.Context, page, size int, fetch func(ctx context.Context, page, size int) ([]T, int, error)) ([]T, int, error) {
	if size > 0 && size <= pageMax {
		return fetch(ctx, page, size)
	}

	from := page * size
	start := from - from%pageMax
	var rows []T
	total := 0
	err := proton.Pages(ctx, func(ctx context.Context, i int) (bool, error) {
		got, count, err := fetch(ctx, start/pageMax+i, pageMax)
		if err != nil {
			return false, err
		}
		rows = append(rows, got...)
		total = count
		if !proton.Full(got, pageMax) {
			return false, nil
		}
		return size == 0 || start+len(rows) < from+size, nil
	})
	if err != nil {
		return nil, 0, err
	}

	rows = rows[min(from-start, len(rows)):]
	if size > 0 {
		rows = rows[:min(size, len(rows))]
	}
	return rows, total, nil
}

// Narrowed reports whether anything but the folder and the page was asked for,
// which is what decides whether an empty answer means an empty folder or an
// unmatched filter.
func (o ListOptions) Narrowed() bool {
	return o.Keyword != "" || o.From != "" || o.To != "" || o.Subject != "" ||
		o.After != "" || o.Before != "" || o.Unread
}

// decryptBody decrypts an armored PGP body with decKR and, when verKR is
// non-nil, verifies the embedded signature against it. gopenpgp returns the
// decrypted body alongside a SignatureVerificationError, so a bad/absent
// signature never hides the body - it only changes the verdict.
func decryptBody(armored string, decKR, verKR *pgp.KeyRing) (string, pgphelper.VerifyResult, error) {
	msg, err := pgp.NewPGPMessageFromArmored(armored)
	if err != nil {
		return "", pgphelper.Unverified, fmt.Errorf("parse message: %w", err)
	}
	var verifyTime int64
	if verKR != nil {
		verifyTime = pgp.GetUnixTime()
	}
	dec, err := decKR.Decrypt(msg, verKR, verifyTime)
	if err != nil {
		var sigErr pgp.SignatureVerificationError
		if errors.As(err, &sigErr) {
			return dec.GetString(), pgphelper.Classify(err), nil
		}
		return "", pgphelper.Unverified, fmt.Errorf("decrypt message: %w", err)
	}
	if verKR == nil {
		return dec.GetString(), pgphelper.Unverified, nil
	}
	return dec.GetString(), pgphelper.Verified, nil
}

// crossTableProbe wraps an HTTP 422 from a single-resource GET with a
// best-effort probe of the other table, producing a WrongTableError on hit.
func (s *Service) crossTableProbe(ctx context.Context, id string, err error, callerKind string) error {
	var apiErr *proton.APIError
	if !errors.As(err, &apiErr) || apiErr.HTTPStatus != 422 {
		return err
	}
	var otherPath, otherKind string
	switch callerKind {
	case "messages":
		otherPath = "/mail/v4/conversations/" + id
		otherKind = "conversation"
	case "conversations":
		otherPath = "/mail/v4/messages/" + id
		otherKind = "message"
	default:
		return err
	}
	if probeErr := s.C.Decode(ctx, proton.Request{Method: "GET", Path: otherPath}, nil); probeErr == nil {
		return &WrongTableError{Kind: otherKind, ID: id}
	}
	return err
}

func msgID(m Message) string    { return m.ID }
func msgLabel(m Message) string { return m.FromAddress + "  " + m.Subject }

func convSenderAddr(c Conversation) string {
	if len(c.Senders) > 0 {
		if a, ok := c.Senders[0]["Address"].(string); ok {
			return a
		}
	}
	return ""
}

// Resolve turns a reference into a message ID.
//
// An ID is already the answer. FindMessage looks one up anyway, because the row
// it returns is what a dry run shows before it acts; a caller that only wants the
// ID has no use for the row and no reason to pay for it.
func (s *Service) Resolve(ctx context.Context, r string) (string, error) {
	if ref.Full(r) {
		return r, nil
	}
	m, err := s.FindMessage(ctx, r)
	if err != nil {
		return "", err
	}
	return m.ID, nil
}

// FindMessage resolves a reference to the message itself, not just its ID.
//
// The row is what makes a dry run useful: showing a subject and a sender before
// a bulk delete costs nothing here, because resolving already had to look the
// message up.
func (s *Service) FindMessage(ctx context.Context, r string) (Message, error) {
	if ref.Full(r) {
		msgs, _, err := s.List(ctx, ListOptions{ID: r, Folder: "all", PageSize: 1})
		if err != nil {
			return Message{}, err
		}
		if len(msgs) == 1 {
			return msgs[0], nil
		}
		// An ID the index has not caught up with is still addressable; report it
		// with the identity we do have rather than refusing to act.
		return Message{ID: r}, nil
	}
	msgs, _, err := s.List(ctx, ListOptions{Keyword: r, Folder: "all", PageSize: 20})
	if err != nil {
		return Message{}, err
	}
	return ref.Pick("message", r, msgs, msgID, msgLabel)
}

// ResolveScheduled mirrors Resolve but scopes the keyword search to the
// Scheduled folder, so a REF can only resolve to an unschedulable message.
func (s *Service) ResolveScheduled(ctx context.Context, r string) (string, error) {
	if ref.Full(r) {
		return r, nil
	}
	msgs, _, err := s.List(ctx, ListOptions{Keyword: r, Folder: "scheduled", PageSize: 20})
	if err != nil {
		return "", err
	}
	m, err := ref.Pick("scheduled message", r, msgs, msgID, msgLabel)
	if err != nil {
		return "", err
	}
	return m.ID, nil
}

func (s *Service) ResolveConversation(ctx context.Context, r string) (string, error) {
	if ref.Full(r) {
		return r, nil
	}
	c, err := s.FindConversation(ctx, r)
	if err != nil {
		return "", err
	}
	return c.ID, nil
}

// FindConversation resolves a reference to the thread itself.
func (s *Service) FindConversation(ctx context.Context, r string) (Conversation, error) {
	if ref.Full(r) {
		convs, _, err := s.ConversationsList(ctx, ListOptions{ID: r, Folder: "all", PageSize: 1})
		if err != nil {
			return Conversation{}, err
		}
		if len(convs) == 1 {
			return convs[0], nil
		}
		return Conversation{ID: r}, nil
	}
	convs, _, err := s.ConversationsList(ctx, ListOptions{Keyword: r, Folder: "all", PageSize: 20})
	if err != nil {
		return Conversation{}, err
	}
	return ref.Pick("conversation", r, convs,
		func(c Conversation) string { return c.ID },
		func(c Conversation) string { return convSenderAddr(c) + "  " + c.Subject })
}
