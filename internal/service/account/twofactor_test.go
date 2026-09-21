package account

import (
	"net/url"
	"strings"
	"testing"
)

// The URI is what an authenticator app is given, so what matters about it is
// that an app reads back what Proton will then check: the secret unchanged, and
// the three parameters Proton's codes are computed under.
func TestTheOTPAuthURIIsWhatAnAuthenticatorAppReads(t *testing.T) {
	const secret = "JBSWY3DPEHPK3PXP"
	raw := OTPAuthURI("jane roe@proton.me", secret)

	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse %q: %v", raw, err)
	}
	if u.Scheme != "otpauth" || u.Host != "totp" {
		t.Errorf("scheme and host are %q and %q, want otpauth and totp", u.Scheme, u.Host)
	}
	if label := strings.TrimPrefix(u.Path, "/"); label != "jane roe@proton.me" {
		t.Errorf("the app would list the account as %q", label)
	}
	for field, want := range map[string]string{
		"secret": secret, "issuer": "Proton", "algorithm": "SHA1", "digits": "6", "period": "30",
	} {
		if got := u.Query().Get(field); got != want {
			t.Errorf("%s = %q, want %q", field, got, want)
		}
	}
}
