package mail

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"mime/multipart"
	"net/mail"
	"net/textproto"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	gomime "github.com/ProtonMail/go-mime"
	"github.com/roman-16/proton-cli/internal/mailtext"
	"github.com/roman-16/proton-cli/internal/skip"
)

func (s *Service) Export(ctx context.Context, id string, withAttachments bool) ([]byte, *ExportMeta, error) {
	r, err := s.render(ctx, id, withAttachments)
	if err != nil {
		return nil, nil, err
	}
	return r.doc, &ExportMeta{
		ID:      r.raw.ID,
		Subject: r.raw.Subject,
		From:    senderAddress(r.raw.Sender),
		Time:    r.raw.Time,
	}, nil
}

type rendered struct {
	doc    []byte
	raw    *rawMessage
	sealed bool
}

func (s *Service) render(ctx context.Context, id string, withAttachments bool) (*rendered, error) {
	raw, u, err := s.messageAndKeys(ctx, id)
	if err != nil {
		return nil, err
	}
	rings, ok := u.AddrRings(raw.AddressID)
	if !ok {
		first, _, err := u.FirstAddr()
		if err != nil {
			return nil, err
		}
		rings = first
	}
	body, _, err := s.openBody(ctx, u, *raw, false)
	shut := err != nil
	if err != nil {
		// Recorded and not counted: nothing is missing from the file. The armour
		// goes in as it stands, so the message is exported whole and somebody
		// holding the key can still read it - which is what a backup is for.
		slog.DebugContext(ctx, "mail: a body was exported as ciphertext",
			"kind", string(skip.KindMessage), "reason", string(u.Shut(sealed(raw.Body))),
			"ref", raw.ID, "error", err)
		body = raw.Body
	}

	var parts []mimePart
	if withAttachments {
		for _, a := range raw.Attachments {
			sk, err := decodeSessionKey(rings.Read, a.KeyPackets)
			if err != nil {
				return nil, fmt.Errorf("attachment %s: %w", a.Name, err)
			}
			att := &draftAttachment{
				ID: a.ID, Name: a.Name, MIMEType: a.MIMEType,
				ContentID: normalizeContentID(a.ContentID), SessionKey: sk,
			}
			data, err := s.attachmentBytes(ctx, att)
			if err != nil {
				return nil, err
			}
			parts = append(parts, mimePart{
				Filename: a.Name, MIMEType: a.MIMEType, Data: data,
				ContentID: normalizeContentID(a.ContentID),
			})
		}
	}

	var plain, html string
	if mailtext.IsHTML(raw.MIMEType) {
		html = body
	} else {
		plain = body
	}
	mimeHeaders, mimeBody, err := buildExportMIME(plain, html, parts)
	if err != nil {
		return nil, err
	}

	base := raw.Header
	if strings.TrimSpace(base) == "" {
		base = synthesizeHeaders(raw)
	}
	var out bytes.Buffer
	out.WriteString(mergeHeaders(base, mimeHeaders))
	out.WriteString("\r\n")
	out.Write(mimeBody)
	if !bytes.HasSuffix(mimeBody, []byte("\r\n")) {
		out.WriteString("\r\n")
	}
	return &rendered{doc: out.Bytes(), raw: raw, sealed: shut}, nil
}

type ExportMeta struct {
	ID      string
	Subject string
	From    string
	Time    int64
}

func mergeHeaders(base string, overlay textproto.MIMEHeader) string {
	lines := unfoldHeaders(base)
	replaced := map[string]bool{}
	out := make([]string, 0, len(lines)+len(overlay))
	for _, line := range lines {
		name := headerName(line)
		key := textproto.CanonicalMIMEHeaderKey(name)
		values, ok := overlay[key]
		if !ok {
			out = append(out, line)
			continue
		}
		if replaced[key] {
			continue
		}
		replaced[key] = true
		for _, v := range values {
			out = append(out, key+": "+v)
		}
	}
	for _, key := range sortedHeaderKeys(overlay) {
		if replaced[key] {
			continue
		}
		for _, v := range overlay[key] {
			out = append(out, key+": "+v)
		}
	}
	return strings.Join(out, "\r\n") + "\r\n"
}

