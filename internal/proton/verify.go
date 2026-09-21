package proton

import (
	"fmt"
	"net/url"
	"slices"
	"strings"
)

// verifySubdomain is where Proton serves the human-verification page, as a
// sibling of the API on the same root domain.
const verifySubdomain = "verify"

// dohDomain is the one domain Proton treats as DNS-over-HTTPS, where the API is
// reached by address rather than by a name of its own.
const dohDomain = ".compute.amazonaws.com"

// VerifyURL returns the address of Proton's human-verification page for a
// challenge, ready for a person to open in whatever browser they have.
//
// The page is what turns a challenge into a proof: opened as a top-level window
// rather than embedded, it submits the solved CAPTCHA to
// /core/v4/verification/captcha/{challenge} itself, and the challenge token then
// stands as the proof on a retry. That is why the query is built here rather
// than taken from Details.WebUrl - `embed=1` would make the page hand the answer
// to a host that is not there instead of redeeming it, and `methods` has to name
// captcha alone, since a code delivered by email or SMS is verified and then
// reported to nobody.
//
// An address comes back only for a challenge a person can actually finish, so
// whether one can be offered is asked here rather than in each caller.
//
// Mirrors the external-browser branch of WebClients' verify app
// (applications/verify/src/app/Verify.tsx).
func (c *Client) VerifyURL(hv *HumanVerificationError) (string, error) {
	if hv == nil || hv.Token == "" {
		return "", fmt.Errorf("verify url: no challenge to verify")
	}
	if !slices.Contains(hv.Methods, MethodCaptcha) {
		return "", fmt.Errorf("verify url: Proton offers %s, and only a CAPTCHA can be solved this way",
			strings.Join(hv.Methods, ", "))
	}
	origin, err := c.verifyOrigin(hv.WebURL)
	if err != nil {
		return "", err
	}
	origin.RawQuery = url.Values{
		"methods": {MethodCaptcha},
		"token":   {hv.Token},
	}.Encode()
	return origin.String(), nil
}

// verifyOrigin is where the page is served from.
//
// Proton names it in the refusal, which is the answer that holds for whatever
// host the CLI was pointed at. Deriving it from the API base is the fallback for
// a refusal that names none, and it can only be done where the host is a name
// under a root domain: a local, DoH or bare-IP API serves no verify page at all,
// and guessing one would send somebody to a host that cannot help them.
func (c *Client) verifyOrigin(webURL string) (*url.URL, error) {
	if u, err := url.Parse(webURL); err == nil && u.Scheme != "" && u.Host != "" {
		return &url.URL{Scheme: u.Scheme, Host: u.Host, Path: "/"}, nil
	}
	u, err := url.Parse(c.base)
	if err != nil {
		return nil, fmt.Errorf("verify url: unusable api base %q: %w", c.base, err)
	}
	host, ok := siblingHost(u.Hostname(), verifySubdomain)
	if !ok {
		return nil, fmt.Errorf("verify url: %s serves no verification page", u.Host)
	}
	return &url.URL{Scheme: u.Scheme, Host: host, Path: "/"}, nil
}

// siblingHost moves a host to another of Proton's under the same root domain:
// mail.proton.me and mail-api.proton.me both have verify.proton.me for a
// verification page and account.proton.me for the account app's endpoints.
//
// It reports false for anything that is not a name under a root domain, by
// Proton's own rule for addresses: no top-level domain ends in a digit, and an
// IPv6 hostname contains a colon. Such a host has no siblings, and whatever the
// CLI was pointed at is the only door there is.
func siblingHost(hostname, subdomain string) (string, bool) {
	if hostname == "" || strings.Contains(hostname, ":") || strings.HasSuffix(hostname, dohDomain) {
		return "", false
	}
	if last := hostname[len(hostname)-1]; last >= '0' && last <= '9' {
		return "", false
	}
	dot := strings.Index(hostname, ".")
	if dot < 0 {
		return "", false
	}
	return subdomain + "." + hostname[dot+1:], true
}
