package ui

import (
	"bytes"
	"encoding/json"
	"flag"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/roman-16/proton-cli/internal/errs"
)

// The ui package is the only place that decides what the CLI looks like, so its
// tests are golden tests: they pin the exact bytes of every response kind. That
// makes an accidental change to a label, a width or a plural a failing test
// rather than something a reader notices three releases later.
//
// Regenerate with:  just golden      (go test ./internal/ui -update)

var update = flag.Bool("update", false, "rewrite the golden files")

// fixture builds a UI writing into buffers. Width is left at zero unless a test
// sets it, which matches production for a non-terminal destination: nothing is
// truncated, so the golden files show the full bytes a pipe receives.
func fixture(t *testing.T, opts Options) (*UI, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	// NO_COLOR would otherwise leak in from the developer's environment and make
	// StyleFor's decision depend on where the test runs.
	t.Setenv("NO_COLOR", "1")
	var out, errb bytes.Buffer
	opts.Out, opts.Err = &out, &errb
	if opts.Format == "" {
		opts.Format = FormatText
	}
	return New(opts), &out, &errb
}

// check compares both streams against testdata/<name>.golden. Both are captured
// in one file, labelled, because the split between them is part of the contract:
// a change that moves a line from stdout to stderr must fail.
func check(t *testing.T, name string, out, errb *bytes.Buffer) {
	t.Helper()
	got := "== stdout ==\n" + out.String() + "== stderr ==\n" + errb.String()
	path := filepath.Join("testdata", name+".golden")

	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden: %v (run `just golden` to create it)", err)
	}
	if got != string(want) {
		t.Errorf("output differs from %s\n\n--- want ---\n%s\n--- got ---\n%s", path, want, got)
	}
}

func TestParseFormat(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want Format
		ok   bool
	}{
		{"", FormatText, true},
		{"text", FormatText, true},
		{"json", FormatJSON, true},
		{"yaml", FormatYAML, true},
		{"yml", FormatYAML, true},
		{"xml", "", false},
		{"JSON", "", false},
	} {
		got, err := ParseFormat(tc.in)
		if tc.ok && err != nil {
			t.Errorf("ParseFormat(%q): unexpected error %v", tc.in, err)
			continue
		}
		if !tc.ok {
			if err == nil {
				t.Errorf("ParseFormat(%q): want error", tc.in)
				continue
			}
			// The error has to name the whole domain, since that is the only
			// place a user learns what is accepted.
			for _, want := range []string{"text", "json", "yaml"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("ParseFormat(%q) error %q omits %q", tc.in, err, want)
				}
			}
			continue
		}
		if got != tc.want {
			t.Errorf("ParseFormat(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestFormatMachine(t *testing.T) {
	if FormatText.Machine() {
		t.Error("text is for people, not machines")
	}
	for _, f := range []Format{FormatJSON, FormatYAML} {
		if !f.Machine() {
			t.Errorf("%q should be a machine format", f)
		}
	}
}

// Quiet suppresses commentary but must never suppress the answer. A script that
// passes --quiet still needs its data.
func TestQuietSilencesOnlyStderr(t *testing.T) {
	u, out, errb := fixture(t, Options{Quiet: true})
	u.Note("authenticating")
	u.Hint("3 messages.")
	if err := Result(u, ResultSpec{Action: Created, Kind: "labels", Count: 1, IDs: []string{"abc"}, EmitID: true}); err != nil {
		t.Fatal(err)
	}
	if errb.Len() != 0 {
		t.Errorf("quiet should silence stderr, got %q", errb.String())
	}
	if got := out.String(); got != "abc\n" {
		t.Errorf("quiet must not silence the answer: got %q, want %q", got, "abc\n")
	}
}

// Raw is the pass-through for `proton api`. Integers have to survive it,
// because YAML would otherwise turn 1000 into 1000.0 and break every consumer.
func TestRawKeepsIntegers(t *testing.T) {
	for _, format := range []Format{FormatJSON, FormatYAML} {
		u, out, _ := fixture(t, Options{Format: format})
		if err := Raw(u, []byte(`{"Code":1000,"Ratio":1.5}`)); err != nil {
			t.Fatal(err)
		}
		got := out.String()
		if !strings.Contains(got, "1000") || strings.Contains(got, "1000.0") {
			t.Errorf("%s: integer not preserved: %s", format, got)
		}
		if !strings.Contains(got, "1.5") {
			t.Errorf("%s: float not preserved: %s", format, got)
		}
	}
}

func TestRawJSONRequiresExactlyOneDocument(t *testing.T) {
	for _, tc := range []struct {
		name    string
		raw     string
		wantErr bool
	}{
		{name: "valid", raw: "  {\"Code\":1000}\n\t"},
		{name: "invalid", raw: `{"Code":`, wantErr: true},
		{name: "trailing document", raw: `{"Code":1000} {"Code":1001}`, wantErr: true},
		{name: "trailing garbage", raw: `{"Code":1000} garbage`, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			u, out, _ := fixture(t, Options{Format: FormatJSON})
			err := Raw(u, []byte(tc.raw))
			if (err != nil) != tc.wantErr {
				t.Fatalf("Raw error = %v, want error %v", err, tc.wantErr)
			}
			if tc.wantErr && out.Len() != 0 {
				t.Fatalf("invalid JSON reached stdout: %q", out.String())
			}
			if !tc.wantErr && !json.Valid(out.Bytes()) {
				t.Fatalf("valid response is not JSON: %q", out.String())
			}
		})
	}
}

