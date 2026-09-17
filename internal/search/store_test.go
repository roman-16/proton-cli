package search

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	pgp "github.com/ProtonMail/gopenpgp/v2/crypto"
)

// A store for a test: a temporary directory, one account, and a key of its own.
//
// The key is a real OpenPGP key rather than a stand-in, because how the index
// key is sealed is half of what this package is: a test that skipped it would
// leave the one thing worth checking unchecked.
func testStore(t *testing.T) *Store {
	t.Helper()
	key, err := pgp.GenerateKey("Test", "test@proton.me", "x25519", 0)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	ring, err := pgp.NewKeyRing(key)
	if err != nil {
		t.Fatalf("key ring: %v", err)
	}
	return New(t.TempDir(), func() string { return "user-1" }, func(context.Context) (Keys, error) {
		return Keys{Seal: ring, Open: ring}, nil
	})
}

func record(t *testing.T, id, body string) Record {
	t.Helper()
	data, err := json.Marshal(map[string]string{"body": body})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return Record{ID: id, Data: data}
}

func bodyOf(t *testing.T, r Record) string {
	t.Helper()
	var held map[string]string
	if err := json.Unmarshal(r.Data, &held); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return held["body"]
}

// What goes in comes back out, and the newest record about a thing is the one
// that answers for it.
func TestALogAnswersWithTheNewestRecordOfEachThing(t *testing.T) {
	s := testStore(t)
	log, err := s.Load(t.Context(), AppMail)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if err := log.Append(record(t, "a", "first"), record(t, "b", "other")); err != nil {
		t.Fatalf("append: %v", err)
	}
	if err := log.Append(record(t, "a", "second")); err != nil {
		t.Fatalf("append again: %v", err)
	}
	if err := log.Save(1000); err != nil {
		t.Fatalf("save: %v", err)
	}

	reopened, err := s.Load(t.Context(), AppMail)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got := len(reopened.Records()); got != 2 {
		t.Fatalf("records = %d, want 2", got)
	}
	got, ok := reopened.Get("a")
	if !ok {
		t.Fatal(`"a" is not in the reopened log`)
	}
	if body := bodyOf(t, got); body != "second" {
		t.Errorf("body = %q, want the newest record's %q", body, "second")
	}
}

// A thing the account no longer has leaves the index, and stays gone when the
// log is read again.
func TestATombstoneTakesAThingOutOfTheIndex(t *testing.T) {
	s := testStore(t)
	log, err := s.Load(t.Context(), AppMail)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if err := log.Append(record(t, "a", "first"), record(t, "b", "other")); err != nil {
		t.Fatalf("append: %v", err)
	}
	if err := log.Append(Record{ID: "a", Gone: true}); err != nil {
		t.Fatalf("tombstone: %v", err)
	}
	if err := log.Save(1000); err != nil {
		t.Fatalf("save: %v", err)
	}

	reopened, err := s.Load(t.Context(), AppMail)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if reopened.Has("a") {
		t.Error(`"a" is still in the index after a tombstone`)
	}
	if !reopened.Has("b") {
		t.Error(`"b" went with it`)
	}
}

// A run that stopped between a write and its end leaves a frame that is not
// whole. Everything in front of it is still the index, and the next write goes
// over the torn tail rather than behind it.
func TestATornTailIsReadAsTheEndAndWrittenOver(t *testing.T) {
	s := testStore(t)
	log, err := s.Load(t.Context(), AppMail)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if err := log.Append(record(t, "a", "first"), record(t, "b", "other")); err != nil {
		t.Fatalf("append: %v", err)
	}
	if err := log.Save(1000); err != nil {
		t.Fatalf("save: %v", err)
	}

	path := filepath.Join(s.Dir(), "mail.bin")
	whole, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	half := make([]byte, 8)
	binary.BigEndian.PutUint32(half, 4096)
	if err := os.WriteFile(path, append(whole, half...), 0600); err != nil {
		t.Fatalf("tear: %v", err)
	}

	torn, err := s.Load(t.Context(), AppMail)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got := len(torn.Records()); got != 2 {
		t.Fatalf("records = %d, want the 2 written before the tear", got)
	}
	if torn.lost == 0 {
		t.Error("a torn tail should be reported as bytes that did not read back")
	}
	if torn.State.Stale {
		t.Error("a torn tail is an interrupted write, not a reason to read the account again")
	}
	if err := torn.Append(record(t, "c", "third")); err != nil {
		t.Fatalf("append after a tear: %v", err)
	}
	again, err := s.Load(t.Context(), AppMail)
	if err != nil {
		t.Fatalf("reload again: %v", err)
	}
	if got := len(again.Records()); got != 3 {
		t.Errorf("records = %d, want 3: a write after a tear has to be readable", got)
	}
}

