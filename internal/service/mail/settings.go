package mail

import (
	"context"

	"github.com/roman-16/proton-cli/internal/proton"
)

// The bits of MailSettings.ImageProxy. Block email tracking is the proxy bit.
const (
	StoreRemoteImages = 1
	ProxyRemoteImages = 2
)

type mailSettings struct {
	PMSignature        int
	PMSignatureContent string
	MailCategoryView   any
	AlmostAllMail      any
	Sign               int
	PGPScheme          int
	ImageProxy         int
}

func (m mailSettings) categoryViewOn() bool { return switchedOn(m.MailCategoryView) }

func (m mailSettings) almostAllMail() bool { return switchedOn(m.AlmostAllMail) }

func (m mailSettings) blocksTracking() bool { return m.ImageProxy != 0 }

func (m mailSettings) proxiesImages() bool { return m.ImageProxy&ProxyRemoteImages != 0 }

func switchedOn(v any) bool {
	switch v := v.(type) {
	case bool:
		return v
	case float64:
		return v != 0
	}
	return false
}

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

// BlocksTracking reports whether the account has Block email tracking on, which
// is what decides whether the links in a message are cleaned before it is read.
func (s *Service) BlocksTracking(ctx context.Context) (bool, error) {
	set, err := s.settings(ctx)
	if err != nil {
		return false, err
	}
	return set.blocksTracking(), nil
}
