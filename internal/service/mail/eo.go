package mail

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"

	pgp "github.com/ProtonMail/gopenpgp/v2/crypto"
	"github.com/ProtonMail/gopenpgp/v2/helper"
	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/roman-16/proton-cli/internal/mailtext"
	"github.com/roman-16/proton-cli/internal/proton"
	"github.com/roman-16/proton-cli/internal/skip"
)

// A message sent to an address outside Proton is read from behind a link rather
// than out of a mailbox, by whoever has the password its sender chose. Nothing
// here needs an account.
//
// The crypto is send_eo.go read the other way round. The token that stands in
// for a session is a PGP message encrypted to the password; so is the body; and
// each attachment's key packets are too, which is what makes the session key
// that opens its bytes. An answer goes back sealed to the sender's own key, and
// a second copy sealed to the password, which is the one that stays behind the
// link.

// eoSegment is the path a link puts the message's id after. A link is taken
// from any host: Proton has served these from more than one over the years, and
// the id is the part that names anything.
const eoSegment = "eo"

// EOMaxReplies is how many answers one such message takes, as Proton's own
// client counts them (EO_MAX_REPLIES_NUMBER).
const EOMaxReplies = 5

// EOMessage is a password-protected message as the person it was sent to sees
// it, and what it takes to ask Proton for the rest of it.
type EOMessage struct {
	Full
	// Replies is how many answers have gone back from behind the link, which is
	// what decides whether another one may.
	Replies int `json:"replies"`

	// id names the message, token stands in for a session, and password opens
	// everything either of them reaches.
	id, token, password string
	// senderKey is the armoured public key an answer is sealed to.
	senderKey string
	// sealed is each attachment with the key packets its bytes need, which the
	// listing drops.
	sealed []sealedAttachment
}

// EOReplySpec is what somebody reading such a message wants to send back.
type EOReplySpec struct {
	// Body is the new text, placed above the quoted original.
	Body string
	// HTML says the body is HTML. When nil the original's format is kept.
	HTML *bool
	// Attach are files to send with it.
	Attach []LocalAttachment
	// NoQuote omits the quoted original.
	NoQuote bool
}

// ParseEOLink reads the name of a password-protected message: the link as it
// was sent, or the id in it.
func ParseEOLink(raw string) (string, error) {
	ref := strings.TrimSpace(raw)
	if ref == "" {
		return "", eoNotALink()
	}
	if !strings.ContainsAny(ref, "/:") {
		return ref, nil
	}
	if id := eoIDIn(ref); id != "" {
		return id, nil
	}
	return "", eoNotALink()
}

// IsEOLink reports whether a reference is such a message's link.
//
// It answers for a whole link and never for a bare id: an id is the shape every
// other reference in the mailbox has, so a command that read one as a link
// would refuse to act on messages of its own.
func IsEOLink(raw string) bool { return eoIDIn(strings.TrimSpace(raw)) != "" }

