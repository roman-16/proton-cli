package drive

import (
	"bytes"
	"strings"
	"unicode/utf8"

	"github.com/roman-16/proton-cli/internal/mailtext"
	"github.com/roman-16/proton-cli/internal/mimetype"
)

// Which files the index holds the text of, and what their text is.
//
// A file is judged twice. Before it is fetched, by the type stored beside it,
// which is what says whether downloading it could be worth anything - the same
// list Proton's own clients open as text, so a file that reads as text in Drive
// reads as text here. After it is fetched, by the bytes, because the type is
// whatever the uploader's client made of the name: a .txt holding a photograph
// is a photograph.
//
// What is left out is as much of the answer as what is in. An image is not text
// however its markup reads, so an SVG is found by its name and not by the words
// inside it - otherwise a keyword would match every diagram that mentions a
// path. A PDF and a document are text nobody can get at without parsing them,
// which is not something this does.

const (
	// maxTextSize is how much of a file the index will hold. What is larger than
	// this is data rather than prose - an export, a log, a dump - and the whole
	// index is decrypted into memory to answer a single listing.
	maxTextSize = 1 << 20
	// textBatch is how many texts are fetched in parallel, which is the width
	// the CLI already fetches many small things at.
	textBatch = 10
)

// textTypes are the types that are text without being text/*, as Proton's own
// clients decide it.
var textTypes = map[string]bool{
	"application/javascript":  true,
	"application/json":        true,
	"application/typescript":  true,
	"application/x-csh":       true,
	"application/x-httpd-php": true,
	"application/x-sh":        true,
	"application/x-tex":       true,
	"application/xhtml+xml":   true,
}

// isText reports whether a file of this type is one whose contents a keyword
// searches.
//
// The name is asked where the type says nothing, which is the same rule that
// put the type there: a client stores what the name implies, and one that
// stored nothing leaves the name as the only thing there is to go on.
func isText(mimeType, name string) bool {
	t := mimeType
	if t == "" || t == mimetype.Unknown {
		t = mimetype.ByName(name)
	}
	return strings.HasPrefix(t, "text/") || textTypes[t]
}

// asText is a file's bytes as the index will search them, and false where they
// are not text after all.
//
// Markup is reduced to what a reader would have read, so a keyword matches the
// words of a page rather than the names of its tags.
func asText(mimeType, name string, data []byte) (string, bool) {
	if !utf8.Valid(data) || bytes.IndexByte(data, 0) >= 0 {
		return "", false
	}
	if mailtext.IsHTML(mimeType) || mailtext.IsHTML(mimetype.ByName(name)) {
		return mailtext.HTMLToText(string(data)), true
	}
	return string(data), true
}
