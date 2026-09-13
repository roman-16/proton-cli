package passfile

import (
	"strings"
	"testing"
)

const keePassExport = `<?xml version="1.0" encoding="utf-8" standalone="yes"?>
<KeePassFile>
  <Meta><DatabaseName>Passwords</DatabaseName></Meta>
  <Root>
    <Group>
      <Name>Personal</Name>
      <Entry>
        <String><Key>Title</Key><Value>Proton</Value></String>
        <String><Key>UserName</Key><Value>jane@example.test</Value></String>
        <String><Key>Password</Key><Value ProtectInMemory="True">proton123</Value></String>
        <String><Key>URL</Key><Value>https://account.proton.me</Value></String>
        <String><Key>Notes</Key><Value>a note</Value></String>
        <String><Key>otp</Key><Value>otpauth://totp/x?secret=JBSWY3DPEHPK3PXP</Value></String>
        <String><Key>Ticket</Key><Value>T-1</Value></String>
        <String><Key>Recovery</Key><Value ProtectInMemory="True">abc</Value></String>
      </Entry>
      <Group>
        <Name>Work</Name>
        <Entry>
          <String><Key>Title</Key><Value>Legacy</Value></String>
          <String><Key>UserName</Key><Value>jane</Value></String>
          <String><Key>TOTP Seed</Key><Value>JBSWY3DPEHPK3PXP</Value></String>
          <String><Key>TOTP Settings</Key><Value>30;6</Value></String>
        </Entry>
      </Group>
    </Group>
  </Root>
</KeePassFile>
`

func keePassExportFile(t *testing.T) string {
	t.Helper()
	return written(t, "keepass.xml", []byte(keePassExport))
}

// Every group that holds entries becomes a vault, nested ones included.
func TestAKeePassExportIsRead(t *testing.T) {
	doc := opened(t, keePassExportFile(t), "keepass", nil)
	if len(doc.Vaults) != 2 {
		t.Fatalf("read %d vaults, want 2", len(doc.Vaults))
	}
	if names := vaultNames(doc); names[0] != "Personal" || names[1] != "Work" {
		t.Errorf("the vaults came back as %v", names)
	}
}

func TestAKeePassEntryIsRead(t *testing.T) {
	entry := itemNamed(t, opened(t, keePassExportFile(t), "keepass", nil), "Proton")
	login := entry.Item.GetContent().GetLogin()
	if login.GetItemEmail() != "jane@example.test" || login.GetPassword() != "proton123" {
		t.Errorf("the login came back as %v", login)
	}
	if login.GetTotpUri() == "" {
		t.Error("the one-time code did not come back")
	}
	if got := entry.Item.GetMetadata().GetNote(); got != "a note" {
		t.Errorf("the note came back as %q", got)
	}
}

// A field the database marked as protected comes back hidden.
func TestAKeePassProtectedFieldStaysHidden(t *testing.T) {
	entry := itemNamed(t, opened(t, keePassExportFile(t), "keepass", nil), "Proton")
	got := map[string]bool{}
	for _, f := range entry.Item.GetExtraFields() {
		got[f.GetFieldName()] = f.GetHidden() != nil
	}
	if len(got) != 2 {
		t.Fatalf("the custom fields came back as %v", got)
	}
	if got["Ticket"] {
		t.Error("a plain field came back hidden")
	}
	if !got["Recovery"] {
		t.Error("a protected field came back in the open")
	}
}

// An older plugin wrote the secret and its settings as two fields rather than a
// URI.
func TestAKeePassLegacyOneTimeCodeIsRead(t *testing.T) {
	entry := itemNamed(t, opened(t, keePassExportFile(t), "keepass", nil), "Legacy")
	got := entry.Item.GetContent().GetLogin().GetTotpUri()
	if got == "" {
		t.Fatal("the one-time code did not come back")
	}
	for _, want := range []string{"secret=JBSWY3DPEHPK3PXP", "period=30", "digits=6"} {
		if !strings.Contains(got, want) {
			t.Errorf("the URI %q leaves out %q", got, want)
		}
	}
}

func TestSomethingThatIsNotAKeePassExportIsRefused(t *testing.T) {
	path := written(t, "other.xml", []byte("<other><thing/></other>"))
	if _, err := Open(path, "keepass", nil); err == nil {
		t.Error("another XML file was read as a KeePass export")
	}
}
