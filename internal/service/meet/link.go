package meet

import (
	"net/url"
	"strings"
)

// The shape of a link, as parseMeetingLink in Proton's web client reads it. The
// password sits in the fragment, which a browser never sends to a server.
const (
	linkHost       = "meet.proton.me"
	linkPathPrefix = "/join/id-"
	linkFragment   = "pwd-"
	linkNameLength = 10
)

func Link(name, password string) string {
	return "https://" + linkHost + linkPathPrefix + name + "#" + linkFragment + password
}

// ParseLink takes a link apart into the meeting's link name and the password,
// which is empty for a link without one. ok is false for anything that is not
// a Proton Meet link.
func ParseLink(s string) (name, password string, ok bool) {
	raw := strings.TrimSpace(s)
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || !strings.EqualFold(u.Hostname(), linkHost) {
		return "", "", false
	}
	name, found := strings.CutPrefix(u.Path, linkPathPrefix)
	if !found || !inAlphabet(name, linkNameLength) {
		return "", "", false
	}
	password, found = strings.CutPrefix(u.Fragment, linkFragment)
	if !found || !inAlphabet(password, passwordLength) {
		password = ""
	}
	return name, password, true
}

func inAlphabet(s string, n int) bool {
	if len(s) != n {
		return false
	}
	for _, r := range s {
		if !strings.ContainsRune(passwordAlphabet, r) {
			return false
		}
	}
	return true
}
