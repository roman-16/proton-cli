package contacts

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	gopenpgp "github.com/ProtonMail/gopenpgp/v2/crypto"
	"github.com/roman-16/proton-cli/internal/account/keys"
	"github.com/roman-16/proton-cli/internal/crypto/pgp"
	"github.com/roman-16/proton-cli/internal/proton"
	"github.com/roman-16/proton-cli/internal/ref"
	"github.com/roman-16/proton-cli/internal/skip"
	"github.com/roman-16/proton-cli/internal/vcard"
)

type Service struct {
	C    proton.Doer
	keys keys.Get
}

func New(c proton.Doer, k keys.Get) *Service { return &Service{C: c, keys: k} }

type Contact struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Email and Phone are the first of each, for a listing that has one column.
	Email string `json:"email,omitempty"`
	Phone string `json:"phone,omitempty"`
	// The repeatable fields carry their kind where one was stated, spelled the
	// way --phone and --address accept it.
	Emails    []string `json:"emails"`
	Phones    []string `json:"phones"`
	Addresses []string `json:"addresses"`
	URLs      []string `json:"urls"`

	FirstName   string `json:"first_name,omitempty"`
	LastName    string `json:"last_name,omitempty"`
	Nickname    string `json:"nickname,omitempty"`
	Org         string `json:"org,omitempty"`
	Note        string `json:"note,omitempty"`
	Title       string `json:"title,omitempty"`
	Role        string `json:"role,omitempty"`
	Birthday    string `json:"birthday,omitempty"`
	Anniversary string `json:"anniversary,omitempty"`
	Gender      string `json:"gender,omitempty"`
	Language    string `json:"language,omitempty"`
	Timezone    string `json:"timezone,omitempty"`

	Cards []string `json:"cards"`

	Signature pgp.VerifyResult `json:"signature,omitempty"`

	// signed and clear are the two cards a reader or writer treats apart from the
	// rest. Pins are read off the signed card alone, because a key anywhere else
	// was vouched for by nobody; the clear card is handed back as it came,
	// because it holds a copy of group membership whose home is the label.
	signed, clear string
}

// EmailAddresses are the contact's email addresses with any kind stripped off.
//
// Emails carries "work:jane@example.com" so a listing reads back into a command,
// but an address is an identity as well as a description: it is what a message is
// sent to, what a group's membership names, and what a pinned key belongs to.
// Those all want the address itself.
func (c Contact) EmailAddresses() []string {
	out := make([]string, 0, len(c.Emails))
	for _, raw := range c.Emails {
		out = append(out, vcard.ParseTyped("EMAIL", raw).Value)
	}
	return out
}

// typedTexts renders stored values the way their flags accept them, so what a
// listing shows can be pasted back.
func typedTexts(values []vcard.Typed) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, v := range values {
		out = append(out, v.Text())
	}
	return out
}

// NewContact is everything a contact can be given. A repeatable field may carry
// a kind - "work:jane@example.com" - which is what Proton's own editor offers on
// each of them.
type NewContact struct {
	Name      string
	Emails    []string
	Phones    []string
	Addresses []string
	URLs      []string

	Note        string
	Org         string
	Title       string
	Role        string
	Birthday    string
	Anniversary string
	Gender      string
	Language    string
	Timezone    string
	Nickname    string
	FirstName   string
	LastName    string
}

// writeKey is the key a contact's cards are written under, and the only one they
// are written under - see keys.Unlocked.PrimaryUserKey.
func (s *Service) writeKey(ctx context.Context) (*gopenpgp.KeyRing, error) {
	u, err := s.keys(ctx)
	if err != nil {
		return nil, err
	}
	return u.PrimaryUserKey()
}

