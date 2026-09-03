package contacts

import (
	"context"
	"fmt"
	"strings"

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

func hasEncryptedFields(nc NewContact) bool {
	if len(nc.Phones) > 0 || len(nc.Addresses) > 0 || len(nc.URLs) > 0 {
		return true
	}
	for _, v := range []string{
		nc.Note, nc.Org, nc.Title, nc.Role, nc.Birthday, nc.Anniversary,
		nc.Gender, nc.Language, nc.Timezone, nc.Nickname, nc.FirstName, nc.LastName,
	} {
		if v != "" {
			return true
		}
	}
	return false
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
			cards, verdicts, err := pgp.DecryptCardsRaw(c.Cards, u.UserKR, u.UserKR, nil)
			if err != nil {
				skip.Record(ctx, skip.KindContact, c.ID, skip.Undecryptable, err)
				continue
			}
			ct := contactFromCards(c.ID, cards)
			ct.Cards = cards
			ct.Signature = pgp.Aggregate(verdicts...)
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
	cards, verdicts, err := pgp.DecryptCardsRaw(r.Contact.Cards, u.UserKR, u.UserKR, nil)
	if err != nil {
		return nil, err
	}
	c := contactFromCards(r.Contact.ID, cards)
	c.Cards = cards
	c.Signature = pgp.Aggregate(verdicts...)
	return &c, nil
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
	u, err := s.keys(ctx)
	if err != nil {
		return "", err
	}
	if nc.Name == "" && len(nc.Emails) == 0 {
		return "", fmt.Errorf("name or email is required")
	}
	name := nc.Name
	if name == "" {
		name = vcard.ParseTyped("EMAIL", nc.Emails[0]).Value
	}
	signed := vcard.BuildSigned(signedPart(name, vcard.UID(), nc.Emails, nil))
	signedCard, err := pgp.SignCard(signed, u.UserKR)
	if err != nil {
		return "", err
	}
	cards := []any{signedCard}
	if hasEncryptedFields(nc) {
		enc := vcard.BuildEncrypted(encryptedPart(nc, vcard.Encrypted{}))
		ec, err := pgp.EncryptAndSignCard(enc, u.UserKR, u.UserKR)
		if err != nil {
			return "", err
		}
		cards = append(cards, ec)
	}
	body := map[string]any{
		"Contacts":  []map[string]any{{"Cards": cards}},
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

func (s *Service) Update(ctx context.Context, id string, patch NewContact) error {
	existing, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	// The patch names only what changes; everything else - including properties
	// this tool has no flag for - is read back off the stored card and written
	// out again, so an edit cannot silently drop what it did not mention.
	joined := strings.Join(existing.Cards, "\n")
	before := vcard.ParseEncrypted(joined)
	merged := NewContact{
		Name:      firstNonEmpty(patch.Name, existing.Name),
		Emails:    pickSlice(patch.Emails, existing.Emails),
		Phones:    patch.Phones,
		Addresses: patch.Addresses,
		URLs:      patch.URLs,
	}
	old := vcard.ParseSigned(joined)
	uid := old.UID
	if uid == "" {
		uid = vcard.UID()
	}
	name := merged.Name
	if name == "" && len(merged.Emails) > 0 {
		name = vcard.ParseTyped("EMAIL", merged.Emails[0]).Value
	}
	u, err := s.keys(ctx)
	if err != nil {
		return err
	}
	signedCard, err := pgp.SignCard(vcard.BuildSigned(signedPart(name, uid, merged.Emails, &old)), u.UserKR)
	if err != nil {
		return err
	}
	after := encryptedPart(patch, before)
	cards := []any{signedCard}
	if !isEmptyEncrypted(after) {
		enc := vcard.BuildEncrypted(after)
		ec, err := pgp.EncryptAndSignCard(enc, u.UserKR, u.UserKR)
		if err != nil {
			return err
		}
		cards = append(cards, ec)
	}
	return s.C.Decode(ctx, proton.Request{Method: "PUT", Path: "/contacts/v4/contacts/" + id, Body: map[string]any{"Cards": cards}}, nil)
}

// isEmptyEncrypted reports whether a contact has nothing worth an encrypted card.
func isEmptyEncrypted(f vcard.Encrypted) bool {
	if len(f.Phones) > 0 || len(f.Addresses) > 0 || len(f.URLs) > 0 || len(f.Rest) > 0 {
		return false
	}
	for _, v := range []string{
		f.Note, f.Org, f.Title, f.Role, f.Birthday, f.Anniversary, f.Gender,
		f.Language, f.Timezone, f.Nickname, f.FirstName, f.LastName,
	} {
		if v != "" {
			return false
		}
	}
	return true
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

// ImportResult says what an import did, per contact, so a partial success names
// what did not land rather than reporting one number that hides it.
type ImportResult struct {
	Imported []string         `json:"imported"`
	Skipped  []SkippedContact `json:"skipped"`
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
// The split between what is signed and what is encrypted is Proton's, not the
// file's: the identity properties are signed so a recipient can verify them, and
// the rest is encrypted. A card that says nothing Proton can identify is skipped
// and named.
func (s *Service) Import(ctx context.Context, cards []string) (*ImportResult, error) {
	u, err := s.keys(ctx)
	if err != nil {
		return nil, err
	}
	res := &ImportResult{}
	// Proton takes contacts in batches, and its own client caps them well below
	// the request limit because each one is encrypted before it is sent.
	const batch = 10
	pending := make([]map[string]any, 0, batch)
	names := make([]string, 0, batch)

	flush := func() error {
		if len(pending) == 0 {
			return nil
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
		// Overwrite, because a card carries the UID of the contact it is, and a
		// file being read back is the same contact rather than a second one with
		// the same name. This is what makes an export a backup: edit the file,
		// import it, and the address book says what the file says. Proton's own
		// importer sends exactly this.
		body := map[string]any{"Contacts": pending, "Overwrite": 1, "Import": 1, "Labels": 0}
		if err := s.C.Decode(ctx, proton.Request{
			Method: "POST", Path: "/contacts/v4/contacts", Body: body,
		}, &r); err != nil {
			return err
		}
		for i, resp := range r.Responses {
			name := ""
			if i < len(names) {
				name = names[i]
			}
			if id := resp.Response.Contact.ID; id != "" {
				res.Imported = append(res.Imported, id)
				continue
			}
			reason := resp.Response.Error
			if reason == "" {
				reason = "Proton did not accept it"
			}
			res.Skipped = append(res.Skipped, SkippedContact{Name: name, Reason: reason})
		}
		pending, names = pending[:0], names[:0]
		return nil
	}

	for _, raw := range cards {
		card, ok := vcard.EnsureIdentity(raw)
		if !ok {
			res.Skipped = append(res.Skipped, SkippedContact{
				Reason: "no name and no email, so there is nothing to file it under",
			})
			continue
		}
		signed, encrypted := vcard.SplitForStorage(card)
		signedCard, err := pgp.SignCard(signed, u.UserKR)
		if err != nil {
			res.Skipped = append(res.Skipped, SkippedContact{
				Name: vcard.Field(card, "FN"), Reason: err.Error(),
			})
			continue
		}
		out := []any{signedCard}
		if encrypted != "" {
			ec, err := pgp.EncryptAndSignCard(encrypted, u.UserKR, u.UserKR)
			if err != nil {
				res.Skipped = append(res.Skipped, SkippedContact{
					Name: vcard.Field(card, "FN"), Reason: err.Error(),
				})
				continue
			}
			out = append(out, ec)
		}
		pending = append(pending, map[string]any{"Cards": out})
		names = append(names, vcard.Field(card, "FN"))
		if len(pending) == batch {
			if err := flush(); err != nil {
				return nil, err
			}
		}
	}
	if err := flush(); err != nil {
		return nil, err
	}
	return res, nil
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

	u, err := s.keys(ctx)
	if err != nil {
		return "", err
	}
	old := vcard.ParseSigned(strings.Join(keep.Cards, "\n"))
	uid := old.UID
	if uid == "" {
		uid = vcard.UID()
	}
	name := firstNonEmpty(keep.Name, merged.name)
	signedCard, err := pgp.SignCard(
		vcard.BuildSigned(signedPart(name, uid, merged.emails, &old)), u.UserKR)
	if err != nil {
		return "", err
	}
	cards := []any{signedCard}
	if !isEmptyEncrypted(merged.encrypted) {
		ec, err := pgp.EncryptAndSignCard(vcard.BuildEncrypted(merged.encrypted), u.UserKR, u.UserKR)
		if err != nil {
			return "", err
		}
		cards = append(cards, ec)
	}
	if err := s.C.Decode(ctx, proton.Request{
		Method: "PUT", Path: "/contacts/v4/contacts/" + keep.ID,
		Body: map[string]any{"Cards": cards},
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
