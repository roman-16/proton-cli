package calendar

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"sync"
	"time"

	pgp "github.com/ProtonMail/gopenpgp/v2/crypto"
	"github.com/roman-16/proton-cli/internal/account/keys"
	pgphelper "github.com/roman-16/proton-cli/internal/crypto/pgp"
	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/roman-16/proton-cli/internal/fetch"
	"github.com/roman-16/proton-cli/internal/ical"
	"github.com/roman-16/proton-cli/internal/proton"
	"github.com/roman-16/proton-cli/internal/skip"
)

// PrimaryZone is the zone the account's calendar is drawn in, which is the zone
// Proton's own clients write new events against.
//
// It is the last resort behind everything this machine can say about its own
// zone, so it is asked for at most once and only when the rest of the chain came
// up empty - a Windows or container install still gets a real anchor rather than
// a bare UTC instant.
func (s *Service) PrimaryZone(ctx context.Context) string {
	s.zoneOnce.Do(func() {
		var r struct {
			CalendarUserSettings struct{ PrimaryTimezone string }
		}
		if err := s.C.Decode(ctx, proton.Request{Method: "GET", Path: "/settings/calendar"}, &r); err != nil {
			return
		}
		if name := r.CalendarUserSettings.PrimaryTimezone; name != "" {
			if _, err := time.LoadLocation(name); err == nil {
				s.zone = name
			}
		}
	})
	return s.zone
}

// zoneCache is embedded in Service.
type zoneCache struct {
	zoneOnce sync.Once
	zone     string
}

type Service struct {
	C         proton.Doer
	keys      keys.Get
	canonical map[string]canonicalAddr
	zoneCache

	// What one invocation asks Proton for, asked for once. A calendar's own record,
	// its unlocked keys and the list of calendars are each wanted by several steps
	// of a single command.
	bootstraps fetch.Memo[*bootstrap]
	unlocked   fetch.Memo[*calKeys]
	calendars  fetch.Memo[[]Calendar]
}

func New(c proton.Doer, k keys.Get) *Service { return &Service{C: c, keys: k} }

// member is one account's membership of one calendar. Proton keeps the display
// name, colour and description here rather than on the calendar, because they are
// each member's own.
type member struct {
	ID          string
	CalendarID  string
	Email       string
	AddressID   string
	Name        string
	Color       string
	Description string
}

// bootstrap is everything needed to open a calendar: the membership that names
// it, the passphrase that unlocks its keys, and the keys.
//
// One request answers all of it, which is how the web client opens a calendar
// (getFullCalendar, packages/shared/lib/api/calendars.ts).
type bootstrap struct {
	Keys       []struct{ PrivateKey string }
	Passphrase struct {
		MemberPassphrases []struct {
			MemberID, Passphrase, Signature string
		}
	}
	Members []member
}

func (s *Service) calendarBootstrap(ctx context.Context, calendarID string) (*bootstrap, error) {
	return s.bootstraps.Do(calendarID, func() (*bootstrap, error) {
		var b bootstrap
		if err := s.C.Decode(ctx, proton.Request{
			Method: "GET", Path: "/calendar/v2/" + calendarID + "/bootstrap",
		}, &b); err != nil {
			// The only thing the request named was the calendar, so a server that
			// does not recognise it is answering about the reference, and the answer
			// reads the same whether the reference was a name or an ID.
			if proton.DoesNotExist(err) {
				return nil, &errs.NotFound{Kind: "calendar", Ref: calendarID}
			}
			return nil, err
		}
		return &b, nil
	})
}

// ourMember is the membership this account holds, which is the one whose address
// key we can open.
//
// Proton reports a calendar's members as the ones belonging to whoever asked, so
// there is normally one; matching it to an address of ours is what the web client
// does too (getMemberAndAddress, packages/shared/lib/calendar/members.ts).
func ourMember(members []member, u *keys.Unlocked) (member, *pgp.KeyRing, bool) {
	for _, m := range members {
		if kr, ok := u.AddrKR(m.AddressID); ok {
			return m, kr, true
		}
	}
	return member{}, nil, false
}

// addressKeyRing is the key ring for one of our own addresses, when we hold it.
// An event invited to an address whose keys will not open is left encrypted
// rather than refused, which is what the caller's readErr already says.
func (s *Service) addressKeyRing(ctx context.Context, addressID string) (*pgp.KeyRing, bool) {
	u, err := s.keys(ctx)
	if err != nil {
		return nil, false
	}
	return u.AddrKR(addressID)
}

type calKeys struct {
	calKR    *pgp.KeyRing
	addrKR   *pgp.KeyRing
	memberID string
	email    string
	// addressID is the address this membership belongs to. Sharing names it, so
	// Proton knows which of your addresses is doing the sharing.
	addressID string
	// passphraseKey is the session key of the passphrase that opens this
	// calendar. Giving somebody else access means handing them this key,
	// encrypted to theirs - so it is kept rather than discarded once the
	// passphrase itself is in hand.
	passphraseKey *pgp.SessionKey
}

