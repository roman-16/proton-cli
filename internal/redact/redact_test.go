package redact

import (
	"strings"
	"testing"
)

func TestAnUndeclaredNameCarriesNothing(t *testing.T) {
	r := New([]byte("a salt to test with"))
	if value, keep := r.Apply("subject", "Invoice #2291 is ready"); keep {
		t.Errorf("an undeclared name was written as %q; nothing may be", value)
	}
}

func TestADroppedNameCarriesNothing(t *testing.T) {
	Fields["testing-drop"] = Drop
	defer delete(Fields, "testing-drop")

	r := New([]byte("a salt to test with"))
	if value, keep := r.Apply("testing-drop", "anything at all"); keep {
		t.Errorf("a dropped name was written as %q", value)
	}
}

func TestAHandleIsStableAndSaysNothing(t *testing.T) {
	const id = "5bH2mQxKT9wLpN4vRs8kZc1yXd7fGh3jAe6bUi0oQm2nWr5tYv=="
	r := New([]byte("a salt to test with"))

	first, _ := r.Apply("item", id)
	again, _ := r.Apply("item", id)
	if first != again {
		t.Errorf("the same ID gave %q then %q; a handle has to be stable to be followed", first, again)
	}
	if strings.Contains(first, id[:8]) {
		t.Errorf("%q carries the ID it stands for", first)
	}
	if !strings.HasPrefix(first, "item:") {
		t.Errorf("%q does not say what kind of thing it stands for", first)
	}
}

func TestOneKindNeverBorrowsAnothersHandle(t *testing.T) {
	const id = "5bH2mQxKT9wLpN4vRs8kZc1yXd7fGh3jAe6bUi0oQm2nWr5tYv=="
	r := New([]byte("a salt to test with"))

	asShare, _ := r.Apply("share", id)
	asLink, _ := r.Apply("link", id)
	if strings.TrimPrefix(asShare, "share:") == strings.TrimPrefix(asLink, "link:") {
		t.Error("a share and a link with one ID read as the same thing")
	}
}

func TestTwoMachinesAgreeOnNothing(t *testing.T) {
	const id = "5bH2mQxKT9wLpN4vRs8kZc1yXd7fGh3jAe6bUi0oQm2nWr5tYv=="
	mine, kept := New([]byte("one machine's salt")).Apply("item", id)
	yours, _ := New([]byte("another machine's salt")).Apply("item", id)
	if !kept {
		t.Fatal("nothing was written at all; the test is comparing two absences")
	}
	if mine == yours {
		t.Error("two salts produced one handle; a handle would then be a name anybody could look up")
	}
}

func TestAnAddressKeepsOnlyWhatIdentifiesNobody(t *testing.T) {
	r := New([]byte("a salt to test with"))
	for _, c := range []struct{ in, domain string }{
		{"jane.roe@proton.me", "@proton.me"},
		{"jane.roe@pm.me", "@pm.me"},
		{"jane.roe@her-own-domain.at", "@elsewhere"},
		{"jane.roe@gmail.com", "@elsewhere"},
	} {
		got, _ := r.Apply("signer", c.in)
		if !strings.HasSuffix(got, c.domain) {
			t.Errorf("%s became %q, want it to end %q", c.in, got, c.domain)
		}
		if strings.Contains(got, "jane") || strings.Contains(got, "roe") {
			t.Errorf("%s became %q, which still names her", c.in, got)
		}
	}
}

func TestADomainWrittenInCapitalsIsTheSameDomain(t *testing.T) {
	r := New([]byte("a salt to test with"))
	if got, _ := r.Apply("signer", "jane.roe@PROTON.ME"); !strings.HasSuffix(got, "@proton.me") {
		t.Errorf("%q read a Proton domain as somebody's own", got)
	}
}

func TestAVerificationURLKeepsThePageAndLosesTheToken(t *testing.T) {
	r := New([]byte("a salt to test with"))
	got, _ := r.Apply("error",
		"solve https://verify.proton.me/?methods=captcha&token=eWLVJEM5Op3H5LcsY1cGyUxO")
	if strings.Contains(got, "eWLVJEM5Op3H5LcsY1cGyUxO") {
		t.Errorf("the token survived:\n%s", got)
	}
	if !strings.Contains(got, "verify.proton.me") {
		t.Errorf("the page it was pointing at was lost:\n%s", got)
	}
}

