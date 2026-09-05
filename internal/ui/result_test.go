package ui

import (
	"strings"
	"testing"
)

// ── confirmations ──
//
// The question asked before a removal is the last thing standing between a typo
// and something irretrievable, so its exact bytes are pinned like every other
// response. It has to name what would go, and it may only claim a change cannot
// be undone when that is true - a warning that overstates the stakes is one
// people learn to answer without reading.

func TestConfirmShowsWhatWouldGo(t *testing.T) {
	u, out, errb := fixture(t, Options{In: strings.NewReader("y\n")})
	spec := ResultSpec{
		Action: Deleted, Kind: "messages", Count: 3,
		Preview: func(p *UI) error {
			return Table(p, TableSpec[message]{
				Noun: "messages", Columns: messageColumns()[:4],
				Total: Unknown, Page: Unpaged,
			}, messages())
		},
	}
	ok, err := Confirm(u, spec)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Error("a plain yes should be taken as one")
	}
	if out.Len() != 0 {
		t.Errorf("a question is not an answer, got %q on stdout", out.String())
	}
	check(t, "confirm_forever_preview", out, errb)
}

func TestConfirmWithoutAPreviewIsOneLine(t *testing.T) {
	for _, tc := range []struct {
		name string
		spec ResultSpec
		want string
	}{{
		"forever says so",
		ResultSpec{Action: Deleted, Kind: "labels", Count: 1, Name: "Work"},
		`Would delete label "Work". This cannot be undone. Continue? [y/N] `,
	}, {
		"emptying a trash counts what it would take",
		ResultSpec{Action: Emptied, Kind: "items", Count: 12, Detail: "from the trash"},
		"Would empty 12 items from the trash. This cannot be undone. Continue? [y/N] ",
	}, {
		"uninstalling names the binary",
		ResultSpec{Action: Uninstalled, Count: 1, Name: "/usr/local/bin/proton"},
		"Would uninstall /usr/local/bin/proton. This cannot be undone. Continue? [y/N] ",
	}, {
		"a reversible removal does not claim otherwise",
		ResultSpec{Action: Trashed, Kind: "messages", Count: 3, Detail: "to trash"},
		"Would move 3 messages to trash. Continue? [y/N] ",
	}} {
		t.Run(tc.name, func(t *testing.T) {
			u, _, errb := fixture(t, Options{In: strings.NewReader("y\n")})
			if _, err := Confirm(u, tc.spec); err != nil {
				t.Fatal(err)
			}
			if errb.String() != tc.want {
				t.Errorf("got  %q\nwant %q", errb.String(), tc.want)
			}
		})
	}
}

// --output json still asks - what is worth stopping for is a property of the
// change, not of how its answer is printed - but it has no table to offer, so
// the question stands on its own rather than trailing a JSON document nobody
// asked to read.
func TestConfirmInAMachineFormatSkipsTheTable(t *testing.T) {
	u, out, errb := fixture(t, Options{Format: FormatJSON, In: strings.NewReader("y\n")})
	spec := ResultSpec{
		Action: Deleted, Kind: "messages", Count: 3,
		Preview: func(*UI) error {
			t.Error("a machine format should not render the preview")
			return nil
		},
	}
	ok, err := Confirm(u, spec)
	if err != nil || !ok {
		t.Fatalf("Confirm = (%v, %v)", ok, err)
	}
	want := "Would delete 3 messages. This cannot be undone. Continue? [y/N] "
	if errb.String() != want {
		t.Errorf("got  %q\nwant %q", errb.String(), want)
	}
	if out.Len() != 0 {
		t.Errorf("stdout is the document's alone, got %q", out.String())
	}
}

// The dangerous path is the one that has to be typed out, so everything that is
// not a plain yes means no - including the bare newline of someone pressing
// enter to get their prompt back.
func TestConfirmDefaultsToNo(t *testing.T) {
	spec := ResultSpec{Action: Deleted, Kind: "messages", Count: 3}
	for _, answer := range []string{"\n", "n\n", "no\n", "Y E S\n", "yeah\n", ""} {
		u, _, _ := fixture(t, Options{In: strings.NewReader(answer)})
		ok, err := Confirm(u, spec)
		if err != nil {
			t.Fatalf("%q: %v", answer, err)
		}
		if ok {
			t.Errorf("%q should not have been taken as consent", answer)
		}
	}
	for _, answer := range []string{"y\n", "Y\n", "yes\n", " YES \n"} {
		u, _, _ := fixture(t, Options{In: strings.NewReader(answer)})
		ok, err := Confirm(u, spec)
		if err != nil {
			t.Fatalf("%q: %v", answer, err)
		}
		if !ok {
			t.Errorf("%q is a yes", answer)
		}
	}
}

