package proton

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ProtonMail/go-srp"
)

// What a security key answers with has to reach Proton in the shape its own
// clients send: the challenge handed back exactly as it arrived, three binary
// fields in ordinary base64, and the credential as an array of numbers. None of
// it can be checked against Proton without a key registered on the account, so
// it is pinned here against the shape read out of the web clients.
func TestASecurityKeyAnswerIsSentTheWayProtonReadsIt(t *testing.T) {
	const challenge = `{"publicKey":{"rpId":"account.proton.me","challenge":[1,2,3]}}`

	var sent []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sent, _ = io.ReadAll(r.Body)
		_, _ = w.Write([]byte(`{"Code":1000}`))
	}))
	defer srv.Close()

	offer := SecondFactorOffer{SecurityKey: &SecurityKeyRequest{
		Options: json.RawMessage(challenge), Host: "account.proton.me",
	}}
	c := New(Options{BaseURL: srv.URL, Logger: slog.New(slog.DiscardHandler)})
	if err := c.auth2FA(context.Background(), offer, SecondFactorAnswer{
		SecurityKey: &SecurityKeyAssertion{
			ClientData:        []byte("client data"),
			AuthenticatorData: []byte("authenticator data"),
			Signature:         []byte{0xff, 0x00},
			CredentialID:      []byte{7, 8, 9},
		},
	}); err != nil {
		t.Fatalf("auth2FA: %v", err)
	}

	var body struct {
		FIDO2 struct {
			AuthenticationOptions json.RawMessage
			ClientData            string
			AuthenticatorData     string
			Signature             string
			CredentialID          []int
		}
		TwoFactorCode string `json:",omitempty"`
	}
	if err := json.Unmarshal(sent, &body); err != nil {
		t.Fatalf("the request is not JSON: %v", err)
	}
	if string(body.FIDO2.AuthenticationOptions) != challenge {
		t.Errorf("AuthenticationOptions = %s, want the challenge exactly as it arrived", body.FIDO2.AuthenticationOptions)
	}
	if body.FIDO2.ClientData != "Y2xpZW50IGRhdGE=" {
		t.Errorf("ClientData = %q, want standard base64", body.FIDO2.ClientData)
	}
	if body.FIDO2.AuthenticatorData != "YXV0aGVudGljYXRvciBkYXRh" {
		t.Errorf("AuthenticatorData = %q, want standard base64", body.FIDO2.AuthenticatorData)
	}
	if body.FIDO2.Signature != "/wA=" {
		t.Errorf("Signature = %q, want standard base64", body.FIDO2.Signature)
	}
	if len(body.FIDO2.CredentialID) != 3 || body.FIDO2.CredentialID[0] != 7 {
		t.Errorf("CredentialID = %v, want the credential as an array of numbers", body.FIDO2.CredentialID)
	}
	if body.TwoFactorCode != "" {
		t.Errorf("a code was sent alongside the key: %q", body.TwoFactorCode)
	}
}

// Proton says an account has a security key by naming the keys it has. One that
// names none is a door with nothing behind it, and offering it would leave the
// person waiting to touch something that could never answer.
func TestWhatAnAccountIsOffered(t *testing.T) {
	challenge := json.RawMessage(`{"publicKey":{}}`)
	registered := twoFA{Enabled: twoFAOTP | twoFAFIDO2}
	registered.FIDO2.AuthenticationOptions = challenge
	registered.FIDO2.RegisteredKeys = []struct{ Name string }{{Name: "a yubikey"}}

	bare := twoFA{Enabled: twoFAFIDO2}
	bare.FIDO2.AuthenticationOptions = challenge

	if offer := registered.offer("account.proton.me"); !offer.TOTP || offer.SecurityKey == nil {
		t.Errorf("offer = %+v, want both a code and a key", offer)
	} else if offer.SecurityKey.Host != "account.proton.me" {
		t.Errorf("host = %q, want the host that sent the challenge", offer.SecurityKey.Host)
	}
	if offer := bare.offer("account.proton.me"); offer.TOTP || offer.SecurityKey != nil {
		t.Errorf("offer = %+v, want nothing offered for a key that is not registered", offer)
	}
	if offer := (twoFA{Enabled: twoFAOTP}).offer("account.proton.me"); !offer.TOTP || offer.SecurityKey != nil {
		t.Errorf("offer = %+v, want only a code", offer)
	}
}

