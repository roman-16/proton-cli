package keys

import (
	"crypto"
	"time"

	"github.com/ProtonMail/go-crypto/openpgp/packet"
	pgp "github.com/ProtonMail/gopenpgp/v2/crypto"
)

// The shape of a key this build writes, in one place.
//
// A key of this account's is read by every other client the account has, and
// Proton refuses one that is unlike the keys its own clients make - which it
// does over choices the standard leaves open and calls equivalent. So the
// choices are not made where a key happens to be needed: they are made here,
// once, and every key goes through them. There are three of them - what a
// signature is hashed with, what the key says it prefers, and what clock it is
// dated by - and Proton's clients settle all three the same way for a key of an
// address, a key derived for a forwarding, and a key re-locked from one.

// Generation is what every key this build makes is made under.
//
// An EdDSA primary over Curve25519 with a Curve25519 encryption subkey, hashed
// with SHA-512, saying it prefers AES-256 and SHA-512 - which is what Proton's
// clients write - and dated by Proton's clock rather than this machine's, since
// a key dated in the future is one Proton refuses outright.
//
// Fresh each time: the derivation a forwarding key comes out of writes the
// algorithm and curve it insists on into whatever it is handed.
func (u *Unlocked) Generation() *packet.Config {
	return &packet.Config{
		Algorithm:              packet.PubKeyAlgoEdDSA,
		DefaultCipher:          packet.CipherAES256,
		DefaultCompressionAlgo: packet.CompressionZLIB,
		DefaultHash:            crypto.SHA512,
		Time:                   u.clock(),
	}
}

// clock is the time generator a key is dated by.
func (u *Unlocked) clock() func() time.Time {
	if u.Now == nil {
		return time.Now
	}
	return u.Now
}

// LockAndArmor writes a key out the way it goes to Proton: locked under the
// passphrase the account holds it by, and armoured with nothing but the key.
//
// The armour carries no headers because Proton's clients write none, and a key
// is compared with theirs byte for byte in places nobody here can see.
func LockAndArmor(key *pgp.Key, passphrase []byte) (string, error) {
	locked, err := key.Lock(passphrase)
	if err != nil {
		return "", err
	}
	return locked.ArmorWithCustomHeaders("", "")
}
