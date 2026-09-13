package passfile

import (
	"net/url"
	"strings"

	"github.com/roman-16/proton-cli/internal/otp"
	pb "github.com/roman-16/proton-cli/internal/service/pass/proto"
)

// item is what a reader found, before it is turned into the item Pass stores.
//
// One struct carries every kind because the readers fill the same few things
// whatever they are reading, and each builder takes only the fields its kind
// has.
type item struct {
	Name       string
	Note       string
	Fields     []Field
	Trashed    bool
	CreateTime int64
	ModifyTime int64

	Email    string
	Username string
	Password string
	TOTP     string
	URLs     []string
	// Modes are the rules that decide when an address fills, for the managers
	// that keep one per address. An address with no rule of its own fills
	// wherever it matches, which is what Pass does with every other file.
	Modes map[string]pb.AutofillUrl_Mode

	Holder string
	Number string
	CVV    string
	Expiry string
	PIN    string

	PrivateKey string
	PublicKey  string

	SSID     string
	Security pb.WifiSecurity

	Identity *pb.ItemIdentity
	Sections []*pb.CustomSection
	Alias    string
}

// Field is a custom field on an item.
type Field struct {
	Name   string
	Value  string
	Hidden bool
	TOTP   bool
}

// LoginURLs are the addresses a login item carries.
//
// The app keeps them in autofill_urls, each with the rule that decides when it
// fills, and repeats the plain ones in urls for clients too old to know the
// field. An item written by such a client has only urls, so that is what is read
// when the newer field is empty.
func LoginURLs(l *pb.ItemLogin) []string {
	autofill := l.GetAutofillUrls()
	if len(autofill) == 0 {
		return l.GetUrls()
	}
	out := make([]string, 0, len(autofill))
	for _, u := range autofill {
		out = append(out, u.GetUrl())
	}
	return out
}

// SetLoginURLs puts addresses on a login item, writing both fields the app
// writes.
//
// urls is what an older client reads, and it may only hold the addresses that
// fill everywhere, so it carries exactly the ones set here.
func SetLoginURLs(l *pb.ItemLogin, urls []string) {
	autofill := make([]*pb.AutofillUrl, 0, len(urls))
	for _, u := range urls {
		autofill = append(autofill, &pb.AutofillUrl{Url: u, Mode: pb.AutofillUrl_Default})
	}
	l.AutofillUrls = autofill
	l.Urls = urls
}

// defaultModeURLs are the addresses that fill wherever they match, which are the
// only ones an older client may be shown.
func defaultModeURLs(urls []*pb.AutofillUrl) []string {
	out := make([]string, 0, len(urls))
	for _, u := range urls {
		if u.GetMode() == pb.AutofillUrl_Default {
			out = append(out, u.GetUrl())
		}
	}
	return out
}

func (in item) entry(kind string, content *pb.Content) Entry {
	out := &pb.Item{
		Metadata:    &pb.Metadata{Name: in.Name, Note: in.Note},
		Content:     content,
		ExtraFields: extraFields(in.Fields, in.Name),
	}
	return Entry{
		Item: out, Kind: kind, Name: in.Name,
		Trashed:    in.Trashed,
		CreateTime: in.CreateTime, ModifyTime: in.ModifyTime,
		AliasEmail: in.Alias,
	}
}

func (in item) login() Entry {
	urls := cleanURLs(in.URLs)
	in.Name = named(in.Name, hostOf(urls), "Unnamed item")
	login := &pb.ItemLogin{
		ItemEmail:    in.Email,
		ItemUsername: in.Username,
		Password:     in.Password,
		TotpUri:      totpURI(in.TOTP, in.Name),
	}
	SetLoginURLs(login, urls)
	for _, u := range login.AutofillUrls {
		u.Mode = in.Modes[u.Url]
	}
	login.Urls = defaultModeURLs(login.AutofillUrls)
	return in.entry("login", &pb.Content{Content: &pb.Content_Login{Login: login}})
}

func (in item) note() Entry {
	in.Name = named(in.Name, "", "Unnamed note")
	return in.entry("note", &pb.Content{Content: &pb.Content_Note{Note: &pb.ItemNote{}}})
}

func (in item) card() Entry {
	in.Name = named(in.Name, "", "Unnamed credit card")
	return in.entry("credit-card", &pb.Content{Content: &pb.Content_CreditCard{CreditCard: &pb.ItemCreditCard{
		CardholderName:     in.Holder,
		Number:             in.Number,
		VerificationNumber: in.CVV,
		ExpirationDate:     in.Expiry,
		Pin:                in.PIN,
	}}})
}

