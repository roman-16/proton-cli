package calendar

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"log/slog"
	"net/url"
	"sort"

	pgp "github.com/ProtonMail/gopenpgp/v2/crypto"

	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/roman-16/proton-cli/internal/proton"
	"github.com/roman-16/proton-cli/internal/skip"
)

// Publishing a calendar as a link anybody can follow.
//
// A link is opened with two keys and Proton keeps neither of them in the clear:
// a cache key sealed to the calendar's own keys, and - for a link that shows
// details - the calendar's passphrase masked with a key made for the link. The
// URL carries both, so nothing has to be remembered for one to be handed out
// again, and a link mislaid is rebuilt rather than revoked.
//
// The two levels differ in what the account gives away. A limited link answers
// only whether its owner is busy, which Proton can serve from what it already
// knows. A full one carries the key that opens the calendar in the query
// string, where Proton reads it to build the feed - so publishing one hands
// Proton what it is otherwise never given.

// What a link shows the person who follows it.
const (
	AccessLimited = "limited"
	AccessFull    = "full"
)

// Proton's own numbering of the two.
const (
	levelLimited = 0
	levelFull    = 1
)

// LabelLimit is how long a link's label may be. What is stored is the encrypted
// form, which the endpoint bounds, so the bound is kept on the way in.
const LabelLimit = 500

// linkHost is where a published calendar is opened, and it is Proton's own host
// whatever this build talks to: what follows the link is somebody else's
// calendar app, which knows nothing of another environment.
const linkHost = "https://calendar.proton.me"

// Link is one published link as a listing shows it.
//
// It carries no URL. Every URL opens the calendar for whoever holds it, so a
// listing that printed them would put a row of working keys on the screen;
// rebuilding one is what LinkOpen is for.
type Link struct {
	ID         string `json:"id"`
	CalendarID string `json:"calendar_id"`
	Calendar   string `json:"calendar"`
	// Access is what a follower sees: free/busy alone, or every detail.
	Access string `json:"access"`
	// Name is the label, which only this account ever sees.
	Name    string `json:"name,omitempty"`
	Created int64  `json:"created"`
}

// LinkURL is a link with the address it is opened at.
type LinkURL struct {
	Link
	URL string `json:"url"`
}

// NewLink is what a link is published with.
type NewLink struct {
	// Full publishes every detail of every event rather than free/busy alone.
	Full bool
	// Name is the label this account will know the link by.
	Name string
}

// rawLink is one link as Proton keeps it.
type rawLink struct {
	CalendarUrlID       string
	CalendarID          string
	AccessLevel         int
	EncryptedPurpose    string
	EncryptedCacheKey   string
	EncryptedPassphrase string
	CreateTime          int64
}

// Links reads the links published on one calendar.
func (s *Service) Links(ctx context.Context, cal Calendar) ([]Link, error) {
	ck, err := s.unlockCalendar(ctx, cal.ID)
	if err != nil {
		return nil, err
	}
	raws, err := s.rawLinks(ctx, cal.ID)
	if err != nil {
		return nil, err
	}
	out := make([]Link, 0, len(raws))
	for _, raw := range raws {
		out = append(out, raw.link(ctx, cal.Name, ck))
	}
	sortLinks(out)
	return out, nil
}

// LinksAll reads every link on every calendar that can carry one, which is a
// personal calendar of your own.
//
// A calendar that will not open costs its own links and not the answer: the
// links on the others are still worth showing, and the record is what says the
// listing is short.
func (s *Service) LinksAll(ctx context.Context) ([]Link, error) {
	cals, err := s.CalendarsList(ctx)
	if err != nil {
		return nil, err
	}
	var out []Link
	for _, cal := range cals {
		if cal.Shareable() != nil {
			continue
		}
		links, err := s.Links(ctx, cal)
		if err != nil {
			skip.Record(ctx, skip.KindCalendar, cal.ID, skip.Unreadable, err)
			continue
		}
		out = append(out, links...)
	}
	sortLinks(out)
	return out, nil
}

