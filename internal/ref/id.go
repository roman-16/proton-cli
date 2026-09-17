package ref

import "strings"

// What a reference looks like, declared once, so that the grammar a listing
// prints and the grammar the next command reads are one thing rather than two
// that happen to agree.
//
// The shapes are written down from measurement. Every one below was taken from a
// live account rather than inferred:
//
//	kind                                       length  padding  bytes
//	messages, conversations, labels, folders,      88  "=="        64
//	  addresses, contacts, calendars, events,
//	  Pass shares and items, vaults
//	Drive links, Drive share invitations           22  none        16
//	session UIDs                                   32  none         -
//
// The alphabet is URL-safe base64 throughout, which Proton's own clients rely
// on: Pass routes parse a share out of a path with `share/([^/]+)`, and the
// calendar joins a calendar and an event as `${calendarID}.${eventID}` - neither
// would be safe if an ID could contain a slash or a dot.
//
// Session UIDs are lowercase alphanumeric. They are recognised all the same, so
// that classifying a reference never depends on which command is asking.

// ShortLen is how many characters of an ID identify it in interactive output.
// Eight is short enough to read back off the screen and long enough that
// collisions within one account are vanishingly rare; when two cached IDs do
// collide, resolution reports the ambiguity rather than guessing.
const ShortLen = 8

// The two separators a reference may contain. Neither can occur inside an ID,
// which is what lets a compound reference stay a single pasteable token.
const (
	// Compound joins a thing to the container that addresses it: a Pass item
	// inside its share, an event inside its calendar.
	Compound = "/"
	// Occurrence names one instance of a recurring event, and is not an ID: it
	// is a timestamp, so it is never shortened or matched against the cache.
	Occurrence = "@"
)

// Shorten writes a reference the way interactive output shows it: ShortLen
// characters of each ID it joins, with what follows an "@" carried over whole
// because that names an occurrence rather than a thing.
//
// Leading dashes are skipped, so a short ID never begins with one and never has
// to be marked as data on the command line. '-' is one of base64url's sixty-four
// characters, so about one ID in sixty-four starts with one; the eight
// characters are still a run taken verbatim out of the ID, begun one place
// later, and Matches is how they are found again.
func Shorten(reference string) string {
	if reference == "" {
		return reference
	}
	parts, occurrence := Split(reference)
	for i, p := range parts {
		parts[i] = shortenID(p)
	}
	out := Join(parts...)
	if occurrence == "" && !strings.Contains(reference, Occurrence) {
		return out
	}
	return out + Occurrence + occurrence
}

// Matches reports whether short names full: what Shorten writes for an ID is a
// prefix of that ID once its leading dashes are skipped, and so is any longer
// run the user types instead - the whole ID included.
//
// The dashes are skipped on both sides, because they are not part of what
// identifies an ID: a listing prints the ID whole, dash and all, so an ID that
// begins with one has to answer to itself as it was printed as well as to the
// shell-safe run Shorten writes.
func Matches(full, short string) bool {
	return strings.HasPrefix(body(full), body(short))
}

// Full reports whether s is a complete ID as Proton issues them.
//
// The padded form is matched by its ending rather than its exact length so that
// a shape this CLI has not seen is still recognised as complete; the two
// unpadded forms have no ending to match and are matched by length.
func Full(s string) bool {
	switch {
	case len(s) >= 60 && strings.HasSuffix(s, "=="):
		return isBase64URL(s)
	case len(s) == 22:
		return isBase64URL(s)
	case len(s) == 32:
		return isLowerAlnum(s)
	}
	return false
}

// Short reports whether s could be a shortened ID, and so worth looking up in
// the cache of what has been shown.
//
// The predicate is deliberately loose: plenty of search terms ("Personal",
// "invoice-2024") match it. That costs nothing, because a lookup is only used
// when it hits - a miss falls through to the original text, which the service
// layer then treats as a name to search for. A leading dash is the one thing it
// rules out, because Shorten never writes one.
func Short(s string) bool {
	return len(s) >= ShortLen && s[0] != '-' && !Full(s) && len(s) < 60 &&
		!strings.HasSuffix(s, "==") && isBase64URL(s)
}

// Split takes a reference apart into the IDs it joins and the occurrence it
// names, either of which may be absent.
//
// It is the inverse of Join, and the one place the notation is parsed. A
// reference with no separator comes back as a single part, which is the shape a
// human handle - a subject, a name, a path - also takes.
func Split(reference string) (parts []string, occurrence string) {
	body, occurrence, _ := strings.Cut(reference, Occurrence)
	return strings.Split(body, Compound), occurrence
}

// Join writes the IDs back as the single token a user pastes.
func Join(parts ...string) string { return strings.Join(parts, Compound) }

// Unambiguous reports whether s begins with a dash and is a reference whatever
// command it was passed to, so that argv can mark it as data before the flag
// parser reaches it.
//
// Shape settles it, on two rules. A token opening with two dashes is a long
// flag, because that is what two dashes mean - and a flag's name is spelt from
// the same alphabet an ID is, so "--second-password-file" is twenty-two legal
// characters and would otherwise read as a Drive link. Below that, no single
// shorthand and no run of them is 22, 32 or 88 characters of base64, and nothing
// shorter has to be asked about at all, because a short ID never begins with a
// dash.
//
// An ID that opens with two dashes is therefore the user's to mark, with the
// "--" every shell already has for it.
func Unambiguous(s string) bool {
	if len(s) < 2 || s[0] != '-' || s[1] == '-' {
		return false
	}
	parts, _ := Split(s)
	for _, p := range parts {
		if !Full(p) {
			return false
		}
	}
	return true
}

// body is the ID with the leading dashes skipped: everything that identifies it,
// and nothing a shell would read as the start of a flag.
func body(id string) string { return strings.TrimLeft(id, "-") }

func shortenID(id string) string {
	b := body(id)
	if len(b) <= ShortLen {
		return b
	}
	return b[:ShortLen]
}

func isBase64URL(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'A' && c <= 'Z':
		case c >= 'a' && c <= 'z':
		case c >= '0' && c <= '9':
		case c == '-' || c == '_' || c == '=':
		default:
			return false
		}
	}
	return true
}

func isLowerAlnum(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z':
		case c >= '0' && c <= '9':
		default:
			return false
		}
	}
	return true
}
