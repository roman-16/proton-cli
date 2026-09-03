// Package redact is what makes a diagnostic log safe to hand to a stranger.
//
// Everything the CLI logs passes through here on its way to a stream or a file,
// and nothing that identifies the person running it survives the trip. An
// address becomes a handle, a Proton ID becomes a handle, an API route keeps its
// shape and loses its arguments, and free text is rewritten rather than trusted.
//
// The handles are stable: the same address is the same handle in every record of
// every run on one machine, so a reader can follow which of three addresses
// failed twice without ever learning whose it is. They are derived under a salt
// that never leaves the machine, so they mean nothing anywhere else - two users
// reporting the same bug produce different handles for the same address, and
// neither handle can be turned back into one.
package redact

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strconv"
	"strings"

	"github.com/roman-16/proton-cli/internal/ref"
)

// Policy is what may be written for a declared attribute.
//
// It is a property of the attribute's name rather than of the value, which is
// what makes the guarantee checkable: a name is visible in the source, and a
// value is only visible at run time. Nothing may be logged under a name no
// policy is declared for.
type Policy int

const (
	// Keep writes the value as it stands. It is for the values that describe
	// the machinery rather than the person: a count, a duration, a status, an
	// HTTP method, one of a fixed set of words.
	Keep Policy = iota
	// Handle replaces the value with a stable meaningless name for it, prefixed
	// by the attribute it stood under: `address:3f9c1e`.
	Handle
	// Address treats the value as an email: the local part becomes a handle and
	// the domain survives only if it is one of Proton's own, which every account
	// has and so identifies nobody.
	Address
	// Route treats the value as an API path: the segments that name the endpoint
	// survive and the ones that name a thing do not.
	Route
	// Text treats the value as prose that was assembled elsewhere - an error
	// message, most of all - and rewrites what looks like an address, an ID or a
	// path anywhere inside it. It is the weakest policy, for the one kind of
	// value that cannot be reduced to a shape, which is why so few attributes
	// carry it.
	Text
	// Drop writes nothing at all.
	Drop
)

// protonDomains are the domains an address may keep, because every Proton
// account can have one and so carrying it tells a reader nothing about who the
// account belongs to. Anything else - a custom domain, an external address a
// message was sent to - collapses to `elsewhere`, since a rare domain names a
// person about as well as their address does.
var protonDomains = map[string]bool{
	"pm.me":          true,
	"proton.black":   true,
	"proton.me":      true,
	"protonmail.ch":  true,
	"protonmail.com": true,
}

// handleLength is how much of the digest a handle shows. Six hex characters
// distinguish the handful of addresses, keys and shares one account has, while
// staying short enough to read across a line.
const handleLength = 6

// Redactor applies the policies under one salt.
type Redactor struct{ salt []byte }

// New returns a redactor keyed by salt. A nil or empty salt is replaced by a
// fresh random one, which keeps handles stable for as long as the process lives
// and meaningless afterwards - the right answer when there is no log to be
// consistent with.
func New(salt []byte) *Redactor {
	if len(salt) == 0 {
		salt = make([]byte, saltLength)
		_, _ = rand.Read(salt)
	}
	return &Redactor{salt: salt}
}

// Apply is the whole of what this package does to one attribute: the policy
// declared for the name decides, and a name with no policy is refused.
func (r *Redactor) Apply(name, value string) (string, bool) {
	policy, declared := Fields[name]
	if !declared || policy == Drop {
		return "", false
	}
	switch policy {
	case Handle:
		return r.handle(name, value), true
	case Address:
		return r.address(value), true
	case Route:
		return r.route(value), true
	case Text:
		return r.text(value), true
	}
	return value, true
}

// Declared reports whether anything may be logged under this name. It is what
// the conformance test asks, so that an attribute nobody declared is a build
// failure rather than a leak.
func Declared(name string) bool {
	_, ok := Fields[name]
	return ok
}

// handle is the stable meaningless name for a value, under its own kind so that
// a share and a link that happen to share an ID do not read as the same thing.
func (r *Redactor) handle(kind, value string) string {
	if value == "" {
		return ""
	}
	mac := hmac.New(sha256.New, r.salt)
	_, _ = mac.Write([]byte(kind))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(value))
	return kind + ":" + hex.EncodeToString(mac.Sum(nil))[:handleLength]
}

