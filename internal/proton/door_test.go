package proton

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// Which front door a request goes through.
//
// Proton's API is one gateway behind several hosts, and not every host routes
// every path: the recovery phrase answers at the account host and is Path not
// found at the one Mail is served from. A request sent to the wrong door comes
// back as a 404 that reads like a missing feature, so the rule is pinned here.

func TestARequestGoesThroughTheDoorItNames(t *testing.T) {
	for _, tc := range []struct {
		base        string
		accountHost bool
		want        string
	}{
		{base: "https://mail.proton.me/api", want: "https://mail.proton.me/api"},
		{base: "https://mail.proton.me/api", accountHost: true, want: "https://account.proton.me/api"},
		{base: "https://mail-api.proton.me/api", accountHost: true, want: "https://account.proton.me/api"},
		// A host with no siblings keeps whatever the CLI was pointed at: a
		// local API, an address rather than a name, Proton's DoH front.
		{base: "http://localhost:8080", accountHost: true, want: "http://localhost:8080"},
		{base: "https://192.0.2.10/api", accountHost: true, want: "https://192.0.2.10/api"},
		{base: "https://ec2-1-2-3-4.compute.amazonaws.com/api", accountHost: true,
			want: "https://ec2-1-2-3-4.compute.amazonaws.com/api"},
	} {
		c := New(Options{BaseURL: tc.base, Logger: slog.New(slog.DiscardHandler)})
		if got := c.doorFor(Request{AccountHost: tc.accountHost}); got != tc.want {
			t.Errorf("%s (account host %v) goes to %s, want %s",
				tc.base, tc.accountHost, got, tc.want)
		}
	}
}

// And the whole way through, for both kinds of request.
//
// A proving request is the one worth the trouble: it is taken apart into an
// SRP exchange and built back into a request, and what the caller declared has
// to survive that. The endpoints wanting a proof in the body are the account
// app's, so a door dropped on the way is a 404 on every one of them.
func TestARequestReachesTheHostItNamed(t *testing.T) {
	proton := newProvingServer(t, "correct horse")
	knocked := &knocking{to: proton.URL, t: t}
	c := New(Options{
		BaseURL: "https://mail.proton.me/api", Logger: slog.New(slog.DiscardHandler),
		HTTPClient: &http.Client{Transport: knocked},
	})
	c.SetTokens("uid", "access", "refresh")
	c.SetScopeResolver(func(context.Context, Scope) (ScopeCredentials, error) {
		return ScopeCredentials{Username: "me@proton.me", Password: []byte("correct horse")}, nil
	})

	if err := c.Decode(t.Context(), Request{
		Method: "GET", Path: "/core/v4/settings",
	}, nil); err != nil {
		t.Fatalf("an ordinary request: %v", err)
	}
	if err := c.Decode(t.Context(), Request{
		Method: "PUT", Path: "/core/v4/settings/2fa/totp", AccountHost: true, Proves: true,
	}, nil); err != nil {
		t.Fatalf("a proving request: %v", err)
	}

	// The parameters an exchange fetches go where the proof goes: they are two
	// halves of one conversation, and Proton spends the session it issued them
	// under.
	want := []string{"mail.proton.me", "account.proton.me", "account.proton.me"}
	if fmt.Sprint(knocked.hosts) != fmt.Sprint(want) {
		t.Errorf("knocked at %v, want %v", knocked.hosts, want)
	}
}

// knocking records the host each request was addressed to and hands it to the
// server standing in for Proton, which is what lets a test watch a door being
// chosen without a name that has to resolve.
type knocking struct {
	to    string
	t     *testing.T
	hosts []string
}

func (k *knocking) RoundTrip(r *http.Request) (*http.Response, error) {
	k.hosts = append(k.hosts, r.URL.Hostname())
	to, err := url.Parse(k.to)
	if err != nil {
		k.t.Fatalf("stand-in address: %v", err)
	}
	out := r.Clone(r.Context())
	out.URL.Scheme, out.URL.Host, out.Host = to.Scheme, to.Host, to.Host
	// The stand-in is the API root, where Proton serves it under /api.
	out.URL.Path = strings.TrimPrefix(out.URL.Path, "/api")
	return http.DefaultTransport.RoundTrip(out)
}
