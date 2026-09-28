package offline

import "testing"

func TestPausingAGroupNamesOneOfTheTwoWithASwitch(t *testing.T) {
	refuses(t, 1, []string{"pass", "breaches", "disable"},
		"Name one address, or pass --type for every address of one kind.")
	refuses(t, 1, []string{"pass", "breaches", "enable", "jane@proton.me", "--type", "alias"},
		"Name one address, or pass --type for every address of one kind.")
	refuses(t, 1, []string{"pass", "breaches", "disable", "--type", "custom"},
		"Addresses you added are paused one at a time.")
	refuses(t, 1, []string{"pass", "breaches", "list", "--type", "vault"},
		"--type accepts:", "proton", "custom", "alias")
}

func TestResolvingABreachNamesOne(t *testing.T) {
	refuses(t, 1, []string{"account", "breaches", "resolve"}, "REF")
	refuses(t, 1, []string{"pass", "breaches", "resolve"}, "REF")
}
