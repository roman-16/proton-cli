package secret

import (
	"strings"
	"testing"
	"unicode"
)

// The list is Proton's own, so a passphrase made here is one Pass could have
// made. It is the EFF long wordlist, less the word Proton leaves out.
func TestTheWordlistIsProtons(t *testing.T) {
	if len(Words) != 7775 {
		t.Errorf("the wordlist holds %d words, want 7775", len(Words))
	}
	for _, w := range Words {
		if w == "racism" {
			t.Error("the wordlist holds a word Proton's own leaves out")
		}
		if w != strings.ToLower(w) || strings.ContainsFunc(w, unicode.IsSpace) {
			t.Errorf("%q is not a plain lowercase word", w)
		}
	}
}

// Proton's defaults: five words, hyphens, capitalised, a digit after each word.
func TestAPassphraseIsWhatProtonWouldHaveMade(t *testing.T) {
	got, err := Passphrase(Options{Words: DefaultWords, Separator: DefaultSeparator, Digits: true, Upper: true})
	if err != nil {
		t.Fatalf("Passphrase: %v", err)
	}
	words := strings.Split(got, "-")
	if len(words) != DefaultWords {
		t.Fatalf("%q is %d words, want %d", got, len(words), DefaultWords)
	}
	for _, w := range words {
		if !unicode.IsUpper(rune(w[0])) {
			t.Errorf("%q does not start with a capital, in %q", w, got)
		}
		if !unicode.IsDigit(rune(w[len(w)-1])) {
			t.Errorf("%q does not end in a digit, in %q", w, got)
		}
		if !known(strings.ToLower(strings.TrimRight(w, "0123456789"))) {
			t.Errorf("%q is not from the wordlist, in %q", w, got)
		}
	}
}

// Every switch reads the same way it does for a password: what is off is absent.
func TestAPassphraseLeavesOutWhatWasNotAskedFor(t *testing.T) {
	got, err := Passphrase(Options{Words: 4, Separator: "space"})
	if err != nil {
		t.Fatalf("Passphrase: %v", err)
	}
	words := strings.Split(got, " ")
	if len(words) != 4 {
		t.Fatalf("%q is %d words, want 4", got, len(words))
	}
	if strings.ContainsAny(got, "0123456789") {
		t.Errorf("%q holds a digit that was not asked for", got)
	}
	if got != strings.ToLower(got) {
		t.Errorf("%q holds a capital that was not asked for", got)
	}
}

// A separator that is a set is drawn from per gap, which is what makes it worth
// more than a fixed character.
func TestASeparatorThatIsASetVaries(t *testing.T) {
	seen := map[string]bool{}
	for range 40 {
		got, err := Passphrase(Options{Words: 6, Separator: "symbol", Upper: true})
		if err != nil {
			t.Fatalf("Passphrase: %v", err)
		}
		for _, r := range got {
			if strings.ContainsRune("0123456789!@#$%^&*", r) {
				seen[string(r)] = true
			}
		}
	}
	if len(seen) < 4 {
		t.Errorf("six words over forty passphrases used only %d separators: %v", len(seen), seen)
	}
}

// Words is what decides which kind is made, wherever one is made.
func TestMakeChoosesByWords(t *testing.T) {
	pw, err := Make(Options{Length: 20, Digits: true, Symbols: true, Upper: true})
	if err != nil {
		t.Fatalf("Make: %v", err)
	}
	if len(pw) != 20 || strings.Contains(pw, "-") {
		t.Errorf("%q is not a password of twenty characters", pw)
	}
	phrase, err := Make(Options{Words: 3, Separator: "hyphen", Upper: true})
	if err != nil {
		t.Fatalf("Make: %v", err)
	}
	if len(strings.Split(phrase, "-")) != 3 {
		t.Errorf("%q is not three words", phrase)
	}
}

func known(word string) bool {
	for _, w := range Words {
		if w == word {
			return true
		}
	}
	return false
}
