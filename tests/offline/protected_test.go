package offline

import (
	"os"
	"path/filepath"
	"testing"
)

// A password-protected message needs no account, so what is left to judge from
// the command line is the link and the password. Both are judged here, before
// anything is asked of Proton.

func TestAMessageBehindAPasswordNeedsItsPassword(t *testing.T) {
	refuses(t, 1, []string{"mail", "protected", "get", "9fK2pQ7xNv4mB8"},
		"The message's password is required", "--eo-password-file")
}

func TestSomethingThatIsNotSuchAMessageIsRefused(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "pw")
	if err := os.WriteFile(file, []byte("correct horse battery"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, ref := range []string{
		"https://mail.proton.me/u/0/inbox",
		"https://mail.proton.me/eo/",
	} {
		refuses(t, 1, []string{"mail", "protected", "get", ref, "--eo-password-file", file},
			"not a password-protected message")
	}
}

// Such a link at the mailbox is an ordinary mistake - it is what somebody has
// in front of them - so it is answered with the command that opens it.
func TestSuchALinkTypedAtTheMailboxPointsAtTheCommandThatOpensIt(t *testing.T) {
	refuses(t, 3, []string{"mail", "messages", "get", "https://mail.proton.me/eo/9fK2pQ7xNv4mB8"},
		"not one in your mailbox", "proton mail protected get")
}