// LinkCreate publishes a calendar and returns the link it made.
func (s *Service) LinkCreate(ctx context.Context, cal Calendar, n NewLink) (*LinkURL, error) {
	ck, err := s.unlockCalendar(ctx, cal.ID)
	if err != nil {
		return nil, err
	}
	cacheKey, salt, hash, err := newCacheKey()
	if err != nil {
		return nil, err
	}
	sealedCacheKey, err := sealToCalendar(ck, cacheKey)
	if err != nil {
		return nil, fmt.Errorf("seal the key that opens the link: %w", err)
	}

	body := map[string]any{
		"AccessLevel":         levelLimited,
		"CacheKeySalt":        salt,
		"CacheKeyHash":        hash,
		"EncryptedCacheKey":   sealedCacheKey,
		"EncryptedPassphrase": nil,
		"EncryptedPurpose":    nil,
		"PassphraseID":        nil,
	}
	var passphraseKey []byte
	if n.Full {
		sk, err := pgp.GenerateSessionKey()
		if err != nil {
			return nil, err
		}
		masked, err := s.maskPassphrase(ck, sk.Key)
		if err != nil {
			return nil, err
		}
		passphraseKey = sk.Key
		body["AccessLevel"] = levelFull
		body["EncryptedPassphrase"] = base64.StdEncoding.EncodeToString(masked)
		body["PassphraseID"] = ck.passphraseID
	}
	if n.Name != "" {
		sealedName, err := sealToCalendar(ck, n.Name)
		if err != nil {
			return nil, fmt.Errorf("seal the link's label: %w", err)
		}
		body["EncryptedPurpose"] = sealedName
	}

	var r struct{ CalendarUrl rawLink }
	if err := s.C.Decode(ctx, proton.Request{
		Method: "POST", Path: "/calendar/v1/" + cal.ID + "/urls", Body: body,
	}, &r); err != nil {
		return nil, err
	}
	made := r.CalendarUrl.link(ctx, cal.Name, ck)
	// The name is taken from what was asked for rather than from the answer:
	// Proton echoes the sealed form back, and opening it again to read a string
	// already in hand would only add a way to disagree with it.
	made.Name = n.Name
	return &LinkURL{Link: made, URL: linkURL(made.ID, cacheKey, passphraseKey)}, nil
}

// LinkOpen rebuilds the address a link is opened at.
func (s *Service) LinkOpen(ctx context.Context, l Link) (*LinkURL, error) {
	ck, err := s.unlockCalendar(ctx, l.CalendarID)
	if err != nil {
		return nil, err
	}
	raws, err := s.rawLinks(ctx, l.CalendarID)
	if err != nil {
		return nil, err
	}
	for _, raw := range raws {
		if raw.CalendarUrlID != l.ID {
			continue
		}
		cacheKey, err := openCacheKey(ck, raw)
		if err != nil {
			return nil, err
		}
		var passphraseKey []byte
		if raw.EncryptedPassphrase != "" {
			if passphraseKey, err = s.unmaskPassphrase(ck, raw.EncryptedPassphrase); err != nil {
				return nil, err
			}
		}
		return &LinkURL{
			Link: raw.link(ctx, l.Calendar, ck),
			URL:  linkURL(raw.CalendarUrlID, cacheKey, passphraseKey),
		}, nil
	}
	return nil, &errs.NotFound{Kind: "link", Ref: l.ID}
}

// LinkRename changes the label a link is known by here. An empty name takes the
// label off.
func (s *Service) LinkRename(ctx context.Context, l Link, name string) error {
	ck, err := s.unlockCalendar(ctx, l.CalendarID)
	if err != nil {
		return err
	}
	var purpose any
	if name != "" {
		sealed, err := sealToCalendar(ck, name)
		if err != nil {
			return fmt.Errorf("seal the link's label: %w", err)
		}
		purpose = sealed
	}
	return s.C.Decode(ctx, proton.Request{
		Method: "PUT", Path: "/calendar/v1/" + l.CalendarID + "/urls/" + l.ID,
		Body: map[string]any{"EncryptedPurpose": purpose},
	}, nil)
}

// LinkRevoke stops a link working. The calendar is untouched.
func (s *Service) LinkRevoke(ctx context.Context, l Link) error {
	return s.C.Decode(ctx, proton.Request{
		Method: "DELETE", Path: "/calendar/v1/" + l.CalendarID + "/urls/" + l.ID,
	}, nil)
}

func (s *Service) rawLinks(ctx context.Context, calendarID string) ([]rawLink, error) {
	var r struct{ CalendarUrls []rawLink }
	if err := s.C.Decode(ctx, proton.Request{
		Method: "GET", Path: "/calendar/v1/" + calendarID + "/urls",
	}, &r); err != nil {
		return nil, err
	}
	return r.CalendarUrls, nil
}

// link is one row of a listing, with the label opened.
func (raw rawLink) link(ctx context.Context, calendar string, ck *calKeys) Link {
	access := AccessLimited
	if raw.AccessLevel == levelFull {
		access = AccessFull
	}
	return Link{
		ID: raw.CalendarUrlID, CalendarID: raw.CalendarID, Calendar: calendar,
		Access: access, Name: raw.label(ctx, ck), Created: raw.CreateTime,
	}
}

