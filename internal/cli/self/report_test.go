package self

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/roman-16/proton-cli/internal/runlog"
)

func report() Report {
	return Report{
		Version:  "2.4.1",
		Revision: "ef6c17c5cbfd",
		Go:       "go1.26.5",
		Platform: "linux/amd64",
		Install:  "standalone",
		Config:   "default location",
		Output:   "text",
		Confirm:  "default",
		Zone:     "Europe/Vienna",
		Profile:  "default",
		Logging:  true,
		Kept:     8,
		Days:     3,
		Runs: []ReportedRun{{
			ID:      "a91f",
			Command: "proton mail messages list",
			Version: "2.4.1",
			Started: time.Date(2026, 9, 3, 8, 33, 25, 0, time.UTC),
			Took:    "604ms",
			Exit:    7,
			Ended:   true,
			Records: []string{
				`{"time":"2026-09-03T08:33:25.101Z","level":"INFO","msg":"run","run":"a91f"}`,
				`{"time":"2026-09-03T08:33:25.705Z","level":"ERROR","msg":"run failed","run":"a91f","exit":7}`,
			},
		}},
	}
}

// The form asks what was run, what was expected and what happened instead. A
// report that asked for them too would have them answered in the wrong box.
func TestTheReportAsksForNothingTheFormAsksFor(t *testing.T) {
	got := report().Text()
	for _, asked := range []string{"What I ran", "What I expected", "What happened instead"} {
		if strings.Contains(got, asked) {
			t.Errorf("the report asks for %q, which the form already asks for:\n%s", asked, got)
		}
	}
}

