package mail

import (
	"context"
	"fmt"
	"log/slog"
	"sort"

	"github.com/roman-16/proton-cli/internal/proton"
)

// An SMTP token lets a device or a service send mail from one of the account's
// addresses without its password - Proton's "SMTP submission", on the IMAP/SMTP
// settings page.
//
// The address is the username a mail client is given and the token is the
// password. Proton hands the token out once, at the moment it is made, and
// keeps nothing that can show it again.

// Where a device connects. Both are the same for every account and neither is
// carried by a token, so a token is answered with them rather than from them.
const (
	SMTPServer = "smtp.protonmail.ch"
	SMTPPort   = 587
)

// SMTPToken is one token, as the account sees it.
type SMTPToken struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Address is the address the token sends from, or that address's ID where
	// the account no longer holds it.
	Address  string `json:"address"`
	Created  int64  `json:"created"`
	LastUsed int64  `json:"last_used,omitempty"`
	// Secret is the token as a device is given it, known at the moment it is
	// made and never again.
	Secret string `json:"token,omitempty"`
}

// rawSMTPToken is the token as Proton writes it.
type rawSMTPToken struct {
	SmtpTokenID  string
	AddressID    string
	Name         string
	CreateTime   int64
	LastUsedTime int64
}

func (r rawSMTPToken) token(ctx context.Context, emails map[string]string) SMTPToken {
	address, named := emails[r.AddressID]
	if !named {
		// Recorded and not counted: the ID stands in its place on the screen, so
		// the row says for itself that the address behind it is gone.
		slog.DebugContext(ctx, "an smtp token sends from an address the account no longer holds")
		address = r.AddressID
	}
	return SMTPToken{
		ID: r.SmtpTokenID, Name: r.Name, Address: address,
		Created: r.CreateTime, LastUsed: r.LastUsedTime,
	}
}

// SMTPTokens is every token on the account, newest first.
//
// The address listing is read too: a token carries the ID of the address it
// sends from, and the address itself is what was typed into the device.
func (s *Service) SMTPTokens(ctx context.Context) ([]SMTPToken, error) {
	var r struct{ SmtpTokens []rawSMTPToken }
	if err := s.C.Decode(ctx, proton.Request{
		Method: "GET", Path: "/mail/v4/smtptokens",
	}, &r); err != nil {
		return nil, s.orNoPlan(ctx, err, "SMTP tokens")
	}
	addrs, err := s.AddressesList(ctx)
	if err != nil {
		return nil, err
	}
	emails := make(map[string]string, len(addrs))
	for _, a := range addrs {
		emails[a.ID] = a.Email
	}
	out := make([]SMTPToken, 0, len(r.SmtpTokens))
	for _, raw := range r.SmtpTokens {
		out = append(out, raw.token(ctx, emails))
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Created > out[j].Created })
	return out, nil
}

// SMTPTokenCreate makes a token that sends from one address, and hands it back
// in the clear: the one moment it exists anywhere but inside the device.
func (s *Service) SMTPTokenCreate(ctx context.Context, address Address, name string) (SMTPToken, error) {
	var r struct{ SmtpTokenCode string }
	if err := s.C.Decode(ctx, proton.Request{
		Method: "POST", Path: "/mail/v4/smtptokens",
		Body: map[string]any{"AddressID": address.ID, "Name": name},
	}, &r); err != nil {
		return SMTPToken{}, err
	}
	if r.SmtpTokenCode == "" {
		return SMTPToken{}, fmt.Errorf("proton made the token and did not hand it back")
	}
	return SMTPToken{Name: name, Address: address.Email, Secret: r.SmtpTokenCode}, nil
}

// SMTPTokenDelete stops a token working. Whatever holds it is refused from then
// on.
func (s *Service) SMTPTokenDelete(ctx context.Context, id string) error {
	return s.C.Decode(ctx, proton.Request{
		Method: "DELETE", Path: "/mail/v4/smtptokens/" + id,
	}, nil)
}
