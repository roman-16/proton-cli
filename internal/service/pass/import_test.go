package pass

import (
	"strings"
	"testing"

	"github.com/roman-16/proton-cli/internal/passfile"
)

func vault(name string, items ...passfile.Entry) passfile.Vault {
	return passfile.Vault{Name: name, Items: items}
}

func entry(name string) passfile.Entry {
	return passfile.Entry{Name: name, Kind: "login"}
}

// A file that names no vault - which is most of them - lands in the vault the
// app opens on.
func TestAFileWithNoVaultsLandsInTheFirstOne(t *testing.T) {
	doc := &passfile.Document{Vaults: []passfile.Vault{vault("", entry("GitHub"))}}
	existing := []Vault{{ShareID: "s-1", Name: "Personal"}, {ShareID: "s-2", Name: "Work"}}

	placed, err := (&Service{}).placeVaults(t.Context(), doc, existing, "")
	if err != nil {
		t.Fatalf("placeVaults: %v", err)
	}
	if len(placed) != 1 {
		t.Fatalf("the file was placed in %d vaults", len(placed))
	}
	if placed[0].ShareID != "s-1" || placed[0].New {
		t.Errorf("it landed in %v", placed[0])
	}
}

// A vault the account already has is the one it lands in, and one it does not
// have is made.
func TestAVaultInTheFileIsMatchedByName(t *testing.T) {
	doc := &passfile.Document{Vaults: []passfile.Vault{
		vault("Work", entry("GitLab")),
		vault("Archive", entry("Old")),
		vault("Work", entry("Jira")),
	}}
	existing := []Vault{{ShareID: "s-1", Name: "Personal"}, {ShareID: "s-2", Name: "Work"}}

	placed, err := (&Service{}).placeVaults(t.Context(), doc, existing, "")
	if err != nil {
		t.Fatalf("placeVaults: %v", err)
	}
	if len(placed) != 2 {
		t.Fatalf("the file was placed in %d vaults", len(placed))
	}
	if placed[0].ShareID != "s-2" || placed[0].New {
		t.Errorf("Work landed in %v", placed[0])
	}
	// The same vault named twice is one vault.
	if len(placed[0].Items) != 2 {
		t.Errorf("Work came back with %d items", len(placed[0].Items))
	}
	if !placed[1].New || placed[1].Name != "Archive" {
		t.Errorf("Archive landed in %v", placed[1])
	}
}

// The plan's vault limit is known before anything is sent, so the items of a
// vault that will not fit are named in the dry run rather than refused one by
// one at the end.
func TestAVaultBeyondThePlansLimitIsNamedUpFront(t *testing.T) {
	two := 2
	limits := &Limits{Vaults: &two}
	existing := []Vault{{ShareID: "s-1", Name: "Personal"}, {ShareID: "s-2", Name: "Work"}}
	if got := vaultRoom(existing, limits); got != 0 {
		t.Errorf("an account at its limit has room for %d more", got)
	}

	skipped := vaultOverLimit(placedVault{
		Name: "Archive", New: true, Items: []passfile.Entry{entry("Old"), entry("Older")},
	}, existing, limits)
	if len(skipped) != 2 {
		t.Fatalf("%d items were named, want 2", len(skipped))
	}
	if !strings.Contains(skipped[0].Reason, "allows 2 vaults") {
		t.Errorf("the reason is %q", skipped[0].Reason)
	}
}

// A paid plan has no vault limit, and the account is not held to one.
func TestAPlanWithNoVaultLimitHasRoom(t *testing.T) {
	existing := []Vault{{ShareID: "s-1", Name: "Personal"}}
	if got := vaultRoom(existing, &Limits{}); got <= 0 {
		t.Errorf("a plan with no limit has room for %d more vaults", got)
	}
}

// An alias is an address Proton owns. The only one that can come back is one
// this account made and has since deleted.
func TestWhichAliasesCanComeBack(t *testing.T) {
	alias := func(address string) passfile.Entry {
		return passfile.Entry{Name: address, Kind: "alias", AliasEmail: address}
	}
	held := map[string]bool{"kept@passmail.com": true}

	cases := []struct {
		what           string
		entry          passfile.Entry
		fileUser, user string
		want           string
	}{
		{"an alias this account made and deleted", alias("gone@passmail.com"), "u-1", "u-1", ""},
		{"an alias this account still holds", alias("kept@passmail.com"), "u-1", "u-1", "already holds"},
		{"an alias from another account", alias("other@passmail.com"), "u-2", "u-1", "belongs to the account"},
		{"an alias from a file that names no account", alias("x@passmail.com"), "", "u-1", "belongs to the account"},
		{"an alias with no address", passfile.Entry{Kind: "alias"}, "u-1", "u-1", "does not say which address"},
		{"anything that is not an alias", entry("GitHub"), "u-2", "u-1", ""},
	}
	for _, c := range cases {
		got := refusedEntry(c.entry, c.fileUser, c.user, held)
		switch {
		case c.want == "" && got != "":
			t.Errorf("%s was refused: %s", c.what, got)
		case c.want != "" && !strings.Contains(got, c.want):
			t.Errorf("%s was refused with %q, which does not say %q", c.what, got, c.want)
		}
	}
}

// Each way an attachment is refused is known before anything is sent.
func TestWhichAttachmentsTravelWithAnImport(t *testing.T) {
	file := func(name string, size int64) passfile.File {
		return passfile.File{Name: name, Size: size}
	}
	paid := StorageLimits{Allowed: true, MaxFileSize: 100 << 20}

	cases := []struct {
		what   string
		limits StorageLimits
		files  []passfile.File
		want   string
	}{
		{"a free plan", StorageLimits{}, []passfile.File{file("a.pdf", 1024)}, "paid Pass plan"},
		{"a file over the limit", paid, []passfile.File{file("big.tiff", 200<<20)}, "at most"},
		{"an empty file", paid, []passfile.File{file("empty.pdf", 0)}, "it is empty"},
		{"a file the plan takes", paid, []passfile.File{file("a.pdf", 1024)}, ""},
	}
	for _, c := range cases {
		files, skipped := plannedFiles(passfile.Entry{Name: "x", Files: c.files}, c.limits)
		switch {
		case c.want == "" && len(files) != 1:
			t.Errorf("%s did not travel: %v", c.what, skipped)
		case c.want != "" && len(skipped) != 1:
			t.Errorf("%s was taken", c.what)
		case c.want != "" && !strings.Contains(skipped[0].Reason, c.want):
			t.Errorf("%s was refused with %q, which does not say %q", c.what, skipped[0].Reason, c.want)
		}
	}
}
