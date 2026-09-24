package offline

import (
	"os"
	"path/filepath"
	"testing"

	pgp "github.com/ProtonMail/gopenpgp/v2/crypto"
)

// What a change to a key, an export or an import is given on the command line is
// judged there: a change that names nothing, marks that contradict each other, a
// passphrase too short to lock a file with, and a file whose keys do not open.
// A script that got any of these wrong hears so before it signs in.

func TestAKeyChangeThatSaysNothingIsRefused(t *testing.T) {
	refuses(t, 1, []string{"account", "keys", "update", "7Hn2Lw0R"}, "Nothing to change.")
}

func TestAKeyIsNotMadeUnprimaryByName(t *testing.T) {
	refuses(t, 1, []string{"account", "keys", "update", "7Hn2Lw0R", "--primary=false"},
		"stops being primary when another key is made primary")
}

func TestMarksThatContradictEachOtherAreRefused(t *testing.T) {
	refuses(t, 1, []string{"account", "keys", "update", "7Hn2Lw0R", "--compromised", "--obsolete=false"},
		"contradict each other")
	refuses(t, 1, []string{"account", "keys", "update", "7Hn2Lw0R", "--primary", "--compromised"},
		"contradict each other")
}

func TestAPublicExportTakesNoPassphrase(t *testing.T) {
	refuses(t, 1, []string{"account", "keys", "export", "7Hn2Lw0R",
		"--passphrase-file", secretFile(t, "a long enough passphrase")}, "has nothing to lock")
}

func TestAPrivateExportIsLockedWithEightCharactersOrMore(t *testing.T) {
	refuses(t, 1, []string{"account", "keys", "export", "7Hn2Lw0R", "--private",
		"--passphrase-file", secretFile(t, "short")}, "at least eight characters")
}

func TestAPrivateExportWithNobodyToAskNamesTheFlag(t *testing.T) {
	refuses(t, 1, []string{"account", "keys", "export", "7Hn2Lw0R", "--private"},
		"A passphrase is required to lock the exported key.", "--passphrase-file")
}

func TestAnImportReadsOneThingFromStandardInput(t *testing.T) {
	refuses(t, 1, []string{"account", "keys", "import", "me@proton.me", "-", "--passphrase-file", "-"},
		"both read standard input")
}

func TestAnImportOfAPublicKeyIsRefused(t *testing.T) {
	key := generatedKey(t)
	public, err := key.GetArmoredPublicKey()
	if err != nil {
		t.Fatal(err)
	}
	refuses(t, 1, []string{"account", "keys", "import", "me@proton.me", keyFile(t, "public.asc", public)},
		"holds no private key")
}

// One passphrase serves a run, so two files locked differently are two runs, and
// the refusal says so before anything is sent.
func TestAnImportOfKeysLockedDifferentlySaysToImportThemApart(t *testing.T) {
	first := lockedWith(t, "the first passphrase")
	second := lockedWith(t, "the second passphrase")
	refuses(t, 1, []string{"account", "keys", "import", "me@proton.me",
		keyFile(t, "first.asc", first), keyFile(t, "second.asc", second),
		"--passphrase-file", secretFile(t, "the first passphrase")},
		"second.asc did not open with that passphrase", "import each file on its own")
}

func generatedKey(t *testing.T) *pgp.Key {
	t.Helper()
	key, err := pgp.GenerateKey("me", "me@example.invalid", "x25519", 0)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func lockedWith(t *testing.T, passphrase string) string {
	t.Helper()
	locked, err := generatedKey(t).Lock([]byte(passphrase))
	if err != nil {
		t.Fatal(err)
	}
	armored, err := locked.Armor()
	if err != nil {
		t.Fatal(err)
	}
	return armored
}

func keyFile(t *testing.T, name, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