// storedCards are the cards one contact is stored as: the signed card always,
// the encrypted card when it says anything, and the clear card when it does.
//
// Whether a card is worth storing is the same question for a contact being
// created, one being edited and one being read out of a file, so it is asked
// here and of the rendered card rather than of whatever went into it. The clear
// card is the one Proton stores in the open - group membership, as CATEGORIES -
// and goes in as it was handed over, unsigned, which is how Proton's own client
// writes it.
func storedCards(ctx context.Context, kr *gopenpgp.KeyRing, signed, encrypted, clear string) ([]any, error) {
	signedCard, err := pgp.SignCard(signed, kr)
	if err != nil {
		return nil, err
	}
	out, kinds := []any{signedCard}, []string{"signed"}
	if vcard.HasProperties(encrypted) {
		ec, err := pgp.EncryptAndSignCard(encrypted, kr, kr)
		if err != nil {
			return nil, err
		}
		out, kinds = append(out, ec), append(kinds, "encrypted+signed")
	}
	if vcard.HasProperties(clear) {
		out, kinds = append(out, &pgp.Card{Type: pgp.CardClear, Data: clear}), append(kinds, "clear")
	}
	// Which cards a write carried is what tells apart a request Proton refused
	// for one card from one it refused for another, and it is not recoverable
	// from the answer.
	slog.DebugContext(ctx, "contacts: cards written", "cards", strings.Join(kinds, ","))
	return out, nil
}

// encryptedPart lays the new values over what the contact already had, so
// editing one field does not drop the properties this tool has no flag for.
func encryptedPart(nc NewContact, previous vcard.Encrypted) vcard.Encrypted {
	f := previous
	for _, group := range []struct {
		property string
		raw      []string
		into     *[]vcard.Typed
	}{
		{"TEL", nc.Phones, &f.Phones},
		{"ADR", nc.Addresses, &f.Addresses},
		{"URL", nc.URLs, &f.URLs},
	} {
		if group.raw == nil {
			continue
		}
		values := make([]vcard.Typed, 0, len(group.raw))
		for _, raw := range group.raw {
			t := vcard.ParseTyped(group.property, raw)
			if t.Value == "" {
				continue
			}
			values = append(values, t)
		}
		*group.into = values
	}
	for _, kv := range []struct {
		value string
		into  *string
	}{
		{nc.Note, &f.Note}, {nc.Org, &f.Org}, {nc.Title, &f.Title},
		{nc.Role, &f.Role}, {nc.Birthday, &f.Birthday},
		{nc.Anniversary, &f.Anniversary}, {nc.Gender, &f.Gender},
		{nc.Language, &f.Language}, {nc.Timezone, &f.Timezone},
		{nc.Nickname, &f.Nickname}, {nc.FirstName, &f.FirstName},
		{nc.LastName, &f.LastName},
	} {
		if kv.value != "" {
			*kv.into = kv.value
		}
	}
	return f
}

// signedPart builds the signed card for a set of addresses, carrying over the
// pinned keys and crypto settings any previous card held for an address that
// survives. Rebuilding from the addresses alone would silently unpin keys.
func signedPart(name, uid string, emails []string, previous *vcard.Signed) vcard.Signed {
	model := vcard.Signed{Name: name, UID: uid}
	for _, raw := range emails {
		if raw == "" {
			continue
		}
		// An address carries its kind the way every other repeatable value does,
		// as KIND:VALUE, so the kind has to come off before the address is stored
		// or Proton is handed "work:jane@example.com" and refuses it.
		typed := vcard.ParseTyped("EMAIL", raw)
		addr := typed.Value
		e := vcard.SignedEmail{Address: addr, Kind: typed.Kind}
		if previous != nil {
			if prev := previous.FindEmail(addr); prev != nil {
				e.KeyValues, e.Encrypt, e.Sign, e.Scheme = prev.KeyValues, prev.Encrypt, prev.Sign, prev.Scheme
			}
		}
		model.Emails = append(model.Emails, e)
	}
	return model
}

func (s *Service) List(ctx context.Context) ([]Contact, error) {
	var out []Contact
	for page := 0; ; page++ {
		var r struct {
			Contacts []struct {
				ID    string
				Cards []map[string]any
			}
		}
		q := proton.Query("Page", fmt.Sprintf("%d", page), "PageSize", "50")
		// The first page and the keys are asked for together; every page after it
		// finds them already there.
		u, err := s.keys.Alongside(ctx, func(ctx context.Context) error {
			return s.C.Decode(ctx, proton.Request{Method: "GET", Path: "/contacts/v4/contacts/export", Query: q}, &r)
		})
		if err != nil {
			return nil, err
		}
		if len(r.Contacts) == 0 {
			break
		}
		for _, c := range r.Contacts {
			ct, err := openContact(c.ID, c.Cards, u)
			if err != nil {
				skip.Record(ctx, skip.KindContact, c.ID, skip.Undecryptable, err)
				continue
			}
			out = append(out, ct)
		}
		if len(r.Contacts) < 50 {
			break
		}
	}
	return out, nil
}

