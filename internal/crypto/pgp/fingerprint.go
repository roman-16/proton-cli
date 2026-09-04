package pgp

import (
	"strings"

	pgp "github.com/ProtonMail/gopenpgp/v2/crypto"
)

// Fingerprint returns a short, human-comparable identifier for an armoured public
// key: the last sixteen hex digits of its fingerprint, grouped in fours.
//
// A pinned key is reported by fingerprint rather than by its armoured block,
// because a table is for recognising a key, not for transporting one. Sixteen
// digits is what a person can compare against another client's display.
func Fingerprint(armored string) string {
	key, err := pgp.NewKeyFromArmored(armored)
	if err != nil {
		return "(unreadable)"
	}
	fp := strings.ToUpper(key.GetFingerprint())
	if len(fp) > 16 {
		fp = fp[len(fp)-16:]
	}
	var b strings.Builder
	for i, r := range fp {
		if i > 0 && i%4 == 0 {
			b.WriteByte(' ')
		}
		b.WriteRune(r)
	}
	return b.String()
}
