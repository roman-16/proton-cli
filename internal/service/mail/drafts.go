package mail

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"

	pgp "github.com/ProtonMail/gopenpgp/v2/crypto"
	"github.com/roman-16/proton-cli/internal/account/keys"
	"github.com/roman-16/proton-cli/internal/mailtext"
	"github.com/roman-16/proton-cli/internal/proton"
	"github.com/roman-16/proton-cli/internal/ref"
)

// Draft is Content stored server-side: its message ID, the content it holds, and
// the session key of every attachment on it. Those keys are what the send path
// re-wraps per recipient, so holding them here is what lets a draft be sent
// later without re-uploading anything.
type Draft struct {
	ID          string
	Content     Content
	Attachments []*draftAttachment
}

// draftAttachment is one attachment on a draft.
type draftAttachment struct {
	ID         string
	Name       string
	MIMEType   string
	ContentID  string
	Size       int64
	SessionKey *pgp.SessionKey
	// Data is the plaintext. Composing fills it in; loading a stored draft leaves
	// it nil until a PGP/MIME package needs to embed the attachment verbatim.
	Data []byte
}

func (a *draftAttachment) isInline() bool { return a.ContentID != "" }

// DraftAttachment is the public view of one attachment on a draft, for display
// and for resolving a name back to an ID.
type DraftAttachment struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Size     int64  `json:"size"`
	MIMEType string `json:"mime_type"`
	Inline   bool   `json:"inline"`
}

// AttachmentList lists what is attached to the draft.
func (d *Draft) AttachmentList() []DraftAttachment {
	out := make([]DraftAttachment, 0, len(d.Attachments))
	for _, a := range d.Attachments {
		out = append(out, DraftAttachment{
			ID: a.ID, Name: a.Name, Size: a.Size, MIMEType: a.MIMEType, Inline: a.isInline(),
		})
	}
	return out
}

// bytes returns the attachment's plaintext, downloading and decrypting it the
// first time it is needed.
func (s *Service) attachmentBytes(ctx context.Context, a *draftAttachment) ([]byte, error) {
	if a.Data != nil {
		return a.Data, nil
	}
	if a.SessionKey == nil {
		return nil, fmt.Errorf("attachment %s: no session key to decrypt with", a.Name)
	}
	resp, err := s.C.Do(ctx, proton.Request{Method: "GET", Path: "/mail/v4/attachments/" + a.ID})
	if err != nil {
		return nil, err
	}
	msg, err := a.SessionKey.Decrypt(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("decrypt attachment %s: %w", a.Name, err)
	}
	a.Data = msg.GetBinary()
	return a.Data, nil
}

// DraftsList lists the Drafts folder.
func (s *Service) DraftsList(ctx context.Context, page, pageSize int) ([]Message, int, error) {
	return s.List(ctx, ListOptions{Folder: "drafts", Page: page, PageSize: pageSize})
}

// ResolveDraft resolves a REF within the Drafts folder, so editing or sending a
// draft by subject can never reach a message that has already gone out.
func (s *Service) ResolveDraft(ctx context.Context, r string) (string, error) {
	if ref.Full(r) {
		return r, nil
	}
	msgs, _, err := s.List(ctx, ListOptions{Keyword: r, Folder: "drafts", PageSize: 20})
	if err != nil {
		return "", err
	}
	m, err := ref.Pick("draft", r, msgs, msgID, msgLabel)
	if err != nil {
		return "", err
	}
	return m.ID, nil
}

// prepareBody assigns Content-IDs to inline attachments, appends their cid:
// references to the body, and encrypts the result to the sender's own key - the
// form Proton stores a draft in.
func prepareBody(c *Content) (string, error) {
	if err := assignInlineContentIDs(c); err != nil {
		return "", err
	}
	enc, err := c.From.Keys.Write.Encrypt(pgp.NewPlainMessageFromString(c.Body), c.From.Keys.Write)
	if err != nil {
		return "", fmt.Errorf("encrypt draft: %w", err)
	}
	return enc.GetArmored()
}

