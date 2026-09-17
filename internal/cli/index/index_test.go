package index

import (
	"context"
	"errors"
	"testing"

	"github.com/roman-16/proton-cli/internal/progress"
	"github.com/roman-16/proton-cli/internal/search"
)

// What INDEXED says is what a search over that index would reach.
//
// A mailbox goes through two states that are not the same answer: still being
// read, where older mail is missing outright, and read but still downloading
// bodies, where every message is there and the text of the older ones is not.
func TestTheListSaysHowMuchOfAnAppASearchWouldReach(t *testing.T) {
	for _, tc := range []struct {
		name string
		st   search.Status
		want string
	}{
		{
			name: "a mailbox still being read",
			st:   search.Status{App: search.AppMail, Indexed: 12400, Total: 48213},
			want: "12400 of 48213 messages",
		},
		{
			name: "a mailbox whose bodies are still arriving",
			st:   search.Status{App: search.AppMail, Indexed: 10887, Total: 10887, Bodies: 2830, Complete: true},
			want: "10887 messages, 2830 bodies",
		},
		{
			name: "a mailbox that holds everything",
			st:   search.Status{App: search.AppMail, Indexed: 10887, Total: 10887, Bodies: 10887, Complete: true},
			want: "10887 messages",
		},
		{
			name: "an app whose things have no bodies to download",
			st:   search.Status{App: search.AppDrive, Indexed: 2422, Total: 2422, Complete: true},
			want: "2422 items",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := indexedCell(tc.st); got != tc.want {
				t.Errorf("INDEXED = %q, want %q", got, tc.want)
			}
		})
	}
}

// A run answers a feed that gave up rather than leaving it for the next one.
//
// Proton says outright when its history no longer covers the gap, and nothing
// after that ever says what happened in it. What settles it is reading the
// account, which is what a build does - so the run builds again instead of
// finishing with an index that is quietly missing whatever changed.
func TestARunReadsAgainWhenTheFeedCouldNotSayWhatChanged(t *testing.T) {
	session := &refreshingOnce{}
	got, err := run(context.Background(), session, progress.Nop{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if session.builds != 2 || session.syncs != 2 {
		t.Errorf("the run built %d times and caught up %d, want each twice", session.builds, session.syncs)
	}
	if got.Indexed != 3 {
		t.Errorf("run = %+v, want what both readings took in", got)
	}
}

// A feed that keeps refreshing is left to the next run rather than spun on.
func TestARunStopsReadingAgainRatherThanSpinning(t *testing.T) {
	session := &refreshingOnce{always: true}
	if _, err := run(context.Background(), session, progress.Nop{}); err != nil {
		t.Fatalf("run: %v", err)
	}
	if session.builds != reconciles {
		t.Errorf("the run built %d times, want it to stop at %d", session.builds, reconciles)
	}
}

// An index whose build fails says so rather than carrying on to the catch-up.
func TestARunStopsAtAFailedBuild(t *testing.T) {
	session := &refreshingOnce{fail: errors.New("the mailbox would not be read")}
	if _, err := run(context.Background(), session, progress.Nop{}); err == nil {
		t.Fatal("a failed build reported success")
	}
	if session.syncs != 0 {
		t.Errorf("the run caught up %d times after a failed build", session.syncs)
	}
}

// refreshingOnce is an index whose feed gives up the first time it is asked.
type refreshingOnce struct {
	builds, syncs int
	always        bool
	fail          error
}

func (s *refreshingOnce) Build(context.Context, progress.Sink) (search.Result, error) {
	if s.fail != nil {
		return search.Result{}, s.fail
	}
	s.builds++
	return search.Result{Indexed: s.builds}, nil
}

func (s *refreshingOnce) Sync(context.Context) (search.Result, error) {
	s.syncs++
	return search.Result{Refreshed: s.always || s.syncs == 1}, nil
}

func (s *refreshingOnce) Status() search.Status { return search.Status{} }
