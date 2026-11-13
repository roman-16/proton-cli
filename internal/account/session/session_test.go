package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/roman-16/proton-cli/internal/profile"
)

func TestUnlockedTracksTheSealedKeyPassword(t *testing.T) {
	if (&Session{UID: "u"}).Unlocked() {
		t.Error("a session with no blob is not unlocked")
	}
	if !(&Session{UID: "u", EncKeyBlob: "blob"}).Unlocked() {
		t.Error("a session with a blob is unlocked")
	}
	var none *Session
	if none.Unlocked() {
		t.Error("a missing session is not unlocked")
	}
}

func TestPathInStaysOneElementUnderTheSessionDirectory(t *testing.T) {
	d := t.TempDir()
	for _, tc := range []struct{ in, want string }{
		{"work", "work.json"},
		{"", "default.json"},
		{"default", "default.json"},
		{"my-work.2", "my-work.2.json"},
	} {
		name, err := profile.Parse(tc.in)
		if err != nil {
			t.Fatalf("profile.Parse(%q): %v", tc.in, err)
		}
		got := pathIn(d, name, ".json")
		want := filepath.Join(d, "sessions", tc.want)
		if got != want {
			t.Errorf("pathIn(%q) = %q, want %q", tc.in, got, want)
		}
	}
}

func TestSessionMarshalUsesEncKeyBlob(t *testing.T) {
	b, err := json.Marshal(Session{UID: "u", AccessToken: "a", RefreshToken: "r", EncKeyBlob: "blob"})
	if err != nil {
		t.Fatal(err)
	}
	out := string(b)
	if !strings.Contains(out, `"enc_key_blob":"blob"`) {
		t.Errorf("expected enc_key_blob in JSON, got: %s", out)
	}
}

func TestSaveReplacesTheFileRatherThanRewritingIt(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	name, err := profile.Parse("primary")
	if err != nil {
		t.Fatal(err)
	}

	if err := Save(name, &Session{UID: "uid", AccessToken: "first", RefreshToken: "r1", BaseURL: base}); err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := range 200 {
			_ = Save(name, &Session{
				UID:          "uid",
				AccessToken:  fmt.Sprintf("token-%d", i),
				RefreshToken: fmt.Sprintf("refresh-%d", i),
				BaseURL:      base,
			})
		}
	}()
	for {
		select {
		case <-done:
			return
		default:
		}
		got, err := Load(name)
		if err != nil {
			t.Fatalf("Load while saving: %v", err)
		}
		if got == nil {
			t.Fatal("Load found no session while one was being saved")
		}
		if got.UID != "uid" || got.AccessToken == "" || got.RefreshToken == "" {
			t.Fatalf("Load saw a partial session: %+v", got)
		}
	}
}

func TestSaveLeavesNoLeftoverFiles(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	name, err := profile.Parse("primary")
	if err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if err := Save(name, &Session{UID: "uid", AccessToken: "a", RefreshToken: "r"}); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := os.ReadDir(filepath.Join(dir, "proton-cli", "sessions"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("the sessions directory holds %v, want just the session file", names)
	}
}

func TestSavedSessionIsReadableOnlyByItsOwner(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	name, err := profile.Parse("primary")
	if err != nil {
		t.Fatal(err)
	}
	if err := Save(name, &Session{UID: "uid", AccessToken: "a", RefreshToken: "r"}); err != nil {
		t.Fatal(err)
	}
	p, err := Path(name)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("mode = %o, want 600", perm)
	}
}

const base = "https://mail.proton.me/api"

func TestLoadTellsAMissingSessionFromADamagedOne(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	name := profile.MustParse("primary")

	if s, err := Load(name); s != nil || err != nil {
		t.Fatalf("Load with no file = %v, %v; want nothing and no error", s, err)
	}

	p, err := Path(name)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{
		"garbage",
		`{"uid":"u","access_token":"a","refresh_token":"r"}`,
		`{"uid":"u","access_token":"a","base_url":"` + base + `"}`,
	} {
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(name); !errors.Is(err, ErrDamaged) {
			t.Errorf("Load(%s) = %v, want ErrDamaged", body, err)
		}
	}
}

func TestHoldLetsOneCommandAtATimeChangeASession(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	name := profile.MustParse("primary")

	release, err := Hold(context.Background(), name)
	if err != nil {
		t.Fatal(err)
	}
	second := make(chan struct{})
	go func() {
		next, err := Hold(context.Background(), name)
		if err != nil {
			t.Error(err)
			return
		}
		next()
		close(second)
	}()
	select {
	case <-second:
		t.Fatal("a second holder got in while the first still held the session")
	case <-time.After(3 * holdPoll):
	}
	release()
	select {
	case <-second:
	case <-time.After(time.Second):
		t.Fatal("the second holder never got in after the first let go")
	}
}

func TestHoldGivesUpWhenTheCommandDoes(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	name := profile.MustParse("primary")

	release, err := Hold(context.Background(), name)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	ctx, cancel := context.WithTimeout(context.Background(), 2*holdPoll)
	defer cancel()
	if _, err := Hold(ctx, name); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Hold under a cancelled command = %v, want its deadline", err)
	}
}