// Signing in is what an unattended job does before its real work, at an hour
// nobody is watching. Proton's edge has bad moments, and a moment is not a
// reason to lose the run: the requests that spend nothing are asked again.
func TestSigningInRidesOutAPassingServerFailure(t *testing.T) {
	shrinkBackoff(t)

	var asked int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		asked++
		if asked == 1 {
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte("<html><head><title>502 Bad Gateway</title></head></html>"))
			return
		}
		_, _ = w.Write([]byte(`{"Code":1000,"UID":"u","AccessToken":"a","RefreshToken":"r"}`))
	}))
	defer srv.Close()

	c := New(Options{BaseURL: srv.URL, Logger: slog.New(slog.DiscardHandler)})
	sess, err := c.createSession(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sess.UID != "u" {
		t.Errorf("UID = %q, want the session from the second attempt", sess.UID)
	}
	if asked != 2 {
		t.Errorf("the server was asked %d times, want a second attempt", asked)
	}
}

// The session a sign-in starts from is asked for by name. Proton answers a
// request without the header with a cookie session instead, which is not the one
// the SRP exchange that follows is about to be run against.
func TestCreatingASessionAsksForAnUnauthenticatedOne(t *testing.T) {
	srv, received := newHeaderCaptureServer(t)

	c := New(Options{BaseURL: srv.URL, Logger: slog.New(slog.DiscardHandler)})
	if _, err := c.createSession(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(*received) != 1 {
		t.Fatalf("the server was asked %d times, want once", len(*received))
	}
	if got := (*received)[0].Get("x-enforce-unauthsession"); got != "true" {
		t.Errorf("x-enforce-unauthsession = %q, want true", got)
	}
}

// An HTML error page from the edge is Proton failing, and it reads as that: a
// status, an exit code a script can wait on, and not a sentence about JSON.
func TestAServerFailureSigningInReadsAsOne(t *testing.T) {
	shrinkBackoff(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("<html><head><title>502 Bad Gateway</title></head></html>"))
	}))
	defer srv.Close()

	c := New(Options{BaseURL: srv.URL, Logger: slog.New(slog.DiscardHandler)})
	_, err := c.createSession(context.Background())
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("err = %v, want an API error carrying the status", err)
	}
	if apiErr.HTTPStatus != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", apiErr.HTTPStatus)
	}
	if apiErr.ExitCode() != 5 {
		t.Errorf("exit = %d, want 5 (a server problem)", apiErr.ExitCode())
	}
	if got := apiErr.Error(); got != "[HTTP 502] Bad Gateway" {
		t.Errorf("error = %q, want it to name what Proton answered", got)
	}
}

// A connection that never got anywhere is a network failure wherever it happens,
// including before there is a session. Reporting it as anything else tells a job
// its arguments were wrong.
func TestAFailedConnectionSigningInIsANetworkFailure(t *testing.T) {
	shrinkBackoff(t)

	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	dead := srv.URL
	srv.Close()

	c := New(Options{BaseURL: dead, Logger: slog.New(slog.DiscardHandler)})
	_, err := c.createSession(context.Background())
	var netErr *NetworkError
	if !errors.As(err, &netErr) {
		t.Fatalf("err = %v, want *NetworkError", err)
	}
	if netErr.ExitCode() != 5 {
		t.Errorf("exit = %d, want 5", netErr.ExitCode())
	}
}

