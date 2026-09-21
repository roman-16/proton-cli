package bip39

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
)

// The BIP39 reference vectors: the phrase and the bytes it stands for.
func TestEntropyReadsTheReferenceVectors(t *testing.T) {
	for _, tc := range []struct{ phrase, entropy string }{
		{"abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about",
			"00000000000000000000000000000000"},
		{"legal winner thank year wave sausage worth useful legal winner thank yellow",
			"7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f"},
		{"letter advice cage absurd amount doctor acoustic avoid letter advice cage above",
			"80808080808080808080808080808080"},
		{"zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo wrong",
			"ffffffffffffffffffffffffffffffff"},
		{strings.Repeat("abandon ", 23) + "art",
			"0000000000000000000000000000000000000000000000000000000000000000"},
		{strings.Repeat("zoo ", 23) + "vote",
			"ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"},
	} {
		got, err := Entropy(tc.phrase)
		if err != nil {
			t.Errorf("%q: %v", tc.phrase, err)
			continue
		}
		if hex.EncodeToString(got) != tc.entropy {
			t.Errorf("%q: read %x, want %s", tc.phrase, got, tc.entropy)
		}
	}
}

// The same vectors the other way: what Proton writes down for these bytes.
func TestWordsWritesTheReferenceVectors(t *testing.T) {
	for _, tc := range []struct{ entropy, phrase string }{
		{"00000000000000000000000000000000",
			"abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"},
		{"7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f",
			"legal winner thank year wave sausage worth useful legal winner thank yellow"},
		{"80808080808080808080808080808080",
			"letter advice cage absurd amount doctor acoustic avoid letter advice cage above"},
		{"ffffffffffffffffffffffffffffffff",
			"zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo wrong"},
		{"0000000000000000000000000000000000000000000000000000000000000000",
			strings.Repeat("abandon ", 23) + "art"},
	} {
		entropy, err := hex.DecodeString(tc.entropy)
		if err != nil {
			t.Fatal(err)
		}
		got, err := Words(entropy)
		if err != nil {
			t.Errorf("%s: %v", tc.entropy, err)
			continue
		}
		if got != tc.phrase {
			t.Errorf("%s: wrote %q, want %q", tc.entropy, got, tc.phrase)
		}
	}
}

// What is written is read back: the phrase a recovery phrase is set with has to
// be one the same account can recover with.
func TestWordsAndEntropyAreInverses(t *testing.T) {
	entropy := make([]byte, 16)
	for i := range 256 {
		sum := sha256.Sum256([]byte{byte(i)})
		copy(entropy, sum[:])
		phrase, err := Words(entropy)
		if err != nil {
			t.Fatalf("%x: %v", entropy, err)
		}
		back, err := Entropy(phrase)
		if err != nil {
			t.Fatalf("%q: %v", phrase, err)
		}
		if !bytes.Equal(back, entropy) {
			t.Fatalf("%x became %q, which reads back as %x", entropy, phrase, back)
		}
		if n := len(strings.Fields(phrase)); n != 12 {
			t.Fatalf("%x became %d words, want 12", entropy, n)
		}
	}
}

func TestWordsRefusesALengthTheSchemeHasNoPhraseFor(t *testing.T) {
	for _, n := range []int{0, 8, 15, 17, 33} {
		if _, err := Words(make([]byte, n)); !errors.Is(err, ErrLength) {
			t.Errorf("%d bytes: got %v, want %v", n, err, ErrLength)
		}
	}
}

func TestEntropyIgnoresCaseAndSpacing(t *testing.T) {
	got, err := Entropy("  Zoo ZOO zoo\tzoo zoo zoo zoo zoo\nzoo zoo zoo WRONG ")
	if err != nil {
		t.Fatal(err)
	}
	if hex.EncodeToString(got) != "ffffffffffffffffffffffffffffffff" {
		t.Errorf("read %x", got)
	}
}

func TestEntropyRefusesWhatIsNotAPhrase(t *testing.T) {
	for _, tc := range []struct {
		phrase string
		want   error
	}{
		{"zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo", ErrLength},
		{"zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo", ErrLength},
		{"", ErrLength},
		{"zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo proton", ErrWord},
		{"zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo", ErrChecksum},
	} {
		if _, err := Entropy(tc.phrase); !errors.Is(err, tc.want) {
			t.Errorf("%q: got %v, want %v", tc.phrase, err, tc.want)
		}
	}
}

// The list is the one every BIP39 implementation ships, byte for byte.
func TestTheListIsTheBIP39EnglishList(t *testing.T) {
	if len(words) != 2048 {
		t.Fatalf("%d words, want 2048", len(words))
	}
	sum := sha256.Sum256([]byte(english))
	if got := hex.EncodeToString(sum[:]); got != "2f5eed53a4727b4bf8880d8f3f199efc90e58503646d9ff8eff3a2ed3b24dbda" {
		t.Errorf("the list hashes to %s, which is not the BIP39 English list", got)
	}
}