// draftPayload is the Message object createDraft and updateDraft both take. The
// sender carries the address's display name, so recipients see a name rather
// than a bare address.
func draftPayload(c Content, armoredBody string) map[string]any {
	name := c.From.Address.DisplayName
	if name == "" {
		name = c.From.Address.Email
	}
	out := map[string]any{
		"ToList":    recipientList(c.To),
		"CCList":    recipientList(c.CC),
		"BCCList":   recipientList(c.BCC),
		"Subject":   c.Subject,
		"Sender":    map[string]string{"Address": c.From.Address.Email, "Name": name},
		"Body":      armoredBody,
		"MIMEType":  c.mimeType(),
		"AddressID": c.From.Address.ID,
	}
	return out
}

// DraftCreate stores Content as a draft: the body is encrypted to the sender's
// key, attachments carried over from a parent get their session keys re-wrapped,
// and new attachments are uploaded against the new draft.
func (s *Service) DraftCreate(ctx context.Context, c Content) (*Draft, error) {
	armored, err := prepareBody(&c)
	if err != nil {
		return nil, err
	}
	u, err := s.keys(ctx)
	if err != nil {
		return nil, err
	}
	packets, err := s.rekeyCarried(u, c)
	if err != nil {
		return nil, err
	}
	body := map[string]any{"Message": draftPayload(c, armored)}
	if c.ParentID != "" {
		body["ParentID"] = c.ParentID
	}
	if c.Action != nil {
		body["Action"] = *c.Action
	}
	if len(packets) > 0 {
		body["AttachmentKeyPackets"] = packets
	}
	var resp struct {
		Message struct {
			ID          string
			Attachments []rawAttachment
		}
	}
	if err := s.C.Decode(ctx, proton.Request{Method: "POST", Path: "/mail/v4/messages", Body: body}, &resp); err != nil {
		return nil, err
	}
	// Attachments carried from a parent are re-created on the new draft with
	// their own IDs and their session keys re-wrapped to this address, so the
	// response is the only authoritative list - reusing the parent's IDs would
	// make every per-recipient key packet reference an attachment that does not
	// exist on this message.
	d := &Draft{ID: resp.Message.ID, Content: c}
	for _, a := range resp.Message.Attachments {
		att, err := draftAttachmentFrom(c.From.Keys.Read, a)
		if err != nil {
			s.discard(ctx, d.ID)
			return nil, err
		}
		d.Attachments = append(d.Attachments, att)
	}
	uploaded, err := s.uploadAttachments(ctx, c.From.Keys.Write, d.ID, c.Attach)
	if err != nil {
		// A half-built draft is worse than none: drop it so the caller's error is
		// the only thing left behind.
		s.discard(ctx, d.ID)
		return nil, err
	}
	d.Attachments = append(d.Attachments, uploaded...)
	return d, nil
}

// DraftUpdate rewrites a stored draft's recipients, subject and body, and
// uploads any newly added attachments. Attachments already on the draft are
// separate resources and are left alone; DraftDetach removes one.
func (s *Service) DraftUpdate(ctx context.Context, id string, c Content) (*Draft, error) {
	armored, err := prepareBody(&c)
	if err != nil {
		return nil, err
	}
	if err := s.C.Decode(ctx, proton.Request{
		Method: "PUT", Path: "/mail/v4/messages/" + id,
		Body: map[string]any{"Message": draftPayload(c, armored)},
	}, nil); err != nil {
		return nil, err
	}
	if _, err := s.uploadAttachments(ctx, c.From.Keys.Write, id, c.Attach); err != nil {
		return nil, err
	}
	// Re-read so the returned draft carries every attachment, pre-existing and
	// newly uploaded, with its session key.
	return s.DraftLoad(ctx, id)
}

