// Package bip39 turns a recovery phrase into the bytes it stands for, and back.
//
// A Proton recovery phrase is twelve words from the BIP39 English list, which
// encode sixteen random bytes and a checksum over them. The bytes are what the
// account's keys were locked under when the phrase was set, so turning the words
// back into them is the whole of what recovering with a phrase needs from here.
package bip39

import (
	"crypto/sha256"
	_ "embed"
	"errors"
	"math/big"
	"strings"
)

//go:embed english.txt
var english string

// words is the list, in the order that gives each word its index.
var words = strings.Fields(english)

// index is each word's position in the list.
var index = func() map[string]int {
	m := make(map[string]int, len(words))
	for i, w := range words {
		m[w] = i
	}
	return m
}()

// The ways a phrase fails to read. They are told apart because the answers
// differ: a word not on the list is a typo, a count that is not one the scheme
// makes is a phrase with a word missing or one too many, and a checksum that
// does not match is a right-looking phrase with a word swapped for another.
var (
	ErrWord     = errors.New("a word is not on the list")
	ErrLength   = errors.New("not a number of words a phrase can have")
	ErrChecksum = errors.New("the checksum does not match")
)

// Each word carries this many bits of the phrase.
const bitsPerWord = 11

// Words is the phrase that stands for these bytes: the bytes and a checksum
// over them, read eleven bits at a time as positions in the list.
//
// Sixteen bytes make the twelve words Proton hands out. Any length the scheme
// allows works, because the arithmetic is the same one Entropy undoes and
// writing it for one length would only make the pair harder to read together.
func Words(entropy []byte) (string, error) {
	bits := len(entropy) * 8
	if bits < 128 || bits > 256 || bits%32 != 0 {
		return "", ErrLength
	}
	checksumBits := uint(bits / 32)
	sum := sha256.Sum256(entropy)
	acc := new(big.Int).SetBytes(entropy)
	acc.Lsh(acc, checksumBits)
	acc.Or(acc, big.NewInt(int64(sum[0]>>(8-checksumBits))))

	n := (bits + int(checksumBits)) / bitsPerWord
	out := make([]string, n)
	mask := big.NewInt(int64(1)<<bitsPerWord - 1)
	pick := new(big.Int)
	for i := n - 1; i >= 0; i-- {
		out[i] = words[pick.And(acc, mask).Int64()]
		acc.Rsh(acc, bitsPerWord)
	}
	return strings.Join(out, " "), nil
}

// Entropy turns a phrase back into the bytes it encodes, checking it on the way.
//
// Letter case and spacing are not part of a phrase, so neither decides anything
// here. The list is English, whose words are plain ASCII, which is why lowering
// the case is all the normalising a word needs.
func Entropy(phrase string) ([]byte, error) {
	ws := strings.Fields(strings.ToLower(phrase))
	n := len(ws)
	if n < 12 || n > 24 || n%3 != 0 {
		return nil, ErrLength
	}
	acc := new(big.Int)
	for _, w := range ws {
		i, ok := index[w]
		if !ok {
			return nil, ErrWord
		}
		acc.Lsh(acc, bitsPerWord)
		acc.Or(acc, big.NewInt(int64(i)))
	}
	// The last n/3 bits are the checksum; everything before them is the entropy.
	checksumBits := uint(n / 3)
	checksum := new(big.Int).And(acc, big.NewInt(int64(1)<<checksumBits-1))
	entropy := acc.Rsh(acc, checksumBits).FillBytes(make([]byte, (n*bitsPerWord-int(checksumBits))/8))
	sum := sha256.Sum256(entropy)
	if uint64(sum[0]>>(8-checksumBits)) != checksum.Uint64() {
		return nil, ErrChecksum
	}
	return entropy, nil
}
