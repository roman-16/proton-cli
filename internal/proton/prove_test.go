package proton

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ProtonMail/go-srp"
)

// A request that carries its own proof of the password.
//
// Proton guards the credentials two ways, and this is the half no answer to a
// refusal can supply: the proof has to be in the request the first time. So
// what is checked here is that the whole of it arrives at once - the proof, the
// second factor, and whatever the endpoint itself wanted - and that the server
// is still made to prove itself back.

// provingServer is Proton's half: the SRP parameters, and an endpoint that
// checks a proof against the verifier a password was registered under.
type provingServer struct {
	*httptest.Server
	// body is what the guarded endpoint was sent.
	body map[string]any
	// wrongFactor makes it refuse the second factor, which is the one refusal
	// the person can act on.
	wrongFactor bool
	// twoFactor is what it says the account is asked for beyond the password.
	twoFactor int
}

func newProvingServer(t *testing.T, password string) *provingServer {
	t.Helper()
	stored, err := Modulus{ID: "modulus-id", Value: signedTestModulus}.Verifier([]byte(password))
	if err != nil {
		t.Fatalf("verifier: %v", err)
	}
	verifier, err := base64.StdEncoding.DecodeString(stored.Value)
	if err != nil {
		t.Fatalf("verifier decode: %v", err)
	}
	server, err := srp.NewServerFromSigned(signedTestModulus, verifier, srpBits)
	if err != nil {
		t.Fatalf("server setup: %v", err)
	}
	ephemeral, err := server.GenerateChallenge()
	if err != nil {
		t.Fatalf("challenge: %v", err)
	}

	s := &provingServer{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/core/v4/auth/info":
			var asked map[string]any
			_ = json.NewDecoder(r.Body).Decode(&asked)
			if _, named := asked["ReauthScope"]; named {
				t.Error("a proving request asked for a scope; it buys none")
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"Code": 1000, "Modulus": signedTestModulus,
				"ServerEphemeral": base64.StdEncoding.EncodeToString(ephemeral),
				"Version":         stored.Version, "Salt": stored.Salt,
				"SRPSession": "srp-session",
				"2FA":        map[string]any{"Enabled": s.twoFactor},
			})
		case "/core/v4/settings":
			_ = json.NewEncoder(w).Encode(map[string]any{"Code": 1000})
		case "/core/v4/settings/2fa/totp":
			if err := json.NewDecoder(r.Body).Decode(&s.body); err != nil {
				t.Errorf("proof body: %v", err)
			}
			if s.wrongFactor {
				w.WriteHeader(http.StatusUnprocessableEntity)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"Code": wrongTOTPCode, "Error": "Incorrect login credentials",
				})
				return
			}
			clientEphemeral, _ := base64.StdEncoding.DecodeString(text(s.body["ClientEphemeral"]))
			clientProof, _ := base64.StdEncoding.DecodeString(text(s.body["ClientProof"]))
			serverProof, err := server.VerifyProofs(clientEphemeral, clientProof)
			if err != nil {
				w.WriteHeader(http.StatusUnauthorized)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"Code": invalidLoginCode, "Error": "Incorrect login credentials",
				})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"Code": 1000, "ServerProof": base64.StdEncoding.EncodeToString(serverProof),
			})
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	t.Cleanup(s.Close)
	return s
}

func text(v any) string {
	s, _ := v.(string)
	return s
}

// proving is a client that answers with the password and the code below.
func proving(t *testing.T, s *provingServer, password, code string) *Client {
	t.Helper()
	c := New(Options{BaseURL: s.URL, Logger: slog.New(slog.DiscardHandler)})
	c.SetTokens("uid", "access", "refresh")
	c.SetScopeResolver(func(_ context.Context, scope Scope) (ScopeCredentials, error) {
		if scope != ScopePassword {
			t.Errorf("asked for the %s scope, want the password one", scope)
		}
		return ScopeCredentials{Username: "me@proton.me", Password: []byte(password)}, nil
	})
	c.SetSecondFactorResolver(func(context.Context, SecondFactorOffer) (SecondFactorAnswer, error) {
		return SecondFactorAnswer{TOTP: code}, nil
	})
	return c
}

