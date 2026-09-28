package proton

import (
	"log/slog"
	"net/http"
	"testing"
)

// Proton asks for a stronger session in more than one way, and only one of them
// has a name in WebClients. Missing an answer means the elevation never runs and
// the user sees a raw 403 instead of being asked for their password.
func TestIsMissingScope(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		want   bool
	}{
		{
			name:   "the code WebClients knows",
			status: http.StatusForbidden,
			body:   `{"Code":9100,"Error":"Access token does not have sufficient scope"}`,
			want:   true,
		},
		{
			name:   "the code deleting a calendar answers with",
			status: http.StatusForbidden,
			body:   `{"Code":9101,"Details":{"MissingScopes":["locked"]},"Error":"Access token does not have sufficient scope"}`,
			want:   true,
		},
		{
			name:   "a named scope outweighs an unknown code",
			status: http.StatusForbidden,
			body:   `{"Code":401234,"Details":{"MissingScopes":["password"]}}`,
			want:   true,
		},
		{
			name:   "a forbidden that is about something else",
			status: http.StatusForbidden,
			body:   `{"Code":2011,"Error":"Not allowed"}`,
		},
		{
			name:   "the right body under the wrong status",
			status: http.StatusBadRequest,
			body:   `{"Code":9100}`,
		},
		{
			// The Pass refusal is the one whose status is not relied on, so it is
			// recognised under a status no other scope would be.
			name:   "Pass refusing a session that has no extra password",
			status: http.StatusBadRequest,
			body:   `{"Code":9108,"Details":{"MissingScopes":["pass"]},"Error":"Access token does not have sufficient scope"}`,
			want:   true,
		},
		{
			name:   "a body that is not JSON",
			status: http.StatusForbidden,
			body:   `<html>gateway</html>`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := isMissingScope(tc.status, []byte(tc.body)); got != tc.want {
				t.Errorf("got %v want %v", got, tc.want)
			}
		})
	}
}

// The retry asks for the scope the server named, and falls back to the weakest of
// them when it named none.
//
// Pass is the one that matters most to get right: it is answered with a different
// secret, so reading it as one of the others asks for the account password and
// then fails anyway.
func TestScopeFromBody(t *testing.T) {
	for _, tc := range []struct {
		body string
		want Scope
	}{
		{`{"Details":{"MissingScopes":["locked"]}}`, ScopeLocked},
		{`{"Details":{"MissingScopes":["password"]}}`, ScopePassword},
		{`{"Details":{"MissingScopes":["locked","password"]}}`, ScopePassword},
		{`{"Code":9108,"Details":{"MissingScopes":["pass"]}}`, ScopePass},
		{`{"Code":9108}`, ScopePass},
		{`{"Details":{"MissingScopes":["pass"]}}`, ScopePass},
		{`{"Details":{"MissingScopes":["password","pass"]}}`, ScopePass},
		{`{"Code":9100}`, ScopeLocked},
		{`not json`, ScopeLocked},
	} {
		if got := scopeFromBody([]byte(tc.body)); got != tc.want {
			t.Errorf("scopeFromBody(%s) = %s, want %s", tc.body, got, tc.want)
		}
	}
}

func TestAnElevationNamesNoAccount(t *testing.T) {
	account := newLegacyAccount(t, "alice.smith", "correct horse")
	c := New(Options{BaseURL: account.URL, Logger: slog.New(slog.DiscardHandler)})
	c.SetTokens("uid", "access", "refresh")

	if err := c.Elevate(t.Context(), ScopePassword, ScopeCredentials{Password: []byte("correct horse")}); err != nil {
		t.Fatalf("elevating: %v", err)
	}
	if len(account.asked) != 1 {
		t.Fatalf("the parameters were asked for %d times, want once", len(account.asked))
	}
	if named, ok := account.asked[0]["Username"]; ok {
		t.Errorf("the parameters were asked for %v, want the session's own account", named)
	}
}
