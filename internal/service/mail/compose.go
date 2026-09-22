package mail

import (
	"fmt"
	"net/mail"
	"strings"

	"github.com/roman-16/proton-cli/internal/mailtext"
)

// A message the CLI sends passes through three stages: Content describes what it
// says, a draft is Content stored server-side, and Delivery describes how it
// goes out. Splitting them is what lets one code path serve `messages send`,
// `drafts create`, `drafts send`, `messages reply` and `messages forward`
// instead of one function with a dozen switches.

const (
	mimeTypePlain = "text/plain"
	mimeTypeHTML  = "text/html"
)

// Draft actions Proton uses to thread a new message onto the one it answers.
// Passing them on createDraft is what makes the server write In-Reply-To and
// References and flag the parent as replied or forwarded.
const (
	ActionReply    = 0
	ActionReplyAll = 1
	ActionForward  = 2
)

// Recipient is an addressee. Name is the display name the recipient's client
// shows; Proton falls back to the address when it is empty.
type Recipient struct {
	Name    string `json:"name,omitempty"`
	Address string `json:"address"`
}

// ParseRecipient accepts a bare address or an RFC 5322 mailbox with a display
// name, so `--to "Alice <alice@example.com>"` works as well as `--to alice@…`.
func ParseRecipient(s string) Recipient {
	s = strings.TrimSpace(s)
	if a, err := mail.ParseAddress(s); err == nil {
		return Recipient{Name: a.Name, Address: a.Address}
	}
	return Recipient{Address: s}
}

func ParseRecipients(ss []string) []Recipient {
	out := make([]Recipient, 0, len(ss))
	for _, s := range ss {
		if strings.TrimSpace(s) == "" {
			continue
		}
		out = append(out, ParseRecipient(s))
	}
	return out
}

// String renders the recipient for headers and quote blocks.
func (r Recipient) String() string {
	if r.Name == "" {
		return r.Address
	}
	return fmt.Sprintf("%s <%s>", r.Name, r.Address)
}

// LocalAttachment is an attachment supplied by the caller: bytes plus metadata,
// read from disk by the cli layer or generated in memory (a calendar invitation).
type LocalAttachment struct {
	Filename string
	MIMEType string
	Data     []byte
	// Inline marks an image to embed in an HTML body. Composing assigns it a
	// Content-ID and appends a cid: reference, which is what makes Proton record
	// the part with disposition "inline".
	Inline bool
	// ContentID is filled in during composition for inline parts.
	ContentID string
}

// CarriedAttachment is an attachment already stored on another message, kept on
// a forward. Only its identity and key travel: Proton copies the data packet
// itself once the re-wrapped session key proves we can read it, so the bytes are
// never uploaded a second time.
type CarriedAttachment struct {
	ID   string
	Name string
	// KeyPackets is base64 and encrypted to the parent message's address key.
	KeyPackets string
}

// Content is the message itself - exactly what a draft stores.
type Content struct {
	From        *Sender
	To, CC, BCC []Recipient
	Subject     string
	Body        string
	HTML        bool

	Attach []LocalAttachment
	Carry  []CarriedAttachment

	// ParentID and Action thread this message onto the one it answers.
	ParentID string
	Action   *int
	// ParentAddressID names the address the parent was encrypted to, whose key
	// unwraps the session keys of everything in Carry.
	ParentAddressID string

	// Receipt asks the recipients to confirm that they read the message.
	Receipt bool
	// flags are the message's flag word as Proton holds it, filled in when a
	// stored draft is loaded. Writing the draft back keeps the bits this content
	// says nothing about.
	flags int64
}

// flagWord is the flag value a draft is stored with: what Proton already holds,
// with the receipt request as this content states it.
func (c Content) flagWord() int64 {
	if c.Receipt {
		return c.flags | flagReceiptRequest
	}
	return c.flags &^ flagReceiptRequest
}

// Delivery is how a message goes out, independent of what it says. It applies at
// send time only, which is why a draft never stores it.
type Delivery struct {
	// At schedules the send for an absolute Unix time when non-zero.
	At int64
	// ExpiresInSeconds makes the message self-destruct. Encrypted-for-outside
	// sends default to 28 days when this is zero.
	ExpiresInSeconds int
	// EOPassword password-protects the message for recipients outside Proton
	// instead of sending them cleartext.
	EOPassword     string
	EOPasswordHint string
	// PinnedKeys carries contact-pinned encryption preferences per recipient
	// address, resolved by the caller from Contacts.
	PinnedKeys map[string]*PinnedRecipient
}

func (c Content) mimeType() string {
	if c.HTML {
		return mimeTypeHTML
	}
	return mimeTypePlain
}

// plainBody returns the body flattened to text, for packages that cannot carry
// HTML (PGP-Inline) and for plaintext alternatives in exported MIME.
func (c Content) plainBody() string {
	if c.HTML {
		return mailtext.HTMLToText(c.Body)
	}
	return c.Body
}

// RecipientAddresses flattens To/CC/BCC into one list, dropping
// case-insensitive duplicates while preserving first-seen order.
func (c Content) RecipientAddresses() []string {
	seen := make(map[string]struct{})
	var out []string
	for _, list := range [][]Recipient{c.To, c.CC, c.BCC} {
		for _, r := range list {
			key := strings.ToLower(r.Address)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, r.Address)
		}
	}
	return out
}

// HasRecipients reports whether the message is addressed to anyone at all.
func (c Content) HasRecipients() bool {
	return len(c.To)+len(c.CC)+len(c.BCC) > 0
}

// AttachmentCount is how many attachments the message will carry, new and
// carried over together.
func (c Content) AttachmentCount() int { return len(c.Attach) + len(c.Carry) }

// AppendSignature places the sender address's stored signature into the body the
// way the web client does: after the new text, and above any quoted original, so
// a reply reads message, signature, then what it answers.
//
// quote is the quoted-original block, already rendered in the body's format; it
// is empty for a message that answers nothing.
func (c *Content) AppendSignature(signature, quote string) {
	sig := strings.TrimSpace(signature)
	separator := "\n\n"
	if c.HTML {
		separator = "<br><br>"
		if sig != "" {
			sig = `<div class="protonmail_signature_block">` + sig + `</div>`
		}
	} else if sig != "" {
		sig = mailtext.HTMLToText(sig)
	}
	// An empty part is dropped rather than joined, so a forward with no covering
	// note does not open with blank lines.
	var parts []string
	for _, part := range []string{c.Body, sig, quote} {
		if strings.TrimSpace(part) != "" {
			parts = append(parts, part)
		}
	}
	c.Body = strings.Join(parts, separator)
}

// recipientList renders a recipient list for a draft payload.
func recipientList(rs []Recipient) []map[string]string {
	out := make([]map[string]string, 0, len(rs))
	for _, r := range rs {
		name := r.Name
		if name == "" {
			name = r.Address
		}
		out = append(out, map[string]string{"Address": r.Address, "Name": name})
	}
	return out
}

func recipientsFromRaw(raw []map[string]any) []Recipient {
	out := make([]Recipient, 0, len(raw))
	for _, m := range raw {
		addr, _ := m["Address"].(string)
		if addr == "" {
			continue
		}
		name, _ := m["Name"].(string)
		out = append(out, Recipient{Name: name, Address: addr})
	}
	return out
}
