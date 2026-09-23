package mail

import (
	"context"
	"strings"

	"github.com/roman-16/proton-cli/internal/proton"
)

// Outgoing mail carries the sending address's signature followed by Proton's own
// footer, exactly as the web client composes it. The footer is an account
// setting the CLI has to read, so it is fetched once per run and cached.

// protonFooterHTML is the default footer Proton appends when PMSignature is on.
const protonFooterHTML = `Sent with <a href="https://proton.me/mail/home">Proton Mail</a> secure email.`

// pmSignatureEnabled is bit 1 of MailSettings.PMSignature (PM_SIGNATURE.ENABLED).
// Bit 2 marks it locked, which free accounts are - they cannot turn it off, so
// the CLI must include it for them just as the web client does.
const pmSignatureEnabled = 1

type mailSettings struct {
	PMSignature        int
	PMSignatureContent string
	MailCategoryView   any
}

func (m mailSettings) categoryViewOn() bool {
	switch v := m.MailCategoryView.(type) {
	case bool:
		return v
	case float64:
		return v != 0
	}
	return false
}

// settings fetches and caches the account's mail settings.
func (s *Service) settings(ctx context.Context) (mailSettings, error) {
	s.settingsOnce.Do(func() {
		var resp struct{ MailSettings mailSettings }
		if err := s.C.Decode(ctx, proton.Request{Method: "GET", Path: "/mail/v4/settings"}, &resp); err != nil {
			s.settingsErr = err
			return
		}
		s.settingsCache = resp.MailSettings
	})
	return s.settingsCache, s.settingsErr
}

// protonFooter returns Proton's footer when the account has it enabled, honouring
// a custom PMSignatureContent when one is set.
func (m mailSettings) protonFooter() string {
	if m.PMSignature&pmSignatureEnabled == 0 {
		return ""
	}
	if m.PMSignatureContent != "" {
		return m.PMSignatureContent
	}
	return protonFooterHTML
}

// SignatureBlock renders what gets appended to outgoing mail from sender: the
// address's own signature, then Proton's footer when the account includes one.
// The result is always HTML; plaintext bodies flatten it when appending.
func (s *Service) SignatureBlock(ctx context.Context, sender *Sender) (string, error) {
	own := strings.TrimSpace(sender.Address.Signature)
	set, err := s.settings(ctx)
	if err != nil {
		// A settings read failure must not block a send; the address signature
		// alone is still correct, just without Proton's footer.
		return own, nil
	}
	footer := set.protonFooter()
	switch {
	case own == "" && footer == "":
		return "", nil
	case own == "":
		return footer, nil
	case footer == "":
		return own, nil
	}
	return own + "<br><br>" + footer, nil
}