// A record that went bad where it lay is not a torn tail: the records after it
// are as good as they were and are read on, the index says it owes a reading of
// the account for the one it lost, and the first write leaves a file without
// the bad frame in it.
func TestARecordThatWentBadIsSkippedAndOwed(t *testing.T) {
	for _, tc := range []struct {
		name  string
		spoil func(frame []byte)
	}{
		{name: "in its contents", spoil: func(frame []byte) { frame[frameLen+len(frame)/2] ^= 0xff }},
		{name: "in its length", spoil: func(frame []byte) { frame[0] ^= 0xff }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := testStore(t)
			log, err := s.Load(t.Context(), AppMail)
			if err != nil {
				t.Fatalf("load: %v", err)
			}
			if err := log.Append(record(t, "a", "first"), record(t, "b", "second"), record(t, "c", "third")); err != nil {
				t.Fatalf("append: %v", err)
			}
			if err := log.Save(1000); err != nil {
				t.Fatalf("save: %v", err)
			}

			path := filepath.Join(s.Dir(), "mail.bin")
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			start, end := frameAt(data, 1)
			tc.spoil(data[start:end])
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatalf("spoil: %v", err)
			}

			spoiled, err := s.Load(t.Context(), AppMail)
			if err != nil {
				t.Fatalf("reload: %v", err)
			}
			if !spoiled.State.Stale {
				t.Error("a record that would not open should leave the index owing a reading of the account")
			}
			if spoiled.Has("b") {
				t.Error("the record that went bad still answers")
			}
			if !spoiled.Has("a") {
				t.Error("the record before the bad one went with it")
			}
			if tc.name == "in its contents" && !spoiled.Has("c") {
				t.Error("the record after the bad one was thrown away with it")
			}

			if err := spoiled.Save(1001); err != nil {
				t.Fatalf("save after a bad record: %v", err)
			}
			mended, err := s.Load(t.Context(), AppMail)
			if err != nil {
				t.Fatalf("reload after mending: %v", err)
			}
			if mended.corrupt != 0 || mended.lost != 0 {
				t.Errorf("after a write the file still holds %d bad frames and %d stray bytes", mended.corrupt, mended.lost)
			}
			if got, want := len(mended.Records()), len(spoiled.Records()); got != want {
				t.Errorf("records = %d after mending, want the %d that read back", got, want)
			}
			if !mended.State.Stale {
				t.Error("mending the file is not reading the account: the index still owes one")
			}
		})
	}
}

// frameAt is where the n-th frame of a log begins and ends.
func frameAt(data []byte, n int) (int, int) {
	at := 0
	for range n {
		at += frameLen + int(binary.BigEndian.Uint32(data[at:at+frameLen]))
	}
	return at, at + frameLen + int(binary.BigEndian.Uint32(data[at:at+frameLen]))
}

// An index is sealed to the account it was built for. Another account's keys
// open none of it, and the record says whose it is so that the refusal is a
// sentence rather than a decryption failure per record.
func TestAnIndexBelongsToTheAccountThatBuiltIt(t *testing.T) {
	s := testStore(t)
	log, err := s.Load(t.Context(), AppMail)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if err := log.Append(record(t, "a", "first")); err != nil {
		t.Fatalf("append: %v", err)
	}
	if err := log.Save(1000); err != nil {
		t.Fatalf("save: %v", err)
	}

	somebodyElse := New(s.Dir(), func() string { return "user-2" }, s.unlock)
	if _, err := somebodyElse.Status(AppMail); err != ErrOtherAccount {
		t.Errorf("status for another account = %v, want %v", err, ErrOtherAccount)
	}
	if _, err := somebodyElse.Load(t.Context(), AppMail); err != ErrOtherAccount {
		t.Errorf("load for another account = %v, want %v", err, ErrOtherAccount)
	}
}