func TestARouteKeepsWhatItCalledAndLosesWhatItCalledItAbout(t *testing.T) {
	r := New([]byte("a salt to test with"))
	for _, c := range []struct{ in, want string }{
		{"/core/v4/users", "/core/v4/users"},
		{"/mail/v4/messages", "/mail/v4/messages"},
		{
			"/drive/shares/5bH2mQxKT9wLpN4vRs8kZc/links/9xL4pQrTz2mKd8vBn6cXs1",
			"/drive/shares/{id}/links/{id}",
		},
		{"/mail/v4/messages?PageSize=5&Keyword=divorce", "/mail/v4/messages"},
	} {
		if got, _ := r.Apply("path", c.in); got != c.want {
			t.Errorf("%s became %q, want %q", c.in, got, c.want)
		}
	}
}

func TestFreeTextLosesEverythingShaped(t *testing.T) {
	r := New([]byte("a salt to test with"))
	message := "upload /home/jane/Documents/tax return 2025.pdf: " +
		"encrypt for jane.roe@her-own-domain.at: " +
		"no key 5bH2mQxKT9wLpN4vRs8kZc1yXd7fGh3jAe6bUi0oQm2nWr5tYv=="

	got, _ := r.Apply("error", message)
	for _, leaked := range []string{
		"jane.roe", "her-own-domain.at", "tax return", "/home/jane",
		"5bH2mQxKT9wLpN4vRs8kZc1yXd7fGh3j",
	} {
		if strings.Contains(got, leaked) {
			t.Errorf("%q survived redaction:\n%s", leaked, got)
		}
	}
	if !strings.Contains(got, "upload") || !strings.Contains(got, "no key") {
		t.Errorf("the message lost what it was saying:\n%s", got)
	}
}

func TestRedactingTwiceChangesNothing(t *testing.T) {
	r := New([]byte("a salt to test with"))
	once, _ := r.Apply("error", "encrypt for jane.roe@proton.me failed")
	twice, _ := r.Apply("error", once)
	if once != twice {
		t.Errorf("redaction is not idempotent:\n%s\n%s", once, twice)
	}
}

func TestACountIsWrittenAsItStands(t *testing.T) {
	r := New([]byte("a salt to test with"))
	if got, keep := r.Apply("status", "404"); !keep || got != "404" {
		t.Errorf("a status became %q; the machinery's own numbers are what a report is read for", got)
	}
}

func TestEveryDeclaredNameHasAPolicyThatMeansSomething(t *testing.T) {
	for name, policy := range Fields {
		if name == "" {
			t.Error("a nameless attribute is declared")
		}
		if policy < Keep || policy > Drop {
			t.Errorf("%q is declared with a policy that is not one", name)
		}
	}
}

// A path segment is judged by the shape of a Proton ID, not by its length.
// Proton's own routes carry words longer than its shortest ID, so a threshold
// that catches the ID eats the endpoint - which is what the record is for.
func TestALongEndpointNameIsNotAnID(t *testing.T) {
	r := New([]byte("a salt to test with"))
	for _, path := range []string{
		"/mail/v4/incomingdefaults",
		"/mail/v4/incomingdefaults/delete",
		"/drive/shares/{id}/links/{id}/checkAvailableHashes",
		"/drive/v2/volumes/{id}/trash/restore_multiple",
		"/pass/v1/user/alias/settings/default_mailbox_id",
		"/pass/v1/share/{id}/alias/{id}/contact/{n}/blocked",
	} {
		if got, _ := r.Apply("path", path); got != path {
			t.Errorf("%s became %q; the endpoint is what the record is read for", path, got)
		}
	}
}

func TestEveryShapeOfIDLeavesAPath(t *testing.T) {
	r := New([]byte("a salt to test with"))
	for _, c := range []struct{ in, want string }{
		// 88 characters, padded: a message, a link, a share.
		{"/mail/v4/messages/5bH2mQxKT9wLpN4vRs8kZc1yXd7fGh3jAe6bUi0oQm2nWr5tYv8pLd3xTc9vBn6cXs1wYf5hJ3gA==",
			"/mail/v4/messages/{id}"},
		// 22 characters, unpadded.
		{"/pass/v1/share/5bH2mQxKT9wLpN4vRs8kZc/item", "/pass/v1/share/{id}/item"},
		// 32 lowercase alphanumerics.
		{"/core/v4/keys/0123456789abcdef0123456789abcdef", "/core/v4/keys/{id}"},
	} {
		if got, _ := r.Apply("path", c.in); got != c.want {
			t.Errorf("%s became %q, want %q", c.in, got, c.want)
		}
	}
}