// label opens the name this account gave a link.
func (raw rawLink) label(ctx context.Context, ck *calKeys) string {
	if raw.EncryptedPurpose == "" {
		return ""
	}
	msg, err := pgp.NewPGPMessageFromArmored(raw.EncryptedPurpose)
	if err == nil {
		var opened *pgp.PlainMessage
		if opened, err = ck.calKR.Decrypt(msg, nil, pgp.GetUnixTime()); err == nil {
			return opened.GetString()
		}
	}
	// Recorded and not counted: the link is on the screen and works, and what
	// would not open is a label nobody but this account ever sees. Counting it
	// would report a listing short that is holding everything it has.
	slog.DebugContext(ctx, "a calendar link's label would not open",
		"link", raw.CalendarUrlID, "error", err)
	return ""
}

func sortLinks(links []Link) {
	sort.SliceStable(links, func(i, j int) bool {
		if links[i].Calendar != links[j].Calendar {
			return links[i].Calendar < links[j].Calendar
		}
		return links[i].Created < links[j].Created
	})
}

// sealToCalendar encrypts to the calendar's keys, which is what reads a link's
// own keys and its label back.
func sealToCalendar(ck *calKeys, text string) (string, error) {
	msg, err := ck.calKR.Encrypt(pgp.NewPlainMessageFromString(text), nil)
	if err != nil {
		return "", err
	}
	return msg.GetArmored()
}

func openCacheKey(ck *calKeys, raw rawLink) (string, error) {
	msg, err := pgp.NewPGPMessageFromArmored(raw.EncryptedCacheKey)
	if err != nil {
		return "", err
	}
	opened, err := ck.calKR.Decrypt(msg, nil, pgp.GetUnixTime())
	if err != nil {
		return "", fmt.Errorf("open the key that opens the link: %w", err)
	}
	return opened.GetString(), nil
}

// maskPassphrase hides the calendar's passphrase behind a key made for one
// link, which is what Proton stores and what the URL undoes.
func (s *Service) maskPassphrase(ck *calKeys, key []byte) ([]byte, error) {
	passphrase, err := ck.rawPassphrase()
	if err != nil {
		return nil, err
	}
	return mask(key, passphrase)
}

// unmaskPassphrase takes the link's key back out of what Proton stores, which
// is how a URL is rebuilt from an account alone.
func (s *Service) unmaskPassphrase(ck *calKeys, stored string) ([]byte, error) {
	masked, err := base64.StdEncoding.DecodeString(stored)
	if err != nil {
		return nil, fmt.Errorf("read the link's stored passphrase: %w", err)
	}
	passphrase, err := ck.rawPassphrase()
	if err != nil {
		return nil, err
	}
	return mask(masked, passphrase)
}

// rawPassphrase is the bytes behind the calendar's passphrase, which is what
// the mask is the same length as.
func (ck *calKeys) rawPassphrase() ([]byte, error) {
	raw, err := base64.StdEncoding.DecodeString(string(ck.passphrase))
	if err != nil {
		return nil, fmt.Errorf("read the calendar's passphrase: %w", err)
	}
	return raw, nil
}

// mask is the reversible combination of a key and a passphrase of one length.
func mask(key, passphrase []byte) ([]byte, error) {
	if len(key) != len(passphrase) {
		return nil, fmt.Errorf("a link's key is %d bytes and the passphrase is %d",
			len(key), len(passphrase))
	}
	out := make([]byte, len(key))
	for i := range key {
		out[i] = key[i] ^ passphrase[i]
	}
	return out, nil
}

// newCacheKey makes the key a link is followed with, the salt it is stored
// under, and the hash Proton checks an arriving key against.
func newCacheKey() (key, salt, hash string, err error) {
	raw, err := randomBytes(16)
	if err != nil {
		return "", "", "", err
	}
	rawSalt, err := randomBytes(8)
	if err != nil {
		return "", "", "", err
	}
	key = base64.URLEncoding.EncodeToString(raw)
	salt = base64.StdEncoding.EncodeToString(rawSalt)
	sum := sha256.Sum256([]byte(salt + key))
	return key, salt, base64.StdEncoding.EncodeToString(sum[:]), nil
}

func randomBytes(n int) ([]byte, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	return b, nil
}

// linkURL is the address a link is opened at, keys and all.
func linkURL(urlID, cacheKey string, passphraseKey []byte) string {
	address := linkHost + "/api/calendar/v1/url/" + urlID + "/calendar.ics" +
		"?CacheKey=" + url.QueryEscape(cacheKey)
	if len(passphraseKey) == 0 {
		return address
	}
	return address + "&PassphraseKey=" +
		url.QueryEscape(base64.URLEncoding.EncodeToString(passphraseKey))
}