func (in item) identity() Entry {
	in.Name = named(in.Name, "", "Unnamed identity")
	idn := in.Identity
	if idn == nil {
		idn = &pb.ItemIdentity{}
	}
	return in.entry("identity", &pb.Content{Content: &pb.Content_Identity{Identity: idn}})
}

func (in item) sshKey() Entry {
	in.Name = named(in.Name, "", "Unnamed SSH key")
	return in.entry("ssh-key", &pb.Content{Content: &pb.Content_SshKey{SshKey: &pb.ItemSSHKey{
		PrivateKey: in.PrivateKey, PublicKey: in.PublicKey, Sections: in.Sections,
	}}})
}

func (in item) wifi() Entry {
	in.Name = named(in.Name, "", "Unnamed Wi-Fi network")
	return in.entry("wifi", &pb.Content{Content: &pb.Content_Wifi{Wifi: &pb.ItemWifi{
		Ssid: in.SSID, Password: in.Password, Security: in.Security, Sections: in.Sections,
	}}})
}

func (in item) custom() Entry {
	in.Name = named(in.Name, "", "Unnamed custom item")
	return in.entry("custom", &pb.Content{Content: &pb.Content_Custom{Custom: &pb.ItemCustom{Sections: in.Sections}}})
}

func extraFields(fields []Field, label string) []*pb.ExtraField {
	var out []*pb.ExtraField
	for _, f := range fields {
		if f.Name == "" && f.Value == "" {
			continue
		}
		out = append(out, extraField(f, label))
	}
	return out
}

func extraField(f Field, label string) *pb.ExtraField {
	out := &pb.ExtraField{FieldName: f.Name}
	switch {
	case f.TOTP:
		out.Content = &pb.ExtraField_Totp{Totp: &pb.ExtraTotp{TotpUri: totpURI(f.Value, label)}}
	case f.Hidden:
		out.Content = &pb.ExtraField_Hidden{Hidden: &pb.ExtraHiddenField{Content: f.Value}}
	default:
		out.Content = &pb.ExtraField_Text{Text: &pb.ExtraTextField{Content: f.Value}}
	}
	return out
}

// section gathers fields under a heading, which is where the kinds that have
// sections keep what does not fit their own fields.
func section(name string, fields []Field, label string) []*pb.CustomSection {
	built := extraFields(fields, label)
	if len(built) == 0 {
		return nil
	}
	return []*pb.CustomSection{{SectionName: name, SectionFields: built}}
}

func named(name, fallback, unnamed string) string {
	for _, candidate := range []string{strings.TrimSpace(name), fallback} {
		if candidate != "" {
			return candidate
		}
	}
	return unnamed
}

// identifier splits the one field most managers have into the two Pass keeps.
//
// Pass stores the address and the user name apart, and every other manager
// stores whichever one the site asked for in a single column, so the shape of
// the value is the only thing that can tell them apart.
func identifier(value string) (email, username string) {
	value = strings.TrimSpace(value)
	if isEmail(value) {
		return value, ""
	}
	return "", value
}

func isEmail(value string) bool {
	local, domain, found := strings.Cut(value, "@")
	if !found || local == "" || strings.ContainsAny(value, " \t") {
		return false
	}
	host, _, _ := strings.Cut(domain, ".")
	return host != "" && strings.Contains(domain, ".") && !strings.HasSuffix(domain, ".")
}

func cleanURLs(in []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, raw := range in {
		u := strings.TrimSpace(raw)
		if u == "" || seen[u] {
			continue
		}
		seen[u] = true
		out = append(out, u)
	}
	return out
}

func hostOf(urls []string) string {
	for _, raw := range urls {
		if !strings.Contains(raw, "://") {
			raw = "https://" + raw
		}
		if u, err := url.Parse(raw); err == nil && u.Hostname() != "" {
			return u.Hostname()
		}
	}
	return ""
}

// totpURI is the shape Pass stores a TOTP secret in.
//
// Managers export either a whole otpauth:// URI or the bare secret, and Pass
// takes a URI, so a bare secret is given the defaults every authenticator
// assumes. A secret that is not a secret at all is dropped rather than stored as
// something that will never produce a code.
func totpURI(value, label string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if _, err := otp.Parse(value); err != nil {
		return ""
	}
	if strings.HasPrefix(strings.ToLower(value), "otpauth:") {
		return value
	}
	secret := strings.Map(func(r rune) rune {
		switch r {
		case ' ', '-', '_', '\t', '\n':
			return -1
		}
		return r
	}, value)
	if label == "" {
		label = "Proton Pass"
	}
	q := url.Values{}
	q.Set("secret", strings.ToUpper(secret))
	q.Set("algorithm", "SHA1")
	q.Set("digits", "6")
	q.Set("period", "30")
	return "otpauth://totp/" + url.PathEscape(label) + "?" + q.Encode()
}
