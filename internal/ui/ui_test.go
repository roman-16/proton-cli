package ui

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

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

// Out carries the API's response and nothing else, so a body that is not JSON
// goes to Err and the command fails. Anything else hands a parser a proxy's
// error page and calls it a success.
func TestRawRefusesABodyThatIsNotJSON(t *testing.T) {
	for _, format := range []Format{FormatText, FormatJSON, FormatYAML} {
		u, out, errb := fixture(t, Options{Format: format})
		err := Raw(u, []byte("<html>502 Bad Gateway</html>"))
		if err == nil {
			t.Fatalf("%s: Raw accepted a body that is not JSON", format)
		}
		if out.Len() != 0 {
			t.Errorf("%s: stdout got %q", format, out.String())
		}
		if !strings.Contains(errb.String(), "502 Bad Gateway") {
			t.Errorf("%s: the body is not on stderr: %q", format, errb.String())
		}
		var coder errs.ExitCoder
		if !errors.As(err, &coder) || coder.ExitCode() != 5 {
			t.Errorf("%s: a server that answers with rubbish is exit 5, got %v", format, err)
		}
	}
}

// A HEAD has no body, and neither has an endpoint that answers with no content.
// Nothing to write is not the same as something unwritable.
func TestRawPassesABodyWithNothingInIt(t *testing.T) {
	u, out, errb := fixture(t, Options{Format: FormatJSON})
	if err := Raw(u, nil); err != nil {
		t.Fatal(err)
	}
	if out.Len() != 0 || errb.Len() != 0 {
		t.Errorf("an empty body wrote out=%q err=%q", out.String(), errb.String())
	}
}

// The logging verbosity is settled before any command runs, so a mistyped level
// is refused rather than silently becoming the default - which used to look
// exactly like the logging working and having nothing to report.
func TestParseLogLevel(t *testing.T) {
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
	WriteError(u.Err, errs.Problemf("No message matching %q.", "Invoice #9999"), u.errStyle, false)

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

// An instruction is part of the question rather than commentary on it, so it is
// the one thing --quiet does not take away: a run waiting for a finger on a
// security key, having said nothing about why, cannot be told from a hang.
func TestAnInstructionSurvivesQuiet(t *testing.T) {
	u, out, errb := fixture(t, Options{Quiet: true})
	u.Instruct("Touch your security key.")
	if errb.String() != "Touch your security key.\n" {
		t.Errorf("err = %q, want the instruction", errb.String())
	}
	if out.Len() != 0 {
		t.Errorf("an instruction reached the answer stream: %q", out.String())
	}
}

// Dropping omitempty is half the promise. A nil slice in Go marshals to null
// just as surely as a missing key breaks a consumer, so the machine format
// writes every empty collection out as one.
func TestMachineOutputSpellsEmptyCollectionsOut(t *testing.T) {
	type inner struct {
		Tags []string `json:"tags"`
	}
	type row struct {
		Names   []string          `json:"names"`
		Labels  map[string]string `json:"labels"`
		Nested  []inner           `json:"nested"`
		Blob    []byte            `json:"blob"`
		At      time.Time         `json:"at"`
		Pointed *inner            `json:"pointed"`
	}

	out, errb := &bytes.Buffer{}, &bytes.Buffer{}
	u := New(Options{Format: FormatJSON})
	u.Out, u.Err = out, errb
	if err := u.encode(row{Nested: []inner{{}}}); err != nil {
		t.Fatal(err)
	}

	var got map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"names", "nested"} {
		if _, ok := got[key].([]any); !ok {
			t.Errorf("%s = %#v, want a list", key, got[key])
		}
	}
	if _, ok := got["labels"].(map[string]any); !ok {
		t.Errorf("labels = %#v, want an object", got["labels"])
	}
	// Every depth, not only the top level.
	nested, _ := got["nested"].([]any)
	if len(nested) != 1 {
		t.Fatalf("nested = %#v", got["nested"])
	}
	if first, _ := nested[0].(map[string]any); first["tags"] == nil {
		t.Errorf("a list inside a list stayed null: %#v", nested[0])
	}
	// A byte slice is a string in JSON, and a time writes itself: neither is a
	// collection to spell out, and rebuilding a time field by field would hand
	// back the zero instant.
	if got["blob"] != nil {
		t.Errorf("blob = %#v, want null", got["blob"])
	}
	if got["at"] != "0001-01-01T00:00:00Z" {
		t.Errorf("at = %#v, want the zero instant it was given", got["at"])
	}
	if got["pointed"] != nil {
		t.Errorf("pointed = %#v, want null", got["pointed"])
	}
}

