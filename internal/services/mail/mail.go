// Package mail provides Proton Mail operations.
package mail

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	pgp "github.com/ProtonMail/gopenpgp/v2/crypto"
	"github.com/roman-16/proton-cli/internal/api"
	"github.com/roman-16/proton-cli/internal/keys"
)

// Proton built-in mailbox label IDs.
var MailboxLabelIDs = map[string]string{
	"inbox":   "0",
	"drafts":  "8",
	"sent":    "7",
	"trash":   "3",
	"spam":    "4",
	"archive": "6",
	"starred": "10",
	"all":     "5",
}

// ResolveFolder returns the Proton label ID for a folder name/alias; unknown
// strings are passed through so callers can use custom-label IDs directly.
func ResolveFolder(name string) string {
	if id, ok := MailboxLabelIDs[strings.ToLower(name)]; ok {
		return id
	}
	return name
}

// Service is the Mail domain service.
type Service struct{ C *api.Client }

// New constructs a mail service.
func New(c *api.Client) *Service { return &Service{C: c} }

// Message is a list-view mail message.
type Message struct {
	ID             string `json:"id"`
	Subject        string `json:"subject"`
	FromName       string `json:"from_name,omitempty"`
	FromAddress    string `json:"from_address"`
	Time           int64  `json:"time"`
	Unread         int    `json:"unread"`
	NumAttachments int    `json:"num_attachments"`
}

// Full is a decrypted single-message view.
type Full struct {
	ID          string           `json:"id"`
	Subject     string           `json:"subject"`
	Sender      map[string]any   `json:"sender"`
	ToList      []map[string]any `json:"to_list"`
	Body        string           `json:"body"`
	MIMEType    string           `json:"mime_type"`
	AddressID   string           `json:"address_id"`
	Attachments []Attachment     `json:"attachments,omitempty"`
}

// Attachment describes a message attachment.
type Attachment struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Size       int64  `json:"size"`
	MIMEType   string `json:"mime_type"`
	KeyPackets string `json:"-"`
}

// ListOptions filters for List.
type ListOptions struct {
	Folder   string
	Page     int
	PageSize int
	Unread   bool
}

// SearchOptions filters for Search.
type SearchOptions struct {
	Keyword, From, To, Subject, Folder, After, Before string
	Limit                                             int
	Unread                                            bool
}

// List returns a page of messages.
func (s *Service) List(ctx context.Context, opts ListOptions) ([]Message, int, error) {
	if opts.PageSize <= 0 {
		opts.PageSize = 25
	}
	q := url.Values{}
	q.Set("LabelID", ResolveFolder(opts.Folder))
	q.Set("Page", fmt.Sprintf("%d", opts.Page))
	q.Set("PageSize", fmt.Sprintf("%d", opts.PageSize))
	q.Set("Sort", "Time")
	q.Set("Desc", "1")
	if opts.Unread {
		q.Set("Unread", "1")
	}
	var r struct {
		Total    int
		Messages []struct {
			ID             string
			Subject        string
			Unread         int
			Time           int64
			Sender         struct{ Name, Address string }
			NumAttachments int
		}
	}
	if err := s.C.Send(ctx, api.Request{Method: "GET", Path: "/mail/v4/messages", Query: q}, &r); err != nil {
		return nil, 0, err
	}
	out := make([]Message, 0, len(r.Messages))
	for _, m := range r.Messages {
		out = append(out, Message{
			ID: m.ID, Subject: m.Subject, Unread: m.Unread, Time: m.Time,
			FromName: m.Sender.Name, FromAddress: m.Sender.Address,
			NumAttachments: m.NumAttachments,
		})
	}
	return out, r.Total, nil
}

