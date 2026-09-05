// Package vcard models the contact cards Proton Contacts stores.
//
// Proton splits a contact across a signed card, which holds the name and the
// email addresses so the server can index them, an encrypted card holding
// everything else, and a clear card holding the one thing the server has to
// read as well as index: which groups each address is in, as CATEGORIES.
// Per-email settings - a pinned key, whether to encrypt or sign to that address,
// its groups - hang off the card under the same vCard group as the address,
// which is why reading and rebuilding a contact has to preserve groups rather
// than flatten them.
package vcard

import (
	"crypto/rand"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/roman-16/proton-cli/internal/contentline"
)

// UID mints an identifier for a new contact, from a cryptographic source so that
// two contacts created in the same moment cannot collide.
func UID() string { return "proton-cli-" + rand.Text() }

// Field returns the first value of a property, ignoring any group it sits under.
func Field(text, name string) string {
	if vs := Values(text, name); len(vs) > 0 {
		return vs[0]
	}
	return ""
}

// Values returns every value of a property, in document order.
func Values(text, name string) []string {
	name = strings.ToUpper(name)
	var out []string
	for _, l := range contentline.ParseAll(text) {
		if l.Name == name {
			out = append(out, contentline.UnescapeText(l.Value))
		}
	}
	return out
}

// EmailGroup returns the group (for example "item1") whose EMAIL property matches
// email, or "". Proton stores each address's key settings under the same group as
// the address.
func EmailGroup(text, email string) string {
	want := canonical(email)
	for _, l := range contentline.ParseAll(text) {
		if l.Name == "EMAIL" && l.Group != "" && canonical(l.Value) == want {
			return l.Group
		}
	}
	return ""
}

// GroupValues returns every value of a property within one group, ordered by the
// vCard PREF parameter. Properties without a preference keep document order,
// after those that have one.
func GroupValues(text, group, field string) []string {
	field = strings.ToUpper(field)
	type ranked struct {
		pref  int
		value string
	}
	var found []ranked
	for i, l := range contentline.ParseAll(text) {
		if l.Group != group || l.Name != field {
			continue
		}
		found = append(found, ranked{pref: pref(l.Params, i), value: l.Value})
	}
	sort.SliceStable(found, func(a, b int) bool { return found[a].pref < found[b].pref })
	out := make([]string, len(found))
	for i, f := range found {
		out[i] = f.value
	}
	return out
}

// GroupValue returns the most-preferred value of a property within one group.
func GroupValue(text, group, field string) string {
	if vs := GroupValues(text, group, field); len(vs) > 0 {
		return vs[0]
	}
	return ""
}

// pref reads the PREF parameter, falling back to a large number that preserves
// document order among properties that do not declare one.
func pref(params contentline.Params, docIndex int) int {
	if v := params.Get("PREF"); v != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			return n
		}
	}
	return 1_000_000 + docIndex
}

func canonical(email string) string {
	return strings.ToLower(strings.TrimSpace(strings.TrimPrefix(email, "mailto:")))
}

// SignedEmail is one address in the signed card, with the pinned keys and
// per-address crypto settings stored under its group.
type SignedEmail struct {
	Address string
	// Kind is the TYPE the address carries - home, work, other - or "" for one
	// that names no kind.
	Kind      string
	KeyValues []string // raw KEY property values, in preference order
	Encrypt   *bool
	Sign      *bool
	Scheme    string
}

// Text renders the address the way --email accepts it, so what a listing shows
// can be handed straight back to a command.
func (e SignedEmail) Text() string { return Typed{Kind: e.Kind, Value: e.Address}.Text() }

// Signed is the part of a contact that Proton signs but does not encrypt.
type Signed struct {
	Name   string
	UID    string
	Emails []SignedEmail
}

// FindEmail returns the entry for addr, or nil.
func (c *Signed) FindEmail(addr string) *SignedEmail {
	want := canonical(addr)
	for i := range c.Emails {
		if canonical(c.Emails[i].Address) == want {
			return &c.Emails[i]
		}
	}
	return nil
}