// unlockCalendar opens a calendar's keys.
func (s *Service) unlockCalendar(ctx context.Context, calendarID string) (*calKeys, error) {
	return s.unlocked.Do(calendarID, func() (*calKeys, error) {
		var b *bootstrap
		u, err := s.keys.Alongside(ctx, func(ctx context.Context) error {
			var err error
			b, err = s.calendarBootstrap(ctx, calendarID)
			return err
		})
		if err != nil {
			return nil, err
		}
		me, addrKR, ok := ourMember(b.Members, u)
		if !ok {
			return nil, fmt.Errorf("no matching address key for calendar %s", calendarID)
		}

		var calPass []byte
		var passphraseKey *pgp.SessionKey
		for _, mp := range b.Passphrase.MemberPassphrases {
			if mp.MemberID != me.ID {
				continue
			}
			msg, err := pgp.NewPGPMessageFromArmored(mp.Passphrase)
			if err != nil {
				return nil, err
			}
			sig, err := pgp.NewPGPSignatureFromArmored(mp.Signature)
			if err != nil {
				return nil, err
			}
			dec, err := addrKR.Decrypt(msg, nil, pgp.GetUnixTime())
			if err != nil {
				return nil, fmt.Errorf("decrypt calendar passphrase: %w", err)
			}
			if err := addrKR.VerifyDetached(dec, sig, pgp.GetUnixTime()); err != nil {
				return nil, err
			}
			calPass = dec.GetBinary()
			// The session key is what a new member is given, so it is taken here
			// where the passphrase is already being opened rather than by
			// decrypting the same message a second time later.
			if split, err := msg.SplitMessage(); err == nil {
				passphraseKey, _ = addrKR.DecryptSessionKey(split.GetBinaryKeyPacket())
			}
			break
		}
		if calPass == nil {
			return nil, fmt.Errorf("no passphrase found for member %s", me.ID)
		}

		calKR, err := pgp.NewKeyRing(nil)
		if err != nil {
			return nil, err
		}
		for _, k := range b.Keys {
			locked, err := pgp.NewKeyFromArmored(k.PrivateKey)
			if err != nil {
				skip.Record(ctx, skip.KindKey, calendarID, skip.Malformed, err)
				continue
			}
			unlocked, err := locked.Unlock(calPass)
			if err != nil {
				skip.Record(ctx, skip.KindKey, calendarID, skip.Unlockable, err)
				continue
			}
			_ = calKR.AddKey(unlocked)
		}
		if calKR.CountEntities() == 0 {
			return nil, fmt.Errorf("none of this calendar's %d keys could be unlocked", len(b.Keys))
		}
		return &calKeys{
			calKR: calKR, addrKR: addrKR, memberID: me.ID, email: me.Email,
			addressID: me.AddressID, passphraseKey: passphraseKey,
		}, nil
	})
}

// decryptEvent decrypts an event's cards and parses them into one model.
//
// The session key is wrapped to decryptionKR - the calendar key for an event you
// own, the invited address key for an invitation you received - via keyPacket;
// signatures are checked against verificationKR.
//
// A failure to decrypt is returned rather than folded into empty fields. Reading
// an event that cannot be read should say so, and writing one back must be
// impossible: an update built from blanks would sign those blanks over the real
// title, location, description, rule and exclusions.
func decryptEvent(cards []map[string]any, keyPacket string, decryptionKR, verificationKR *pgp.KeyRing) (ical.VEvent, pgphelper.VerifyResult, error) {
	kp, err := base64.StdEncoding.DecodeString(keyPacket)
	if err != nil {
		return ical.VEvent{}, pgphelper.Unverified, fmt.Errorf("decode event key packet: %w", err)
	}
	decrypted, verdicts, err := pgphelper.DecryptCardsRaw(cards, decryptionKR, verificationKR, kp)
	if err != nil {
		return ical.VEvent{}, pgphelper.Unverified, fmt.Errorf("decrypt event content: %w", err)
	}
	v, err := ical.Parse(strings.Join(decrypted, "\r\n"))
	if err != nil {
		return ical.VEvent{}, pgphelper.Unverified, fmt.Errorf("read event content: %w", err)
	}
	return v, pgphelper.Aggregate(verdicts...), nil
}

// defaultDays is how many days a listing covers when it is not told which.
const defaultDays = 30

// DefaultDays are the first and last day a listing covers when it is not told
// which: today, and the rest of the month ahead.
func DefaultDays() (first, last time.Time) {
	now := time.Now()
	first = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	return first, first.AddDate(0, 0, defaultDays-1)
}