// Search returns messages matching the given filters.
func (s *Service) Search(ctx context.Context, opts SearchOptions) ([]Message, int, error) {
	if opts.Limit <= 0 {
		opts.Limit = 25
	}
	q := url.Values{}
	folder := opts.Folder
	if folder == "" {
		folder = "all"
	}
	q.Set("LabelID", ResolveFolder(folder))
	q.Set("Sort", "Time")
	q.Set("Desc", "1")
	q.Set("PageSize", fmt.Sprintf("%d", opts.Limit))
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
		q.Set("To", opts.To)
	}
	if opts.Subject != "" {
		q.Set("Subject", opts.Subject)
	}
	if opts.After != "" {
		t, err := time.Parse("2006-01-02", opts.After)
		if err != nil {
			return nil, 0, fmt.Errorf("invalid --after: %w", err)
		}
		q.Set("Begin", fmt.Sprintf("%d", t.Unix()))
	}
	if opts.Before != "" {
		t, err := time.Parse("2006-01-02", opts.Before)
		if err != nil {
			return nil, 0, fmt.Errorf("invalid --before: %w", err)
		}
		q.Set("End", fmt.Sprintf("%d", t.Unix()))
	}
	var r struct {
		Total    int
		Messages []struct {
			ID             string
			Subject        string
			Unread         int
			Time           int64
			Sender         struct{ Name, Address string }
			NumAttachments int
		}
	}
	if err := s.C.Send(ctx, api.Request{Method: "GET", Path: "/mail/v4/messages", Query: q}, &r); err != nil {
		return nil, 0, err
	}
	out := make([]Message, 0, len(r.Messages))
	for _, m := range r.Messages {
		out = append(out, Message{
			ID: m.ID, Subject: m.Subject, Unread: m.Unread, Time: m.Time,
			FromName: m.Sender.Name, FromAddress: m.Sender.Address,
			NumAttachments: m.NumAttachments,
		})
	}
	return out, r.Total, nil
}

// Read returns a single message with decrypted body.
func (s *Service) Read(ctx context.Context, u *keys.Unlocked, id string) (*Full, error) {
	var r struct {
		Message struct {
			ID          string
			Subject     string
			Sender      map[string]any
			ToList      []map[string]any
			Body        string
			MIMEType    string
			AddressID   string
			Attachments []struct {
				ID, Name, MIMEType, KeyPackets string
				Size                           int64
			}
		}
	}
	if err := s.C.Send(ctx, api.Request{Method: "GET", Path: "/mail/v4/messages/" + id}, &r); err != nil {
		return nil, err
	}
	addrKR, ok := u.AddrKR(r.Message.AddressID)
	if !ok {
		kr, _, _, err := u.FirstAddrKR()
		if err != nil {
			return nil, err
		}
		addrKR = kr
	}
	body, err := decryptBody(r.Message.Body, addrKR)
	if err != nil {
		body = "(decryption failed: " + err.Error() + ")"
	}
	atts := make([]Attachment, 0, len(r.Message.Attachments))
	for _, a := range r.Message.Attachments {
		atts = append(atts, Attachment{ID: a.ID, Name: a.Name, Size: a.Size, MIMEType: a.MIMEType, KeyPackets: a.KeyPackets})
	}
	return &Full{
		ID:          r.Message.ID,
		Subject:     r.Message.Subject,
		Sender:      r.Message.Sender,
		ToList:      r.Message.ToList,
		Body:        body,
		MIMEType:    r.Message.MIMEType,
		AddressID:   r.Message.AddressID,
		Attachments: atts,
	}, nil
}

// Trash moves messages to trash.
func (s *Service) Trash(ctx context.Context, ids []string) error {
	return s.C.Send(ctx, api.Request{
		Method: "PUT", Path: "/mail/v4/messages/label",
		Body: map[string]any{"LabelID": "3", "IDs": ids},
	}, nil)
}

// Delete permanently deletes messages.
func (s *Service) Delete(ctx context.Context, ids []string) error {
	return s.C.Send(ctx, api.Request{
		Method: "PUT", Path: "/mail/v4/messages/delete",
		Body: map[string]any{"IDs": ids},
	}, nil)
}

// Move moves messages to the given folder (name or label ID).
func (s *Service) Move(ctx context.Context, ids []string, folder string) error {
	return s.C.Send(ctx, api.Request{
		Method: "PUT", Path: "/mail/v4/messages/label",
		Body: map[string]any{"LabelID": ResolveFolder(folder), "IDs": ids},
	}, nil)
}

