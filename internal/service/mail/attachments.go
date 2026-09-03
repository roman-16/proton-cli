package mail

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"mime"
	"mime/multipart"
	"os"
	"path/filepath"
	"sort"
	"strings"

	pgp "github.com/ProtonMail/gopenpgp/v2/crypto"
	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/roman-16/proton-cli/internal/proton"
)

// ConversationAttachment carries its parent MessageID so callers can
// disambiguate and download against the correct message.
type ConversationAttachment struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Size        int64  `json:"size"`
	MIMEType    string `json:"mime_type"`
	Disposition string `json:"disposition"`
	MessageID   string `json:"message_id"`
}

func (s *Service) ConversationAttachmentsList(ctx context.Context, convID string, includeInline bool) ([]ConversationAttachment, error) {
	var r struct{ Messages []rawMessage }
	if err := s.C.Decode(ctx, proton.Request{Method: "GET", Path: "/mail/v4/conversations/" + convID}, &r); err != nil {
		return nil, s.crossTableProbe(ctx, convID, err, "conversations")
	}
	sort.SliceStable(r.Messages, func(i, j int) bool { return r.Messages[i].Time < r.Messages[j].Time })
	var out []ConversationAttachment
	for _, m := range r.Messages {
		for _, a := range m.Attachments {
			att := Attachment{Disposition: a.Disposition}
			if !includeInline && att.IsInline() {
				continue
			}
			out = append(out, ConversationAttachment{
				ID: a.ID, Name: a.Name, Size: a.Size,
				MIMEType: a.MIMEType, Disposition: a.Disposition, MessageID: m.ID,
			})
		}
	}
	return out, nil
}

// AttachmentsList drops inline attachments unless includeInline is set.
func (s *Service) AttachmentsList(ctx context.Context, msgID string, includeInline bool) ([]Attachment, error) {
	var r struct {
		Message struct {
			Attachments []struct {
				ID, Name, MIMEType, Disposition string
				Size                            int64
			}
		}
	}
	if err := s.C.Decode(ctx, proton.Request{Method: "GET", Path: "/mail/v4/messages/" + msgID}, &r); err != nil {
		return nil, err
	}
	out := make([]Attachment, 0, len(r.Message.Attachments))
	for _, a := range r.Message.Attachments {
		out = append(out, Attachment{ID: a.ID, Name: a.Name, Size: a.Size, MIMEType: a.MIMEType, Disposition: a.Disposition})
	}
	if !includeInline {
		out = FilterInline(out)
	}
	return out, nil
}

func (s *Service) AttachmentDownload(ctx context.Context, msgID, attID string) ([]byte, string, error) {
	var r struct {
		Message struct {
			AddressID   string
			Attachments []struct {
				ID, Name, KeyPackets string
			}
		}
	}
	u, err := s.keys.Alongside(ctx, func(ctx context.Context) error {
		return s.C.Decode(ctx, proton.Request{Method: "GET", Path: "/mail/v4/messages/" + msgID}, &r)
	})
	if err != nil {
		return nil, "", err
	}
	var keyPackets, name string
	for _, a := range r.Message.Attachments {
		if a.ID == attID {
			keyPackets, name = a.KeyPackets, a.Name
			break
		}
	}
	if keyPackets == "" {
		return nil, "", &errs.NotFound{Kind: "attachment", Ref: attID}
	}
	addrKR, ok := u.AddrKR(r.Message.AddressID)
	if !ok {
		kr, _, err := u.FirstAddr()
		if err != nil {
			return nil, "", err
		}
		addrKR = kr
	}
	resp, err := s.C.Do(ctx, proton.Request{Method: "GET", Path: "/mail/v4/attachments/" + attID})
	if err != nil {
		return nil, "", err
	}
	kp, err := base64.StdEncoding.DecodeString(keyPackets)
	if err != nil {
		return nil, "", fmt.Errorf("decode key packets: %w", err)
	}
	split := pgp.NewPGPSplitMessage(kp, resp.Body)
	dec, err := addrKR.Decrypt(split.GetPGPMessage(), nil, 0)
	if err != nil {
		return nil, "", fmt.Errorf("decrypt attachment: %w", err)
	}
	return dec.GetBinary(), name, nil
}

// ReadLocalAttachment reads a file into a LocalAttachment, resolving its MIME
// type from the extension. inline marks an image to embed in an HTML body.
func ReadLocalAttachment(path string, inline bool) (LocalAttachment, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return LocalAttachment{}, err
	}
	return LocalAttachment{
		Filename: filepath.Base(path),
		MIMEType: mimeTypeForPath(path),
		Data:     data,
		Inline:   inline,
	}, nil
}

func mimeTypeForPath(path string) string {
	if t := mime.TypeByExtension(filepath.Ext(path)); t != "" {
		return t
	}
	return "application/octet-stream"
}

// assignInlineContentIDs gives every inline attachment a Content-ID and appends
// an <img src="cid:..."> reference to the body, so the image renders where the
// message says it should. Uploading with a Content-ID is what makes Proton
// record the part as disposition "inline", and it only works in an HTML body.
func assignInlineContentIDs(c *Content) error {
	var imgs strings.Builder
	for i := range c.Attach {
		a := &c.Attach[i]
		if !a.Inline || a.ContentID != "" {
			continue
		}
		if !c.HTML {
			return errs.Problemf("Inline attachments need an HTML body.").
				Hint("pass --html, or attach the image with --attach instead")
		}
		cid, err := newContentID(c.From.Address.Email)
		if err != nil {
			return err
		}
		a.ContentID = cid
		fmt.Fprintf(&imgs, "<img src=%q alt=%q>", "cid:"+cid, a.Filename)
	}
	c.Body += imgs.String()
	return nil
}

