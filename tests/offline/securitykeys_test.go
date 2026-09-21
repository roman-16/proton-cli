package offline

import (
	"strings"
	"testing"
)

// What a security key is called is settled on the command line, so it is
// settled before anything reaches Proton and long before anybody is asked to
// touch a key. A run that got as far as the ceremony before saying the name was
// missing would have spent a password prompt and a touch on it.

func TestASecurityKeyIsNamedBeforeAnybodyTouchesOne(t *testing.T) {
	refuses(t, 1, []string{"account", "settings", "security-keys", "create"},
		"A security key needs a name.")
	refuses(t, 1, []string{"account", "settings", "security-keys", "create", "--name", "  "},
		"A security key needs a name.")
	refuses(t, 1, []string{"account", "settings", "security-keys", "create",
		"--name", strings.Repeat("k", 129)}, "128 characters at most")
}

// Renaming with nothing to rename it to is the same: the command line already
// says there is no change to make.
func TestRenamingASecurityKeyNeedsTheNewName(t *testing.T) {
	refuses(t, 1, []string{"account", "settings", "security-keys", "update", "5bH2mQxK"},
		"Nothing to change.")
	refuses(t, 1, []string{"account", "settings", "security-keys", "update", "5bH2mQxK", "--name", ""},
		"A security key needs a name.")
}