// Mark sets read/unread/starred flags on messages.
func (s *Service) Mark(ctx context.Context, ids []string, read, unread, starred, unstar bool) error {
	body := map[string]any{"IDs": ids}
	if read {
		if err := s.C.Send(ctx, api.Request{Method: "PUT", Path: "/mail/v4/messages/read", Body: body}, nil); err != nil {
			return err
		}
	}
	if unread {
		if err := s.C.Send(ctx, api.Request{Method: "PUT", Path: "/mail/v4/messages/unread", Body: body}, nil); err != nil {
			return err
		}
	}
	if starred {
		if err := s.C.Send(ctx, api.Request{Method: "PUT", Path: "/mail/v4/messages/label", Body: map[string]any{"LabelID": "10", "IDs": ids}}, nil); err != nil {
			return err
		}
	}
	if unstar {
		if err := s.C.Send(ctx, api.Request{Method: "PUT", Path: "/mail/v4/messages/unlabel", Body: map[string]any{"LabelID": "10", "IDs": ids}}, nil); err != nil {
			return err
		}
	}
	return nil
}

// AttachmentsList returns the attachment metadata for a message.
func (s *Service) AttachmentsList(ctx context.Context, msgID string) ([]Attachment, error) {
	var r struct {
		Message struct {
			Attachments []struct {
				ID       string
				Name     string
				Size     int64
				MIMEType string
			}
		}
	}
	if err := s.C.Send(ctx, api.Request{Method: "GET", Path: "/mail/v4/messages/" + msgID}, &r); err != nil {
		return nil, err
	}
	out := make([]Attachment, 0, len(r.Message.Attachments))
	for _, a := range r.Message.Attachments {
		out = append(out, Attachment{ID: a.ID, Name: a.Name, Size: a.Size, MIMEType: a.MIMEType})
	}
	return out, nil
}

// AttachmentDownload returns the decrypted bytes of a single attachment.
func (s *Service) AttachmentDownload(ctx context.Context, u *keys.Unlocked, msgID, attID string) ([]byte, string, error) {
	var r struct {
		Message struct {
			AddressID   string
			Attachments []struct {
				ID, Name, KeyPackets string
			}
		}
	}
	if err := s.C.Send(ctx, api.Request{Method: "GET", Path: "/mail/v4/messages/" + msgID}, &r); err != nil {
		return nil, "", err
	}
	var keyPackets, name string
	for _, a := range r.Message.Attachments {
		if a.ID == attID {
			keyPackets = a.KeyPackets
			name = a.Name
			break
		}
	}
	if keyPackets == "" {
		return nil, "", fmt.Errorf("attachment %s not found in message %s", attID, msgID)
	}
	addrKR, ok := u.AddrKR(r.Message.AddressID)
	if !ok {
		kr, _, _, err := u.FirstAddrKR()
		if err != nil {
			return nil, "", err
		}
		addrKR = kr
	}
	resp, err := s.C.Do(ctx, api.Request{Method: "GET", Path: "/mail/v4/attachments/" + attID})
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

func decryptBody(armored string, addrKR *pgp.KeyRing) (string, error) {
	msg, err := pgp.NewPGPMessageFromArmored(armored)
	if err != nil {
		return "", fmt.Errorf("parse message: %w", err)
	}
	dec, err := addrKR.Decrypt(msg, nil, pgp.GetUnixTime())
	if err != nil {
		return "", fmt.Errorf("decrypt message: %w", err)
	}
	return dec.GetString(), nil
}

// Label describes a label or folder.
type Label struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
	Type  int    `json:"type"`
	Path  string `json:"path,omitempty"`
}

// LabelsList returns all labels + folders.
func (s *Service) LabelsList(ctx context.Context) ([]Label, []Label, error) {
	var labels, folders struct {
		Labels []struct {
			ID, Name, Color, Path string
			Type                  int
		}
	}
	if err := s.C.Send(ctx, api.Request{Method: "GET", Path: "/core/v4/labels", Query: keys.Query("Type", "1")}, &labels); err != nil {
		return nil, nil, err
	}
	if err := s.C.Send(ctx, api.Request{Method: "GET", Path: "/core/v4/labels", Query: keys.Query("Type", "3")}, &folders); err != nil {
		return nil, nil, err
	}
	toLabel := func(l struct {
		ID, Name, Color, Path string
		Type                  int
	}) Label {
		return Label{ID: l.ID, Name: l.Name, Color: l.Color, Type: l.Type, Path: l.Path}
	}
	var ll, ff []Label
	for _, l := range labels.Labels {
		ll = append(ll, toLabel(l))
	}
	for _, l := range folders.Labels {
		ff = append(ff, toLabel(l))
	}
	return ll, ff, nil
}

