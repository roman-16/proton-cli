package mail

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"strings"
	"testing"

	pgp "github.com/ProtonMail/gopenpgp/v2/crypto"
	"github.com/ProtonMail/gopenpgp/v2/helper"
	"github.com/roman-16/proton-cli/internal/proton"
)

// What a sender chose, and what Proton then holds: the id in the link, the
// token that stands in for a session, and the password that opens both.
const (
	eoTestID       = "9fK2pQ7xNv4mB8"
	eoTestToken    = "dG9rZW4tZm9yLXRoZS1tZXNzYWdl"
	eoTestPassword = "correct horse battery"
	eoTestBody     = "Here are the numbers we discussed."
)

func TestParseEOLink(t *testing.T) {
	for _, tt := range []struct {
		raw     string
		id      string
		refused bool
	}{
		{raw: "https://mail.proton.me/eo/9fK2pQ7xNv4mB8", id: eoTestID},
		{raw: "  https://mail.proton.me/eo/9fK2pQ7xNv4mB8  ", id: eoTestID},
		{raw: "https://mail.proton.me/eo/9fK2pQ7xNv4mB8/", id: eoTestID},
		{raw: "https://mail.protonmail.com/eo/9fK2pQ7xNv4mB8", id: eoTestID},
		{raw: "mail.proton.me/eo/9fK2pQ7xNv4mB8", id: eoTestID},
		{raw: "9fK2pQ7xNv4mB8", id: eoTestID},
		{raw: "https://mail.proton.me/eo/", refused: true},
		{raw: "https://mail.proton.me/u/0/inbox", refused: true},
		{raw: "", refused: true},
	} {
		id, err := ParseEOLink(tt.raw)
		if tt.refused {
			if err == nil {
				t.Errorf("ParseEOLink(%q) read a message out of it: %q", tt.raw, id)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseEOLink(%q): %v", tt.raw, err)
			continue
		}
		if id != tt.id {
			t.Errorf("ParseEOLink(%q) = %q, want %q", tt.raw, id, tt.id)
		}
	}
}

// A bare id is the shape every reference in the mailbox has, so only a whole
// link says "this one is not yours".
func TestOnlyAWholeLinkIsOneSomebodySentYou(t *testing.T) {
	for _, tt := range []struct {
		raw  string
		link bool
	}{
		{raw: "https://mail.proton.me/eo/9fK2pQ7xNv4mB8", link: true},
		{raw: "9fK2pQ7xNv4mB8"},
		{raw: "Invoice #2291"},
		{raw: "5bH2mQxK"},
	} {
		if got := IsEOLink(tt.raw); got != tt.link {
			t.Errorf("IsEOLink(%q) = %v, want %v", tt.raw, got, tt.link)
		}
	}
}

// eoServer is Proton holding one password-protected message: a token sealed to
// the password, a body sealed the same way, one attachment whose key packets
// are too, and whatever answer came back.
type eoServer struct {
	t           *testing.T
	sender      *pgp.KeyRing
	attachment  []byte
	attKeyPacke []byte
	attData     []byte
	replies     int
	expires     int64

	replyForm []byte
	replyType string
}

func newEOServer(t *testing.T) *eoServer {
	t.Helper()
	s := &eoServer{t: t, sender: genMailKeyRing(t), attachment: []byte("%PDF-1.7 numbers"), expires: 1778000000}

	sk, err := pgp.GenerateSessionKey()
	if err != nil {
		t.Fatalf("GenerateSessionKey: %v", err)
	}
	if s.attData, err = sk.Encrypt(pgp.NewPlainMessage(s.attachment)); err != nil {
		t.Fatalf("encrypt the attachment: %v", err)
	}
	if s.attKeyPacke, err = pgp.EncryptSessionKeyWithPassword(sk, []byte(eoTestPassword)); err != nil {
		t.Fatalf("seal the attachment key: %v", err)
	}
	return s
}

func (s *eoServer) Do(_ context.Context, r proton.Request) (*proton.Response, error) {
	switch {
	case strings.HasPrefix(r.Path, "/mail/v4/eo/attachment/"):
		return &proton.Response{Status: 200, Body: s.attData}, nil
	default:
		body, err := s.answer(r)
		if err != nil {
			return nil, err
		}
		return &proton.Response{Status: 200, Body: body}, nil
	}
}

func (s *eoServer) Decode(_ context.Context, r proton.Request, out any) error {
	body, err := s.answer(r)
	if err != nil {
		return err
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(body, out)
}

func (s *eoServer) answer(r proton.Request) ([]byte, error) {
	switch r.Path {
	case "/mail/v4/eo/token/" + eoTestID:
		sealed, err := helper.EncryptMessageWithPassword([]byte(eoTestPassword), eoTestToken)
		if err != nil {
			return nil, err
		}
		return json.Marshal(map[string]any{"Code": 1000, "Token": sealed})
	case "/mail/v4/eo/message":
		return s.message()
	case "/mail/v4/eo/reply":
		s.replyForm, _ = r.Body.([]byte)
		s.replyType = r.ContentType
		s.replies++
		return []byte(`{"Code":1000}`), nil
	}
	s.t.Fatalf("unexpected request %s %s", r.Method, r.Path)
	return nil, nil
}

func (s *eoServer) message() ([]byte, error) {
	body, err := helper.EncryptMessageWithPassword([]byte(eoTestPassword), eoTestBody)
	if err != nil {
		return nil, err
	}
	signer, err := s.sender.GetKey(0)
	if err != nil {
		return nil, err
	}
	key, err := signer.GetArmoredPublicKey()
	if err != nil {
		return nil, err
	}
	replies := make([]map[string]any, s.replies)
	for i := range replies {
		replies[i] = map[string]any{"Time": 1776000000 + i}
	}
	return json.Marshal(map[string]any{
		"Code": 1000,
		"Message": map[string]any{
			"Subject":        "Q3 numbers",
			"Sender":         map[string]any{"Name": "Jane Roe", "Address": "jane@proton.me"},
			"ToList":         []map[string]any{{"Address": "me@example.com"}},
			"Recipient":      "me@example.com",
			"Time":           1776000000,
			"ExpirationTime": s.expires,
			"Body":           body,
			"MIMEType":       "text/plain",
			"Replies":        replies,
			"Attachments": []map[string]any{{
				"ID": "att-1", "Name": "q3-report.pdf", "Size": len(s.attachment),
				"MIMEType": "application/pdf", "Disposition": "attachment",
				"KeyPackets": base64.StdEncoding.EncodeToString(s.attKeyPacke),
			}},
		},
		"PublicKey": key,
	})
}

func openEO(t *testing.T, srv *eoServer, ref string) *EOMessage {
	t.Helper()
	m, err := New(srv, testKeys(nil)).OpenEO(t.Context(), ref, eoTestPassword)
	if err != nil {
		t.Fatalf("OpenEO: %v", err)
	}
	return m
}

// The password opens the token, the token asks for the message, and the same
// password opens its body. Everything the reader is shown comes out of that.
func TestOpeningAPasswordProtectedMessage(t *testing.T) {
	srv := newEOServer(t)
	m := openEO(t, srv, "https://mail.proton.me/eo/"+eoTestID)

	if m.Subject != "Q3 numbers" {
		t.Errorf("subject = %q", m.Subject)
	}
	if m.Body != eoTestBody {
		t.Errorf("body = %q, want %q", m.Body, eoTestBody)
	}
	if m.Expires != srv.expires {
		t.Errorf("expires = %d, want %d", m.Expires, srv.expires)
	}
	if m.Replies != 0 {
		t.Errorf("replies = %d, want none", m.Replies)
	}
	if len(m.Attachments) != 1 || m.Attachments[0].Name != "q3-report.pdf" {
		t.Fatalf("attachments = %+v", m.Attachments)
	}
	// The key packets open the bytes and are nobody's business on a listing.
	if m.Attachments[0].KeyPackets != "" {
		t.Error("the listing carries key packets")
	}
}

// A wrong password is the ordinary mistake, so it is a sentence about the
// password rather than a failure about PGP.
func TestAWrongPasswordIsSaidPlainly(t *testing.T) {
	srv := newEOServer(t)
	_, err := New(srv, testKeys(nil)).OpenEO(t.Context(), eoTestID, "not the password")
	if err == nil {
		t.Fatal("a wrong password opened the message")
	}
	if !strings.Contains(err.Error(), "password does not open") {
		t.Errorf("err = %v, want it to name the password", err)
	}
}

// An attachment's key packets are sealed to the password too, so the bytes come
// back with no account anywhere in it.
func TestDownloadingAnAttachmentBehindAPassword(t *testing.T) {
	srv := newEOServer(t)
	m := openEO(t, srv, eoTestID)

	data, name, err := New(srv, testKeys(nil)).EOAttachment(t.Context(), m, "q3-report.pdf")
	if err != nil {
		t.Fatalf("EOAttachment: %v", err)
	}
	if name != "q3-report.pdf" {
		t.Errorf("name = %q", name)
	}
	if string(data) != string(srv.attachment) {
		t.Errorf("data = %q, want %q", data, srv.attachment)
	}
}

// An answer goes back twice over: sealed to the sender's key, which is the copy
// they read, and sealed to the password, which is the copy that stays behind
// the link.
func TestAnAnswerIsSealedToTheSenderAndToThePassword(t *testing.T) {
	srv := newEOServer(t)
	s := New(srv, testKeys(nil))
	m := openEO(t, srv, eoTestID)

	if err := s.EOReply(t.Context(), m, EOReplySpec{Body: "Got them, thanks."}); err != nil {
		t.Fatalf("EOReply: %v", err)
	}
	parts := eoFormParts(t, srv)

	sealed, err := pgp.NewPGPMessageFromArmored(parts["Body"])
	if err != nil {
		t.Fatalf("the sender's copy is not a PGP message: %v", err)
	}
	read, err := srv.sender.Decrypt(sealed, nil, 0)
	if err != nil {
		t.Fatalf("the sender cannot open their copy: %v", err)
	}
	if !strings.Contains(read.GetString(), "Got them, thanks.") {
		t.Errorf("the sender's copy reads %q", read.GetString())
	}
	// The original is quoted under the answer, as it is in every other reply.
	if !strings.Contains(read.GetString(), eoTestBody) {
		t.Errorf("the sender's copy does not quote the original: %q", read.GetString())
	}

	behind, err := helper.DecryptMessageWithPassword([]byte(eoTestPassword), parts["ReplyBody"])
	if err != nil {
		t.Fatalf("the password does not open the copy left behind: %v", err)
	}
	if behind != read.GetString() {
		t.Errorf("the two copies differ:\n%q\n%q", behind, read.GetString())
	}
	if m.Replies != 1 {
		t.Errorf("replies = %d, want the answer counted", m.Replies)
	}
}

// --no-quote is the way to answer without carrying the original back.
func TestAnAnswerCanLeaveTheOriginalOut(t *testing.T) {
	srv := newEOServer(t)
	m := openEO(t, srv, eoTestID)
	if err := New(srv, testKeys(nil)).EOReply(t.Context(), m, EOReplySpec{Body: "Noted.", NoQuote: true}); err != nil {
		t.Fatalf("EOReply: %v", err)
	}
	behind, err := helper.DecryptMessageWithPassword([]byte(eoTestPassword), eoFormParts(t, srv)["ReplyBody"])
	if err != nil {
		t.Fatalf("open the copy left behind: %v", err)
	}
	if strings.Contains(behind, eoTestBody) {
		t.Errorf("the original was quoted anyway: %q", behind)
	}
}

// An attachment on the answer goes as the two packets Proton's form wants, both
// under a name the form carries more than once.
func TestAnAnswerCarriesItsAttachments(t *testing.T) {
	srv := newEOServer(t)
	m := openEO(t, srv, eoTestID)
	err := New(srv, testKeys(nil)).EOReply(t.Context(), m, EOReplySpec{
		Body:   "Signed copy attached.",
		Attach: []LocalAttachment{{Filename: "signed.pdf", MIMEType: "application/pdf", Data: []byte("signed bytes")}},
	})
	if err != nil {
		t.Fatalf("EOReply: %v", err)
	}
	parts := eoFormParts(t, srv)
	if parts["Filename[]"] != "signed.pdf" || parts["MIMEType[]"] != "application/pdf" {
		t.Errorf("the form names the attachment %q/%q", parts["Filename[]"], parts["MIMEType[]"])
	}
	packet := pgp.NewPGPSplitMessage([]byte(parts["KeyPackets[]"]), []byte(parts["DataPacket[]"]))
	read, err := srv.sender.Decrypt(packet.GetPGPMessage(), nil, 0)
	if err != nil {
		t.Fatalf("the sender cannot open the attachment: %v", err)
	}
	if string(read.GetBinary()) != "signed bytes" {
		t.Errorf("the attachment reads %q", read.GetBinary())
	}
}

// Proton takes five answers from behind one link, and the sixth is refused here
// rather than by a request that spends a round trip to be told no.
func TestTheSixthAnswerIsRefusedBeforeTheRequest(t *testing.T) {
	srv := newEOServer(t)
	srv.replies = EOMaxReplies
	m := openEO(t, srv, eoTestID)

	err := New(srv, testKeys(nil)).EOReply(t.Context(), m, EOReplySpec{Body: "One more."})
	if err == nil {
		t.Fatal("a sixth answer was sent")
	}
	if srv.replyForm != nil {
		t.Error("the refusal still sent the form")
	}
	if !strings.Contains(err.Error(), "5 answers") {
		t.Errorf("err = %v, want it to say how many are allowed", err)
	}
}

// eoFormParts is the multipart body the reply went out as, flattened to the
// last value under each name.
func eoFormParts(t *testing.T, srv *eoServer) map[string]string {
	t.Helper()
	if srv.replyForm == nil {
		t.Fatal("no reply was sent")
	}
	_, params, err := mime.ParseMediaType(srv.replyType)
	if err != nil {
		t.Fatalf("the reply's content type is %q: %v", srv.replyType, err)
	}
	r := multipart.NewReader(strings.NewReader(string(srv.replyForm)), params["boundary"])
	form, err := r.ReadForm(1 << 20)
	if err != nil {
		t.Fatalf("read the reply's form: %v", err)
	}
	out := map[string]string{}
	for name, values := range form.Value {
		out[name] = values[len(values)-1]
	}
	for name, files := range form.File {
		f, err := files[len(files)-1].Open()
		if err != nil {
			t.Fatalf("open %s: %v", name, err)
		}
		data, err := io.ReadAll(f)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		out[name] = string(data)
	}
	return out
}
