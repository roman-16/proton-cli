package secret

import (
	_ "embed"
	"strings"
	"unicode"
)

// A passphrase is words rather than characters: something long enough to be
// worth having and short enough to read out loud.
//
// The list is Proton's, which is the EFF long wordlist with the one word Pass
// leaves out, so a passphrase made here is one Pass could have made.

//go:embed wordlist.txt
var wordlist string

// Words is the list a passphrase is drawn from.
var Words = strings.Fields(wordlist)

// Separators are what may stand between two words. Two of them are drawn per
// gap rather than fixed, which is what makes a passphrase of five words carry
// more than five choices.
var Separators = map[string]string{
	"hyphen":     "-",
	"space":      " ",
	"period":     ".",
	"comma":      ",",
	"underscore": "_",
	"digit":      digits,
	"symbol":     digits + passphraseSymbols,
}

// SeparatorNames are the separators, for a flag that offers a set.
func SeparatorNames() []string {
	return []string{"comma", "digit", "hyphen", "period", "space", "symbol", "underscore"}
}

// DefaultWords and DefaultSeparator are Proton's own, so `--words` alone makes
// what the Pass generator makes.
const (
	DefaultWords     = 5
	DefaultSeparator = "hyphen"
)

// passphraseSymbols are the ones Proton draws a separator from.
const passphraseSymbols = "!@#$%^&*"

// Passphrase makes one.
//
// Each word is capitalised and followed by a digit unless asked otherwise, which
// is what Proton's generator does and what carries a passphrase past the length
// rules that would otherwise refuse it.
func Passphrase(o Options) (string, error) {
	count := o.Words
	if count <= 0 {
		count = DefaultWords
	}
	separator, ok := Separators[o.Separator]
	if !ok {
		separator = Separators[DefaultSeparator]
	}

	var out strings.Builder
	for i := range count {
		if i > 0 {
			// A separator that is a set is drawn from per gap; one that is a
			// single character is itself.
			s, err := pickFrom(separator)
			if err != nil {
				return "", err
			}
			out.WriteString(s)
		}
		word, err := pickWord()
		if err != nil {
			return "", err
		}
		if o.Upper {
			word = capitalize(word)
		}
		out.WriteString(word)
		if o.Digits {
			d, err := pick(digits)
			if err != nil {
				return "", err
			}
			out.WriteByte(d)
		}
	}
	return out.String(), nil
}

func pickWord() (string, error) {
	n, err := index(len(Words))
	if err != nil {
		return "", err
	}
	return Words[n], nil
}

// pickFrom is one character of a set, or the set itself when it is already one
// character.
func pickFrom(set string) (string, error) {
	if len(set) <= 1 {
		return set, nil
	}
	c, err := pick(set)
	if err != nil {
		return "", err
	}
	return string(c), nil
}

func capitalize(word string) string {
	if word == "" {
		return word
	}
	r := []rune(word)
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}