// Nothing about a thing is written outside the ciphertext. The index key is not
// in the clear either: what is beside the log is counts and a cursor.
func TestNothingAboutAThingIsWrittenInTheClear(t *testing.T) {
	s := testStore(t)
	log, err := s.Load(t.Context(), AppMail)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if err := log.Append(
		record(t, "message-id-in-the-clear", "a body nobody else may read"),
		Record{ID: "deleted-message-id", Gone: true},
	); err != nil {
		t.Fatalf("append: %v", err)
	}
	log.State.Cursor = "event-cursor"
	if err := log.Save(1000); err != nil {
		t.Fatalf("save: %v", err)
	}

	for _, name := range []string{"mail.bin", "mail.json", "key"} {
		data, err := os.ReadFile(filepath.Join(s.Dir(), name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		for _, secret := range []string{"message-id-in-the-clear", "deleted-message-id", "a body nobody else may read"} {
			if bytes.Contains(data, []byte(secret)) {
				t.Errorf("%s holds %q in the clear", name, secret)
			}
		}
	}
}

// Deleting the last index takes the key with it: a key that opens nothing is a
// key nothing should keep.
func TestDeletingTheLastIndexTakesTheKey(t *testing.T) {
	s := testStore(t)
	log, err := s.Load(t.Context(), AppMail)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if err := log.Append(record(t, "a", "first")); err != nil {
		t.Fatalf("append: %v", err)
	}
	if err := log.Save(1000); err != nil {
		t.Fatalf("save: %v", err)
	}
	if err := s.Delete(AppMail); err != nil {
		t.Fatalf("delete: %v", err)
	}
	for _, name := range []string{"mail.bin", "mail.json", "key"} {
		if _, err := os.Stat(filepath.Join(s.Dir(), name)); !os.IsNotExist(err) {
			t.Errorf("%s survived the removal", name)
		}
	}
	indexes, err := s.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(indexes) != 0 {
		t.Errorf("indexes = %d, want none", len(indexes))
	}
}

// Only one run writes at a time, and it says so rather than waiting: whatever
// holds the directory is keeping the index current anyway.
func TestOnlyOneRunWritesAtATime(t *testing.T) {
	s := testStore(t)
	held, err := s.Claim()
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	other := New(s.Dir(), s.account, s.unlock)
	if _, err := other.Claim(); err != ErrBusy {
		t.Errorf("second claim = %v, want %v", err, ErrBusy)
	}
	held.Release()
	again, err := other.Claim()
	if err != nil {
		t.Fatalf("claim after release: %v", err)
	}
	again.Release()
}

// Removing one index leaves the others alone, and leaves the key that opens
// them where it is.
//
// The key is the directory's rather than the index's, so taking it away with
// one app's log would leave the others as records nothing can read.
func TestDeletingOneIndexLeavesTheOthers(t *testing.T) {
	s := testStore(t)
	for _, app := range []App{AppCalendar, AppDrive, AppMail} {
		log, err := s.Load(t.Context(), app)
		if err != nil {
			t.Fatalf("load %s: %v", app, err)
		}
		if err := log.Append(record(t, "a", "first")); err != nil {
			t.Fatalf("append %s: %v", app, err)
		}
		if err := log.Save(1000); err != nil {
			t.Fatalf("save %s: %v", app, err)
		}
	}
	if err := s.Delete(AppDrive); err != nil {
		t.Fatalf("delete: %v", err)
	}
	left, err := s.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(left) != 2 {
		t.Fatalf("indexes left = %d, want the two that were not named", len(left))
	}
	if _, err := os.Stat(filepath.Join(s.Dir(), "key")); err != nil {
		t.Errorf("the key that opens the others went with it: %v", err)
	}
	for _, name := range []string{"mail.bin", "calendar.bin"} {
		if _, err := os.Stat(filepath.Join(s.Dir(), name)); err != nil {
			t.Errorf("%s went with it: %v", name, err)
		}
	}
}
