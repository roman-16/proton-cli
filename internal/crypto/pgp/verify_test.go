package pgp

import (
	"errors"
	"testing"

	"github.com/ProtonMail/gopenpgp/v3/constants"
	gopenpgp "github.com/ProtonMail/gopenpgp/v3/crypto"
)

func TestClassify(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want VerifyResult
	}{
		{"nil error is verified", nil, Verified},
		{"status OK", gopenpgp.SignatureVerificationError{Status: constants.SIGNATURE_OK}, Verified},
		{"not signed", gopenpgp.SignatureVerificationError{Status: constants.SIGNATURE_NOT_SIGNED}, Unsigned},
		{"no verifier", gopenpgp.SignatureVerificationError{Status: constants.SIGNATURE_NO_VERIFIER}, Unverified},
		{"failed", gopenpgp.SignatureVerificationError{Status: constants.SIGNATURE_FAILED}, Invalid},
		{"bad context", gopenpgp.SignatureVerificationError{Status: constants.SIGNATURE_BAD_CONTEXT}, Invalid},
		{"non-signature error is unverified", errors.New("network down"), Unverified},
		{"wrapped signature error", errWrap{gopenpgp.SignatureVerificationError{Status: constants.SIGNATURE_FAILED}}, Invalid},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := Classify(tc.err); got != tc.want {
				t.Errorf("Classify(%v) = %q, want %q", tc.err, got, tc.want)
			}
		})
	}
}

// errWrap wraps an error so errors.As must unwrap to find the
// SignatureVerificationError.
type errWrap struct{ inner error }

func (e errWrap) Error() string { return "wrapped: " + e.inner.Error() }
func (e errWrap) Unwrap() error { return e.inner }

func TestAggregate(t *testing.T) {
	tests := []struct {
		name string
		in   []VerifyResult
		want VerifyResult
	}{
		{"empty set is unsigned", nil, Unsigned},
		{"all verified", []VerifyResult{Verified, Verified}, Verified},
		{"any invalid wins over verified", []VerifyResult{Verified, Invalid}, Invalid},
		{"invalid wins even after unverified", []VerifyResult{Unverified, Invalid}, Invalid},
		{"invalid wins regardless of order", []VerifyResult{Invalid, Verified, Unverified}, Invalid},
		{"unverified beats verified", []VerifyResult{Verified, Unverified}, Unverified},
		{"verified beats unsigned", []VerifyResult{Unsigned, Verified}, Verified},
		{"all unsigned stays unsigned", []VerifyResult{Unsigned, Unsigned}, Unsigned},
		{"unverified beats unsigned", []VerifyResult{Unsigned, Unverified}, Unverified},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := Aggregate(tc.in...); got != tc.want {
				t.Errorf("Aggregate(%v) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