// With nobody to ask, the same account of the change becomes the error, so an
// unattended run's log says what it declined to do rather than only that it
// wanted permission.
func TestRefusalStatesTheChange(t *testing.T) {
	for _, tc := range []struct {
		spec ResultSpec
		want string
	}{
		{ResultSpec{Action: Deleted, Kind: "messages", Count: 112},
			"Would delete 112 messages. This cannot be undone."},
		{ResultSpec{Action: Trashed, Kind: "messages", Count: 3, Detail: "to trash"},
			"Would move 3 messages to trash."},
		{ResultSpec{Action: Deleted, Kind: "messages", Count: 3, Preview: func(*UI) error { return nil }},
			"Would delete 3 messages. This cannot be undone."},
	} {
		if got := tc.spec.Refusal(); got != tc.want {
			t.Errorf("got  %q\nwant %q", got, tc.want)
		}
	}
}

// The three confirmation shapes, side by side. Each is chosen by what the caller
// actually knows, and none of them ever prints "1 message(s)".
func TestResultMessageShapes(t *testing.T) {
	for _, tc := range []struct {
		name string
		spec ResultSpec
		want string
	}{{
		"named single with a kind worth saying",
		ResultSpec{Action: Created, Kind: "labels", Count: 1, Name: "Work"},
		`✓ Created label "Work".`,
	}, {
		"named single where the kind adds nothing",
		ResultSpec{Action: Uploaded, Count: 1, Name: "trail-map.txt", Detail: "to /Documents"},
		"✓ Uploaded trail-map.txt to /Documents.",
	}, {
		"a count, plural",
		ResultSpec{Action: Trashed, Kind: "messages", Count: 3, Detail: "to trash"},
		"✓ Moved 3 messages to trash.",
	}, {
		"a count of exactly one agrees in number",
		ResultSpec{Action: Trashed, Kind: "messages", Count: 1, Detail: "to trash"},
		"✓ Moved 1 message to trash.",
	}, {
		"an irregular plural",
		ResultSpec{Action: Updated, Kind: "addresses", Count: 1},
		"✓ Updated 1 address.",
	}, {
		"nothing matched",
		ResultSpec{Action: Trashed, Kind: "messages", Count: 0},
		"✓ Nothing to move.",
	}} {
		t.Run(tc.name, func(t *testing.T) {
			u, out, errb := fixture(t, Options{})
			if err := Result(u, tc.spec); err != nil {
				t.Fatal(err)
			}
			if got := strings.TrimRight(errb.String(), "\n"); got != tc.want {
				t.Errorf("got  %q\nwant %q", got, tc.want)
			}
			if out.Len() != 0 {
				t.Errorf("a confirmation belongs on stderr, got %q on stdout", out.String())
			}
		})
	}
}

// A create puts the ID on stdout and the sentence on stderr, so `ID=$(...)`
// captures the ID and nothing else.
func TestResultSplitsIDFromConfirmation(t *testing.T) {
	u, out, errb := fixture(t, Options{})
	spec := ResultSpec{
		Action: Created, Kind: "labels", Count: 1, Name: "Work",
		IDs: []string{"kQ81mDx4T9wLpN4vRs8kZc=="}, EmitID: true,
	}
	if err := Result(u, spec); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); got != "kQ81mDx4T9wLpN4vRs8kZc==\n" {
		t.Errorf("stdout should be the bare ID, got %q", got)
	}
	check(t, "result_created", out, errb)
}

// An ID is data, so it is never shortened even when a terminal is attached: the
// next command may run on another machine.
func TestResultIDIsNeverShortened(t *testing.T) {
	t.Setenv("PROTON_CLI_FORCE_TTY", "1")
	u, out, _ := fixture(t, Options{})
	full := "kQ81mDx4T9wLpN4vRs8kZc=="
	if err := Result(u, ResultSpec{Action: Created, Kind: "labels", Count: 1, IDs: []string{full}, EmitID: true}); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(out.String()); got != full {
		t.Errorf("emitted ID was altered: %q", got)
	}
}

func TestResultBulk(t *testing.T) {
	u, out, errb := fixture(t, Options{})
	spec := ResultSpec{Action: Trashed, Kind: "messages", Count: 3, Detail: "to trash",
		IDs: []string{"hR8sT2vW", "kM4nP9qL", "zC7bX1yE"}}
	if err := Result(u, spec); err != nil {
		t.Fatal(err)
	}
	check(t, "result_bulk", out, errb)
}