func (s *Service) Get(ctx context.Context, id string) (*Contact, error) {
	var r struct {
		Contact struct {
			ID    string
			Cards []map[string]any
		}
	}
	u, err := s.keys.Alongside(ctx, func(ctx context.Context) error {
		return s.C.Decode(ctx, proton.Request{Method: "GET", Path: "/contacts/v4/contacts/" + id}, &r)
	})
	if err != nil {
		return nil, err
	}
	c, err := openContact(r.Contact.ID, r.Contact.Cards, u)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// openContact decrypts a contact's cards and reads the contact out of them.
//
// The cards come back in the order Proton sent them, so which text was the
// signed card and which the clear one is known here and nowhere later; both are
// kept apart for the readers and writers that need them by type.
func openContact(id string, raw []map[string]any, u *keys.Unlocked) (Contact, error) {
	cards, verdicts, err := pgp.DecryptCardsRaw(raw, u.UserKR, u.UserKR, nil)
	if err != nil {
		return Contact{}, err
	}
	c := contactFromCards(id, cards)
	c.Cards = cards
	c.Signature = pgp.Aggregate(verdicts...)
	for i, m := range raw {
		switch t, _ := m["Type"].(float64); int(t) {
		case pgp.CardSigned:
			if c.signed == "" {
				c.signed = cards[i]
			}
		case pgp.CardClear:
			if c.clear == "" {
				c.clear = cards[i]
			}
		}
	}
	return c, nil
}

func (s *Service) Resolve(ctx context.Context, r string) (string, error) {
	if ref.Full(r) {
		return r, nil
	}
	contacts, err := s.List(ctx)
	if err != nil {
		return "", err
	}
	needle := strings.ToLower(r)
	var matches []Contact
	for _, c := range contacts {
		match := strings.Contains(strings.ToLower(c.Name), needle)
		for _, e := range c.EmailAddresses() {
			if strings.Contains(strings.ToLower(e), needle) {
				match = true
				break
			}
		}
		if match {
			matches = append(matches, c)
		}
	}
	c, err := ref.Pick("contact", r, matches,
		func(c Contact) string { return c.ID },
		func(c Contact) string { return fmt.Sprintf("%s <%s>", c.Name, c.Email) })
	if err != nil {
		return "", err
	}
	return c.ID, nil
}

func (s *Service) Create(ctx context.Context, nc NewContact) (string, error) {
	if nc.Name == "" && len(nc.Emails) == 0 {
		return "", fmt.Errorf("name or email is required")
	}
	name := nc.Name
	if name == "" {
		name = vcard.ParseTyped("EMAIL", nc.Emails[0]).Value
	}
	kr, err := s.writeKey(ctx)
	if err != nil {
		return "", err
	}
	stored, err := storedCards(ctx, kr,
		vcard.BuildSigned(signedPart(name, vcard.UID(), nc.Emails, nil)),
		vcard.BuildEncrypted(encryptedPart(nc, vcard.Encrypted{})), "")
	if err != nil {
		return "", err
	}
	body := map[string]any{
		"Contacts":  []map[string]any{{"Cards": stored}},
		"Overwrite": 0,
		"Labels":    0,
	}
	var r struct {
		Responses []struct {
			Response struct {
				Code    int
				Error   string
				Contact struct{ ID string }
			}
		}
	}
	if err := s.C.Decode(ctx, proton.Request{Method: "POST", Path: "/contacts/v4/contacts", Body: body}, &r); err != nil {
		return "", err
	}
	// Proton answers this endpoint in batches, so whether the contact was taken is
	// inside a 200 rather than in its status. A card it would not have is an error
	// here, not an empty ID handed back as though it had worked.
	if len(r.Responses) == 0 {
		return "", fmt.Errorf("nothing came back about the contact")
	}
	res := r.Responses[0].Response
	if res.Contact.ID == "" {
		if res.Error != "" {
			return "", fmt.Errorf("%s", res.Error)
		}
		return "", fmt.Errorf("the contact was refused, and no reason was given")
	}
	return res.Contact.ID, nil
}

// Update lays a patch over a contact and writes it back, returning the verdict
// on the card it rewrote for the caller to say.
func (s *Service) Update(ctx context.Context, id string, patch NewContact) (pgp.VerifyResult, error) {
	existing, err := s.Get(ctx, id)
	if err != nil {
		return "", err
	}
	// The patch names only what changes; everything else - including properties
	// this tool has no flag for - is read back off the stored card and written
	// out again, so an edit cannot silently drop what it did not mention.
	joined := strings.Join(existing.Cards, "\n")
	old := vcard.ParseSigned(joined)
	uid := old.UID
	if uid == "" {
		uid = vcard.UID()
	}
	emails := pickSlice(patch.Emails, existing.Emails)
	name := firstNonEmpty(patch.Name, existing.Name)
	if name == "" && len(emails) > 0 {
		name = vcard.ParseTyped("EMAIL", emails[0]).Value
	}
	kr, err := s.writeKey(ctx)
	if err != nil {
		return "", err
	}
	// The clear card is rebuilt by address rather than carried over: the signed
	// card numbers its addresses afresh, and a CATEGORIES left under the old
	// number would be about whichever address holds it now.
	signed := vcard.BuildSigned(signedPart(name, uid, emails, &old))
	stored, err := storedCards(ctx, kr, signed,
		vcard.BuildEncrypted(encryptedPart(patch, vcard.ParseEncrypted(joined))),
		vcard.BuildClear(signed, vcard.StoredMembership(existing.signed, existing.clear)))
	if err != nil {
		return "", err
	}
	return existing.Signature, s.C.Decode(ctx, proton.Request{Method: "PUT", Path: "/contacts/v4/contacts/" + id, Body: map[string]any{"Cards": stored}}, nil)
}

func (s *Service) Delete(ctx context.Context, ids []string) error {
	return s.C.Decode(ctx, proton.Request{Method: "PUT", Path: "/contacts/v4/contacts/delete", Body: map[string]any{"IDs": ids}}, nil)
}

func contactFromCards(id string, cards []string) Contact {
	joined := strings.Join(cards, "\n")
	f := vcard.ParseEncrypted(joined)
	var emails []string
	for _, e := range vcard.ParseSigned(joined).Emails {
		emails = append(emails, e.Text())
	}
	c := Contact{
		ID:          id,
		Name:        vcard.Field(joined, "FN"),
		FirstName:   f.FirstName,
		LastName:    f.LastName,
		Nickname:    f.Nickname,
		Emails:      emails,
		Phones:      typedTexts(f.Phones),
		Addresses:   typedTexts(f.Addresses),
		URLs:        typedTexts(f.URLs),
		Org:         f.Org,
		Note:        f.Note,
		Title:       f.Title,
		Role:        f.Role,
		Birthday:    f.Birthday,
		Anniversary: f.Anniversary,
		Gender:      f.Gender,
		Language:    f.Language,
		Timezone:    f.Timezone,
	}
	if len(emails) > 0 {
		c.Email = emails[0]
	}
	if len(c.Phones) > 0 {
		c.Phone = c.Phones[0]
	}
	return c
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func pickSlice(a, b []string) []string {
	if len(a) > 0 {
		return a
	}
	return b
}

// ── import ──

// ImportOptions is what an import is told beyond the file.
type ImportOptions struct {
	// Groups says whether the groups the file puts addresses in are applied,
	// creating the ones the account does not have.
	Groups bool
	// GroupColor is the accent colour a group created on the way gets.
	GroupColor string
}

// ImportResult says what an import did, per contact, so a partial success names
// what did not land rather than reporting one number that hides it.
type ImportResult struct {
	Imported []string         `json:"imported"`
	Skipped  []SkippedContact `json:"skipped"`
	// Grouped is how many addresses were put in a group, GroupsUsed the groups
	// they went into, and GroupsCreated the ones among those that did not exist
	// before - the one thing about an import worth a second look.
	Grouped       int      `json:"grouped"`
	GroupsUsed    []string `json:"groups_used"`
	GroupsCreated []string `json:"groups_created"`
	// GroupsFailed names the groups whose addresses could not be put in them,
	// and why. The contacts are in the book either way.
	GroupsFailed []GroupFailure `json:"groups_failed"`
}

// GroupFailure is one group an import could not apply, and why.
type GroupFailure struct {
	Name   string `json:"name"`
	Reason string `json:"reason"`
}

func (f GroupFailure) String() string {
	return fmt.Sprintf("The addresses meant for %q were not put in it: %s.", f.Name, f.Reason)
}

// SkippedContact is one card an import could not take, and why.
type SkippedContact struct {
	Name   string `json:"name,omitempty"`
	Reason string `json:"reason"`
}

// String names the card, or says it had no name to be known by, which is itself
// usually the reason it was skipped.
func (s SkippedContact) String() string {
	if s.Name == "" {
		return fmt.Sprintf("Skipped a card: %s.", s.Reason)
	}
	return fmt.Sprintf("Skipped %q: %s.", s.Name, s.Reason)
}

// Import writes vCards into the address book.
//
// Each card goes in whole rather than through the CLI's own field list, so a
// property this tool has no flag for - an anniversary, a photo, a second postal
// address - survives the trip instead of being quietly dropped on the way in.
//
// The split between what is signed, what is encrypted and what is clear is
// Proton's, not the file's: the identity properties are signed so a recipient
// can verify them, the groups are clear so the server can read them, and the
// rest is encrypted. A card that says nothing Proton can identify is skipped
// and named.
//
// The groups a file names are applied the way Proton's own importer applies
// them: a contact is written, the addresses it came back with are put in the
// groups the file gave them, and a group the account does not have is created.
// Membership lives on the label; the CATEGORIES in the clear card is a copy of
// it, so both are written.
func (s *Service) Import(ctx context.Context, documents []string, opts ImportOptions) (*ImportResult, error) {
	// Before anything is reported as skipped: a hierarchy that will not open is
	// the run's failure, not a fault in every card in the file.
	kr, err := s.writeKey(ctx)
	if err != nil {
		return nil, err
	}
	res := &ImportResult{}
	// Proton takes contacts in batches, and its own client caps them well below
	// the request limit because each one is encrypted before it is sent.
	const batch = 10
	type offered struct {
		name   string
		groups vcard.Membership
	}
	pending := make([]map[string]any, 0, batch)
	offers := make([]offered, 0, batch)
	// wanted is which addresses each group is to hold, gathered across every
	// batch so the labelling is one request per group rather than one per
	// contact.
	wanted := map[string][]string{}

	flush := func() error {
		if len(pending) == 0 {
			return nil
		}
		var r struct {
			Responses []struct {
				Response struct {
					Code    int
					Error   string
					Contact struct {
						ID            string
						ContactEmails []struct{ ID, Email string }
					}
				}
			}
		}
		// Overwrite, because a card carries the UID of the contact it is, and a
		// file being read back is the same contact rather than a second one with
		// the same name. This is what makes an export a backup: edit the file,
		// import it, and the address book says what the file says. Proton's own
		// importer sends exactly this, and labels afterwards as this does.
		body := map[string]any{"Contacts": pending, "Overwrite": 1, "Import": 1, "Labels": 0}
		if err := s.C.Decode(ctx, proton.Request{
			Method: "POST", Path: "/contacts/v4/contacts", Body: body,
		}, &r); err != nil {
			return err
		}
		for i, resp := range r.Responses {
			var offer offered
			if i < len(offers) {
				offer = offers[i]
			}
			ct := resp.Response.Contact
			if ct.ID == "" {
				reason := resp.Response.Error
				if reason == "" {
					reason = "Proton did not accept it"
				}
				res.Skipped = append(res.Skipped, SkippedContact{Name: offer.name, Reason: reason})
				continue
			}
			res.Imported = append(res.Imported, ct.ID)
			for _, e := range ct.ContactEmails {
				for _, name := range offer.groups[canonicalEmail(e.Email)] {
					wanted[name] = append(wanted[name], e.ID)
				}
			}
		}
		pending, offers = pending[:0], offers[:0]
		return nil
	}

	for _, raw := range documents {
		card, ok := vcard.EnsureIdentity(raw)
		if !ok {
			res.Skipped = append(res.Skipped, SkippedContact{
				Reason: "no name and no email, so there is nothing to file it under",
			})
			continue
		}
		signed, encrypted, clear := vcard.SplitForStorage(card)
		if !opts.Groups {
			clear = ""
		}
		stored, err := storedCards(ctx, kr, signed, encrypted, clear)
		if err != nil {
			res.Skipped = append(res.Skipped, SkippedContact{
				Name: vcard.Field(card, "FN"), Reason: err.Error(),
			})
			continue
		}
		pending = append(pending, map[string]any{"Cards": stored})
		offers = append(offers, offered{name: vcard.Field(card, "FN"), groups: vcard.StoredMembership(signed, clear)})
		if len(pending) == batch {
			if err := flush(); err != nil {
				return nil, err
			}
		}
	}
	if err := flush(); err != nil {
		return nil, err
	}
	if len(wanted) > 0 {
		s.applyGroups(ctx, wanted, opts.GroupColor, res)
	}
	return res, nil
}

// applyGroups puts the imported addresses in the groups the file gave them.
//
// The contacts are already in the book by the time this runs, so a group that
// cannot be applied is a fact about the result rather than a reason to fail it:
// it is named on the result, and the person can put the addresses in by hand.
func (s *Service) applyGroups(ctx context.Context, wanted map[string][]string, color string, res *ImportResult) {
	names := make([]string, 0, len(wanted))
	for name := range wanted {
		names = append(names, name)
	}
	sort.Strings(names)
	ids, created, err := s.groupsNamed(ctx, names, color)
	res.GroupsCreated = created
	if err != nil {
		for _, name := range names {
			if _, ok := ids[name]; !ok {
				res.GroupsFailed = append(res.GroupsFailed, GroupFailure{Name: name, Reason: err.Error()})
			}
		}
	}
	for _, name := range names {
		id, ok := ids[name]
		if !ok {
			continue
		}
		addresses := dedupe(wanted[name])
		if err := s.GroupAddEmails(ctx, id, addresses); err != nil {
			res.GroupsFailed = append(res.GroupsFailed, GroupFailure{Name: name, Reason: err.Error()})
			continue
		}
		res.Grouped += len(addresses)
		res.GroupsUsed = append(res.GroupsUsed, name)
	}
}

func dedupe(ids []string) []string {
	seen := make(map[string]bool, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

// ── merging duplicates ──

// Duplicate is a set of contacts an address book holds more than one of, and the
// address that says so.
type Duplicate struct {
	// Email is what they have in common. Two contacts sharing an address are the
	// same person; two merely sharing a name are not, which is why the name is
	// not what decides.
	Email string `json:"email"`
	// Contacts are the duplicates, oldest first. The first is kept and the rest
	// are folded into it.
	Contacts []Contact `json:"contacts"`
}

// Duplicates groups an address book by shared address.
//
// A shared **address** is the test, not a shared name: two people are routinely
// called the same thing, and no address book should quietly fold them together.
// Two entries reachable at one address are one person.
//
// Addresses are compared canonically, so the same mailbox written two ways -
// case, or the dots Gmail ignores - is recognised as one.
func Duplicates(all []Contact) []Duplicate {
	byEmail := map[string][]Contact{}
	order := []string{}
	for _, ct := range all {
		seen := map[string]bool{}
		for _, raw := range ct.EmailAddresses() {
			addr := canonicalEmail(raw)
			if addr == "" || seen[addr] {
				continue
			}
			seen[addr] = true
			if _, ok := byEmail[addr]; !ok {
				order = append(order, addr)
			}
			byEmail[addr] = append(byEmail[addr], ct)
		}
	}
	var out []Duplicate
	claimed := map[string]bool{}
	for _, addr := range order {
		group := byEmail[addr]
		if len(group) < 2 {
			continue
		}
		// A contact already folded into an earlier group is not offered again;
		// merging it twice would move it out from under the first merge.
		fresh := make([]Contact, 0, len(group))
		for _, ct := range group {
			if claimed[ct.ID] {
				continue
			}
			fresh = append(fresh, ct)
		}
		if len(fresh) < 2 {
			continue
		}
		for _, ct := range fresh {
			claimed[ct.ID] = true
		}
		out = append(out, Duplicate{Email: addr, Contacts: fresh})
	}
	return out
}

// canonicalEmail reduces an address to what decides whether two of them are the
// same mailbox: case never does, and neither does a kind written in front of it.
func canonicalEmail(raw string) string {
	addr := raw
	if i := strings.Index(addr, ":"); i >= 0 && !strings.Contains(addr[:i], "@") {
		addr = addr[i+1:]
	}
	return strings.ToLower(strings.TrimSpace(addr))
}

// Merge folds a set of duplicates into the first of them and deletes the rest.
//
// The survivor keeps its own identity, so anything referring to it - a group, a
// pinned key - still does afterwards. Everything the others had that it did not
// is added; nothing it already had is overwritten, because the kept contact is
// the one the user chose to keep.
func (s *Service) Merge(ctx context.Context, group Duplicate) (string, error) {
	if len(group.Contacts) < 2 {
		return "", fmt.Errorf("nothing to merge")
	}
	keep := group.Contacts[0]
	merged := mergeCards(group.Contacts)

	old := vcard.ParseSigned(strings.Join(keep.Cards, "\n"))
	uid := old.UID
	if uid == "" {
		uid = vcard.UID()
	}
	kr, err := s.writeKey(ctx)
	if err != nil {
		return "", err
	}
	signed := vcard.BuildSigned(signedPart(firstNonEmpty(keep.Name, merged.name), uid, merged.emails, &old))
	stored, err := storedCards(ctx, kr, signed,
		vcard.BuildEncrypted(merged.encrypted),
		vcard.BuildClear(signed, vcard.StoredMembership(keep.signed, keep.clear)))
	if err != nil {
		return "", err
	}
	if err := s.C.Decode(ctx, proton.Request{
		Method: "PUT", Path: "/contacts/v4/contacts/" + keep.ID,
		Body: map[string]any{"Cards": stored},
	}, nil); err != nil {
		return "", err
	}

	// The survivor is written before the others are removed, so a failure
	// between the two leaves duplicates rather than losing anything.
	var drop []string
	for _, ct := range group.Contacts[1:] {
		drop = append(drop, ct.ID)
	}
	if err := s.Delete(ctx, drop); err != nil {
		return keep.ID, err
	}
	return keep.ID, nil
}

// mergedContact is what a set of duplicates amounts to.
type mergedContact struct {
	name      string
	emails    []string
	encrypted vcard.Encrypted
}

// mergeCards folds a set of contacts together, first one winning every scalar
// and every list keeping the union in the order it was met.
func mergeCards(contacts []Contact) mergedContact {
	var out mergedContact
	seenEmail := map[string]bool{}
	seen := map[string]bool{}

	for _, ct := range contacts {
		joined := strings.Join(ct.Cards, "\n")
		f := vcard.ParseEncrypted(joined)
		if out.name == "" {
			out.name = ct.Name
		}
		for _, addr := range vcard.Values(joined, "EMAIL") {
			key := canonicalEmail(addr)
			if key == "" || seenEmail[key] {
				continue
			}
			seenEmail[key] = true
			out.emails = append(out.emails, addr)
		}
		for _, group := range []struct {
			from []vcard.Typed
			into *[]vcard.Typed
		}{
			{f.Phones, &out.encrypted.Phones},
			{f.Addresses, &out.encrypted.Addresses},
			{f.URLs, &out.encrypted.URLs},
		} {
			for _, v := range group.from {
				key := strings.ToLower(strings.TrimSpace(v.Value))
				if key == "" || seen[key] {
					continue
				}
				seen[key] = true
				*group.into = append(*group.into, v)
			}
		}
		for _, kv := range []struct {
			value string
			into  *string
		}{
			{f.Note, &out.encrypted.Note}, {f.Org, &out.encrypted.Org},
			{f.Title, &out.encrypted.Title}, {f.Role, &out.encrypted.Role},
			{f.Birthday, &out.encrypted.Birthday},
			{f.Anniversary, &out.encrypted.Anniversary},
			{f.Gender, &out.encrypted.Gender}, {f.Language, &out.encrypted.Language},
			{f.Timezone, &out.encrypted.Timezone}, {f.Nickname, &out.encrypted.Nickname},
			{f.FirstName, &out.encrypted.FirstName}, {f.LastName, &out.encrypted.LastName},
		} {
			if *kv.into == "" {
				*kv.into = kv.value
			}
		}
		out.encrypted.Rest = append(out.encrypted.Rest, f.Rest...)
	}
	return out
}
