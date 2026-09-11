package skip

import (
	"context"
	"testing"
)

func TestNothingSkippedIsNothingToSay(t *testing.T) {
	_, tally := With(context.Background())
	if tally.Count() != 0 || tally.Kind() != "" || tally.Hides() {
		t.Error("a fresh tally already has something to report")
	}
}

func TestASkipIsCountedAndNamed(t *testing.T) {
	ctx, tally := With(context.Background())
	Record(ctx, KindItem, "abc", Undecryptable, nil)
	Record(ctx, KindItem, "def", Malformed, nil)

	if tally.Count() != 2 {
		t.Errorf("counted %d, want 2", tally.Count())
	}
	if tally.Kind() != KindItem {
		t.Errorf("kind is %q, want %q", tally.Kind(), KindItem)
	}
	if tally.Hides() {
		t.Error("an item is not a container; nothing was hidden by losing it")
	}
}

func TestTheFirstKindStandsForAllOfThem(t *testing.T) {
	ctx, tally := With(context.Background())
	Record(ctx, KindContact, "abc", Undecryptable, nil)
	Record(ctx, KindKey, "def", Malformed, nil)

	if tally.Kind() != KindContact {
		t.Errorf("kind is %q, want the first one recorded; the log carries the breakdown", tally.Kind())
	}
	if tally.Count() != 2 {
		t.Errorf("counted %d, want 2", tally.Count())
	}
}

func TestAContainerSaysItTookThingsWithIt(t *testing.T) {
	for _, kind := range []Kind{KindFolder, KindShare, KindVault} {
		ctx, tally := With(context.Background())
		Record(ctx, kind, "abc", Unlockable, nil)
		if !tally.Hides() {
			t.Errorf("%q went missing and nothing says its contents did too", kind)
		}
	}
}

func TestALockedKeyIsCountedApart(t *testing.T) {
	ctx, tally := With(context.Background())
	Record(ctx, KindMessage, "abc", Locked, nil)
	Record(ctx, KindMessage, "def", Undecryptable, nil)

	if tally.Count() != 2 {
		t.Errorf("counted %d, want 2", tally.Count())
	}
	if tally.Locked() != 1 {
		t.Errorf("counted %d sealed to a locked key, want 1", tally.Locked())
	}
	var none *Tally
	if none.Locked() != 0 {
		t.Error("a nil tally claims something was sealed to a locked key")
	}
}

func TestRecordingWithNobodyCountingIsHarmless(t *testing.T) {
	Record(context.Background(), KindItem, "abc", Undecryptable, nil)
}

func TestANilTallyAnswersForItself(t *testing.T) {
	var tally *Tally
	if tally.Count() != 0 || tally.Kind() != "" || tally.Hides() {
		t.Error("a nil tally claims something went missing")
	}
}

func TestEveryHidingKindIsAKind(t *testing.T) {
	known := map[Kind]bool{}
	for _, k := range []Kind{
		KindAddress, KindCalendar, KindContact, KindEvent, KindFolder, KindInvitation,
		KindItem, KindKey, KindMember, KindProfile, KindReminder, KindShare, KindVault,
		KindVolume,
	} {
		known[k] = true
	}
	for k := range Hides {
		if !known[k] {
			t.Errorf("%q hides its contents but is not one of the declared kinds", k)
		}
	}
}