// address keeps the shape of an email and none of its content, so that a reader
// can tell three addresses apart, tell a Proton address from one on a custom
// domain, and learn nothing else.
func (r *Redactor) address(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if alreadyDone.MatchString(value) {
		return value
	}
	local, domain, ok := strings.Cut(value, "@")
	if !ok {
		return r.handle("address", value)
	}
	kept := "elsewhere"
	if protonDomains[strings.ToLower(domain)] {
		kept = strings.ToLower(domain)
	}
	return r.handle("address", local+"@"+strings.ToLower(domain)) + "@" + kept
}

// route keeps the endpoint an API path names and drops what it names it about,
// so a reader sees which call failed without seeing which of the account's
// things it was about. A query string goes entirely: its values are IDs and
// search terms, and its names are not worth the risk of keeping the wrong one.
//
// Which segments name a thing is ref.Full's answer rather than a guess made
// here. A rule of thumb about length looks right and is not: Proton has routes
// ending `incomingdefaults`, `checkAvailableHashes`, `restore_multiple` and
// `default_mailbox_id`, so any threshold short enough to catch its shortest ID
// also eats the name of the endpoint - which is the most useful thing in the
// record. The shapes an ID comes in are declared once, where references are read
// and written, and borrowing that is what stops the two disagreeing.
func (r *Redactor) route(value string) string {
	path, _, _ := strings.Cut(value, "?")
	segments := strings.Split(path, "/")
	for i, s := range segments {
		if ref.Full(s) {
			segments[i] = "{id}"
		}
	}
	return strings.Join(segments, "/")
}

var (
	urlInText   = regexp.MustCompile(`[A-Za-z][A-Za-z0-9+.-]*://[^\s]+`)
	emailInText = regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}`)
	idInText    = regexp.MustCompile(`[A-Za-z0-9_=-]{20,}`)
	// A local path usually contains spaces when it is the one worth hiding, so
	// the run continues past them, up to whatever ends a path in the way this
	// CLI's own error chains punctuate one.
	//
	// What comes before the path is matched and put back, because that is the
	// only way to say "not part of a URL" in a language with no lookbehind: a
	// slash following a colon or another slash belongs to something url has
	// already dealt with.
	pathInText = regexp.MustCompile(`(^|[^A-Za-z0-9_:/])((?:[A-Za-z]:\\|/)[^:;,"'` + "`" + `]{2,})`)
	// alreadyDone matches what address has already produced, so that redacting
	// twice is the same as redacting once. Without it a second pass reads the
	// handle's own domain as an address and stands in for the stand-in.
	alreadyDone = regexp.MustCompile(`^[0-9a-f]{` + strconv.Itoa(handleLength) + `}@[A-Za-z0-9.-]+$`)
)

// pathStandIn is what a local path becomes. The basename does not survive: a
// filename is the most telling thing on a disk, and "the third file in the
// folder" is not a fact any bug turns on.
const pathStandIn = "<path>"

// text rewrites prose that was assembled somewhere this package cannot reach.
//
// An error message is the value most worth having and the one value whose
// content cannot be predicted: it may have picked up an address from a
// recipient, an ID from a reference, a filename from the disk, a token from a
// challenge. Each of those has a shape, and what has a shape can be found.
//
// The order is what makes it work. Addresses go before paths because a URL's
// host is not a directory; paths go before IDs because a path may contain one
// and the whole path is leaving anyway; and URLs go first, since everything
// after would otherwise read one as several of its parts.
func (r *Redactor) text(value string) string {
	value = urlInText.ReplaceAllStringFunc(value, r.url)
	value = emailInText.ReplaceAllStringFunc(value, r.address)
	value = pathInText.ReplaceAllString(value, "${1}"+pathStandIn)
	return idInText.ReplaceAllStringFunc(value, func(s string) string {
		return r.handle("id", s)
	})
}

// url keeps where a request went and drops what it was about: the scheme and
// host say which service answered, the path keeps its shape, and the query goes
// - a verification token and a search term both live there.
func (r *Redactor) url(value string) string {
	scheme, rest, ok := strings.Cut(value, "://")
	if !ok {
		return value
	}
	host, path, hasPath := strings.Cut(rest, "/")
	out := scheme + "://" + host
	if hasPath {
		out += r.route("/" + path)
	}
	return out
}