// A dry run says what it would do and then shows exactly which things it would
// do it to, because a count alone is not enough to approve a deletion.
func TestResultDryRunShowsTheSelection(t *testing.T) {
	u, out, errb := fixture(t, Options{})
	preview := func(p *UI) error {
		return Table(p, TableSpec[message]{
			Noun: "messages", Columns: messageColumns()[:4],
			Total: Unknown, Page: Unpaged,
		}, messages())
	}
	spec := ResultSpec{
		Action: Trashed, Kind: "messages", Count: 3, Detail: "to trash",
		DryRun: true, Preview: preview,
	}
	if err := Result(u, spec); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(errb.String(), "Dry run - would move 3 messages to trash:") {
		t.Errorf("missing the dry-run line: %q", errb.String())
	}
	// A dry run answers nothing, so none of it may land on stdout: the preview
	// has to survive `--dry-run > /dev/null`.
	if out.Len() != 0 {
		t.Errorf("the preview belongs on stderr, got %q on stdout", out.String())
	}
	check(t, "result_dry_run", out, errb)
}

// ── a change made on a short reading ──
//
// A selection is a listing the command acted on instead of printing. When part
// of it could not be read, the change was made on less than it claims, and the
// person approving it is owed the sentence a listing would have carried - at
// every moment the change is described, and in the machine object under the
// key the table envelope already uses.

func shortReading() IncompleteSpec {
	return IncompleteSpec{
		Count: 1, Kind: "folder", Hides: true,
		Remedy: "This is a bug or damaged data - `proton report` has the details.",
	}
}

func TestResultSaysWhatTheRunCouldNotRead(t *testing.T) {
	u, out, errb := fixture(t, Options{})
	spec := ResultSpec{
		Action: Trashed, Kind: "items", Count: 12, Detail: "to trash",
		Skipped: shortReading(),
	}
	if err := Result(u, spec); err != nil {
		t.Fatal(err)
	}
	check(t, "result_unread", out, errb)
}

func TestResultDryRunSaysWhatTheRunCouldNotRead(t *testing.T) {
	u, out, errb := fixture(t, Options{})
	spec := ResultSpec{
		Action: Trashed, Kind: "messages", Count: 3, Detail: "to trash",
		DryRun: true, Skipped: shortReading(),
		Preview: func(p *UI) error {
			return Table(p, TableSpec[message]{
				Noun: "messages", Columns: messageColumns()[:4],
				Total: Unknown, Page: Unpaged,
			}, messages())
		},
	}
	if err := Result(u, spec); err != nil {
		t.Fatal(err)
	}
	check(t, "result_dry_run_unread", out, errb)
}

func TestConfirmSaysWhatTheRunCouldNotReadBeforeAsking(t *testing.T) {
	u, out, errb := fixture(t, Options{In: strings.NewReader("y\n")})
	spec := ResultSpec{
		Action: Deleted, Kind: "messages", Count: 3, Skipped: shortReading(),
		Preview: func(p *UI) error {
			return Table(p, TableSpec[message]{
				Noun: "messages", Columns: messageColumns()[:4],
				Total: Unknown, Page: Unpaged,
			}, messages())
		},
	}
	if _, err := Confirm(u, spec); err != nil {
		t.Fatal(err)
	}
	check(t, "confirm_unread", out, errb)
}

func TestResultObjectSaysHowManyItCouldNotRead(t *testing.T) {
	u, out, errb := fixture(t, Options{Format: FormatJSON})
	spec := ResultSpec{Action: Trashed, Kind: "items", Count: 12, Skipped: shortReading()}
	if err := Result(u, spec); err != nil {
		t.Fatal(err)
	}
	if errb.Len() != 0 {
		t.Errorf("machine mode should write nothing to stderr, got %q", errb.String())
	}
	if !strings.Contains(out.String(), `"skipped": 1`) {
		t.Errorf("the object does not carry the count: %s", out.String())
	}
	// And says nothing when nothing was skipped, so a whole reading has exactly
	// the shape it always had.
	u, out, _ = fixture(t, Options{Format: FormatJSON})
	if err := Result(u, ResultSpec{Action: Trashed, Kind: "items", Count: 12}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "skipped") {
		t.Errorf("a whole reading reports a skip: %s", out.String())
	}
}

func TestResultDryRunWithoutPreviewEndsInAPeriod(t *testing.T) {
	u, _, errb := fixture(t, Options{})
	spec := ResultSpec{Action: Emptied, Kind: "items", Count: 12, Detail: "from the trash", DryRun: true}
	if err := Result(u, spec); err != nil {
		t.Fatal(err)
	}
	want := "Dry run - would empty 12 items from the trash.\n"
	if errb.String() != want {
		t.Errorf("got  %q\nwant %q", errb.String(), want)
	}
}

