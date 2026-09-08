package drive

import (
	"context"
	"strings"
	"testing"

	pgphelper "github.com/roman-16/proton-cli/internal/crypto/pgp"
	"github.com/roman-16/proton-cli/internal/proton"
)

func TestComposePassword(t *testing.T) {
	t.Run("generated only", func(t *testing.T) {
		full, flags, custom := composePassword("ABCDEFGHIJKL", LinkOptions{})
		if full != "ABCDEFGHIJKL" || flags != flagGeneratedPassword || custom != "" {
			t.Errorf("got (%q,%d,%q), want generated-only", full, flags, custom)
		}
	})
	t.Run("with custom", func(t *testing.T) {
		full, flags, custom := composePassword("ABCDEFGHIJKL", LinkOptions{SetPassword: true, CustomPassword: "s3cret"})
		if full != "ABCDEFGHIJKLs3cret" || flags != flagCustomAndGeneratedPassword || custom != "s3cret" {
			t.Errorf("got (%q,%d,%q), want generated+custom", full, flags, custom)
		}
	})
	t.Run("password flag set but empty stays generated-only", func(t *testing.T) {
		full, flags, custom := composePassword("ABCDEFGHIJKL", LinkOptions{SetPassword: true, CustomPassword: ""})
		if full != "ABCDEFGHIJKL" || flags != flagGeneratedPassword || custom != "" {
			t.Errorf("got (%q,%d,%q), want generated-only", full, flags, custom)
		}
	})
}

func TestPermFor(t *testing.T) {
	if permFor(true) != permEdit {
		t.Errorf("permFor(true) = %d, want %d", permFor(true), permEdit)
	}
	if permFor(false) != permView {
		t.Errorf("permFor(false) = %d, want %d", permFor(false), permView)
	}
}

func TestExpirationDuration(t *testing.T) {
	if got := expirationDuration(LinkOptions{}); got != nil {
		t.Errorf("no expiry should be nil, got %v", got)
	}
	if got := expirationDuration(LinkOptions{SetExpiry: true, ExpireSeconds: 0}); got != nil {
		t.Errorf("zero expiry should be nil, got %v", got)
	}
	if got := expirationDuration(LinkOptions{SetExpiry: true, ExpireSeconds: 604800}); got != 604800 {
		t.Errorf("expiry = %v, want 604800", got)
	}
}

func TestRandomPassword(t *testing.T) {
	p, err := randomPassword(generatedPasswordLen)
	if err != nil {
		t.Fatalf("randomPassword: %v", err)
	}
	if len(p) != generatedPasswordLen {
		t.Errorf("len = %d, want %d", len(p), generatedPasswordLen)
	}
	for _, r := range p {
		if !strings.ContainsRune(passwordCharset, r) {
			t.Errorf("password contains out-of-charset rune %q", r)
		}
	}
	q, _ := randomPassword(generatedPasswordLen)
	if p == q {
		t.Error("two random passwords are identical")
	}
}

func TestToShareLink(t *testing.T) {
	t.Run("appends generated fragment and reads edit bit", func(t *testing.T) {
		u := shareURLResp{PublicUrl: "https://drive.proton.me/urls/TOKEN", Permissions: permEdit}
		link := u.toShareLink("ABCDEFGHIJKL")
		if link.URL != "https://drive.proton.me/urls/TOKEN#ABCDEFGHIJKL" {
			t.Errorf("URL = %q", link.URL)
		}
		if !link.CanEdit {
			t.Error("CanEdit should be true for edit permission")
		}
	})
	t.Run("no fragment when generated empty", func(t *testing.T) {
		u := shareURLResp{PublicUrl: "https://drive.proton.me/urls/TOKEN", Permissions: permView}
		link := u.toShareLink("")
		if link.URL != "https://drive.proton.me/urls/TOKEN" {
			t.Errorf("URL = %q, want no fragment", link.URL)
		}
		if link.CanEdit {
			t.Error("CanEdit should be false for view permission")
		}
	})
}

// A key that cannot be read is the same answer as a signature that does not
// check out against it: not verified. Anything else invents a fifth verdict for
// a field whose values are the four.
//
// It is the answer a public link opened without an account gets for anything it
// does find an address on, because the key lookup is about the account and there
// is none.
func TestAnItemWhoseAuthorsKeyCannotBeReadIsUnverified(t *testing.T) {
	tree := newPublicTree(t, testURLPassword, "Q3-report.pdf", 2)
	s, doer := publicService(t, tree, proton.PublicLinkGeneratedPassword, nil)

	dc, err := s.OpenLink(context.Background(), LinkURL(testToken, testURLPassword), "")
	if err != nil {
		t.Fatalf("OpenLink: %v", err)
	}
	info, err := s.Info(context.Background(), dc, "/")
	if err != nil {
		t.Fatalf("Info: %v", err)
	}
	if info.Signature != string(pgphelper.Unverified) {
		t.Errorf("signature = %q, want %q", info.Signature, pgphelper.Unverified)
	}
	if !doer.sent("GET", "/core/v4/keys/all") {
		t.Error("the author's key was never asked for")
	}
}

func TestRoleLabel(t *testing.T) {
	if roleLabel(permView) != "viewer" {
		t.Errorf("roleLabel(view) = %q, want viewer", roleLabel(permView))
	}
	if roleLabel(permEdit) != "editor" {
		t.Errorf("roleLabel(edit) = %q, want editor", roleLabel(permEdit))
	}
}
