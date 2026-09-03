package mail

import (
	"context"

	"github.com/roman-16/proton-cli/internal/account/keys"
	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/roman-16/proton-cli/internal/mailtext"
)

// AnswerSpec is what the user supplied for a reply or forward; the parent message
// supplies everything else.
type AnswerSpec struct {
	// Action is ActionReply, ActionReplyAll or ActionForward.
	Action int
	// Body is the new text, placed above the signature and the quoted original.
	Body string
	// HTML forces the body's format. When nil the parent's format is kept, which
	// is what the web client does.
	HTML *bool
	// To replaces the derived recipients; CC and BCC add to them.
	To, CC, BCC []Recipient
	// From names the sending address explicitly, overriding the address the
	// parent arrived on.
	From string
	// Attach are new attachments to upload.
	Attach []LocalAttachment
	// NoQuote omits the quoted original.
	NoQuote bool
	// NoAttachments drops the parent's own attachments from a forward.
	NoAttachments bool
	// NoSignature suppresses the sending address's signature and Proton's footer.
	NoSignature bool
}

// Answer builds the Content for a reply or forward of parentID. It resolves the
// sending address, derives subject and recipients from the parent, carries the
// parent's attachments on a forward, and lays the body out as new text,
// signature, then the quoted original.
func (s *Service) Answer(ctx context.Context, parentID string, spec AnswerSpec) (Content, error) {
	raw, u, err := s.messageAndKeys(ctx, parentID)
	if err != nil {
		return Content{}, err
	}

	// A reply leaves from the address the parent arrived on, which for a message
	// we sent is the address it went out from.
	parentAddress := raw.parsedHeader("X-Original-To")
	if raw.isSent() {
		parentAddress = senderAddress(raw.Sender)
	}
	sender, err := resolveSender(u, SenderRequest{
		Explicit:        spec.From,
		ParentAddress:   parentAddress,
		ParentAddressID: raw.AddressID,
	})
	if err != nil {
		return Content{}, err
	}

	body, err := s.decryptForQuote(ctx, u, raw)
	if err != nil {
		return Content{}, err
	}
	parent := replyContext{
		Sender:   Recipient{Address: senderAddress(raw.Sender), Name: senderName(raw.Sender)},
		To:       recipientsFromRaw(raw.ToList),
		CC:       recipientsFromRaw(raw.CCList),
		BCC:      recipientsFromRaw(raw.BCCList),
		ReplyTos: recipientsFromRaw(raw.ReplyTos),
		Subject:  raw.Subject,
		Body:     body,
		HTML:     mailtext.IsHTML(raw.MIMEType),
		Time:     raw.Time,
		Sent:     raw.isSent(),
	}

	html := parent.HTML
	if spec.HTML != nil {
		html = *spec.HTML
	}
	action := spec.Action
	c := Content{
		From:            sender,
		Subject:         subjectFor(action, raw.Subject),
		Body:            spec.Body,
		HTML:            html,
		Attach:          spec.Attach,
		ParentID:        raw.ID,
		Action:          &action,
		ParentAddressID: raw.AddressID,
	}

	if action == ActionForward {
		c.To = spec.To
		c.CC = spec.CC
		c.BCC = spec.BCC
		if !spec.NoAttachments {
			c.Carry = carriedFrom(raw)
		}
	} else {
		to, cc, bcc := replyRecipients(action, parent, sender.Address.Email)
		if len(spec.To) > 0 {
			to = spec.To
		}
		c.To, c.CC, c.BCC = to, append(cc, spec.CC...), append(bcc, spec.BCC...)
	}
	if !c.HasRecipients() {
		if action == ActionForward {
			return Content{}, errs.Problemf("--to is required when forwarding")
		}
		return Content{}, errs.Problemf("The message being replied to carries no address to reply to.").
			Hint("name the recipient yourself with --to")
	}

	var quote string
	if !spec.NoQuote {
		quote = quoteBlock(action, parent, html)
	}
	var signature string
	if !spec.NoSignature {
		if signature, err = s.SignatureBlock(ctx, sender); err != nil {
			return Content{}, err
		}
	}
	c.AppendSignature(signature, quote)
	return c, nil
}

// decryptForQuote decrypts the parent's body for quoting. A body that will not
// decrypt is quoted as its ciphertext rather than failing the reply, matching the
// web client, which quotes the raw body on a decryption error.
func (s *Service) decryptForQuote(ctx context.Context, u *keys.Unlocked, raw *rawMessage) (string, error) {
	kr, ok := u.AddrKR(raw.AddressID)
	if !ok {
		first, _, err := u.FirstAddr()
		if err != nil {
			return "", err
		}
		kr = first
	}
	body, _, err := decryptBody(raw.Body, kr, nil)
	if err != nil {
		return raw.Body, nil
	}
	return body, nil
}

// carriedFrom lists the parent's attachments to keep on a forward.
func carriedFrom(raw *rawMessage) []CarriedAttachment {
	out := make([]CarriedAttachment, 0, len(raw.Attachments))
	for _, a := range raw.Attachments {
		out = append(out, CarriedAttachment{ID: a.ID, Name: a.Name, KeyPackets: a.KeyPackets})
	}
	return out
}

func senderName(sender map[string]any) string {
	if s, ok := sender["Name"].(string); ok {
		return s
	}
	return ""
}