func TestAProvingRequestCarriesTheProofTheFactorAndItsOwnBody(t *testing.T) {
	s := newProvingServer(t, "correct horse")
	s.twoFactor = 1
	c := proving(t, s, "correct horse", "123456")

	if err := c.Decode(t.Context(), Request{
		Method: "PUT", Path: "/core/v4/settings/2fa/totp", Proves: true,
		Body: map[string]any{"Something": "the endpoint wanted"},
	}, nil); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	for _, field := range []string{"ClientProof", "ClientEphemeral", "SRPSession"} {
		if text(s.body[field]) == "" {
			t.Errorf("%s is missing from the request", field)
		}
	}
	if got := text(s.body["TwoFactorCode"]); got != "123456" {
		t.Errorf("TwoFactorCode = %q, want the one the resolver answered", got)
	}
	if got := text(s.body["Something"]); got != "the endpoint wanted" {
		t.Errorf("the endpoint's own field is %q", got)
	}
	// And the password itself is nowhere in it.
	if raw, _ := json.Marshal(s.body); string(raw) != "" && text(s.body["Password"]) != "" {
		t.Error("the request carries the password")
	}
}

// A wrong code is the one refusal here that the person can put right, so it
// reaches them as a sentence rather than as a number under an invitation to
// report a bug.
func TestAProvingRequestSaysWhenTheCodeWasRefused(t *testing.T) {
	s := newProvingServer(t, "correct horse")
	s.twoFactor, s.wrongFactor = 1, true

	err := proving(t, s, "correct horse", "000000").Decode(t.Context(), Request{
		Method: "PUT", Path: "/core/v4/settings/2fa/totp", Proves: true,
	}, nil)
	if err == nil {
		t.Fatal("a refused code came back as success")
	}
	if !strings.Contains(err.Error(), "did not accept that two-factor code") {
		t.Errorf("the refusal reads %q", err)
	}
	var problem interface{ ExitCode() int }
	if !errors.As(err, &problem) || problem.ExitCode() != 2 {
		t.Errorf("a wrong code should read as a credential problem: %v", err)
	}
}

// The same refusal reaching a caller that did not ask for the code - the
// endpoint that confirms a new secret - is left as Proton's own answer, so the
// command that knows what the code was for can say so itself.
func TestARefusedCodeIsRecognisableWhereNobodyPhrasedIt(t *testing.T) {
	for _, tc := range []struct {
		body string
		want bool
	}{
		{body: `{"Code":12060,"Error":"Incorrect login credentials"}`, want: true},
		{body: `{"Code":8002,"Error":"Incorrect login credentials"}`},
	} {
		_, err := classifyErrorBody(http.StatusUnprocessableEntity, []byte(tc.body))
		if got := WrongTwoFactorCode(err); got != tc.want {
			t.Errorf("%s: recognised = %v, want %v", tc.body, got, tc.want)
		}
	}
}

// A dry run sends nothing, whatever the request carries: the guard is one
// check at one point, and a proving request goes through it like any other.
func TestADryRunRefusesAProvingRequest(t *testing.T) {
	s := newProvingServer(t, "correct horse")
	c := proving(t, s, "correct horse", "")
	c.dryRun = true

	err := c.Decode(t.Context(), Request{
		Method: "PUT", Path: "/core/v4/settings/2fa/totp", Proves: true,
	}, nil)
	if !errors.Is(err, ErrDryRun) {
		t.Fatalf("err = %v, want the dry-run refusal", err)
	}
	if s.body != nil {
		t.Error("the dry run sent the request")
	}
}

// A body the proof cannot be merged into is the caller's mistake and is caught
// before anything is sent.
func TestAProvingRequestNeedsABodyTheProofFitsIn(t *testing.T) {
	s := newProvingServer(t, "correct horse")
	err := proving(t, s, "correct horse", "").Decode(t.Context(), Request{
		Method: "PUT", Path: "/core/v4/settings/2fa/totp", Proves: true,
		Body: []byte(`{"Something":"already encoded"}`),
	}, nil)
	if err == nil {
		t.Fatal("a body the proof cannot join was sent anyway")
	}
	if s.body != nil {
		t.Error("the request went out")
	}
}
