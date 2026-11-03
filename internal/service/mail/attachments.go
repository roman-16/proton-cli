package mail

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"mime/multipart"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	pgp "github.com/ProtonMail/gopenpgp/v3/crypto"
	pgphelper "github.com/roman-16/proton-cli/internal/crypto/pgp"
	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/roman-16/proton-cli/internal/mimetype"
	"github.com/roman-16/proton-cli/internal/proton"
	"github.com/roman-16/proton-cli/internal/ref"
	"github.com/roman-16/proton-cli/internal/units"
)

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

type sealedAttachment struct {
	ID, Name, MIMEType, Disposition, KeyPackets string
	Size                                        int64
}

func MatchAttachment[T any](where, reference string, atts []T, describe func(T) Attachment) (T, error) {
	for _, a := range atts {
		if describe(a).ID == reference {
			return a, nil
		}
	}
	matches := make([]T, 0, 1)
	for _, a := range atts {
		if strings.EqualFold(describe(a).Name, reference) {
			matches = append(matches, a)
		}
	}
	if len(matches) == 0 {
		var zero T
		return zero, &errs.NotFound{Kind: "attachment", Where: where, Ref: reference}
	}
	return ref.Pick("attachment", reference, matches,
		func(a T) string { return describe(a).ID },
		func(a T) string {
			at := describe(a)
			return fmt.Sprintf("%s (%s)", at.Name, units.Size(at.Size))
		})
}

func (s *Service) AttachmentDownload(ctx context.Context, msgID, reference string) ([]byte, string, error) {
	var r struct {
		Message struct {
			AddressID   string
			Attachments []sealedAttachment
		}
	}
	u, err := s.keys.Alongside(ctx, func(ctx context.Context) error {
		return s.C.Decode(ctx, proton.Request{Method: "GET", Path: "/mail/v4/messages/" + msgID}, &r)
	})
	if err != nil {
		return nil, "", err
	}
	wanted, err := MatchAttachment("in that message", reference, r.Message.Attachments,
		func(a sealedAttachment) Attachment {
			return Attachment{ID: a.ID, Name: a.Name, Size: a.Size}
		})
	if err != nil {
		return nil, "", err
	}
	attID, keyPackets, name := wanted.ID, wanted.KeyPackets, wanted.Name
	if keyPackets == "" {
		return nil, "", &errs.NotFound{Kind: "attachment", Ref: reference}
	}
	rings, ok := u.AddrRings(r.Message.AddressID)
	if !ok {
		first, _, err := u.FirstAddr()
		if err != nil {
			return nil, "", err
		}
		rings = first
	}
	resp, err := s.C.Do(ctx, proton.Request{Method: "GET", Path: "/mail/v4/attachments/" + attID})
	if err != nil {
		return nil, "", err
	}
	kp, err := base64.StdEncoding.DecodeString(keyPackets)
	if err != nil {
		return nil, "", fmt.Errorf("decode key packets: %w", err)
	}
	msg := pgp.NewPGPSplitMessage(kp, resp.Body)
	dec, err := pgphelper.Decrypt(rings.Read, nil, msg.Bytes(), pgp.Bytes)
	if err != nil {
		return nil, "", u.Explain(fmt.Errorf("decrypt attachment: %w", err), "attachment", msg)
	}
	return dec.Bytes(), name, nil
}

var calendarExtensions = []string{".ical", ".icalendar", ".ics", ".ifb"}

func CalendarAttachments(atts []Attachment) []Attachment {
	var out []Attachment
	for _, a := range atts {
		if slices.Contains(calendarExtensions, strings.ToLower(filepath.Ext(a.Name))) {
			out = append(out, a)
		}
	}
	return out
}

func ReadLocalAttachment(path string, inline bool) (LocalAttachment, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return LocalAttachment{}, err
	}
	return LocalAttachment{
		Filename: filepath.Base(path),
		MIMEType: mimetype.ByName(path),
		Data:     data,
		Inline:   inline,
	}, nil
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

func (s *Service) uploadAttachment(ctx context.Context, addrKR *pgp.KeyRing, messageID string, a LocalAttachment) (*draftAttachment, error) {
	sk, err := pgphelper.PGP.GenerateSessionKey()
	if err != nil {
		return nil, err
	}
	dataPacket, err := pgphelper.EncryptBinaryWithSessionKey(sk, nil, a.Data)
	if err != nil {
		return nil, fmt.Errorf("encrypt attachment data: %w", err)
	}
	keyPacket, err := pgphelper.EncryptSessionKey(addrKR, sk)
	if err != nil {
		return nil, fmt.Errorf("encrypt attachment key: %w", err)
	}
	sig, err := pgphelper.SignBinary(addrKR, a.Data, pgp.Bytes)
	if err != nil {
		return nil, fmt.Errorf("sign attachment: %w", err)
	}

	mimeType := a.MIMEType
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	body, contentType, err := buildAttachmentForm([]formField{
		{Name: "Filename", Value: a.Filename},
		{Name: "MessageID", Value: messageID},
		{Name: "ContentID", Value: a.ContentID},
		{Name: "MIMEType", Value: mimeType},
	}, []formFile{
		{Name: "KeyPackets", Filename: "KeyPackets", Data: keyPacket},
		{Name: "DataPacket", Filename: "DataPacket", Data: dataPacket},
		{Name: "Signature", Filename: "Signature", Data: sig},
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

type formField struct{ Name, Value string }

type formFile struct {
	Name, Filename string
	Data           []byte
}

func buildAttachmentForm(fields []formField, files []formFile) (body []byte, contentType string, err error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for _, f := range fields {
		if err := w.WriteField(f.Name, f.Value); err != nil {
			return nil, "", err
		}
	}
	for _, f := range files {
		part, err := w.CreateFormFile(f.Name, f.Filename)
		if err != nil {
			return nil, "", err
		}
		if _, err := part.Write(f.Data); err != nil {
			return nil, "", err
		}
	}
	if err := w.Close(); err != nil {
		return nil, "", err
	}
	return buf.Bytes(), w.FormDataContentType(), nil
}

func attachmentKeyPackets(recKR *pgp.KeyRing, atts []*draftAttachment) (map[string]string, error) {
	if len(atts) == 0 {
		return nil, nil
	}
	out := make(map[string]string, len(atts))
	for _, a := range atts {
		if a.SessionKey == nil {
			return nil, fmt.Errorf("attachment %s: its key could not be read, so it cannot be sent", a.Name)
		}
		kp, err := pgphelper.EncryptSessionKey(recKR, a.SessionKey)
		if err != nil {
			return nil, err
		}
		out[a.ID] = base64.StdEncoding.EncodeToString(kp)
	}
	return out, nil
}

func attachmentPasswordKeyPackets(atts []*draftAttachment, password string) (map[string]string, error) {
	if len(atts) == 0 {
		return nil, nil
	}
	out := make(map[string]string, len(atts))
	for _, a := range atts {
		if a.SessionKey == nil {
			return nil, fmt.Errorf("attachment %s: its key could not be read, so it cannot be sent", a.Name)
		}
		kp, err := pgphelper.EncryptSessionKeyWithPassword([]byte(password), a.SessionKey)
		if err != nil {
			return nil, err
		}
		out[a.ID] = base64.StdEncoding.EncodeToString(kp)
	}
	return out, nil
}

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