// The logging verbosity is settled before any command runs, so a mistyped level
// is refused rather than silently becoming the default - which used to look
// exactly like the logging working and having nothing to report.
func TestParseLogLevel(t *testing.T) {
	unsetenv(t, "PROTON_LOG_LEVEL")
	for _, tc := range []struct {
		flag string
		want slog.Level
		ok   bool
	}{
		{"", slog.LevelWarn, true},
		{"debug", slog.LevelDebug, true},
		{"DEBUG", slog.LevelDebug, true},
		{" info ", slog.LevelInfo, true},
		{"warn", slog.LevelWarn, true},
		{"error", slog.LevelError, true},
		{"verbose", 0, false},
		{"trace", 0, false},
	} {
		got, err := ParseLogLevel(tc.flag)
		if tc.ok {
			if err != nil {
				t.Errorf("ParseLogLevel(%q): unexpected error %v", tc.flag, err)
			} else if got != tc.want {
				t.Errorf("ParseLogLevel(%q) = %v, want %v", tc.flag, got, tc.want)
			}
			continue
		}
		if err == nil {
			t.Errorf("ParseLogLevel(%q): want an error", tc.flag)
			continue
		}
		// The whole domain has to appear: a reader who guessed wrong needs the list.
		for _, level := range LogLevels {
			if !strings.Contains(err.Error(), level) {
				t.Errorf("ParseLogLevel(%q) error %q omits %q", tc.flag, err, level)
			}
		}
	}
}

// The flag wins over the environment, matching every other setting's resolution
// order.
func TestParseLogLevelPrefersTheFlag(t *testing.T) {
	t.Setenv("PROTON_LOG_LEVEL", "error")
	if got, err := ParseLogLevel("debug"); err != nil || got != slog.LevelDebug {
		t.Errorf("ParseLogLevel = (%v, %v), want debug", got, err)
	}
	if got, err := ParseLogLevel(""); err != nil || got != slog.LevelError {
		t.Errorf("with no flag the environment applies: got (%v, %v)", got, err)
	}
}

func TestParseLogLevelRejectsABadEnvironmentValue(t *testing.T) {
	t.Setenv("PROTON_LOG_LEVEL", "chatty")
	if _, err := ParseLogLevel(""); err == nil {
		t.Error("an unusable PROTON_LOG_LEVEL should be reported, not ignored")
	}
}

// The CLI needs exactly three severities and had only two.
//
// A caveat is not a failure and not chatter: the command worked, and something
// about how it worked is worth knowing. Printed as an ordinary note it sits
// invisibly above a green tick, which is how a warning that a file could not be
// attributed ends up reading as an all-clear.
func TestSeverities(t *testing.T) {
	u, out, errb := fixture(t, Options{})
	u.errStyle = Style{enabled: true, direct: true}

	u.Note("Downloading report.pdf.")
	u.Warn("report.pdf downloaded, but the signature on block 3 is unverified,\nso who wrote it cannot be confirmed.")
	u.Hint("3 messages.")
	WriteError(u.Err, errs.Problemf("No message matching %q.", "Invoice #9999"), u.errStyle)

	check(t, "severities", out, errb)
}

// Every severity is commentary, so none of it may reach the answer stream.
func TestSeveritiesStayOffTheAnswerStream(t *testing.T) {
	u, out, errb := fixture(t, Options{})
	u.Note("note")
	u.Warn("warn")
	u.Hint("hint")
	if out.Len() != 0 {
		t.Errorf("commentary reached stdout: %q", out.String())
	}
	if errb.Len() == 0 {
		t.Error("commentary should reach stderr")
	}
}

// --quiet silences commentary of every severity, including caveats: a script
// that asked for quiet gets its data and nothing else.
func TestQuietSilencesEverySeverity(t *testing.T) {
	u, out, errb := fixture(t, Options{Quiet: true})
	u.Note("note")
	u.Warn("warn")
	u.Hint("hint")
	if out.Len() != 0 || errb.Len() != 0 {
		t.Errorf("--quiet still wrote out=%q err=%q", out.String(), errb.String())
	}
}
