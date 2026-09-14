package twofa

import "testing"

func TestEligibleReadsWhateverShapeAURLWasStoredIn(t *testing.T) {
	for _, stored := range []string{
		"github.com",
		"https://github.com",
		"https://github.com/login",
		"HTTPS://GitHub.com/",
		"https://github.com:443/settings/security",
		"https://gist.github.com",
		"github.com.",
		"  https://github.com  ",
	} {
		if !Eligible(stored) {
			t.Errorf("Eligible(%q) = false; github.com is in the list", stored)
		}
	}
}

func TestEligibleRefusesWhatItCannotPlace(t *testing.T) {
	for _, stored := range []string{
		"",
		"   ",
		"not a url at all",
		"https://",
		"https://example.invalid",
		"https://nothing-like-this-is-listed.example",
	} {
		if Eligible(stored) {
			t.Errorf("Eligible(%q) = true; nothing should have matched", stored)
		}
	}
}

// The two adjustments the list is generated with, checked here so a regeneration
// that lost either fails rather than quietly changing every verdict about them.
func TestTheTwoAdjustmentsSurviveARegeneration(t *testing.T) {
	if !Eligible("https://accounts.google.com") {
		t.Error("google.com should be eligible; it is added to the published list")
	}
	if Eligible("https://account.proton.me") {
		t.Error("proton.me should not be eligible; an account's own two-factor is not a site login")
	}
}

func TestTheListArrived(t *testing.T) {
	if len(domains) < 500 {
		t.Fatalf("the embedded list holds %d domains; it should hold over a thousand", len(domains))
	}
}
