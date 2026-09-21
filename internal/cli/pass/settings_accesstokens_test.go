package pass

import (
	"strings"
	"testing"
	"time"

	passsvc "github.com/roman-16/proton-cli/internal/service/pass"
)

// How long a token works is judged from the command line: it has a floor and a
// ceiling, and no way of saying never.
func TestATokensLifeIsBetweenAnHourAndAYear(t *testing.T) {
	for _, tt := range []struct {
		arg  string
		want time.Duration
	}{
		{"1h", time.Hour},
		{"30d", 30 * 24 * time.Hour},
		{"1y", 365 * 24 * time.Hour},
	} {
		got, err := tokenLife(tt.arg)
		if err != nil || got != tt.want {
			t.Errorf("tokenLife(%q) = %v, %v; want %v", tt.arg, got, err, tt.want)
		}
	}
	for arg, want := range map[string]string{
		"":      "How long should the token work?",
		"never": "always expires",
		"59m":   "between 1h and 1y",
		"2y":    "between 1h and 1y",
		"soon":  "--expires:",
	} {
		_, err := tokenLife(arg)
		if err == nil {
			t.Errorf("tokenLife(%q) was accepted", arg)
		} else if !strings.Contains(err.Error(), want) {
			t.Errorf("tokenLife(%q) refused with %q, want %q in it", arg, err, want)
		}
	}
}

// A token is described by the vaults it reads, as a person knows them, and a
// vault this account cannot name any more is shown by its share.
func TestATokenIsDescribedByTheVaultsItReads(t *testing.T) {
	if got := vaultNames(nil); got != "(none)" {
		t.Errorf("no vaults = %q", got)
	}
	got := vaultNames([]passsvc.TokenGrant{{ShareID: "s1", Vault: "Work"}, {ShareID: "s2"}})
	if got != "Work, s2" {
		t.Errorf("vaults = %q", got)
	}
}
