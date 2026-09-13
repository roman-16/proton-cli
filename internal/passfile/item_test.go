package passfile

import (
	"strings"
	"testing"

	"github.com/roman-16/proton-cli/internal/otp"
)

// Pass keeps the address and the user name apart, and every other manager keeps
// whichever the site asked for in one column.
func TestWhatASingleIdentifierIs(t *testing.T) {
	cases := []struct{ value, email, username string }{
		{"jane@example.test", "jane@example.test", ""},
		{"jane.doe@sub.example.co.uk", "jane.doe@sub.example.co.uk", ""},
		{"jane", "", "jane"},
		{"@nobody", "", "@nobody"},
		{"jane@localhost", "", "jane@localhost"},
		{"jane doe@example.test", "", "jane doe@example.test"},
		{"", "", ""},
	}
	for _, c := range cases {
		email, username := identifier(c.value)
		if email != c.email || username != c.username {
			t.Errorf("%q split into (%q, %q), want (%q, %q)",
				c.value, email, username, c.email, c.username)
		}
	}
}

// An item with no name of its own is called after the site it is for.
func TestAnItemWithNoNameIsCalledAfterItsSite(t *testing.T) {
	cases := []struct{ name, url, want string }{
		{"GitHub", "https://github.com/login", "GitHub"},
		{"", "https://github.com/login", "github.com"},
		{"", "github.com", "github.com"},
		{"", "", "Unnamed item"},
		{"  ", "", "Unnamed item"},
	}
	for _, c := range cases {
		entry := item{Name: c.name, URLs: []string{c.url}}.login()
		if entry.Name != c.want {
			t.Errorf("a login called %q for %q came back as %q", c.name, c.url, entry.Name)
		}
	}
}

// Every kind has a name of its own when the file gave it none.
func TestEveryKindOfItemHasAName(t *testing.T) {
	empty := item{}
	for _, entry := range []Entry{
		empty.note(), empty.card(), empty.identity(),
		empty.sshKey(), empty.wifi(), empty.custom(),
	} {
		if !strings.HasPrefix(entry.Name, "Unnamed ") {
			t.Errorf("a %s with no name came back as %q", entry.Kind, entry.Name)
		}
	}
}

// Managers export either a whole URI or the bare secret, and Pass stores a URI.
func TestWhatASecretBecomes(t *testing.T) {
	uri := "otpauth://totp/GitHub:jane?secret=JBSWY3DPEHPK3PXP&period=30"
	if got := totpURI(uri, "x"); got != uri {
		t.Errorf("a URI came back as %q", got)
	}

	got := totpURI("jbsw y3dp-ehpk_3pxp", "GitHub")
	if got == "" {
		t.Fatal("a bare secret was dropped")
	}
	secret, err := otp.Parse(got)
	if err != nil {
		t.Fatalf("what came out is not a secret: %v", err)
	}
	if secret.Label != "GitHub" {
		t.Errorf("the code is labelled %q", secret.Label)
	}
	for _, want := range []string{"secret=JBSWY3DPEHPK3PXP", "digits=6", "period=30"} {
		if !strings.Contains(got, want) {
			t.Errorf("%q leaves out %q", got, want)
		}
	}

	// Something that is not a secret at all would never produce a code, so it is
	// not stored as one.
	for _, value := range []string{"", "   ", "not a secret at all!"} {
		if got := totpURI(value, "x"); got != "" {
			t.Errorf("%q was stored as %q", value, got)
		}
	}
}

// The same address twice is one address.
func TestTheSameAddressTwiceIsOne(t *testing.T) {
	entry := item{Name: "x", URLs: []string{
		"https://example.test", "https://example.test", "", "  ", "https://other.test",
	}}.login()
	if got := LoginURLs(entry.Item.GetContent().GetLogin()); len(got) != 2 {
		t.Errorf("the URLs came back as %v", got)
	}
}

// A field with nothing in it at all is not a field.
func TestAnEmptyFieldIsNotKept(t *testing.T) {
	entry := item{Name: "x", Fields: []Field{
		{Name: "", Value: ""},
		{Name: "Ticket", Value: "T-1"},
		{Name: "Blank", Value: ""},
	}}.note()
	if got := entry.Item.GetExtraFields(); len(got) != 2 {
		t.Errorf("the fields came back as %v", got)
	}
}
