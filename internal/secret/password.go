// Package secret makes passwords, using the same alphabet and defaults as Proton
// Pass.
//
// It is here rather than in the Pass service because nothing about it involves
// Proton: a password is made on this machine and may never be stored anywhere.
// That is also why it is worth having - a generator you already trust beats
// reaching for whatever is on the path.
package secret

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"strings"
)

// The alphabets Proton's own generator uses.
//
// i, o, l and their capitals are left out of the default set because they are
// the characters people misread; they come back when letters are the only thing
// asked for, since a password of 20 characters drawn from 46 would be weaker for
// no reason. The symbols are the ones that survive a double-click selection.
const (
	upper     = "ABCDEFGHJKMNPQRSTUVWXYZ"
	lower     = "abcdefghjkmnpqrstuvwxyz"
	ambiguous = "iolIOL"
	digits    = "0123456789"
	symbols   = "!#$%&()*+.:;<=>?@[]^"
)

// DefaultLength is Proton's, and long enough that the alphabet hardly matters.
const DefaultLength = 20

// Options say what a password may be made of.
//
// Words is what decides which kind is made: none of them is a password of
// characters, and any of them a passphrase of that many words. The three
// switches read the same either way - digits and capitals in it or not - and
// Symbols and Separator each belong to one kind.
type Options struct {
	Length    int
	Words     int
	Separator string
	Digits    bool
	Symbols   bool
	Upper     bool
}

// Make is a password or a passphrase, whichever the options ask for.
func Make(o Options) (string, error) {
	if o.Words > 0 {
		return Passphrase(o)
	}
	return Password(o)
}

// Password makes one.
//
// Every requested class is guaranteed to appear: a password asked to include
// digits that happens to contain none is a password that will be rejected by
// whatever asked for digits, and re-rolling until it passes is how generators
// leak bias. So one character of each class is placed first and the whole thing
// is then shuffled.
func Password(o Options) (string, error) {
	if o.Length <= 0 {
		o.Length = DefaultLength
	}
	classes := []string{lower}
	if o.Upper {
		classes = append(classes, upper)
	}
	if o.Digits {
		classes = append(classes, digits)
	}
	if o.Symbols {
		classes = append(classes, symbols)
	}
	// Letters alone is a narrow enough alphabet that the misread-prone ones are
	// worth having back.
	if !o.Digits && !o.Symbols {
		classes[0] = lower + ambiguous
	}
	if o.Length < len(classes) {
		return "", fmt.Errorf("a password of %d characters cannot hold one of each of %d kinds",
			o.Length, len(classes))
	}

	pool := strings.Join(classes, "")
	out := make([]byte, 0, o.Length)
	for _, class := range classes {
		c, err := pick(class)
		if err != nil {
			return "", err
		}
		out = append(out, c)
	}
	for len(out) < o.Length {
		c, err := pick(pool)
		if err != nil {
			return "", err
		}
		out = append(out, c)
	}
	if err := shuffle(out); err != nil {
		return "", err
	}
	return string(out), nil
}

func pick(alphabet string) (byte, error) {
	n, err := index(len(alphabet))
	if err != nil {
		return 0, err
	}
	return alphabet[n], nil
}

// index is a number below n, drawn from the cryptographic source.
func index(n int) (int, error) {
	i, err := rand.Int(rand.Reader, big.NewInt(int64(n)))
	if err != nil {
		return 0, err
	}
	return int(i.Int64()), nil
}

// shuffle is Fisher-Yates over the cryptographic source, so the guaranteed
// characters do not sit in a predictable order.
func shuffle(b []byte) error {
	for i := len(b) - 1; i > 0; i-- {
		j, err := rand.Int(rand.Reader, big.NewInt(int64(i+1)))
		if err != nil {
			return err
		}
		b[i], b[j.Int64()] = b[j.Int64()], b[i]
	}
	return nil
}