// DraftLoad reads a stored draft back into Content, decrypting its body and
// recovering each attachment's session key so the draft can be sent as-is.
func (s *Service) DraftLoad(ctx context.Context, id string) (*Draft, error) {
	raw, u, err := s.messageAndKeys(ctx, id)
	if err != nil {
		return nil, err
	}
	sender, err := resolveSender(u, SenderRequest{ParentAddressID: raw.AddressID})
	if err != nil {
		return nil, err
	}
	rings, ok := u.AddrRings(raw.AddressID)
	if !ok {
		rings = sender.Keys
	}
	body, _, err := decryptBody(raw.Body, rings.Read, nil)
	if err != nil {
		return nil, fmt.Errorf("decrypt draft: %w", err)
	}
	d := &Draft{
		ID: raw.ID,
		Content: Content{
			From:    sender,
			To:      recipientsFromRaw(raw.ToList),
			CC:      recipientsFromRaw(raw.CCList),
			BCC:     recipientsFromRaw(raw.BCCList),
			Subject: raw.Subject,
			Body:    body,
			HTML:    mailtext.IsHTML(raw.MIMEType),
		},
	}
	for _, a := range raw.Attachments {
		att, err := draftAttachmentFrom(rings.Read, a)
		if err != nil {
			// A key we cannot unwrap must not stop the draft being read; sending
			// it reports the problem instead.
			att = &draftAttachment{
				ID: a.ID, Name: a.Name, MIMEType: a.MIMEType,
				ContentID: normalizeContentID(a.ContentID), Size: a.Size,
			}
		}
		d.Attachments = append(d.Attachments, att)
	}
	return d, nil
}

// DraftDetach removes one attachment from a draft.
func (s *Service) DraftDetach(ctx context.Context, draftID, attachmentID string) error {
	return s.C.Decode(ctx, proton.Request{
		Method: "DELETE", Path: "/mail/v4/attachments/" + attachmentID,
		Query: proton.Query("MessageID", draftID),
	}, nil)
}

// discard deletes a draft. The delete-messages endpoint is a PUT; using DELETE
// silently fails and leaks the draft.
func (s *Service) discard(ctx context.Context, ids ...string) {
	_, _ = s.C.Do(ctx, proton.Request{
		Method: "PUT", Path: "/mail/v4/messages/delete",
		Body: map[string]any{"IDs": ids},
	})
}

// rekeyCarried re-wraps each carried attachment's session key from the parent's
// address key to the new sender's, keyed by the PARENT's attachment ID, which is
// how createDraft identifies what it is copying.
func (s *Service) rekeyCarried(u *keys.Unlocked, c Content) (map[string]string, error) {
	if len(c.Carry) == 0 {
		return nil, nil
	}
	parentRings, ok := u.AddrRings(c.ParentAddressID)
	if !ok {
		parentRings = c.From.Keys
	}
	packets := make(map[string]string, len(c.Carry))
	for _, a := range c.Carry {
		sk, err := decodeSessionKey(parentRings.Read, a.KeyPackets)
		if err != nil {
			return nil, fmt.Errorf("carry attachment %s: %w", a.Name, err)
		}
		wrapped, err := c.From.Keys.Write.EncryptSessionKey(sk)
		if err != nil {
			return nil, fmt.Errorf("carry attachment %s: %w", a.Name, err)
		}
		packets[a.ID] = base64.StdEncoding.EncodeToString(wrapped)
	}
	return packets, nil
}

// draftAttachmentFrom converts an attachment as the server reports it into the
// form the send path needs, recovering the session key its data packet is under.
func draftAttachmentFrom(kr *pgp.KeyRing, a rawAttachment) (*draftAttachment, error) {
	sk, err := decodeSessionKey(kr, a.KeyPackets)
	if err != nil {
		return nil, fmt.Errorf("attachment %s: %w", a.Name, err)
	}
	return &draftAttachment{
		ID: a.ID, Name: a.Name, MIMEType: a.MIMEType, Size: a.Size,
		ContentID: normalizeContentID(a.ContentID), SessionKey: sk,
	}, nil
}

// decodeSessionKey unwraps a base64 key packet with a key ring.
func decodeSessionKey(kr *pgp.KeyRing, keyPackets string) (*pgp.SessionKey, error) {
	if keyPackets == "" {
		return nil, fmt.Errorf("no key packets")
	}
	raw, err := base64.StdEncoding.DecodeString(keyPackets)
	if err != nil {
		return nil, fmt.Errorf("decode key packets: %w", err)
	}
	return kr.DecryptSessionKey(raw)
}

// normalizeContentID strips the angle brackets Proton stores Content-IDs with.
func normalizeContentID(cid string) string {
	return strings.Trim(cid, "<>")
}