// A rate limit while signing in is waited out like every other one, rather than
// being reported with Proton's JSON quoted at the reader.
func TestARateLimitSigningInIsWaitedOut(t *testing.T) {
	shrinkBackoff(t)

	var asked int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		asked++
		if asked == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"Code":85131,"Error":"Too many requests"}`))
			return
		}
		_, _ = w.Write([]byte(`{"Code":1000,"UID":"u","AccessToken":"a","RefreshToken":"r"}`))
	}))
	defer srv.Close()

	c := New(Options{BaseURL: srv.URL, Logger: slog.New(slog.DiscardHandler)})
	if _, err := c.createSession(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if asked != 2 {
		t.Errorf("the server was asked %d times, want a second attempt", asked)
	}
}

// A two-factor code is single-use and lives for thirty seconds. Whatever goes
// wrong, it is never sent twice: by the time a wait is over the code is spent or
// expired, and Proton answers "incorrect code" - blaming the person for
// something Proton did.
func TestATwoFactorCodeIsNeverSentTwice(t *testing.T) {
	shrinkBackoff(t)

	var asked int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		asked++
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("<html>502</html>"))
	}))
	defer srv.Close()

	c := New(Options{BaseURL: srv.URL, Logger: slog.New(slog.DiscardHandler)})
	err := c.auth2FA(context.Background(),
		SecondFactorOffer{TOTP: true}, SecondFactorAnswer{TOTP: "123456"})
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.HTTPStatus != http.StatusBadGateway {
		t.Fatalf("err = %v, want the 502 reported as what it is", err)
	}
	if asked != 1 {
		t.Errorf("the code was submitted %d times, want once", asked)
	}
}

// The proof cannot be sent again on its own - the SRPSession the parameters
// carried is spent - so the exchange is the unit that gets another go, asking
// for a fresh set each time.
//
// An exchange sends two requests to land one, and the answer it settles on is
// the answer to the second. Getting through on the first go is the ordinary
// outcome and belongs in the table beside the failures: signing in and elevating
// a scope both pass through here, and a hole here is a hole under both.
func TestAnSRPExchangeIsRestartedWholeOrNotAtAll(t *testing.T) {
	shrinkBackoff(t)

	broke := func(times int) []error {
		out := make([]error, times)
		for i := range out {
			out[i] = &APIError{HTTPStatus: 502, Message: "Bad Gateway"}
		}
		return out
	}
	refused := []error{&APIError{HTTPStatus: 422, Code: invalidLoginCode, Message: "Incorrect login credentials"}}
	granted := &Response{Status: 200, Body: []byte(`{"Code":1000,"ServerProof":"proof"}`)}

	// answers is what each run of the exchange gets, in order; a run past the end
	// of the list is granted the scope. runs is how many the loop should make.
	cases := []struct {
		name         string
		secondFactor func(twoFA) (map[string]any, error)
		answers      []error
		runs         int
	}{
		{name: "granted at once", runs: 1},
		{name: "granted after a server that broke", answers: broke(1), runs: 2},
		{name: "a server that stayed broken", answers: broke(transientWaits + 1), runs: transientWaits + 1},
		{name: "credentials Proton refused", answers: refused, runs: 1},
		{
			name: "a server that broke, with a second factor",
			secondFactor: func(twoFA) (map[string]any, error) {
				return map[string]any{"TwoFactorCode": "123456"}, nil
			},
			answers: broke(1), runs: 1,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := New(Options{Logger: slog.New(slog.DiscardHandler)})
			x := srpExchange{method: "POST", path: "/core/v4/auth", secondFactor: tc.secondFactor}

			var runs int
			resp, err := c.retrying(context.Background(),
				Request{Method: x.method, Path: x.path, Repeatable: x.repeatable()},
				func() (*Response, error) {
					runs++
					if runs <= len(tc.answers) {
						return nil, tc.answers[runs-1]
					}
					return granted, nil
				})
			if runs != tc.runs {
				t.Fatalf("the exchange ran %d times, want %d", runs, tc.runs)
			}
			if runs <= len(tc.answers) {
				if want := tc.answers[runs-1]; !errors.Is(err, want) {
					t.Fatalf("err = %v, want the failure the last run got (%v)", err, want)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if resp != granted {
				t.Errorf("answer = %+v, want the one the proof was granted with", resp)
			}
		})
	}
}

// Proton answers with a code of its own alongside the status, and either can be
// the refusal. Reading only one of them is how an HTML error page came to be
// reported as unreadable JSON.
func TestTheRefusalAResponseCarries(t *testing.T) {
	cases := []struct {
		name string
		resp Response
		want error
	}{
		{"done", Response{Status: 200, Body: []byte(`{"Code":1000}`)}, nil},
		{"one answer per item", Response{Status: 200, Body: []byte(`{"Code":1001}`)}, nil},
		{"being done in the background", Response{Status: 200, Body: []byte(`{"Code":1002}`)}, nil},
		{"a body with no code at all", Response{Status: 200, Body: []byte(`{"Total":0}`)}, nil},
		{
			"Proton's own refusal under a 200",
			Response{Status: 200, Body: []byte(`{"Code":2001,"Error":"No"}`)},
			&APIError{HTTPStatus: 200, Code: 2001, Message: "No"},
		},
		{
			"an edge failure with no code to read",
			Response{Status: 502, Body: []byte("<html>502</html>")},
			&APIError{HTTPStatus: 502, Message: "Bad Gateway"},
		},
		{
			"human verification",
			Response{Status: 422, Body: []byte(hvProtonBody)},
			&HumanVerificationError{Token: "chal-xyz"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := responseError(&tc.resp)
			switch want := tc.want.(type) {
			case nil:
				if got != nil {
					t.Fatalf("err = %v, want the answer taken as a success", got)
				}
			case *HumanVerificationError:
				var hvErr *HumanVerificationError
				if !errors.As(got, &hvErr) || hvErr.Token != want.Token {
					t.Fatalf("err = %v, want the human-verification challenge", got)
				}
			case *APIError:
				var apiErr *APIError
				if !errors.As(got, &apiErr) {
					t.Fatalf("err = %v, want an API error", got)
				}
				if apiErr.HTTPStatus != want.HTTPStatus || apiErr.Code != want.Code || apiErr.Message != want.Message {
					t.Errorf("err = %+v, want %+v", apiErr, want)
				}
			}
		})
	}
}

type legacyAccount struct {
	*httptest.Server
	asked []map[string]any
}

func newLegacyAccount(t *testing.T, username, password string) *legacyAccount {
	t.Helper()
	hashed, err := srp.NewAuth(2, username, []byte(password), "", signedTestModulus, "")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	verifier, err := hashed.GenerateVerifier(srpBits)
	if err != nil {
		t.Fatalf("verifier: %v", err)
	}
	server, err := srp.NewServerFromSigned(signedTestModulus, verifier, srpBits)
	if err != nil {
		t.Fatalf("server setup: %v", err)
	}
	a := &legacyAccount{}
	a.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		switch r.URL.Path {
		case "/auth/v4/sessions":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"Code": 1000, "UID": "unauth", "AccessToken": "a", "RefreshToken": "r",
			})
		case "/core/v4/auth/info":
			a.asked = append(a.asked, body)
			ephemeral, err := server.GenerateChallenge()
			if err != nil {
				t.Errorf("challenge: %v", err)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"Code": 1000, "Version": 2, "Username": username,
				"Modulus": signedTestModulus, "ServerEphemeral": base64.StdEncoding.EncodeToString(ephemeral),
				"SRPSession": "srp-session",
			})
		case "/core/v4/auth", "/core/v4/users/password":
			clientEphemeral, _ := base64.StdEncoding.DecodeString(text(body["ClientEphemeral"]))
			clientProof, _ := base64.StdEncoding.DecodeString(text(body["ClientProof"]))
			serverProof, err := server.VerifyProofs(clientEphemeral, clientProof)
			if err != nil {
				w.WriteHeader(http.StatusUnprocessableEntity)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"Code": invalidLoginCode, "Error": "Incorrect login credentials",
				})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"Code": 1000, "UID": "uid", "AccessToken": "access", "RefreshToken": "refresh",
				"ServerProof": base64.StdEncoding.EncodeToString(serverProof),
			})
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	t.Cleanup(a.Close)
	return a
}

func TestALegacyPasswordIsProvedForTheUsernameProtonAnswersWith(t *testing.T) {
	account := newLegacyAccount(t, "alice.smith", "correct horse")
	c := New(Options{BaseURL: account.URL, Logger: slog.New(slog.DiscardHandler)})

	if err := c.Login(t.Context(), "alice@company.com", []byte("correct horse")); err != nil {
		t.Fatalf("signing in with an address of a legacy account: %v", err)
	}
	if uid, _, _ := c.Tokens(); uid != "uid" {
		t.Errorf("the session is %q, want the one the proof was granted", uid)
	}
}
