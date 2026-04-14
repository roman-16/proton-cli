package crypto

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ProtonMail/go-srp"
	"github.com/ProtonMail/gopenpgp/v2/crypto"
	"github.com/roman-16/proton-cli/internal/client"
	"github.com/roman-16/proton-cli/internal/session"
)

// KeyRings holds the unlocked key hierarchy for a session.
type KeyRings struct {
	UserKR    *crypto.KeyRing
	AddrKRs   map[string]*crypto.KeyRing // keyed by AddressID
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

type Salt struct {
	ID      string
	KeySalt string
}

// UnlockKeys fetches user/address keys and unlocks them.
// If the session has a cached SaltedKeyPass, uses that.
// Otherwise derives it from the password and caches it.
func UnlockKeys(ctx context.Context, c *client.Client, password string) (*KeyRings, error) {
	saltedKeyPass := c.SaltedKeyPass()

	if saltedKeyPass == "" {
		if password == "" {
			return nil, fmt.Errorf("password required for encrypted operations.\nSet PROTON_PASSWORD environment variable or use --password flag.\nThe raw 'api' command works without a password.")
		}

		skp, err := deriveSaltedKeyPass(ctx, c, password)
		if err != nil {
			return nil, fmt.Errorf("failed to derive key password: %w", err)
		}
		saltedKeyPass = skp
		c.SetSaltedKeyPass(saltedKeyPass)

		// Persist to session file
		sess := c.Session()
		session.Save(sess)
	}

	user, err := getUser(ctx, c)
	if err != nil {
		return nil, fmt.Errorf("failed to get user: %w", err)
	}

	userKR, err := unlockKeyRing(user.Keys, []byte(saltedKeyPass), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to unlock user keys: %w", err)
	}

	addresses, err := getAddresses(ctx, c)
	if err != nil {
		return nil, fmt.Errorf("failed to get addresses: %w", err)
	}

	addrKRs := make(map[string]*crypto.KeyRing)
	for _, addr := range addresses {
		kr, err := unlockKeyRing(addr.Keys, []byte(saltedKeyPass), userKR)
		if err != nil {
			continue // Some address keys may not be unlockable
		}
		addrKRs[addr.ID] = kr
	}

	if len(addrKRs) == 0 {
		return nil, fmt.Errorf("failed to unlock any address keys")
	}

	return &KeyRings{
		UserKR:    userKR,
		AddrKRs:   addrKRs,
		Addresses: addresses,
	}, nil
}

// AddrKRByEmail returns the address key ring for the given email.
func (kr *KeyRings) AddrKRByEmail(email string) (*crypto.KeyRing, string, error) {
	for _, addr := range kr.Addresses {
		if addr.Email == email {
			if akr, ok := kr.AddrKRs[addr.ID]; ok {
				return akr, addr.ID, nil
			}
		}
	}
	return nil, "", fmt.Errorf("no key ring for address %s", email)
}

// FirstAddrKR returns the first available address key ring.
func (kr *KeyRings) FirstAddrKR() (*crypto.KeyRing, string, error) {
	for _, addr := range kr.Addresses {
		if akr, ok := kr.AddrKRs[addr.ID]; ok {
			return akr, addr.ID, nil
		}
	}
	return nil, "", fmt.Errorf("no address key rings available")
}

// PrimaryAddrKR returns the address key ring for the primary Proton address.
// Falls back to the first available if no proton.me/pm.me address is found.
func (kr *KeyRings) PrimaryAddrKR() (*crypto.KeyRing, string, error) {
	// Prefer proton.me or pm.me addresses
	for _, addr := range kr.Addresses {
		if akr, ok := kr.AddrKRs[addr.ID]; ok {
			email := addr.Email
			if strings.HasSuffix(email, "@proton.me") || strings.HasSuffix(email, "@pm.me") || strings.HasSuffix(email, "@protonmail.com") {
				return akr, addr.ID, nil
			}
		}
	}
	return kr.FirstAddrKR()
}

func deriveSaltedKeyPass(ctx context.Context, c *client.Client, password string) (string, error) {
	// GET /core/v4/keys/salts
	body, _, err := c.Do(ctx, "GET", "/core/v4/keys/salts", nil, "", "", "")
	if err != nil {
		return "", err
	}

	var res struct {
		KeySalts []Salt
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return "", err
	}

	if len(res.KeySalts) == 0 {
		return "", fmt.Errorf("no key salts returned")
	}

	// Use the primary key salt
	salt := res.KeySalts[0]
	keySalt, err := base64.StdEncoding.DecodeString(salt.KeySalt)
	if err != nil {
		return "", err
	}

	saltedPass, err := srp.MailboxPassword([]byte(password), keySalt)
	if err != nil {
		return "", err
	}

	// go-srp returns the full hash, we need the last 31 bytes
	return string(saltedPass[len(saltedPass)-31:]), nil
}

func getUser(ctx context.Context, c *client.Client) (*User, error) {
	body, _, err := c.Do(ctx, "GET", "/core/v4/users", nil, "", "", "")
	if err != nil {
		return nil, err
	}

	var res struct {
		User User
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return nil, err
	}

	return &res.User, nil
}

func getAddresses(ctx context.Context, c *client.Client) ([]Address, error) {
	body, _, err := c.Do(ctx, "GET", "/core/v4/addresses", nil, "", "", "")
	if err != nil {
		return nil, err
	}

	var res struct {
		Addresses []Address
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return nil, err
	}

	return res.Addresses, nil
}

func unlockKeyRing(keys []Key, passphrase []byte, userKR *crypto.KeyRing) (*crypto.KeyRing, error) {
	kr, err := crypto.NewKeyRing(nil)
	if err != nil {
		return nil, err
	}

	for _, key := range keys {
		if key.Active == 0 {
			continue
		}

		var secret []byte
		if key.Token != "" && key.Signature != "" && userKR != nil {
			// Decrypt token with user key ring
			secret, err = decryptToken(key.Token, key.Signature, userKR)
			if err != nil {
				continue
			}
		} else {
			secret = passphrase
		}

		lockedKey, err := crypto.NewKeyFromArmored(key.PrivateKey)
		if err != nil {
			continue
		}

		unlockedKey, err := lockedKey.Unlock(secret)
		if err != nil {
			continue
		}

		if err := kr.AddKey(unlockedKey); err != nil {
			continue
		}
	}

	if kr.CountEntities() == 0 {
		return nil, fmt.Errorf("no keys could be unlocked")
	}

	return kr, nil
}

func decryptToken(tokenArmored, signatureArmored string, kr *crypto.KeyRing) ([]byte, error) {
	msg, err := crypto.NewPGPMessageFromArmored(tokenArmored)
	if err != nil {
		return nil, err
	}

	sig, err := crypto.NewPGPSignatureFromArmored(signatureArmored)
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