// newContentID returns a Content-ID of the form <hex>@<sender-domain>, matching
// the shape Proton's web client generates.
func newContentID(senderEmail string) (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	domain := "proton.me"
	if i := strings.LastIndex(senderEmail, "@"); i >= 0 && i+1 < len(senderEmail) {
		domain = senderEmail[i+1:]
	}
	return hex.EncodeToString(b) + "@" + domain, nil
}

// uploadAttachments uploads each local attachment against a draft, returning
// them with the server IDs and session keys the send path needs.
func (s *Service) uploadAttachments(ctx context.Context, addrKR *pgp.KeyRing, messageID string, atts []LocalAttachment) ([]*draftAttachment, error) {
	out := make([]*draftAttachment, 0, len(atts))
	for _, a := range atts {
		uploaded, err := s.uploadAttachment(ctx, addrKR, messageID, a)
		if err != nil {
			return nil, err
		}
		out = append(out, uploaded)
	}
	return out, nil
}

// uploadAttachment encrypts an attachment with a fresh session key (key packet
// wrapped to the draft's address key), detached-signs it, and uploads it as a
// multipart form against the draft.
func (s *Service) uploadAttachment(ctx context.Context, addrKR *pgp.KeyRing, messageID string, a LocalAttachment) (*draftAttachment, error) {
	msg := pgp.NewPlainMessage(a.Data)

	sk, err := pgp.GenerateSessionKey()
	if err != nil {
		return nil, err
	}
	dataPacket, err := sk.Encrypt(msg)
	if err != nil {
		return nil, fmt.Errorf("encrypt attachment data: %w", err)
	}
	keyPacket, err := addrKR.EncryptSessionKey(sk)
	if err != nil {
		return nil, fmt.Errorf("encrypt attachment key: %w", err)
	}
	sig, err := addrKR.SignDetached(msg)
	if err != nil {
		return nil, fmt.Errorf("sign attachment: %w", err)
	}

	mimeType := a.MIMEType
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	body, contentType, err := buildAttachmentForm(map[string]string{
		"Filename":  a.Filename,
		"MessageID": messageID,
		"ContentID": a.ContentID,
		"MIMEType":  mimeType,
	}, map[string][]byte{
		"KeyPackets": keyPacket,
		"DataPacket": dataPacket,
		"Signature":  sig.GetBinary(),
	})
	if err != nil {
		return nil, err
	}

	var res struct {
		Attachment struct{ ID string }
	}
	if err := s.C.Decode(ctx, proton.Request{
		Method: "POST", Path: "/mail/v4/attachments", Body: body, ContentType: contentType,
	}, &res); err != nil {
		return nil, fmt.Errorf("upload attachment %s: %w", a.Filename, err)
	}
	return &draftAttachment{
		ID: res.Attachment.ID, Name: a.Filename, MIMEType: mimeType,
		ContentID: a.ContentID, Size: int64(len(a.Data)), SessionKey: sk, Data: a.Data,
	}, nil
}

func buildAttachmentForm(fields map[string]string, files map[string][]byte) (body []byte, contentType string, err error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for k, v := range fields {
		if err := w.WriteField(k, v); err != nil {
			return nil, "", err
		}
	}
	for name, data := range files {
		part, err := w.CreateFormFile(name, name)
		if err != nil {
			return nil, "", err
		}
		if _, err := part.Write(data); err != nil {
			return nil, "", err
		}
	}
	if err := w.Close(); err != nil {
		return nil, "", err
	}
	return buf.Bytes(), w.FormDataContentType(), nil
}

// attachmentKeyPackets wraps each attachment session key to an internal
// recipient's key ring (Type-1 per-recipient packets).
func attachmentKeyPackets(recKR *pgp.KeyRing, atts []*draftAttachment) (map[string]string, error) {
	if len(atts) == 0 {
		return nil, nil
	}
	out := make(map[string]string, len(atts))
	for _, a := range atts {
		if a.SessionKey == nil {
			return nil, fmt.Errorf("attachment %s: its key could not be read, so it cannot be sent", a.Name)
		}
		kp, err := recKR.EncryptSessionKey(a.SessionKey)
		if err != nil {
			return nil, err
		}
		out[a.ID] = base64.StdEncoding.EncodeToString(kp)
	}
	return out, nil
}

// attachmentPasswordKeyPackets wraps each attachment session key with the EO
// password (symmetric packets), for encrypted-for-outside recipients.
func attachmentPasswordKeyPackets(atts []*draftAttachment, password string) (map[string]string, error) {
	if len(atts) == 0 {
		return nil, nil
	}
	out := make(map[string]string, len(atts))
	for _, a := range atts {
		if a.SessionKey == nil {
			return nil, fmt.Errorf("attachment %s: its key could not be read, so it cannot be sent", a.Name)
		}
		kp, err := pgp.EncryptSessionKeyWithPassword(a.SessionKey, []byte(password))
		if err != nil {
			return nil, err
		}
		out[a.ID] = base64.StdEncoding.EncodeToString(kp)
	}
	return out, nil
}

// attachmentCleartextKeys exposes raw attachment session keys for external
// (Type-4 cleartext) packages.
func attachmentCleartextKeys(atts []*draftAttachment) map[string]any {
	if len(atts) == 0 {
		return nil
	}
	out := make(map[string]any, len(atts))
	for _, a := range atts {
		if a.SessionKey == nil {
			continue
		}
		out[a.ID] = map[string]any{
			"Key":       base64.StdEncoding.EncodeToString(a.SessionKey.Key),
			"Algorithm": a.SessionKey.Algo,
		}
	}
	return out
}
