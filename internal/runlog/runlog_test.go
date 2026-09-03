package runlog

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestARunIsWrittenAndReadBack(t *testing.T) {
	dir := t.TempDir()
	run, err := Open(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	_, _ = fmt.Fprintf(run.Writer(), `{"time":%q,"run":%q,"command":"proton mail messages list"}`+"\n",
		time.Now().UTC().Format(time.RFC3339Nano), run.ID)
	_, _ = fmt.Fprintf(run.Writer(), `{"run":%q,"exit":7}`+"\n", run.ID)
	if err := run.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	entries, err := List(dir)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("read back %d runs, want 1", len(entries))
	}
	e := entries[0]
	if e.ID != run.ID {
		t.Errorf("id is %q, want %q", e.ID, run.ID)
	}
	if e.Command != "proton mail messages list" {
		t.Errorf("command is %q", e.Command)
	}
	if e.Exit != 7 || !e.Ended || !e.Failed() {
		t.Errorf("exit %d, ended %v, failed %v; want 7, true, true", e.Exit, e.Ended, e.Failed())
	}
	if len(e.Lines) != 2 {
		t.Errorf("kept %d records, want 2", len(e.Lines))
	}
}

func TestTheRunToReportIsTheOneThatFailed(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "2026-09-01.jsonl", `{"run":"aaaa","command":"a","exit":0}`)
	write(t, dir, "2026-09-02.jsonl", `{"run":"bbbb","command":"b","exit":7}`)
	write(t, dir, "2026-09-03.jsonl", `{"run":"cccc","command":"c","exit":0}`)

	got, ok := Latest(dir, "")
	if !ok {
		t.Fatal("found no run to report")
	}
	if got.ID != "bbbb" {
		t.Errorf("picked %q, want the failure bbbb rather than the newest run", got.ID)
	}
}

func TestWithNoFailureTheNewestRunIsTheOne(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "2026-09-01.jsonl", `{"run":"aaaa","exit":0}`)
	write(t, dir, "2026-09-02.jsonl", `{"run":"bbbb","exit":0}`)

	got, ok := Latest(dir, "")
	if !ok {
		t.Fatal("found no run to report")
	}
	if got.ID != "bbbb" {
		t.Errorf("picked %q, want the newest bbbb", got.ID)
	}
}

func TestTheRunDoingTheReportingIsNotTheSubject(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "2026-09-01.jsonl", `{"run":"aaaa","exit":0}`)
	write(t, dir, "2026-09-02.jsonl", `{"run":"bbbb","exit":0}`)

	got, ok := Latest(dir, "bbbb")
	if !ok {
		t.Fatal("found no run to report")
	}
	if got.ID != "aaaa" {
		t.Errorf("picked %q, want aaaa; the reporting run has nothing wrong with it", got.ID)
	}
}

func TestNoRunsIsNoAnswerRatherThanAnEmptyOne(t *testing.T) {
	if _, ok := Latest(t.TempDir(), ""); ok {
		t.Error("claimed to have found a run in an empty directory")
	}
}

func TestNothingButOurOwnFilesIsRead(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "salt", "not a log at all")
	write(t, dir, "2026-09-02.jsonl", `{"run":"good","exit":0}`)

	entries, err := List(dir)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("read %d runs, want 1; only %s files are runs", len(entries), Suffix)
	}
}

func TestClearingADirectoryThatIsNotThereIsFine(t *testing.T) {
	if err := Clear(filepath.Join(t.TempDir(), "never-existed")); err != nil {
		t.Errorf("clear: %v", err)
	}
}

func write(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body+"\n"), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func ids(entries []Entry) string {
	var out []string
	for _, e := range entries {
		out = append(out, e.ID)
	}
	return strings.Join(out, ", ")
}

func TestADayHoldsEveryRunThatHappenedInIt(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "2026-09-03.jsonl", strings.Join([]string{
		`{"time":"2026-09-03T08:00:00Z","run":"aaaa","command":"proton mail messages list"}`,
		`{"time":"2026-09-03T08:00:01Z","run":"aaaa","exit":0}`,
		`{"time":"2026-09-03T09:00:00Z","run":"bbbb","command":"proton drive items list"}`,
		`{"time":"2026-09-03T09:00:01Z","run":"bbbb","exit":7}`,
	}, "\n"))

	entries, err := List(dir)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("read %d runs out of one day, want 2", len(entries))
	}
	if entries[0].ID != "aaaa" || entries[1].ID != "bbbb" {
		t.Errorf("runs came back as %v, want them in the order they started", ids(entries))
	}
	if entries[1].Command != "proton drive items list" || !entries[1].Failed() {
		t.Errorf("the failing run reads as %+v", entries[1])
	}
}

