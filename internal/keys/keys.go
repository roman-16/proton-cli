// Package keys unlocks the Proton user/address/key hierarchy for a session.
package keys

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/url"
	"strings"

	"github.com/ProtonMail/go-srp"
	pgp "github.com/ProtonMail/gopenpgp/v2/crypto"
	"github.com/roman-16/proton-cli/internal/api"
	"github.com/roman-16/proton-cli/internal/session"
)

// Unlocked holds the unlocked key hierarchy.
type Unlocked struct {
	UserKR    *pgp.KeyRing
	AddrKRs   map[string]*pgp.KeyRing
	Addresses []Address
}

type User struct {
	ID   string
	Name string
	Keys []Key
}

type Address struct {
	ID    string
	Email string
	Keys  []Key
}

type Key struct {
	ID         string
	PrivateKey string
	Token      string
	Signature  string
	Primary    int
	Active     int
}

type salt struct {
	ID      string
	KeySalt string
}

// Unlock fetches user/address keys and unlocks them using either the cached
// salted key password on the client, or the provided password if none is
// cached.
func Unlock(ctx context.Context, c *api.Client, password string) (*Unlocked, error) {
	skp := c.SaltedKeyPass()
	if skp == "" {
		if password == "" {
			return nil, fmt.Errorf("password required for encrypted operations;\nset PROTON_PASSWORD, --password, or configure a profile")
		}
		d, err := deriveSaltedKeyPass(ctx, c, password)
		if err != nil {
			return nil, fmt.Errorf("derive key password: %w", err)
		}
		skp = d
		c.SetSaltedKeyPass(skp)
		_ = session.Save(c.Profile(), c.Session())
	}

	user, err := getUser(ctx, c)
	if err != nil {
		return nil, fmt.Errorf("get user: %w", err)
	}
	userKR, err := unlockKeyRing(user.Keys, []byte(skp), nil)
	if err != nil {
		return nil, fmt.Errorf("unlock user keys: %w", err)
	}

	addrs, err := getAddresses(ctx, c)
	if err != nil {
		return nil, fmt.Errorf("get addresses: %w", err)
	}
	addrKRs := map[string]*pgp.KeyRing{}
	for _, a := range addrs {
		if kr, err := unlockKeyRing(a.Keys, []byte(skp), userKR); err == nil {
			addrKRs[a.ID] = kr
		}
	}
	if len(addrKRs) == 0 {
		return nil, fmt.Errorf("failed to unlock any address keys")
	}
	return &Unlocked{UserKR: userKR, AddrKRs: addrKRs, Addresses: addrs}, nil
}

// PrimaryAddrKR returns the key ring for the user's primary proton.me/pm.me
// address, falling back to the first unlockable address.
func (u *Unlocked) PrimaryAddrKR() (*pgp.KeyRing, string, string, error) {
	for _, a := range u.Addresses {
		if kr, ok := u.AddrKRs[a.ID]; ok {
			e := a.Email
			if strings.HasSuffix(e, "@proton.me") || strings.HasSuffix(e, "@pm.me") || strings.HasSuffix(e, "@protonmail.com") {
				return kr, a.ID, a.Email, nil
			}
		}
	}
	return u.FirstAddrKR()
}

// FirstAddrKR returns the first unlockable address key ring.
func (u *Unlocked) FirstAddrKR() (*pgp.KeyRing, string, string, error) {
	for _, a := range u.Addresses {
		if kr, ok := u.AddrKRs[a.ID]; ok {
			return kr, a.ID, a.Email, nil
		}
	}
	return nil, "", "", fmt.Errorf("no address key rings available")
}

// AddrKR returns the key ring for the given address ID.
func (u *Unlocked) AddrKR(addrID string) (*pgp.KeyRing, bool) {
	kr, ok := u.AddrKRs[addrID]
	return kr, ok
}

func deriveSaltedKeyPass(ctx context.Context, c *api.Client, password string) (string, error) {
	var r struct{ KeySalts []salt }
	if err := c.Send(ctx, api.Request{Method: "GET", Path: "/core/v4/keys/salts"}, &r); err != nil {
		return "", err
	}
	if len(r.KeySalts) == 0 {
		return "", fmt.Errorf("no key salts returned")
	}
	ks, err := base64.StdEncoding.DecodeString(r.KeySalts[0].KeySalt)
	if err != nil {
		return "", err
	}
	sp, err := srp.MailboxPassword([]byte(password), ks)
	if err != nil {
		return "", err
	}
	return string(sp[len(sp)-31:]), nil
}

func getUser(ctx context.Context, c *api.Client) (*User, error) {
	var r struct{ User User }
	if err := c.Send(ctx, api.Request{Method: "GET", Path: "/core/v4/users"}, &r); err != nil {
		return nil, err
	}
	return &r.User, nil
}

func getAddresses(ctx context.Context, c *api.Client) ([]Address, error) {
	var r struct{ Addresses []Address }
	if err := c.Send(ctx, api.Request{Method: "GET", Path: "/core/v4/addresses"}, &r); err != nil {
		return nil, err
	}
	return r.Addresses, nil
}

func unlockKeyRing(keys []Key, passphrase []byte, userKR *pgp.KeyRing) (*pgp.KeyRing, error) {
	kr, err := pgp.NewKeyRing(nil)
	if err != nil {
		return nil, err
	}
	for _, k := range keys {
		if k.Active == 0 {
			continue
		}
		secret := passphrase
		if k.Token != "" && k.Signature != "" && userKR != nil {
			s, err := decryptToken(k.Token, k.Signature, userKR)
			if err != nil {
				continue
			}
			secret = s
		}
		locked, err := pgp.NewKeyFromArmored(k.PrivateKey)
		if err != nil {
			continue
		}
		unlocked, err := locked.Unlock(secret)
		if err != nil {
			continue
		}
		_ = kr.AddKey(unlocked)
	}
	if kr.CountEntities() == 0 {
		return nil, fmt.Errorf("no keys could be unlocked")
	}
	return kr, nil
}

func decryptToken(tokenArm, sigArm string, kr *pgp.KeyRing) ([]byte, error) {
	msg, err := pgp.NewPGPMessageFromArmored(tokenArm)
	if err != nil {
		return nil, err
	}
	sig, err := pgp.NewPGPSignatureFromArmored(sigArm)
	if err != nil {
		return nil, err
	}
	dec, err := kr.Decrypt(msg, nil, 0)
	if err != nil {
		return nil, err
	}
	if err := kr.VerifyDetached(dec, sig, 0); err != nil {
		return nil, err
	}
	return dec.GetBinary(), nil
}

// Query makes a url.Values with the provided key/value pairs. Small helper
// kept here to avoid one-liners sprinkled across services.
func Query(kv ...string) url.Values {
	q := url.Values{}
	for i := 0; i+1 < len(kv); i += 2 {
		q.Set(kv[i], kv[i+1])
	}
	return q
}
