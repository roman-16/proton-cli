package self

import (
	"strings"
	"testing"
)

func report() Report {
	return Report{
		Version:  "2.4.1",
		Revision: "ef6c17c5cbfd",
		Go:       "go1.26.5",
		Platform: "linux/amd64",
		Install:  "standalone",
		Config:   "/home/you/.config/proton-cli/config.yaml",
		Output:   "text",
		Confirm:  "default",
		Zone:     "Europe/Vienna",
		Profile:  "default",
		Logging:  true,
		Runs: []ReportedRun{{
			ID:      "a91f",
			Command: "proton mail messages list",
			Exit:    7,
			Records: []string{
				`{"time":"2026-09-03T08:33:25.101Z","level":"INFO","msg":"run","run":"a91f"}`,
				`{"time":"2026-09-03T08:33:25.705Z","level":"ERROR","msg":"run failed","run":"a91f","exit":7}`,
			},
		}},
	}
}

func TestTheReportAsksForWhatOnlyThePersonKnows(t *testing.T) {
	got := report().Text()
	for _, want := range []string{"What I ran:", "What I expected:", "What happened instead:", IssuesPage} {
		if !strings.Contains(got, want) {
			t.Errorf("the report does not ask for %q:\n%s", want, got)
		}
	}
}

func TestTheReportCarriesTheBuildAndTheRun(t *testing.T) {
	got := report().Text()
	for _, want := range []string{
		"2.4.1", "ef6c17c5cbfd", "go1.26.5", "linux/amd64", "standalone",
		"Europe/Vienna", "default",
		"Run a91f: proton mail messages list (exit 7)",
		`"msg":"run failed"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the report does not carry %q:\n%s", want, got)
		}
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
	r.Logging, r.Runs = false, nil
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
	r.Runs = nil
	got := r.Text()
	if !strings.Contains(got, "No run has been recorded yet.") {
		t.Errorf("the report does not say why it has no trace:\n%s", got)
	}
}

func TestEveryRunIsCarriedWhenEveryRunIsAsked(t *testing.T) {
	r := report()
	r.Runs = append(r.Runs, ReportedRun{ID: "4c02", Command: "proton drive files list", Exit: 0,
		Records: []string{`{"run":"4c02","msg":"run finished"}`}})
	got := r.Text()
	for _, want := range []string{"Run a91f", "Run 4c02"} {
		if !strings.Contains(got, want) {
			t.Errorf("the report dropped %q:\n%s", want, got)
		}
	}
}