// Two invocations at once append to the same day, so their records interleave.
// Each is still its own run, because every record says which one it belongs to.
func TestRunsThatOverlappedAreStillTwoRuns(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "2026-09-03.jsonl", strings.Join([]string{
		`{"run":"aaaa","command":"a"}`,
		`{"run":"bbbb","command":"b"}`,
		`{"run":"aaaa","exit":0}`,
		`{"run":"bbbb","exit":7}`,
	}, "\n"))

	entries, err := List(dir)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("read %d runs, want 2", len(entries))
	}
	for _, e := range entries {
		if len(e.Lines) != 2 {
			t.Errorf("run %s took %d records, want only its own 2", e.ID, len(e.Lines))
		}
	}
}

// A torn or unrecognisable line costs that line and not the day around it.
func TestOneBadLineDoesNotLoseTheDay(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "2026-09-03.jsonl", strings.Join([]string{
		`{"run":"aaaa","command":"a"}`,
		`{"run":"aaaa","exit":0}{"run":"bbb`,
		`not a record at all`,
		`{"run":"cccc","command":"c"}`,
		`{"run":"cccc","exit":7}`,
	}, "\n"))

	entries, err := List(dir)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("read %d runs, want the 2 that are readable", len(entries))
	}
	got, ok := Latest(dir, "")
	if !ok || got.ID != "cccc" {
		t.Errorf("the failure after the torn line was not found: %+v", got)
	}
}

func TestPruningKeepsTheLastSixteenDays(t *testing.T) {
	dir := t.TempDir()
	today := time.Now()
	for i := range keepFiles + 4 {
		day := today.AddDate(0, 0, -i)
		write(t, dir, day.Format("2006-01-02")+Suffix, fmt.Sprintf(`{"run":"r%02d","exit":0}`, i))
	}
	Prune(dir)

	entries, err := List(dir)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(entries) != keepFiles {
		t.Errorf("kept %d days, want %d", len(entries), keepFiles)
	}
	if entries[0].ID != fmt.Sprintf("r%02d", keepFiles-1) {
		t.Errorf("oldest kept is %s; pruning takes the oldest days first", entries[0].ID)
	}
}

// Counted rather than dated, so a long gap does not erase what came before it.
// Somebody who reaches for the CLI once a month still has their last sixteen.
func TestALongGapDoesNotEraseTheDaysBeforeIt(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "2024-01-01"+Suffix, `{"run":"old0","exit":7}`)
	write(t, dir, time.Now().Format("2006-01-02")+Suffix, `{"run":"new0","exit":0}`)

	Prune(dir)
	entries, err := List(dir)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("kept %v, want both days", ids(entries))
	}
	got, ok := Latest(dir, "")
	if !ok || got.ID != "old0" {
		t.Errorf("the failure from before the gap was lost: %+v", got)
	}
}

// Today is what this run is writing to, so it can never be what pruning removes.
func TestPruningNeverRemovesToday(t *testing.T) {
	dir := t.TempDir()
	run, err := Open(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	_, _ = fmt.Fprintf(run.Writer(), `{"run":%q,"exit":0}`+"\n", run.ID)
	_ = run.Close()

	Prune(dir)
	if _, err := os.Stat(run.Path); err != nil {
		t.Errorf("today's file went: %v", err)
	}
}

// A run appends to the day rather than replacing it.
func TestASecondRunJoinsTheSameDay(t *testing.T) {
	dir := t.TempDir()
	first, err := Open(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	_, _ = fmt.Fprintf(first.Writer(), `{"run":%q,"exit":0}`+"\n", first.ID)
	_ = first.Close()

	second, err := Open(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if second.Path != first.Path {
		t.Errorf("two runs on one day wrote %s and %s", first.Path, second.Path)
	}
	_, _ = fmt.Fprintf(second.Writer(), `{"run":%q,"exit":7}`+"\n", second.ID)
	_ = second.Close()

	entries, err := List(dir)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("the day holds %d runs, want 2", len(entries))
	}
	if second.ID == first.ID {
		t.Error("two runs on one day share a name")
	}
}
