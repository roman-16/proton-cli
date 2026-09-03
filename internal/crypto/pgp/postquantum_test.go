package pgp

import (
	"os"
	"path/filepath"
	"testing"

	gopenpgp "github.com/ProtonMail/gopenpgp/v2/crypto"
)

// Proton generated these keys for two weeks in May before pausing the rollout,
// so an account can hold them today while no library here can read one. The
// samples are what such a key looks like: a v6 ML-DSA-65 primary with an
// ML-KEM-768 subkey, private and published.
func TestPostQuantumRecognisesProtonsKeys(t *testing.T) {
	for _, name := range []string{"postquantum-private.asc", "postquantum-public.asc"} {
		t.Run(name, func(t *testing.T) {
			armored, err := os.ReadFile(filepath.Join("testdata", name))
			if err != nil {
				t.Fatalf("read %s: %v", name, err)
			}
			if !PostQuantum(string(armored)) {
				t.Error("a post-quantum key was not recognised as one")
			}
		})
	}
}

// Everything else is not one, and saying it is would put the wrong sentence on
// the screen for a key that is merely broken.
func TestPostQuantumOnKeysThatAreNotOne(t *testing.T) {
	key, err := gopenpgp.GenerateKey("ordinary", "ordinary@example.invalid", "x25519", 0)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	ordinary, err := key.GetArmoredPublicKey()
	if err != nil {
		t.Fatalf("armor: %v", err)
	}
	for name, armored := range map[string]string{
		"an ordinary key":  ordinary,
		"nothing at all":   "",
		"not armour":       "hello",
		"armour of no key": "-----BEGIN PGP MESSAGE-----\n\nhQEMA0\n-----END PGP MESSAGE-----\n",
	} {
		t.Run(name, func(t *testing.T) {
			if PostQuantum(armored) {
				t.Error("recognised as a post-quantum key")
			}
		})
	}
}
