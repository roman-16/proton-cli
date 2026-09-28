package offline

import "testing"

func TestDeletingTheAccountNeedsAReason(t *testing.T) {
	refuses(t, 1, []string{"account", "delete", "--message", "Moving everything to one provider"}, `"reason"`)
	refuses(t, 1, []string{"account", "delete", "--reason", "bored", "--message", "Moving everything to one provider"},
		"--reason accepts:", "different-account", "too-expensive", "missing-feature", "other-service", "merge", "other")
}

func TestDeletingTheAccountSaysWhyInTenCharacters(t *testing.T) {
	refuses(t, 1, []string{"account", "delete", "--reason", "missing-feature"},
		"--message is required: say in at least ten characters why you are leaving.")
	refuses(t, 1, []string{"account", "delete", "--reason", "other", "--message", "  too short "},
		"--message is required")
}
