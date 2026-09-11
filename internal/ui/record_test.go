package ui

import (
	"strings"
	"testing"
)

func TestRecordAlignsToLongestLabel(t *testing.T) {
	u, out, errb := fixture(t, Options{})
	fields := []Field{
		{Label: "Title", Value: "Team sync"},
		{Label: "Start", Value: "2026-04-16 14:00"},
		{Label: "End", Value: "2026-04-16 15:00"},
		{Label: "Duration", Value: "1h"},
		{Label: "Location", Value: "Vienna HQ"},
		{Label: "Description", Value: "Numbers and roadmap"},
		{Label: "Recurrence", Value: "FREQ=WEEKLY;COUNT=10"},
		{Label: "Occurrence", Value: "3 of 10"},
		{Label: "Series", Value: "ef56gh78/ab12cd34"},
		{Label: "Zone", Value: "Europe/Vienna"},
		{Label: "Reminders", Value: "-PT15M"},
		{Label: "Calendar", Value: "Work"},
		{Label: "Signature", Value: "verified"},
		{Label: "ID", Value: "ef56gh78/ab12cd34@2026-04-16T14:00", ID: true},
	}
	if err := Record(u, RecordSpec{Fields: fields}); err != nil {
		t.Fatal(err)
	}

	// Every value starts in the same column, and that column is derived from the
	// longest label present - which is the whole point of measuring instead of
	// hard-coding a width. "Description" is the longest here, so values begin at
	// len("Description") + len(":") + len("  ").
	longest := 0
	for _, f := range fields {
		if n := len(f.Label); n > longest {
			longest = n
		}
	}
	col := longest + 1 + 2
	for _, line := range strings.Split(strings.TrimRight(out.String(), "\n"), "\n") {
		if len(line) <= col {
			t.Errorf("line too short to reach the value column: %q", line)
			continue
		}
		if line[col-1] != ' ' || line[col] == ' ' {
			t.Errorf("value does not start at column %d: %q", col, line)
		}
	}
	check(t, "record_event", out, errb)
}

// A short label set must not inherit a wide column from some other view.
func TestRecordNarrowLabels(t *testing.T) {
	u, out, errb := fixture(t, Options{})
	spec := RecordSpec{Fields: []Field{
		{Label: "Name", Value: "Jane Roe"},
		{Label: "Email", Value: "jane@example.com"},
		{Label: "ID", Value: "7Kd91mQx", ID: true},
	}}
	if err := Record(u, spec); err != nil {
		t.Fatal(err)
	}
	check(t, "record_contact", out, errb)
}

// Absence is usually not worth a line, but sometimes it is the answer. Always
// keeps the field so "no signature" can be stated rather than implied.
func TestRecordOmitsEmptyUnlessAlways(t *testing.T) {
	u, out, errb := fixture(t, Options{})
	spec := RecordSpec{Fields: []Field{
		{Label: "Name", Value: "report.pdf"},
		{Label: "MIME Type", Value: ""},
		{Label: "Signature", Value: "", Always: true},
	}}
	if err := Record(u, spec); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "MIME Type") {
		t.Error("an empty field should be dropped")
	}
	if !strings.Contains(out.String(), "Signature") {
		t.Error("an Always field should survive being empty")
	}
	check(t, "record_omits_empty", out, errb)
}

// A multi-line value continues in the value column, so a wrapped address stays
// visually inside its field instead of looking like new fields.
func TestRecordMultilineValueStaysInColumn(t *testing.T) {
	u, out, errb := fixture(t, Options{})
	spec := RecordSpec{Fields: []Field{
		{Label: "To", Value: "alice@proton.me\nbob@example.com\ncarol@example.org"},
		{Label: "Subject", Value: "Quarterly numbers"},
	}}
	if err := Record(u, spec); err != nil {
		t.Fatal(err)
	}
	check(t, "record_multiline", out, errb)
}

// The machine format carries the service's own object, not the display labels:
// JSON keys are the API contract and must not drift with a relabelling.
func TestRecordMachineUsesObjectNotLabels(t *testing.T) {
	u, out, _ := fixture(t, Options{Format: FormatJSON})
	spec := RecordSpec{
		Fields: []Field{{Label: "MIME Type", Value: "application/pdf"}},
		Object: map[string]any{"mime_type": "application/pdf"},
	}
	if err := Record(u, spec); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, `"mime_type"`) {
		t.Errorf("machine output should use the object's keys: %s", got)
	}
	if strings.Contains(got, "MIME Type") {
		t.Errorf("machine output leaked a display label: %s", got)
	}
}

func TestRecordShortensIDsOnTerminal(t *testing.T) {
	t.Setenv("PROTON_CLI_FORCE_TTY", "1")
	u, out, _ := fixture(t, Options{})
	spec := RecordSpec{Fields: []Field{
		{Label: "ID", Value: "5bH2mQxKT9wLpN4vRs8kZc1yXd7fGh3jAe6bUi0oQm2nWr5tYv==", ID: true},
	}}
	if err := Record(u, spec); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "5bH2mQxK\n") {
		t.Errorf("ID should be shortened on a terminal: %q", out.String())
	}
}

// The signature verdict is the one field that carries a verdict rather than a
// value, and the only one where not noticing is a security problem. Each of the
// four reads differently; only "invalid" is an alarm.
func TestRecordSignatureVerdicts(t *testing.T) {
	u, out, errb := fixture(t, Options{})
	u.style = Style{enabled: true, direct: true}
	for _, verdict := range []struct {
		value string
		role  Role
	}{
		{"verified", Success},
		{"unsigned", Plain},
		{"unverified", Caution},
		{"invalid", Danger},
	} {
		if err := Record(u, RecordSpec{Fields: []Field{
			{Label: "Subject", Value: "Invoice #2291 is ready"},
			{Label: "Signature", Value: verdict.value, Role: verdict.role, Always: true},
		}}); err != nil {
			t.Fatal(err)
		}
	}
	check(t, "record_signature", out, errb)
}

// A record assembled out of parts can be short of one, exactly as a listing can,
// and the sentence belongs beside it rather than in a log nobody is reading.
func TestRecordSaysWhenSomethingCouldNotBeRead(t *testing.T) {
	u, out, errb := fixture(t, Options{})
	spec := RecordSpec{
		Fields:  []Field{{Label: "Name", Value: "github.com"}},
		Skipped: IncompleteSpec{Count: 1, Kind: "item"},
	}
	if err := Record(u, spec); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "github.com") {
		t.Errorf("stdout = %q, want the record itself", out.String())
	}
	if !strings.Contains(errb.String(), "1 item could not be decrypted") {
		t.Errorf("stderr = %q, want the caveat", errb.String())
	}
}

// A colour on a record must not change what a pipe receives.
func TestRecordColourLeavesTheTextAlone(t *testing.T) {
	spec := RecordSpec{Fields: []Field{
		{Label: "Signature", Value: "invalid", Role: Danger, Always: true},
		{Label: "Color", Value: "purple", Swatch: "#8080FF"},
	}}

	plain, plainOut, _ := fixture(t, Options{})
	if err := Record(plain, spec); err != nil {
		t.Fatal(err)
	}
	coloured, colOut, _ := fixture(t, Options{})
	coloured.style = Style{enabled: true, direct: true}
	if err := Record(coloured, spec); err != nil {
		t.Fatal(err)
	}
	if stripANSI(colOut.String()) != plainOut.String() {
		t.Errorf("colour changed the text\ncoloured (stripped):\n%s\nplain:\n%s",
			stripANSI(colOut.String()), plainOut.String())
	}
}