func sortedHeaderKeys(h textproto.MIMEHeader) []string {
	keys := make([]string, 0, len(h))
	for k := range h {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func unfoldHeaders(block string) []string {
	block = strings.ReplaceAll(block, "\r\n", "\n")
	var out []string
	for _, line := range strings.Split(strings.TrimRight(block, "\n"), "\n") {
		if line == "" {
			continue
		}
		if (strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t")) && len(out) > 0 {
			out[len(out)-1] += "\r\n" + line
			continue
		}
		out = append(out, line)
	}
	return out
}

func headerName(line string) string {
	if i := strings.Index(line, ":"); i >= 0 {
		return strings.TrimSpace(line[:i])
	}
	return ""
}

func synthesizeHeaders(raw *rawMessage) string {
	var b strings.Builder
	write := func(name, value string) {
		if value != "" {
			fmt.Fprintf(&b, "%s: %s\r\n", name, value)
		}
	}
	write("Date", time.Unix(raw.Time, 0).Format(time.RFC1123Z))
	write("From", Recipient{Name: senderName(raw.Sender), Address: senderAddress(raw.Sender)}.String())
	write("To", joinRecipients(recipientsFromRaw(raw.ToList)))
	write("Cc", joinRecipients(recipientsFromRaw(raw.CCList)))
	write("Subject", raw.Subject)
	if raw.ExternalID != "" {
		write("Message-ID", "<"+strings.Trim(raw.ExternalID, "<>")+">")
	}
	write("MIME-Version", "1.0")
	return b.String()
}

func joinRecipients(rs []Recipient) string {
	out := make([]string, 0, len(rs))
	for _, r := range rs {
		out = append(out, r.String())
	}
	return strings.Join(out, ", ")
}

func MboxEntry(doc []byte, meta *ExportMeta) []byte {
	from := meta.From
	if from == "" {
		from = "MAILER-DAEMON"
	}
	var b bytes.Buffer
	fmt.Fprintf(&b, "From %s %s\n", from, time.Unix(meta.Time, 0).UTC().Format(time.ANSIC))
	for _, line := range strings.Split(strings.ReplaceAll(string(doc), "\r\n", "\n"), "\n") {
		if strings.HasPrefix(strings.TrimLeft(line, ">"), "From ") {
			b.WriteByte('>')
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	b.WriteByte('\n')
	return b.Bytes()
}

type ParsedEML struct {
	To, CC, BCC []Recipient
	Subject     string
	Body        string
	HTML        bool
	Attachments []LocalAttachment
}

func ParseEML(r io.Reader) (*ParsedEML, error) {
	msg, err := mail.ReadMessage(r)
	if err != nil {
		return nil, fmt.Errorf("parse message: %w", err)
	}
	out := &ParsedEML{
		Subject: decodeHeader(msg.Header.Get("Subject")),
		To:      parseAddressList(msg.Header.Get("To")),
		CC:      parseAddressList(msg.Header.Get("Cc")),
		BCC:     parseAddressList(msg.Header.Get("Bcc")),
	}
	var plain, html string
	if err := walkEML(textproto.MIMEHeader(msg.Header), msg.Body, out, &plain, &html); err != nil {
		return nil, err
	}
	if html != "" {
		out.Body, out.HTML = html, true
	} else {
		out.Body = plain
	}
	if strings.TrimSpace(out.Body) == "" && len(out.Attachments) == 0 {
		return nil, fmt.Errorf("the file has no readable body or attachments")
	}
	return out, nil
}

func walkEML(header textproto.MIMEHeader, body io.Reader, out *ParsedEML, plain, html *string) error {
	mediaType, params, err := mime.ParseMediaType(headerOr(header, "Content-Type", "text/plain"))
	if err != nil {
		mediaType, params = "text/plain", map[string]string{}
	}
	if strings.HasPrefix(mediaType, "multipart/") {
		boundary := params["boundary"]
		if boundary == "" {
			return fmt.Errorf("multipart part has no boundary")
		}
		mr := multipart.NewReader(body, boundary)
		for {
			part, err := mr.NextPart()
			if err == io.EOF {
				return nil
			}
			if err != nil {
				return fmt.Errorf("read multipart: %w", err)
			}
			if err := walkEML(part.Header, part, out, plain, html); err != nil {
				return err
			}
		}
	}

	data, err := decodePartBody(header, body, mediaType, params)
	if err != nil {
		return err
	}
	disposition, dispParams, _ := mime.ParseMediaType(header.Get("Content-Disposition"))
	filename := dispParams["filename"]
	if filename == "" {
		filename = params["name"]
	}
	cid := strings.Trim(header.Get("Content-ID"), "<>")
	isAttachment := disposition == "attachment" || filename != "" || cid != ""

	if !isAttachment {
		switch mediaType {
		case mimeTypeHTML:
			*html = string(data)
			return nil
		case mimeTypePlain:
			*plain = string(data)
			return nil
		}
	}
	if filename == "" {
		filename = "attachment"
	}
	out.Attachments = append(out.Attachments, LocalAttachment{
		Filename: filename, MIMEType: mediaType, Data: data,
		Inline: cid != "", ContentID: cid,
	})
	return nil
}

func decodePartBody(header textproto.MIMEHeader, body io.Reader, mediaType string, params map[string]string) ([]byte, error) {
	data, err := io.ReadAll(gomime.DecodeContentEncoding(body, header.Get("Content-Transfer-Encoding")))
	if err != nil {
		return nil, fmt.Errorf("decode part: %w", err)
	}
	if misdeclaredUTF8(params["charset"], data) {
		return data, nil
	}
	decoded, _ := gomime.DecodeCharset(data, mediaType, params)
	return decoded, nil
}

func misdeclaredUTF8(charset string, data []byte) bool {
	switch strings.ToLower(strings.TrimSpace(charset)) {
	case "us-ascii", "ascii", "iso-ir-6", "ansi_x3.4-1968":
	default:
		return false
	}
	return !isASCII(data) && utf8.Valid(data)
}

func isASCII(data []byte) bool {
	for _, b := range data {
		if b > 0x7F {
			return false
		}
	}
	return true
}

func headerOr(h textproto.MIMEHeader, key, fallback string) string {
	if v := h.Get(key); v != "" {
		return v
	}
	return fallback
}

func decodeHeader(v string) string {
	out, err := gomime.DecodeHeader(v)
	if err != nil {
		return v
	}
	return out
}

func parseAddressList(v string) []Recipient {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	addrs, err := mail.ParseAddressList(decodeHeader(v))
	if err != nil {
		return ParseRecipients(strings.Split(v, ","))
	}
	out := make([]Recipient, 0, len(addrs))
	for _, a := range addrs {
		out = append(out, Recipient{Name: a.Name, Address: a.Address})
	}
	return out
}