func TestTheReportCarriesTheBuildAndTheRun(t *testing.T) {
	got := report().Text()
	for _, want := range []string{
		"2.4.1", "ef6c17c5cbfd", "go1.26.5", "linux/amd64", "standalone",
		"Europe/Vienna", "default",
		"Run a91f  proton mail messages list  2.4.1  exit 7  after 604ms  2026-09-03 08:33:25",
		`"msg":"run failed"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the report does not carry %q:\n%s", want, got)
		}
	}
}

// Somebody who updates before reporting is reporting a run from before they
// did, and the build that failed is the one it ran on.
func TestARunSaysWhichBuildItWasRatherThanThisOne(t *testing.T) {
	r := report()
	r.Version = "2.5.0"
	r.Runs[0].Version = "2.4.1"
	got := heading(r.Runs[0])
	if !strings.Contains(got, "2.4.1") || strings.Contains(got, "2.5.0") {
		t.Errorf("the heading %q takes the version off this machine", got)
	}
}

func TestARunKilledOutrightSaysSoRatherThanReadingAsSuccess(t *testing.T) {
	r := report()
	r.Runs[0].Ended, r.Runs[0].Exit, r.Runs[0].Took = false, 0, ""
	got := heading(r.Runs[0])
	if !strings.Contains(got, "no exit recorded") {
		t.Errorf("a run that never finished reads as %q", got)
	}
}

func TestAFactThisMachineCannotAnswerIsLeftOutRatherThanBlank(t *testing.T) {
	r := report()
	r.Revision, r.Zone = "", ""
	got := r.Text()
	for _, absent := range []string{"Revision", "Zone"} {
		if strings.Contains(got, absent) {
			t.Errorf("%q is printed with nothing after it:\n%s", absent, got)
		}
	}
}

func TestWithTheLogOffTheReportSaysSoAndSaysWhatToDo(t *testing.T) {
	r := report()
	r.Logging, r.Runs, r.Kept, r.Days = false, nil, 0, 0
	got := r.Text()
	if !strings.Contains(got, "Turned off on this machine") {
		t.Errorf("the report does not say the log is off:\n%s", got)
	}
	if !strings.Contains(got, "PROTON_NO_LOG") {
		t.Errorf("the report does not say how to turn it back on:\n%s", got)
	}
	if !strings.Contains(got, "2.4.1") {
		t.Errorf("the report gave up entirely; the build is still worth having:\n%s", got)
	}
}

func TestWithNothingRecordedYetTheReportSaysThatInstead(t *testing.T) {
	r := report()
	r.Runs, r.Kept, r.Days = nil, 0, 0
	got := r.Text()
	if !strings.Contains(got, "No run has been recorded yet.") {
		t.Errorf("the report does not say why it has no trace:\n%s", got)
	}
}

// A log that was written and cannot be read back is a different problem from a
// log with nothing in it, and only one of them is about the run being reported.
func TestALogThatCannotBeReadIsSaidRatherThanReadAsEmpty(t *testing.T) {
	r := report()
	r.Runs, r.Kept, r.Days, r.Unread = nil, 0, 0, "permission denied"
	got := r.Text()
	if !strings.Contains(got, "permission denied") {
		t.Errorf("the report does not say what stopped it reading the log:\n%s", got)
	}
	if strings.Contains(got, "No run has been recorded") {
		t.Errorf("an unreadable log reads as an empty one:\n%s", got)
	}
}

// A reader who needs a different run has to know there is one, and how to ask.
func TestTheReportSaysHowMuchElseThereIsToAskFor(t *testing.T) {
	got := report().Text()
	if !strings.Contains(got, "8 runs over 3 days on disk; this is the last one that failed.") {
		t.Errorf("the report does not say what else is on disk:\n%s", got)
	}
}

func TestWithNothingFailedTheReportSaysTheRunIsMerelyTheLast(t *testing.T) {
	r := report()
	r.Runs[0].Exit = 0
	got := r.Text()
	if !strings.Contains(got, "nothing failed, so this is the last one.") {
		t.Errorf("the report implies a failure it did not find:\n%s", got)
	}
}

func TestEveryRunIsCarriedWhenEveryRunIsAsked(t *testing.T) {
	r := report()
	r.Runs = append(r.Runs, ReportedRun{ID: "4c02", Command: "proton drive items list", Ended: true,
		Records: []string{`{"run":"4c02","msg":"run finished"}`}})
	r.Kept = len(r.Runs)
	got := r.Text()
	for _, want := range []string{"Run a91f", "Run 4c02"} {
		if !strings.Contains(got, want) {
			t.Errorf("the report dropped %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "this is the last one") {
		t.Errorf("the report still talks about picking one run:\n%s", got)
	}
}

// A busy run writes hundreds of successful requests. The form takes 65536
// characters, so the paste keeps the two ends and the file keeps everything.
func TestALongRunIsShortenedForTheFormAndWholeInTheFile(t *testing.T) {
	r := report()
	r.Runs[0].Records = busy(600)

	whole := r.Text()
	paste := r.Paste()
	if len(paste) >= len(whole) {
		t.Errorf("the paste is not shorter than the file: %d vs %d", len(paste), len(whole))
	}
	if strings.Count(whole, "\n") < 600 {
		t.Errorf("the file lost records: %d lines", strings.Count(whole, "\n"))
	}
	for _, want := range []string{`"seq":0`, `"seq":599`, "debug records left out"} {
		if !strings.Contains(paste, want) {
			t.Errorf("the paste does not carry %q", want)
		}
	}
	if left, of := r.elided(); left == 0 || of != 600 {
		t.Errorf("elided %d of %d, want some of 600", left, of)
	}
}

// A retry, a request that would not answer, a signature that would not verify:
// those are the middle of a run a reader is looking for.
func TestNothingAboveDebugIsEverLeftOut(t *testing.T) {
	r := report()
	records := busy(600)
	records[300] = `{"level":"WARN","msg":"rate limited by Proton; waiting before trying again","wait_ms":5000}`
	r.Runs[0].Records = records

	if !strings.Contains(r.Paste(), "rate limited") {
		t.Errorf("the one line in the middle worth having was dropped:\n%s", r.Paste())
	}
}

func TestAShortRunIsPastedWhole(t *testing.T) {
	r := report()
	if r.Paste() != r.Text() {
		t.Errorf("a two-record run was shortened:\n%s", r.Paste())
	}
	if left, _ := r.elided(); left != 0 {
		t.Errorf("elided %d records of a run that fits", left)
	}
}

// The run doing the reporting is a run in which nothing went wrong, and carrying
// it would bury the one that did.
func TestTheRunDoingTheReportingIsNotTheSubject(t *testing.T) {
	entries := []runlog.Entry{
		{ID: "aaaa", Path: "2026-09-01.jsonl"},
		{ID: "bbbb", Path: "2026-09-02.jsonl"},
	}
	runs, kept, days := chosen(others(entries, "bbbb"), false)
	if kept != 1 || days != 1 {
		t.Errorf("counted %d runs over %d days, want 1 and 1", kept, days)
	}
	if len(runs) != 1 || runs[0].ID != "aaaa" {
		t.Errorf("picked %+v, want aaaa", runs)
	}
}

func busy(n int) []string {
	records := make([]string, n)
	for i := range records {
		records[i] = fmt.Sprintf(
			`{"level":"DEBUG","msg":"api request","seq":%d,"method":"GET","path":"/mail/v4/messages/{id}","status":200}`, i)
	}
	return records
}
