// Package twofa answers one question about a URL: does the site behind it offer
// a time-based code.
//
// The list is 2fa.directory's, which is what Proton Pass reads for the same
// check, so a login this calls unprotected is one Pass calls unprotected too.
// `just twofa` rewrites it; see scripts/twofa for the two adjustments.
//
// It is a snapshot, and being a snapshot is the point: an answer that depended
// on the network would differ between two machines on the same day, and the
// check exists so that nothing about a stored password has to leave the machine.
package twofa

import (
	_ "embed"
	"net/url"
	"strings"

	"golang.org/x/net/publicsuffix"
)

//go:embed domains.txt
var list string

// domains is the set, built once.
var domains = func() map[string]bool {
	out := make(map[string]bool, 1300)
	for _, d := range strings.Fields(list) {
		out[d] = true
	}
	return out
}()

// Eligible reports whether the site a login points at offers a time-based code.
//
// The stored URL is whatever somebody typed - a bare host, a full address, a
// deep link - so it is reduced to the registrable domain before being looked up:
// the directory lists sites, and accounts.google.com and google.co.uk are both
// the site people mean.
func Eligible(rawURL string) bool {
	host := host(rawURL)
	if host == "" {
		return false
	}
	if domains[host] {
		return true
	}
	registrable, err := publicsuffix.EffectiveTLDPlusOne(host)
	if err != nil {
		return false
	}
	return domains[registrable]
}

// host is the hostname a stored URL names, lowercased and without its port.
func host(rawURL string) string {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		return ""
	}
	if !strings.Contains(trimmed, "//") {
		// A URL stored without a scheme parses as a path, so it is given one
		// rather than being read by hand.
		trimmed = "https://" + trimmed
	}
	u, err := url.Parse(trimmed)
	if err != nil {
		return ""
	}
	return strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
}
