package offline

import (
	"os"
	"path/filepath"
	"testing"
)

// What opens keys a password reset locked arrives on the command line, so
// everything about it that the command line settles is settled here: which of
// the three ways in was chosen, whether the recovery file is there, and whether
// a recovery phrase is one. A run that got as far as Proton before saying any of
// this would have asked for a password on the way.

// The three ways in are one choice. Naming two leaves it unsaid which secret
// would have been used, so it is refused rather than resolved.
func TestOneWayOfRecoveringAtATime(t *testing.T) {
	file := filepath.Join(t.TempDir(), "proton_recovery.asc")
	if err := os.WriteFile(file, []byte("not a recovery file"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"--recovery-phrase", "--recovery-file", file},
		{"--recovery-phrase", "--previous-password-file", file},
		{"--recovery-file", file, "--previous-password-file", file},
		{"--recovery-phrase-file", file, "--recovery-phrase-stdin"},
		{"--previous-password-file", file, "--previous-password-stdin"},
	} {
		refuses(t, 1, append([]string{"account", "keys", "reactivate"}, args...),
			"are set none of the others can be")
	}
}

// A recovery phrase is twelve words from a list, and each way of getting it
// wrong is told apart: the count, a word that is on no list, and a checksum that
// says one word was swapped for another.
func TestARecoveryPhraseIsJudgedBeforeTheNetwork(t *testing.T) {
	for _, tc := range []struct{ phrase, want string }{
		{phrase: "zoo zoo zoo", want: "A recovery phrase is twelve words."},
		{
			phrase: "zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo proton",
			want:   "not one a recovery phrase can hold",
		},
		{
			phrase: "zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo",
			want:   "one of its words is wrong",
		},
	} {
		refuses(t, 1, []string{"account", "keys", "reactivate",
			"--recovery-phrase-file", secretFile(t, tc.phrase)}, tc.want)
	}
}

// A recovery file that is not there is the command line's mistake, and is
// answered before a password is asked for or a request is sent.
func TestARecoveryFileThatIsNotThereIsRefused(t *testing.T) {
	refuses(t, 1, []string{"account", "keys", "reactivate",
		"--recovery-file", filepath.Join(t.TempDir(), "nope.asc")}, "Could not read")
}

// secretFile writes something a flag reads a secret from.
func secretFile(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