// LabelCreate creates a label (isFolder=false) or folder (isFolder=true).
func (s *Service) LabelCreate(ctx context.Context, name, color string, isFolder bool) ([]byte, error) {
	t := 1
	if isFolder {
		t = 3
	}
	resp, err := s.C.Do(ctx, api.Request{
		Method: "POST", Path: "/core/v4/labels",
		Body: map[string]any{"Name": name, "Color": color, "Type": t},
	})
	if err != nil {
		return nil, err
	}
	return resp.Body, nil
}

// LabelDelete deletes labels/folders by ID.
func (s *Service) LabelDelete(ctx context.Context, ids []string) error {
	return s.C.Send(ctx, api.Request{
		Method: "DELETE", Path: "/core/v4/labels",
		Body: map[string]any{"LabelIDs": ids},
	}, nil)
}

// FiltersList returns all sieve filters.
func (s *Service) FiltersList(ctx context.Context) ([]byte, error) {
	resp, err := s.C.Do(ctx, api.Request{Method: "GET", Path: "/mail/v4/filters"})
	if err != nil {
		return nil, err
	}
	return resp.Body, nil
}

// FilterCreate creates a sieve filter.
func (s *Service) FilterCreate(ctx context.Context, name, sieve string, status int) ([]byte, error) {
	resp, err := s.C.Do(ctx, api.Request{
		Method: "POST", Path: "/mail/v4/filters",
		Body: map[string]any{"Name": name, "Sieve": sieve, "Version": 2, "Status": status},
	})
	if err != nil {
		return nil, err
	}
	return resp.Body, nil
}

// FilterDelete deletes a filter by ID.
func (s *Service) FilterDelete(ctx context.Context, id string) error {
	return s.C.Send(ctx, api.Request{Method: "DELETE", Path: "/mail/v4/filters/" + id}, nil)
}

// FilterEnable enables a filter.
func (s *Service) FilterEnable(ctx context.Context, id string) error {
	return s.C.Send(ctx, api.Request{Method: "PUT", Path: "/mail/v4/filters/" + id + "/enable"}, nil)
}

// FilterDisable disables a filter.
func (s *Service) FilterDisable(ctx context.Context, id string) error {
	return s.C.Send(ctx, api.Request{Method: "PUT", Path: "/mail/v4/filters/" + id + "/disable"}, nil)
}

// AddressesList returns all account email addresses.
func (s *Service) AddressesList(ctx context.Context) ([]byte, error) {
	resp, err := s.C.Do(ctx, api.Request{Method: "GET", Path: "/core/v4/addresses"})
	if err != nil {
		return nil, err
	}
	return resp.Body, nil
}

