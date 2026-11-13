package proton

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ProtonMail/go-srp"
	"github.com/roman-16/proton-cli/internal/errs"
)

const signedTestModulus = "-----BEGIN PGP SIGNED MESSAGE-----\nHash: SHA256\n\n" +
	"o4ycZ14/7LfHkuSKWNlpQEh6bwLMVKvo0MFqVq9wHXwkZ/zMcqYaVhqNvLyDB0WY5Uv/Bo23JQsox52lM+4jPydw9/A9saAj" +
	"8erLCc3ZaZHxOl/a8tlYTq7FeDrbhSSgivwTKJ5Y9otla/U8FATZBxqi7nqDihS5/7x/yK3VRnEsBG1i5DcY1UQK3KD9i9v7" +
	"N2QTuGFYnRCv0MFsHzrQZWvUa1NsUhozU5PSV5s7hZkb/p6J3B9ybD6+LzuLS9fyLMcVdxzn2WUXG7JLeBbqsoECUfq9KP2w" +
	"aTzVLELOenWUV1wbioceJsaiP97ViwNJdnKx1ICoYu2c+z8ctVcqlw==\n" +
	"-----BEGIN PGP SIGNATURE-----\nVersion: ProtonMail\nComment: https://protonmail.com\n\n" +
	"wl4EARYIABAFAlwB1j0JEDUFhcTpUY8mAAB02wD5AOhMNS/K6/nvaeRhTr5n\niDGMalQccYlb58XzUEhqf3sBAOcTsz0fP3PVdMQYBbqcBl9Y6LGIG9DF4B4H\nZeLCoyYN\n=cAxM\n" +
	"-----END PGP SIGNATURE-----\n"

type extraPasswordServer struct {
	*httptest.Server
	forgeProof bool
	refreshed  int
}

func newExtraPasswordServer(t *testing.T, password string) *extraPasswordServer {
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

	s := &extraPasswordServer{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case extraPasswordInfoPath:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"Code": 1000,
				"SRPData": map[string]any{
					"Modulus":         signedTestModulus,
					"ServerEphemeral": base64.StdEncoding.EncodeToString(ephemeral),
					"SrpSessionID":    "srp-session",
					"SrpSalt":         stored.Salt,
					"Version":         stored.Version,
				},
			})
		case extraPasswordAuthPath:
			var body struct {
				ClientEphemeral, ClientProof, SrpSessionID string
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("proof body: %v", err)
			}
			if body.SrpSessionID != "srp-session" {
				t.Errorf("SrpSessionID = %q, want the one the challenge named", body.SrpSessionID)
			}
			clientEphemeral, _ := base64.StdEncoding.DecodeString(body.ClientEphemeral)
			clientProof, _ := base64.StdEncoding.DecodeString(body.ClientProof)
			serverProof, err := server.VerifyProofs(clientEphemeral, clientProof)
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"Code": extraPasswordWrongCode, "Error": "Wrong password",
				})
				return
			}
			if s.forgeProof {
				serverProof = append([]byte("no"), serverProof...)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"Code":        1000,
				"ServerProof": base64.StdEncoding.EncodeToString(serverProof),
			})
		case "/auth/v4/refresh":
			s.refreshed++
			_ = json.NewEncoder(w).Encode(map[string]any{
				"Code": 1000, "AccessToken": "unlocked-token", "RefreshToken": "next-refresh",
			})
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	t.Cleanup(s.Close)
	return s
}