// ParseSigned reads a signed card, capturing each address's group so its pinned
// keys and settings survive a rebuild.
func ParseSigned(text string) Signed {
	out := Signed{Name: Field(text, "FN"), UID: Field(text, "UID")}
	seen := map[string]bool{}
	for _, l := range contentline.ParseAll(text) {
		if l.Name != "EMAIL" || l.Group == "" || seen[l.Group] {
			continue
		}
		seen[l.Group] = true
		e := SignedEmail{
			Address:   l.Value,
			Kind:      strings.ToLower(l.Params.Get("TYPE")),
			KeyValues: GroupValues(text, l.Group, "KEY"),
			Scheme:    GroupValue(text, l.Group, "X-PM-SCHEME"),
		}
		if v := GroupValue(text, l.Group, "X-PM-ENCRYPT"); v != "" {
			b := strings.EqualFold(strings.TrimSpace(v), "true")
			e.Encrypt = &b
		}
		if v := GroupValue(text, l.Group, "X-PM-SIGN"); v != "" {
			b := strings.EqualFold(strings.TrimSpace(v), "true")
			e.Sign = &b
		}
		out.Emails = append(out.Emails, e)
	}
	return out
}

// BuildSigned renders a signed card, grouping each address's properties as
// item1..itemN.
func BuildSigned(c Signed) string {
	lines := []contentline.Line{
		{Name: "BEGIN", Value: "VCARD"},
		{Name: "VERSION", Value: "4.0"},
		{Name: "FN", Value: contentline.EscapeText(c.Name)},
		{Name: "UID", Value: c.UID},
	}
	n := 0
	for _, e := range c.Emails {
		if e.Address == "" {
			continue
		}
		n++
		group := fmt.Sprintf("item%d", n)
		params := contentline.Params{{Name: "PREF", Value: strconv.Itoa(n)}}
		if e.Kind != "" {
			params = append(params, contentline.Param{Name: "TYPE", Value: e.Kind})
		}
		lines = append(lines, contentline.Line{
			Group: group, Name: "EMAIL", Params: params, Value: e.Address,
		})
		for i, kv := range e.KeyValues {
			lines = append(lines, contentline.Line{
				Group: group, Name: "KEY",
				Params: contentline.Params{{Name: "PREF", Value: strconv.Itoa(i + 1)}},
				Value:  kv,
			})
		}
		if e.Encrypt != nil {
			lines = append(lines, contentline.Line{Group: group, Name: "X-PM-ENCRYPT", Value: boolText(*e.Encrypt)})
		}
		if e.Sign != nil {
			lines = append(lines, contentline.Line{Group: group, Name: "X-PM-SIGN", Value: boolText(*e.Sign)})
		}
		if e.Scheme != "" {
			lines = append(lines, contentline.Line{Group: group, Name: "X-PM-SCHEME", Value: e.Scheme})
		}
	}
	lines = append(lines, contentline.Line{Name: "END", Value: "VCARD"})
	return contentline.Render(lines)
}