// --output json has to mean JSON even for a mutation. A bare ID here would make
// `--output json` emit something no parser accepts.
func TestResultMachineIsAlwaysStructured(t *testing.T) {
	u, out, errb := fixture(t, Options{Format: FormatJSON})
	spec := ResultSpec{
		Action: Created, Kind: "labels", Count: 1, Name: "Work",
		IDs: []string{"kQ81mDx4"}, EmitID: true,
	}
	if err := Result(u, spec); err != nil {
		t.Fatal(err)
	}
	if errb.Len() != 0 {
		t.Errorf("machine mode should write nothing to stderr, got %q", errb.String())
	}
	got := out.String()
	if !strings.HasPrefix(strings.TrimSpace(got), "{") {
		t.Errorf("machine mode must emit an object, got %q", got)
	}
	// kind is singular: it describes each id, not the collection.
	if !strings.Contains(got, `"kind": "label"`) {
		t.Errorf(`want "kind": "label" in %s`, got)
	}
	check(t, "result_created_json", out, errb)
}

// One command produces one machine document. When the answer is a record shown
// afterwards, the confirmation still reaches a reader but adds no second object
// for a parser to choke on.
func TestResultMachineIsSilentWhenTheAnswerFollows(t *testing.T) {
	spec := ResultSpec{
		Action: Linked, Kind: "links", Count: 1,
		Detail: "for /Documents", AnswerFollows: true,
	}

	u, out, errb := fixture(t, Options{Format: FormatJSON})
	if err := Result(u, spec); err != nil {
		t.Fatal(err)
	}
	if out.Len() != 0 || errb.Len() != 0 {
		t.Errorf("want no output, got stdout %q stderr %q", out.String(), errb.String())
	}

	// A dry run shows no record, so it still has to speak for itself.
	dry := spec
	dry.DryRun = true
	u, out, _ = fixture(t, Options{Format: FormatJSON})
	if err := Result(u, dry); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"dry_run": true`) {
		t.Errorf("a dry run must report itself, got %q", out.String())
	}

	// Text mode is unchanged: the confirmation belongs on stderr.
	u, out, errb = fixture(t, Options{})
	if err := Result(u, spec); err != nil {
		t.Fatal(err)
	}
	if out.Len() != 0 {
		t.Errorf("stdout is the record's, got %q", out.String())
	}
	if !strings.Contains(errb.String(), "Created 1 link for /Documents.") {
		t.Errorf("want the confirmation on stderr, got %q", errb.String())
	}
}

func TestResultMachineDryRunIsFlagged(t *testing.T) {
	u, out, _ := fixture(t, Options{Format: FormatJSON})
	spec := ResultSpec{Action: Trashed, Kind: "messages", Count: 2, DryRun: true,
		IDs: []string{"a", "b"}}
	if err := Result(u, spec); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"dry_run": true`) {
		t.Errorf("a dry run must be visible in machine output: %s", out.String())
	}
}

// Only what cannot be taken back may be declared Forever, and both removals plus
// uninstalling have to be. This is the list the guard reads at run time, so a
// wrong entry here is a `delete` that never asks or a `move` that always does.
func TestOnlyIrreversibleActionsAreForever(t *testing.T) {
	forever := map[string]bool{}
	for _, a := range Actions {
		if a.Cost == Forever {
			forever[a.Key] = true
		}
	}
	want := map[string]bool{"deleted": true, "emptied": true, "uninstalled": true}
	for key := range want {
		if !forever[key] {
			t.Errorf("%q cannot be undone and has to be Forever", key)
		}
	}
	for key := range forever {
		if !want[key] {
			t.Errorf("%q is marked Forever; if that is right, say so here too", key)
		}
	}
	if Trashed.Cost != OutOfSight {
		t.Error("trashing takes things out of sight without destroying them")
	}
}

// Every action carries all three grammatical forms, because the confirmation,
// the preview and the JSON each need a different one.
func TestActionVocabularyIsComplete(t *testing.T) {
	seen := map[string]string{}
	for _, a := range Actions {
		if a.Past == "" || a.Verb == "" || a.Key == "" {
			t.Errorf("incomplete action: %+v", a)
		}
		if a.Past[0] < 'A' || a.Past[0] > 'Z' {
			t.Errorf("Past should open a sentence: %q", a.Past)
		}
		if strings.ToLower(a.Verb) != a.Verb {
			t.Errorf("Verb follows \"would\", so it stays lower case: %q", a.Verb)
		}
		if strings.ToLower(a.Key) != a.Key {
			t.Errorf("Key is a machine value, so it stays lower case: %q", a.Key)
		}
		if prev, dup := seen[a.Key]; dup {
			t.Errorf("duplicate action key %q (also %q)", a.Key, prev)
		}
		seen[a.Key] = a.Past
	}
}