// A list is always a list, even when it is empty.
//
// `--output json` is read by programs, and a program indexes what it is given.
// `omitempty` on a slice or a map drops the key when the collection is empty, so
// the consumer gets null where it asked for something to iterate - which is not
// an empty answer but a hard error:
//
//	$ proton calendar events get Dentist --output json | jq -r '.attendees[]'
//	jq: error (at <stdin>:0): Cannot iterate over null (null)
//
// Absence still says something in this CLI, and deliberately: `total` and `page`
// appear only when a request was paginated, so a consumer can tell page 0 from
// no paging at all. That is why the rule is about containers rather than about
// every field - a container is the one thing a consumer iterates, and null is
// the one value that stops it.
//
// The check is on the tags rather than on the values because there is no list of
// the types that reach encode: they are every view struct in every service. A
// rule the compiler cannot see needs something that walks all of them, or it
// holds only for the ones somebody remembered.
func TestAContainerIsAlwaysAContainerInMachineOutput(t *testing.T) {
	root := filepath.Join("..", "..", "internal")
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		switch {
		case err != nil:
			return err
		case d.IsDir():
			return nil
		case !strings.HasSuffix(path, ".go"), strings.HasSuffix(path, "_test.go"):
			return nil
		case isForeignShape(path):
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return err
		}
		ast.Inspect(file, func(n ast.Node) bool {
			field, ok := n.(*ast.Field)
			if !ok || field.Tag == nil {
				return true
			}
			name, omitEmpty := jsonTag(field.Tag.Value)
			if !omitEmpty || !isContainer(field.Type) {
				return true
			}
			t.Errorf("%s: %q is a list or a map and may not be omitted when empty:\n"+
				"  a consumer that iterates it gets null instead, which is an error rather than nothing",
				path, name)
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// isForeignShape reports a file whose json tags belong to somebody else, and so
// are not this CLI's to decide.
//
// Two of them: the Pass protobuf bindings, which are generated from Proton's
// schema, and the Pass export file, which is the document Proton's own clients
// write and other tools read.
func isForeignShape(path string) bool {
	path = filepath.ToSlash(path)
	return strings.Contains(path, "internal/service/pass/proto/") ||
		strings.HasSuffix(path, "internal/service/pass/export.go")
}

// jsonTag reads a field's json name, and whether it disappears when empty. A tag
// naming no json field, or naming one in Proton's own PascalCase, is a wire
// shape rather than this CLI's answer.
func jsonTag(quoted string) (name string, omitEmpty bool) {
	tag, err := strconv.Unquote(quoted)
	if err != nil {
		return "", false
	}
	value, ok := reflect.StructTag(tag).Lookup("json")
	if !ok {
		return "", false
	}
	parts := strings.Split(value, ",")
	name = parts[0]
	if name == "" || name == "-" || name != strings.ToLower(name) {
		return "", false
	}
	for _, opt := range parts[1:] {
		if opt == "omitempty" {
			return name, true
		}
	}
	return name, false
}

func isContainer(t ast.Expr) bool {
	switch t := t.(type) {
	case *ast.ArrayType:
		// A fixed-size array is not a collection that can be empty, and a byte
		// slice is a string in json rather than something to iterate.
		return t.Len == nil && !isByte(t.Elt)
	case *ast.MapType:
		return true
	}
	return false
}

func isByte(t ast.Expr) bool {
	ident, ok := t.(*ast.Ident)
	return ok && ident.Name == "byte"
}