// Send sends a new mail. Handles both internal (PGP) and external recipients.
func (s *Service) Send(ctx context.Context, u *keys.Unlocked, to, subject, body string) error {
	addrKR, _, senderEmail, err := u.FirstAddrKR()
	if err != nil {
		return err
	}

	// Create draft.
	plainMsg := pgp.NewPlainMessageFromString(body)
	encDraft, err := addrKR.Encrypt(plainMsg, addrKR)
	if err != nil {
		return fmt.Errorf("encrypt draft: %w", err)
	}
	armoredDraft, err := encDraft.GetArmored()
	if err != nil {
		return err
	}
	var draft struct {
		Code    int
		Message struct{ ID string }
	}
	draftBody := map[string]any{
		"Message": map[string]any{
			"ToList":   []map[string]string{{"Address": to, "Name": to}},
			"CCList":   []any{},
			"BCCList":  []any{},
			"Subject":  subject,
			"Sender":   map[string]string{"Address": senderEmail, "Name": ""},
			"Body":     armoredDraft,
			"MIMEType": "text/plain",
		},
	}
	if err := s.C.Send(ctx, api.Request{Method: "POST", Path: "/mail/v4/messages", Body: draftBody}, &draft); err != nil {
		return err
	}
	messageID := draft.Message.ID
	cleanup := func() {
		_, _ = s.C.Do(ctx, api.Request{
			Method: "DELETE", Path: "/mail/v4/messages/delete",
			Body: map[string]any{"IDs": []string{messageID}},
		})
	}

	// Fetch recipient key.
	var keysRes struct {
		Address struct {
			Keys []struct{ PublicKey string }
		}
	}
	if err := s.C.Send(ctx, api.Request{Method: "GET", Path: "/core/v4/keys/all", Query: keys.Query("Email", to)}, &keysRes); err != nil {
		cleanup()
		return err
	}
	internal := len(keysRes.Address.Keys) > 0

	sessionKey, err := pgp.GenerateSessionKey()
	if err != nil {
		cleanup()
		return err
	}
	encBody, err := sessionKey.EncryptAndSign(pgp.NewPlainMessageFromString(body), addrKR)
	if err != nil {
		cleanup()
		return err
	}

	var packages []map[string]any
	if internal {
		bodyKP, err := addrKR.EncryptSessionKey(sessionKey)
		if err != nil {
			cleanup()
			return err
		}
		recKey, err := pgp.NewKeyFromArmored(keysRes.Address.Keys[0].PublicKey)
		if err != nil {
			cleanup()
			return fmt.Errorf("parse recipient key: %w", err)
		}
		recKR, err := pgp.NewKeyRing(recKey)
		if err != nil {
			cleanup()
			return err
		}
		recKP, err := recKR.EncryptSessionKey(sessionKey)
		if err != nil {
			cleanup()
			return err
		}
		packages = []map[string]any{{
			"Addresses": map[string]any{
				to: map[string]any{
					"Type":          1,
					"BodyKeyPacket": base64.StdEncoding.EncodeToString(recKP),
					"Signature":     0,
				},
			},
			"MIMEType":      "text/plain",
			"Type":          1,
			"Body":          base64.StdEncoding.EncodeToString(encBody),
			"BodyKeyPacket": base64.StdEncoding.EncodeToString(bodyKP),
		}}
	} else {
		packages = []map[string]any{{
			"Addresses": map[string]any{
				to: map[string]any{"Type": 4, "Signature": 0},
			},
			"MIMEType": "text/plain",
			"Type":     4,
			"Body":     base64.StdEncoding.EncodeToString(encBody),
			"BodyKey":  map[string]any{"Key": base64.StdEncoding.EncodeToString(sessionKey.Key), "Algorithm": sessionKey.Algo},
		}}
	}

	sendReq := map[string]any{
		"ExpirationTime":   nil,
		"AutoSaveContacts": 0,
		"Packages":         packages,
	}
	resp, err := s.C.Do(ctx, api.Request{Method: "POST", Path: "/mail/v4/messages/" + messageID, Body: sendReq})
	if err != nil {
		cleanup()
		return err
	}
	if resp.Status >= 400 {
		cleanup()
		return fmt.Errorf("send failed: %s", string(resp.Body))
	}
	return nil
}

// Resolve returns a message ID for either a literal ID or a subject/from search.
func (s *Service) Resolve(ctx context.Context, ref string) (string, error) {
	if looksLikeID(ref) {
		return ref, nil
	}
	msgs, _, err := s.Search(ctx, SearchOptions{Keyword: ref, Folder: "all", Limit: 20})
	if err != nil {
		return "", err
	}
	switch len(msgs) {
	case 0:
		return "", fmt.Errorf("no messages matching %q", ref)
	case 1:
		return msgs[0].ID, nil
	}
	lines := []string{fmt.Sprintf("ambiguous: %d messages match %q:", len(msgs), ref)}
	for _, m := range msgs {
		lines = append(lines, fmt.Sprintf("  %s  %s  %s", m.ID, m.FromAddress, m.Subject))
	}
	return "", fmt.Errorf("%s", strings.Join(lines, "\n"))
}

// RawJSON convenience: for commands that want to emit the server payload as-is.
func RawJSON(b []byte) (json.RawMessage, error) { return json.RawMessage(b), nil }

func looksLikeID(s string) bool { return len(s) > 60 && strings.HasSuffix(s, "==") }