func boolText(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// Typed is one value that may say what kind it is: a home phone, a work
// address. vCard carries the kind as a TYPE parameter, and Proton's own editor
// offers one on every repeatable field, so a contact imported from anywhere else
// keeps the distinction instead of collapsing to an untyped list.
//
// Kind is empty when none was given, which is a value in its own right - vCard
// distinguishes "no kind stated" from "other".
type Typed struct {
	Kind  string
	Value string
}

// Kinds are the TYPE values Proton's editor offers, per property. A kind outside
// its property's list is refused rather than stored, because a reader that does
// not recognise it shows nothing.
var Kinds = map[string][]string{
	"EMAIL": {"home", "work", "other"},
	"TEL":   {"home", "work", "other", "cell", "main", "fax", "pager"},
	"ADR":   {"home", "work", "other"},
	"URL":   {"home", "work", "other"},
}

// ParseTyped reads "work:jane@example.com" into its kind and its value.
//
// The split is on the first colon, and only when what precedes it is a kind this
// property offers - so a bare "https://example.com" keeps its scheme, and an
// address with a colon in it is not mangled by a word that only looks like a
// kind.
func ParseTyped(property, raw string) Typed {
	raw = strings.TrimSpace(raw)
	i := strings.Index(raw, ":")
	if i < 0 {
		return Typed{Value: raw}
	}
	kind := strings.ToLower(strings.TrimSpace(raw[:i]))
	for _, k := range Kinds[property] {
		if k == kind {
			return Typed{Kind: k, Value: strings.TrimSpace(raw[i+1:])}
		}
	}
	// Not a kind this property knows: the whole thing is the value.
	return Typed{Value: raw}
}

// Text renders a typed value the way ParseTyped accepts it, so a listing can be
// read back into a command.
func (t Typed) Text() string {
	if t.Kind == "" {
		return t.Value
	}
	return t.Kind + ":" + t.Value
}

// Encrypted is the part of a contact Proton encrypts.
type Encrypted struct {
	Phones    []Typed
	Addresses []Typed
	URLs      []Typed
	Note      string
	Org       string
	Title     string
	Role      string
	Birthday  string
	// Anniversary, Gender, Language, Timezone and Nickname are the rest of what
	// Proton's editor calls "other information".
	Anniversary string
	Gender      string
	Language    string
	Timezone    string
	Nickname    string
	// FirstName and LastName make up the structured name, which is what an
	// address book sorts and merges on. The display name lives in the signed
	// card, because that is the part a recipient verifies.
	FirstName string
	LastName  string
	// Rest are properties this tool has no opinion about, carried through so an
	// imported contact does not lose them on the next edit.
	Rest []contentline.Line
}

// BuildEncrypted renders the encrypted card. Empty properties are left out.
func BuildEncrypted(f Encrypted) string {
	lines := []contentline.Line{
		{Name: "BEGIN", Value: "VCARD"},
		{Name: "VERSION", Value: "4.0"},
	}
	for _, group := range []struct {
		name   string
		values []Typed
	}{
		{"TEL", f.Phones},
		{"ADR", f.Addresses},
		{"URL", f.URLs},
	} {
		n := 0
		for _, v := range group.values {
			if v.Value == "" {
				continue
			}
			n++
			params := contentline.Params{{Name: "PREF", Value: strconv.Itoa(n)}}
			if v.Kind != "" {
				params = append(params, contentline.Param{Name: "TYPE", Value: v.Kind})
			}
			lines = append(lines, contentline.Line{
				Name: group.name, Params: params, Value: contentline.EscapeText(v.Value),
			})
		}
	}
	if f.FirstName != "" || f.LastName != "" {
		// N is five semicolon-separated components: family, given, additional,
		// prefixes, suffixes.
		lines = append(lines, contentline.Line{
			Name:  "N",
			Value: contentline.EscapeText(f.LastName) + ";" + contentline.EscapeText(f.FirstName) + ";;;",
		})
	}
	for _, kv := range []struct{ name, value string }{
		{"NOTE", f.Note},
		{"ORG", f.Org},
		{"TITLE", f.Title},
		{"ROLE", f.Role},
		{"BDAY", f.Birthday},
		{"ANNIVERSARY", f.Anniversary},
		{"GENDER", f.Gender},
		{"LANG", f.Language},
		{"TZ", f.Timezone},
		{"NICKNAME", f.Nickname},
	} {
		if kv.value != "" {
			lines = append(lines, contentline.Line{Name: kv.name, Value: contentline.EscapeText(kv.value)})
		}
	}
	lines = append(lines, f.Rest...)
	lines = append(lines, contentline.Line{Name: "END", Value: "VCARD"})
	return contentline.Render(lines)
}

// ParseEncrypted reads an encrypted card back, keeping what it does not
// recognise so that editing one field cannot drop the rest.
//
// That is the point of Rest. An update rebuilds the card from this struct, so a
// property left out here would be a property deleted by the next `--note`.
//
// Which properties it may carry is decided by the same table that splits a card
// on the way in: the ones another card holds are not this one's to keep.
func ParseEncrypted(card string) Encrypted {
	var f Encrypted
	for _, l := range contentline.ParseAll(card) {
		value := contentline.UnescapeText(l.Value)
		typed := Typed{Kind: strings.ToLower(l.Params.Get("TYPE")), Value: value}
		switch l.Name {
		case "TEL":
			f.Phones = append(f.Phones, typed)
		case "ADR":
			f.Addresses = append(f.Addresses, typed)
		case "URL":
			f.URLs = append(f.URLs, typed)
		case "N":
			parts := strings.SplitN(l.Value, ";", 3)
			if len(parts) > 0 {
				f.LastName = contentline.UnescapeText(parts[0])
			}
			if len(parts) > 1 {
				f.FirstName = contentline.UnescapeText(parts[1])
			}
		case "NOTE":
			f.Note = value
		case "ORG":
			f.Org = value
		case "TITLE":
			f.Title = value
		case "ROLE":
			f.Role = value
		case "BDAY":
			f.Birthday = value
		case "ANNIVERSARY":
			f.Anniversary = value
		case "GENDER":
			f.Gender = value
		case "LANG":
			f.Language = value
		case "TZ":
			f.Timezone = value
		case "NICKNAME":
			f.Nickname = value
		default:
			// A property belonging to another card is not this one's to carry.
			// Carrying the address would make the next edit store a second copy
			// of it, and the edit after that a third; carrying a VERSION would
			// write two of them into a card that must have one.
			if !signedFields[l.Name] && !clearFields[l.Name] {
				f.Rest = append(f.Rest, l)
			}
		}
	}
	return f
}

// ── groups ──

// Category is one group an address is in, as a CATEGORIES property says it.
type Category struct {
	// Group is the vCard group the property sits under, which is the address it
	// is about; "" is a property about every address the card has.
	Group string
	// Name is the group's name.
	Name string
}

// Categories reads the groups a card puts its addresses in.
//
// CATEGORIES carries several names in one value, and Proton's own client writes
// one property per address under that address's group, so both shapes come
// back as one Category per name.
func Categories(card string) []Category {
	var out []Category
	for _, l := range contentline.ParseAll(card) {
		if l.Name != "CATEGORIES" {
			continue
		}
		for _, raw := range contentline.SplitList(l.Value) {
			name := strings.TrimSpace(contentline.UnescapeText(raw))
			if name == "" {
				continue
			}
			out = append(out, Category{Group: l.Group, Name: name})
		}
	}
	return out
}

// Membership is which groups each of a contact's addresses is in, by address.
type Membership map[string][]string

// StoredMembership reads which groups a contact's cards put each address in.
//
// It is keyed by address rather than by vCard group because the group is a
// name for a position: BuildSigned numbers addresses item1..itemN in the order
// it is given them, so a CATEGORIES property left under item1 through an edit
// that reordered the addresses would be about a different person's address. The
// address is what the membership is about, and it survives the renumbering.
//
// A CATEGORIES property under no group is about every address the cards have,
// which is how a file from another address book usually writes it.
func StoredMembership(cards ...string) Membership {
	joined := strings.Join(cards, "\n")
	byGroup := map[string]string{}
	var all []string
	for _, l := range contentline.ParseAll(joined) {
		if l.Name != "EMAIL" {
			continue
		}
		addr := canonical(l.Value)
		all = append(all, addr)
		if l.Group != "" {
			byGroup[l.Group] = addr
		}
	}
	out := Membership{}
	add := func(addr, name string) {
		for _, have := range out[addr] {
			if have == name {
				return
			}
		}
		out[addr] = append(out[addr], name)
	}
	for _, c := range Categories(joined) {
		if c.Group == "" {
			for _, addr := range all {
				add(addr, c.Name)
			}
			continue
		}
		if addr, ok := byGroup[c.Group]; ok {
			add(addr, c.Name)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// BuildClear renders the clear card for a signed card: one CATEGORIES property
// under each address's group, for the addresses that are in any.
//
// It takes the signed card rather than a list of addresses because the groups
// it writes under have to be the ones the signed card gave the addresses, and
// only the rendered card knows those.
func BuildClear(signed string, groups Membership) string {
	lines := []contentline.Line{
		{Name: "BEGIN", Value: "VCARD"},
		{Name: "VERSION", Value: "4.0"},
	}
	for _, l := range contentline.ParseAll(signed) {
		if line, ok := categoriesFor(l, groups); ok {
			lines = append(lines, line)
		}
	}
	lines = append(lines, contentline.Line{Name: "END", Value: "VCARD"})
	return contentline.Render(lines)
}

// categoriesFor is the CATEGORIES property that belongs beside an EMAIL line,
// when the address is in any group. The names are written in a fixed order so
// that two renderings of one contact are the same bytes.
func categoriesFor(l contentline.Line, groups Membership) (contentline.Line, bool) {
	if l.Name != "EMAIL" || l.Group == "" {
		return contentline.Line{}, false
	}
	names := groups[canonical(l.Value)]
	if len(names) == 0 {
		return contentline.Line{}, false
	}
	sorted := append([]string(nil), names...)
	sort.Strings(sorted)
	escaped := make([]string, len(sorted))
	for i, n := range sorted {
		escaped[i] = contentline.EscapeText(n)
	}
	return contentline.Line{Group: l.Group, Name: "CATEGORIES", Value: strings.Join(escaped, ",")}, true
}

// ── whole documents ──

// Document renders a contact's cards as one vCard, with each address's groups
// written beside it.
//
// Proton stores a contact as several cards - a signed one, an encrypted one, a
// clear one - each a complete vCard carrying a disjoint slice of the
// properties. A file for another address book has to be one card with all of
// them, so the bodies are merged and wrapped once.
//
// Group membership is written from what the caller knows, not from the clear
// card. The clear card is a copy of a fact whose home is the label, and no
// client rewrites it when an address is put in a group or taken out, so it is
// what the contact said when it was last edited rather than what is true. It is
// dropped here and CATEGORIES is written fresh under each address's group, which
// is how Proton's own client writes it and what makes a file read back put the
// address in the same groups.
//
// UID and VERSION appear in every card and must appear once here, so the first
// of each wins and the rest are dropped.
func Document(cards []string, groups Membership) string {
	lines := []contentline.Line{
		{Name: "BEGIN", Value: "VCARD"},
		{Name: "VERSION", Value: "4.0"},
	}
	seen := map[string]bool{"VERSION": true}
	for _, card := range cards {
		for _, l := range contentline.ParseAll(card) {
			if l.Name == "CATEGORIES" {
				continue
			}
			// A property that may only appear once is taken from the first card
			// that has it; everything else may repeat.
			if once[l.Name] {
				if seen[l.Name] {
					continue
				}
				seen[l.Name] = true
			}
			lines = append(lines, l)
			if line, ok := categoriesFor(l, groups); ok {
				lines = append(lines, line)
			}
		}
	}
	lines = append(lines, contentline.Line{Name: "END", Value: "VCARD"})
	return contentline.Render(lines)
}

// once names the properties vCard allows at most one of. Everything else - an
// address, a phone, an email - may legitimately repeat.
var once = map[string]bool{
	"UID": true, "VERSION": true, "FN": true, "N": true, "BDAY": true,
	"ANNIVERSARY": true, "GENDER": true, "PRODID": true, "REV": true, "KIND": true,
}

// HasProperties reports whether a card says anything about a contact.
//
// Every card carries a VERSION of its own, so a card holding nothing else is an
// empty one - and an empty card is not stored: it would be a blob to open on
// every read that says nothing when it opens. This is the only thing that
// decides that, for a card built from a contact's details and for one split out
// of a file alike.
func HasProperties(card string) bool {
	for _, l := range contentline.ParseAll(card) {
		if l.Name != "VERSION" {
			return true
		}
	}
	return false
}

// ParseDocuments splits a .vcf file into the vCards it holds.
//
// A file may carry any number of contacts one after another, so this reads by
// BEGIN/END rather than merging: two cards in a file are two people, which is the
// same distinction ical.ParseCalendar draws between a card and a file.
func ParseDocuments(text string) []string {
	var out []string
	var current []string
	depth := 0
	for _, raw := range contentline.Unfold(text) {
		l, ok := contentline.Parse(raw)
		if !ok {
			continue
		}
		switch {
		case l.Name == "BEGIN" && strings.EqualFold(l.Value, "VCARD"):
			depth++
			if depth == 1 {
				current = []string{raw}
				continue
			}
		case l.Name == "END" && strings.EqualFold(l.Value, "VCARD"):
			depth--
			if depth == 0 {
				current = append(current, raw)
				out = append(out, strings.Join(current, "\r\n"))
				current = nil
				continue
			}
		}
		if depth > 0 {
			current = append(current, raw)
		}
	}
	return out
}

// EnsureIdentity gives a card the two properties Proton requires of every
// contact: a display name and a unique identifier.
//
// A file from another address book often has neither - Google writes N and no
// FN, some tools write no UID at all - and a contact without them is one Proton
// refuses. The name falls back to the structured name, then to the first email,
// so an import does not silently drop the rows that were merely untidy.
func EnsureIdentity(card string) (string, bool) {
	name, uid := Field(card, "FN"), Field(card, "UID")
	if name == "" {
		name = strings.Trim(strings.ReplaceAll(Field(card, "N"), ";", " "), " ")
	}
	if name == "" {
		if emails := Values(card, "EMAIL"); len(emails) > 0 {
			name = emails[0]
		}
	}
	if name == "" {
		return "", false
	}
	if uid != "" && Field(card, "FN") != "" {
		return card, true
	}
	var lines []contentline.Line
	for _, l := range contentline.ParseAll(card) {
		if l.Name == "FN" || l.Name == "UID" || l.Name == "VERSION" {
			continue
		}
		lines = append(lines, l)
	}
	head := []contentline.Line{
		{Name: "BEGIN", Value: "VCARD"},
		{Name: "VERSION", Value: "4.0"},
		{Name: "FN", Value: contentline.EscapeText(name)},
		{Name: "UID", Value: firstNonEmpty(uid, UID())},
	}
	lines = append(head, lines...)
	lines = append(lines, contentline.Line{Name: "END", Value: "VCARD"})
	return contentline.Render(lines), true
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// SplitForStorage divides a whole vCard into the three cards Proton stores it
// as.
//
// The split is Proton's and not the file's. The identity properties - the
// display name, the identifier, the addresses and their key settings - are
// **signed** so that anyone reading the contact can check they were not altered;
// the groups each address is in are **clear**, because the server has to read
// them; everything else is **encrypted**, because a phone number and a note are
// nobody's business. A card's own VERSION belongs to no card and is written
// fresh by each.
//
// A property this tool has no opinion about lands in the encrypted card, which is
// the safe default: an unrecognised property is more likely to be personal than
// to be an identity somebody needs to verify.
//
// All three cards always come back rendered. Whether one is worth storing is
// HasProperties' to say, so that a card built from a file and a card built from
// a contact's details are judged by the same thing.
func SplitForStorage(card string) (signed, encrypted, clear string) {
	signedLines := []contentline.Line{
		{Name: "BEGIN", Value: "VCARD"},
		{Name: "VERSION", Value: "4.0"},
	}
	encryptedLines := append([]contentline.Line(nil), signedLines...)
	clearLines := append([]contentline.Line(nil), signedLines...)

	for _, l := range group(contentline.ParseAll(card)) {
		switch {
		case l.Name == "CATEGORIES":
			clearLines = append(clearLines, l)
		case clearFields[l.Name]:
			// Written fresh by each card, so carried by none.
			continue
		case signedFields[l.Name]:
			signedLines = append(signedLines, l)
		default:
			encryptedLines = append(encryptedLines, l)
		}
	}
	end := contentline.Line{Name: "END", Value: "VCARD"}
	return contentline.Render(append(signedLines, end)),
		contentline.Render(append(encryptedLines, end)),
		contentline.Render(append(clearLines, end))
}

// group gives every address a group of its own.
//
// Proton stores an address's key settings - whether to encrypt to it, which
// scheme, which pinned keys - as properties sharing that address's vCard group,
// so an address with no group has nowhere to hang them and is refused outright.
// A card written by hand, or exported by anything but Proton, ordinarily has
// none.
func group(lines []contentline.Line) []contentline.Line {
	taken := map[string]bool{}
	for _, l := range lines {
		if l.Group != "" {
			taken[l.Group] = true
		}
	}

	next := 1
	free := func() string {
		for {
			name := fmt.Sprintf("item%d", next)
			next++
			if !taken[name] {
				taken[name] = true
				return name
			}
		}
	}

	// A group shared by two addresses is as unusable as no group at all, since
	// the settings under it could not say which address they meant.
	claimed := map[string]bool{}
	out := make([]contentline.Line, len(lines))
	copy(out, lines)
	for i, l := range out {
		if l.Name != "EMAIL" {
			continue
		}
		if l.Group == "" || claimed[l.Group] {
			out[i].Group = free()
		}
		claimed[out[i].Group] = true
	}
	return out
}

// Where a property lives is one question, asked here and nowhere else.
//
// SplitForStorage asks it of a card being stored and ParseEncrypted asks it of
// one being read back, and they have to agree: a property the split sends to the
// signed card and the read files as the encrypted card's own is a property that
// ends up in both, one more copy of it on every edit.
//
// The answer is Proton's. signedFields and clearFields mirror SIGNED_FIELDS and
// CLEAR_FIELDS in its own client, which is what decides where a property is
// readable from; everything else is encrypted, and the Encrypted struct's own
// switch says which of those this tool models rather than merely carries.
// CATEGORIES is the one clear field with something to say, and the one the clear
// card is stored for.
var (
	signedFields = map[string]bool{
		"FN": true, "UID": true, "EMAIL": true,
		"KEY": true, "X-PM-MIMETYPE": true, "X-PM-ENCRYPT": true,
		"X-PM-ENCRYPT-UNTRUSTED": true, "X-PM-SIGN": true,
		"X-PM-SCHEME": true, "X-PM-TLS": true,
	}
	clearFields = map[string]bool{"VERSION": true, "PRODID": true, "CATEGORIES": true}
)
