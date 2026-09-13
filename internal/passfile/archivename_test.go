package passfile

import "testing"

// The identifiers a Proton account gives out, for the naming an archive does:
// the stamp a file's name carries is made of these, so a short stand-in would
// make a name the app never writes.
const (
	exampleShare = "zZ4c1dEXAMPLEshare=="
	exampleFile  = "kQ81mDx4EXAMPLEfile=="
)

// The name a file takes inside an archive is the app's own, because the archive
// is: what this writes, Proton Pass reads back.
func TestWhatAFileIsCalledInsideAnArchive(t *testing.T) {
	// The values are the app's own function's, run over the same arguments.
	cases := []struct{ name, want string }{
		{"passport.pdf", "passport.78e91fe938d5208c.pdf"},
		{"archive.tar.gz", "archive.tar.78e91fe938d5208c.gz"},
		{"receipts", "receipts.78e91fe938d5208c"},
	}
	for _, c := range cases {
		got := ArchiveEntry(exampleShare, exampleFile, c.name)
		if got != c.want {
			t.Errorf("ArchiveEntry(%q) = %q, want %q", c.name, got, c.want)
		}
	}
}

// What an archive renamed, an import names back.
func TestWhatAFileIsCalledAfterAnArchive(t *testing.T) {
	cases := []struct{ entry, want string }{
		{"Proton Pass/files/passport.78e91fe938d5208c.pdf", "passport.pdf"},
		{"passport.78e91fe938d5208c.pdf", "passport.pdf"},
		{"archive.tar.78e91fe938d5208c.gz", "archive.tar.gz"},
		// A file that had no extension of its own was given the stamp as one.
		{"receipts.78e91fe938d5208c", "receipts"},
		// Anything else keeps the name it has.
		{"plain.pdf", "plain.pdf"},
	}
	for _, c := range cases {
		if got := ArchiveEntryName(c.entry); got != c.want {
			t.Errorf("ArchiveEntryName(%q) = %q, want %q", c.entry, got, c.want)
		}
	}
}

// A name survives the round trip through an archive, whatever it holds.
func TestANameSurvivesTheArchive(t *testing.T) {
	for _, name := range []string{"passport.pdf", "archive.tar.gz", "a.b.c.txt", "receipts"} {
		entry := ArchiveEntry(exampleShare, exampleFile, name)
		if got := ArchiveEntryName(entry); got != name {
			t.Errorf("%q came back as %q (via %q)", name, got, entry)
		}
	}
}
