package proton

import (
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ProtonMail/go-srp"
)

// A verifier is what Proton is given instead of a password, so what it has to be
// worth is exactly this: the password proves against it, and nothing else does.
// go-srp's own server is the other half, because a stub would accept whatever
// this package sent it.
func TestAVerifierProvesThePasswordItWasBuiltFrom(t *testing.T) {
	v, err := Modulus{ID: "modulus-id", Value: signedTestModulus}.Verifier([]byte("correct horse"))
	if err != nil {
		t.Fatalf("verifier: %v", err)
	}
	if v.ModulusID != "modulus-id" {
		t.Errorf("ModulusID = %q, want the group's", v.ModulusID)
	}
	if v.Version != 4 {
		t.Errorf("Version = %d, want 4, which hashes no username", v.Version)
	}
	salt, err := base64.StdEncoding.DecodeString(v.Salt)
	if err != nil {
		t.Fatalf("salt is not base64: %v", err)
	}
	if len(salt) != srpSaltBytes {
		t.Errorf("salt is %d bytes, want %d", len(salt), srpSaltBytes)
	}
	verifier, err := base64.StdEncoding.DecodeString(v.Value)
	if err != nil {
		t.Fatalf("verifier is not base64: %v", err)
	}

	server, err := srp.NewServerFromSigned(signedTestModulus, verifier, srpBits)
	if err != nil {
		t.Fatalf("server setup: %v", err)
	}
	ephemeral, err := server.GenerateChallenge()
	if err != nil {
		t.Fatalf("challenge: %v", err)
	}
	for _, tc := range []struct {
		what     string
		password string
		proves   bool
	}{
		{what: "the password it was built from", password: "correct horse", proves: true},
		{what: "any other password", password: "wrong horse"},
	} {
		t.Run(tc.what, func(t *testing.T) {
			auth, err := srp.NewAuth(v.Version, "", []byte(tc.password), v.Salt, signedTestModulus,
				base64.StdEncoding.EncodeToString(ephemeral))
			if err != nil {
				t.Fatalf("SRP setup: %v", err)
			}
			proofs, err := auth.GenerateProofs(srpBits)
			if err != nil {
				t.Fatalf("proofs: %v", err)
			}
			_, err = server.VerifyProofs(proofs.ClientEphemeral, proofs.ClientProof)
			if proved := err == nil; proved != tc.proves {
				t.Errorf("proved = %v, want %v (%v)", proved, tc.proves, err)
			}
		})
	}
}

// The salt is what keeps two things given the same password from sharing a
// verifier, so it is made fresh each time rather than once per modulus.
func TestEachVerifierGetsItsOwnSalt(t *testing.T) {
	m := Modulus{ID: "modulus-id", Value: signedTestModulus}
	first, err := m.Verifier([]byte("correct horse"))
	if err != nil {
		t.Fatalf("verifier: %v", err)
	}
	second, err := m.Verifier([]byte("correct horse"))
	if err != nil {
		t.Fatalf("verifier: %v", err)
	}
	if first.Salt == second.Salt || first.Value == second.Value {
		t.Error("the same password twice produced the same verifier")
	}
}

func TestFetchModulusReadsBothHalvesOfTheAnswer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != modulusPath {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"Code": 1000, "Modulus": signedTestModulus, "ModulusID": "modulus-id",
		})
	}))
	t.Cleanup(srv.Close)

	m, err := FetchModulus(t.Context(), New(Options{BaseURL: srv.URL, Logger: slog.New(slog.DiscardHandler)}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.ID != "modulus-id" || m.Value != signedTestModulus {
		t.Errorf("modulus = %q/%q, want both halves of the answer", m.ID, m.Value)
	}
}
