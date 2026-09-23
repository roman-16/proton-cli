package inflect

import "strings"

func Singular(plural string) string {
	if first, rest, ok := strings.Cut(plural, " or "); ok {
		return Singular(first) + " or " + Singular(rest)
	}
	switch {
	case strings.HasSuffix(plural, "ses"), strings.HasSuffix(plural, "xes"),
		strings.HasSuffix(plural, "ches"), strings.HasSuffix(plural, "shes"):
		return strings.TrimSuffix(plural, "es")
	case strings.HasSuffix(plural, "ies"):
		return strings.TrimSuffix(plural, "ies") + "y"
	case strings.HasSuffix(plural, "s"):
		return strings.TrimSuffix(plural, "s")
	}
	return plural
}

func Plural(singular string) string {
	if first, rest, ok := strings.Cut(singular, " or "); ok {
		return Plural(first) + " or " + Plural(rest)
	}
	switch {
	case strings.HasSuffix(singular, "s"), strings.HasSuffix(singular, "x"),
		strings.HasSuffix(singular, "ch"), strings.HasSuffix(singular, "sh"):
		return singular + "es"
	case consonantThenY(singular):
		return strings.TrimSuffix(singular, "y") + "ies"
	}
	return singular + "s"
}

func consonantThenY(word string) bool {
	return len(word) > 1 && word[len(word)-1] == 'y' && !strings.ContainsRune("aeiou", rune(word[len(word)-2]))
}