// eoIDIn is the id a link carries, and nothing when it carries none.
func eoIDIn(raw string) string {
	path := raw
	if parsed, err := url.Parse(raw); err == nil {
		path = parsed.Path
		if parsed.Opaque != "" {
			path = parsed.Opaque
		}
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	for i, part := range parts {
		if part == eoSegment && i+1 < len(parts) && parts[i+1] != "" {
			return parts[i+1]
		}
	}
	return ""
}

func eoNotALink() error {
	return errs.Problemf("That is not a password-protected message.").
		Hint("Give the link you were sent, or the id in it.")
}

// OpenEO opens a password-protected message with the password that was sent
// separately.
func (s *Service) OpenEO(ctx context.Context, ref, password string) (*EOMessage, error) {
	id, err := ParseEOLink(ref)
	if err != nil {
		return nil, err
	}
	var sealed struct{ Token string }
	if err := s.C.Decode(ctx, proton.EOTokenRequest(id), &sealed); err != nil {
		return nil, eoGone(ctx, err)
	}
	if sealed.Token == "" {
		return nil, errs.Problemf("Proton named no token for that message, so there is nothing to open it with.")
	}
	token, err := helper.DecryptMessageWithPassword([]byte(password), sealed.Token)
	if err != nil {
		// Recorded and not counted: nothing is missing from an answer, because the
		// caller is told outright that the password was wrong. What the log adds is
		// which of the two ways it failed - a password that does not open the token
		// against a token that is not a PGP message at all.
		slog.DebugContext(ctx, "mail: a password-protected message did not open",
			"kind", string(skip.KindMessage), "reason", string(skip.Undecryptable), "error", err)
		return nil, errs.Problemf("That password does not open this message.").
			Hint("The password is the one whoever sent it gave you, not your Proton password.")
	}

	var answer struct {
		Message struct {
			Subject        string
			Sender         map[string]any
			ToList         []map[string]any
			CCList         []map[string]any
			Recipient      string
			Time           int64
			ExpirationTime int64
			Body           string
			MIMEType       string
			Attachments    []sealedAttachment
			Replies        []struct{ Time int64 }
		}
		PublicKey string
	}
	if err := s.C.Decode(ctx, proton.EOMessageRequest(id, token), &answer); err != nil {
		return nil, eoRefused(ctx, err)
	}
	body, err := helper.DecryptMessageWithPassword([]byte(password), answer.Message.Body)
	if err != nil {
		return nil, fmt.Errorf("decrypt the body of a password-protected message: %w", err)
	}

	m := &EOMessage{
		Full: Full{
			Subject: answer.Message.Subject, Sender: answer.Message.Sender,
			ToList: answer.Message.ToList, CCList: answer.Message.CCList,
			Time: answer.Message.Time, Expires: answer.Message.ExpirationTime,
			Body: body, MIMEType: answer.Message.MIMEType,
		},
		Replies: len(answer.Message.Replies),
		id:      id, token: token, password: password,
		senderKey: answer.PublicKey, sealed: answer.Message.Attachments,
	}
	for _, a := range answer.Message.Attachments {
		m.Attachments = append(m.Attachments, Attachment{
			ID: a.ID, Name: a.Name, Size: a.Size,
			MIMEType: a.MIMEType, Disposition: a.Disposition,
		})
	}
	return m, nil
}

// EOAttachment decrypts one attachment of such a message, named by its ID or by
// its own name.
func (s *Service) EOAttachment(ctx context.Context, m *EOMessage, reference string) ([]byte, string, error) {
	wanted, err := MatchAttachment("in that message", reference, m.sealed,
		func(a sealedAttachment) Attachment {
			return Attachment{ID: a.ID, Name: a.Name, Size: a.Size}
		})
	if err != nil {
		return nil, "", err
	}
	if wanted.KeyPackets == "" {
		return nil, "", errs.Naming(wanted.Name,
			errs.Problemf("That attachment arrived without the key that opens it."))
	}
	kp, err := base64.StdEncoding.DecodeString(wanted.KeyPackets)
	if err != nil {
		return nil, "", fmt.Errorf("decode attachment key packets: %w", err)
	}
	sk, err := pgp.DecryptSessionKeyWithPassword(kp, []byte(m.password))
	if err != nil {
		return nil, "", errs.Naming(wanted.Name,
			fmt.Errorf("open the key of an attachment on a password-protected message: %w", err))
	}
	resp, err := s.C.Do(ctx, proton.EOAttachmentRequest(m.id, m.token, wanted.ID))
	if err != nil {
		return nil, "", eoRefused(ctx, err)
	}
	dec, err := sk.Decrypt(resp.Body)
	if err != nil {
		return nil, "", errs.Naming(wanted.Name,
			fmt.Errorf("decrypt an attachment on a password-protected message: %w", err))
	}
	return dec.GetBinary(), wanted.Name, nil
}

// EOReply answers such a message, which goes to whoever sent it and nobody
// else: it is the only address the link knows.
func (s *Service) EOReply(ctx context.Context, m *EOMessage, spec EOReplySpec) error {
	if m.Replies >= EOMaxReplies {
		return errs.Problemf("This message has had its %d answers, which is all Proton allows from behind a link.",
			EOMaxReplies).
			Hint("Write to the sender from your own mail instead.")
	}
	if m.senderKey == "" {
		return errs.Problemf("Proton named no key for the sender, so an answer could not be sealed to them.")
	}
	sender, err := pgp.NewKeyFromArmored(m.senderKey)
	if err != nil {
		return fmt.Errorf("read the sender's key: %w", err)
	}
	senderKR, err := pgp.NewKeyRing(sender)
	if err != nil {
		return fmt.Errorf("read the sender's key: %w", err)
	}

	html := mailtext.IsHTML(m.MIMEType)
	if spec.HTML != nil {
		html = *spec.HTML
	}
	text := eoReplyBody(m, spec, html)
	sealedToSender, err := senderKR.Encrypt(pgp.NewPlainMessageFromString(text), nil)
	if err != nil {
		return fmt.Errorf("encrypt the answer to the sender: %w", err)
	}
	armoured, err := sealedToSender.GetArmored()
	if err != nil {
		return fmt.Errorf("encrypt the answer to the sender: %w", err)
	}
	// The second copy is what stays behind the link, where the password is the
	// only key there is.
	sealedToPassword, err := helper.EncryptMessageWithPassword([]byte(m.password), text)
	if err != nil {
		return fmt.Errorf("encrypt the answer to the password: %w", err)
	}

	fields := []formField{{Name: "Body", Value: armoured}, {Name: "ReplyBody", Value: sealedToPassword}}
	var files []formFile
	for _, a := range spec.Attach {
		packet, err := senderKR.EncryptAttachment(pgp.NewPlainMessage(a.Data), a.Filename)
		if err != nil {
			return errs.Naming(a.Filename, fmt.Errorf("encrypt an attachment to the sender: %w", err))
		}
		mimeType := a.MIMEType
		if mimeType == "" {
			mimeType = "application/octet-stream"
		}
		fields = append(fields,
			formField{Name: "Filename[]", Value: a.Filename},
			formField{Name: "MIMEType[]", Value: mimeType},
			formField{Name: "ContentID[]", Value: a.ContentID})
		files = append(files,
			formFile{Name: "KeyPackets[]", Filename: a.Filename, Data: packet.KeyPacket},
			formFile{Name: "DataPacket[]", Filename: a.Filename, Data: packet.DataPacket})
	}
	body, contentType, err := buildAttachmentForm(fields, files)
	if err != nil {
		return err
	}
	if err := s.C.Decode(ctx, proton.EOReplyRequest(m.id, m.token, body, contentType), nil); err != nil {
		return eoRefused(ctx, err)
	}
	m.Replies++
	return nil
}

// EOSubject is what an answer to such a message is called, which is worked out
// here rather than sent: the reply endpoint takes a body and nothing else.
func (m *EOMessage) EOSubject() string { return subjectFor(ActionReply, m.Subject) }

// eoReplyBody lays the answer out as the new text above the quoted original,
// the way every other answer in this CLI is laid out. There is no signature:
// the address behind the link is not one of ours to have one.
func eoReplyBody(m *EOMessage, spec EOReplySpec, html bool) string {
	c := Content{Body: spec.Body, HTML: html}
	var quote string
	if !spec.NoQuote {
		quote = quoteBlock(ActionReply, replyContext{
			Sender:  Recipient{Address: senderAddress(m.Sender), Name: senderName(m.Sender)},
			To:      recipientsFromRaw(m.ToList),
			CC:      recipientsFromRaw(m.CCList),
			Subject: m.Subject,
			Body:    m.Body,
			HTML:    mailtext.IsHTML(m.MIMEType),
			Time:    m.Time,
		}, html)
	}
	c.AppendSignature("", quote)
	return c.Body
}

// eoGone phrases Proton not knowing the id a link named, which is the same
// answer for one that was mistyped and one whose message has lapsed: Proton
// keeps no record of a message it has thrown away.
func eoGone(ctx context.Context, err error) error {
	var apiErr *proton.APIError
	if !errors.As(err, &apiErr) || apiErr.HTTPStatus != 400 {
		return err
	}
	slog.DebugContext(ctx, "mail: a password-protected message was not there",
		"status", apiErr.HTTPStatus, "code", apiErr.Code)
	return errs.Problemf("That link does not open anything.").
		Hint("A password-protected message is gone 28 days after it was sent.").
		Exit(3)
}

// eoRefused phrases Proton turning down the token a password unwrapped, which
// it does when the message lapsed between one request and the next.
func eoRefused(ctx context.Context, err error) error {
	var apiErr *proton.APIError
	if !errors.As(err, &apiErr) || apiErr.HTTPStatus != 401 {
		return err
	}
	slog.DebugContext(ctx, "mail: a password-protected message stopped answering",
		"status", apiErr.HTTPStatus, "code", apiErr.Code)
	return errs.Problemf("That message is no longer open.").
		Hint("A password-protected message is gone 28 days after it was sent.").
		Exit(3)
}