func TestUnlockingPassProvesTheExtraPasswordAndKeepsTheSession(t *testing.T) {
	srv := newExtraPasswordServer(t, "correct horse")
	c := New(Options{BaseURL: srv.URL, Logger: slog.New(slog.DiscardHandler)})
	store := savedAs(c, SavedTokens{UID: "uid", Access: "stale-token", Refresh: "refresh-token"})

	if err := c.unlockPass(t.Context(), []byte("correct horse")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if srv.refreshed != 1 {
		t.Errorf("the session was renewed %d times, want once", srv.refreshed)
	}
	if _, access, refresh := c.Tokens(); access != "unlocked-token" || refresh != "next-refresh" {
		t.Errorf("tokens are %q/%q, want the renewed pair", access, refresh)
	}
	if store.saved.Access != "unlocked-token" {
		t.Error("the renewed session was never saved, so the next run would ask again")
	}
}

func TestProvingTheExtraPasswordRenewsNothingOnItsOwn(t *testing.T) {
	srv := newExtraPasswordServer(t, "correct horse")
	c := New(Options{BaseURL: srv.URL, Logger: slog.New(slog.DiscardHandler)})
	c.SetTokens("uid", "stale-token", "refresh-token")

	if err := c.ProveExtraPassword(t.Context(), []byte("correct horse")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if srv.refreshed != 0 {
		t.Errorf("the session was renewed %d times, want not at all", srv.refreshed)
	}
}

func TestAForgedServerProofIsRefused(t *testing.T) {
	srv := newExtraPasswordServer(t, "correct horse")
	srv.forgeProof = true
	c := New(Options{BaseURL: srv.URL, Logger: slog.New(slog.DiscardHandler)})
	c.SetTokens("uid", "stale-token", "refresh-token")

	err := c.unlockPass(t.Context(), []byte("correct horse"))
	if err == nil {
		t.Fatal("a server that proved nothing was accepted")
	}
	if srv.refreshed != 0 {
		t.Error("the session was renewed for an exchange that failed")
	}
}

func TestAWrongExtraPasswordSaysWhichSecretItWas(t *testing.T) {
	srv := newExtraPasswordServer(t, "correct horse")
	c := New(Options{BaseURL: srv.URL, Logger: slog.New(slog.DiscardHandler)})
	c.SetTokens("uid", "stale-token", "refresh-token")

	err := c.unlockPass(t.Context(), []byte("wrong horse"))
	if err == nil {
		t.Fatal("a wrong extra password was accepted")
	}
	var coder errs.ExitCoder
	if !errors.As(err, &coder) || coder.ExitCode() != 2 {
		t.Errorf("err = %v, want one exiting 2", err)
	}
	var hinter errs.Hinter
	if !errors.As(err, &hinter) || len(hinter.Hints()) == 0 {
		t.Errorf("err = %v, want one that says what to do next", err)
	}
}

func TestTooManyWrongExtraPasswordsReadsAsAnEndedSession(t *testing.T) {
	spent := &APIError{HTTPStatus: http.StatusBadRequest, Code: extraPasswordSpentCode, Message: "Too many attempts"}
	err := extraPasswordRefusal(spent)
	problem, ok := err.(*errs.Problem)
	if !ok {
		t.Fatalf("err = %T, want a problem with its own words", err)
	}
	if problem.ExitCode() != 2 {
		t.Errorf("exit code = %d, want 2", problem.ExitCode())
	}
	if len(problem.Hints()) == 0 || problem.Hints()[0] != "proton account login" {
		t.Errorf("hints = %v, want signing in again", problem.Hints())
	}
	other := &APIError{HTTPStatus: http.StatusInternalServerError, Code: 500, Message: "Server error"}
	if got := extraPasswordRefusal(other); !errors.Is(got, other) {
		t.Errorf("err = %v, want the API error untouched", got)
	}
}

func TestAChallengeThatProvesNothingIsRefusedBeforeItIsUsed(t *testing.T) {
	whole := extraPasswordChallenge{
		Modulus: signedTestModulus, ServerEphemeral: "ephemeral",
		SrpSessionID: "srp-session", SrpSalt: "salt", Version: 4,
	}
	if err := whole.check(); err != nil {
		t.Errorf("a whole challenge was refused: %v", err)
	}
	for _, tc := range []struct {
		name      string
		challenge extraPasswordChallenge
	}{
		{"nothing at all", extraPasswordChallenge{}},
		{"no modulus", extraPasswordChallenge{ServerEphemeral: "e", SrpSessionID: "s", SrpSalt: "salt", Version: 4}},
		{"no session", extraPasswordChallenge{Modulus: signedTestModulus, ServerEphemeral: "e", SrpSalt: "salt", Version: 4}},
		{"a version no extra password is stored under", extraPasswordChallenge{
			Modulus: signedTestModulus, ServerEphemeral: "e", SrpSessionID: "s", SrpSalt: "salt", Version: 2}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.challenge.check(); err == nil {
				t.Error("accepted")
			}
		})
	}
}
