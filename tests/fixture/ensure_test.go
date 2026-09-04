package fixture

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
)

// recorder stands in for the CLI, answering with rows a test states and
// remembering what it was asked to run.
type recorder struct {
	rows []map[string]any
	ran  [][]string
	fail error
}

func (r *recorder) run(profile string, args ...string) (string, error) {
	if r.fail != nil {
		return "", r.fail
	}
	r.ran = append(r.ran, args)
	if !slices.Contains(args, "list") {
		return "", nil
	}
	var parts []string
	for _, row := range r.rows {
		var fields []string
		for k, v := range row {
			fields = append(fields, fmt.Sprintf("%q:%q", k, Str(v)))
		}
		slices.Sort(fields)
		parts = append(parts, "{"+strings.Join(fields, ",")+"}")
	}
	return `{"items":[` + strings.Join(parts, ",") + `],"count":0}`, nil
}

func vaults() Collection {
	return Collection{
		What: "vault", List: []string{"pass", "vaults", "list"}, Key: "name",
		IDKeys: []string{"share_id"}, Remove: []string{"pass", "vaults", "delete"},
		Pins: []Pin{{
			ID:     "Personal",
			Create: []string{"pass", "vaults", "create", "--name", "Personal"},
		}},
	}
}

func TestEnsureLeavesAMatchingRowAlone(t *testing.T) {
	r := &recorder{rows: []map[string]any{{"name": "Personal", "share_id": "abc"}}}
	row, err := Ensure(r.run, "primary", vaults(), vaults().Pins[0], r.rows)
	if err != nil {
		t.Fatal(err)
	}
	if Str(row["share_id"]) != "abc" {
		t.Errorf("row = %v", row)
	}
	for _, args := range r.ran {
		if slices.Contains(args, "create") || slices.Contains(args, "delete") {
			t.Errorf("a matching row was touched: %v", args)
		}
	}
}

func TestEnsureMakesAMissingRow(t *testing.T) {
	r := &recorder{}
	// The listing is empty until the create, then holds the new row.
	made := false
	run := func(profile string, args ...string) (string, error) {
		if slices.Contains(args, "create") {
			made = true
			r.rows = []map[string]any{{"name": "Personal", "share_id": "abc"}}
		}
		return r.run(profile, args...)
	}
	if _, err := Ensure(run, "primary", vaults(), vaults().Pins[0], r.rows); err != nil {
		t.Fatal(err)
	}
	if !made {
		t.Error("a missing row was not made")
	}
}

// A wrong row is worse than a missing one: it passes a presence check and fails
// an assertion somewhere far away. So it is removed and made again.
func TestEnsureReplacesARowThatDisagrees(t *testing.T) {
	c := vaults()
	c.Pins[0].Fields = map[string]string{"colour": "green"}
	r := &recorder{rows: []map[string]any{{"name": "Personal", "share_id": "abc", "colour": "red"}}}
	run := func(profile string, args ...string) (string, error) {
		if slices.Contains(args, "create") {
			r.rows = []map[string]any{{"name": "Personal", "share_id": "abc", "colour": "green"}}
		}
		return r.run(profile, args...)
	}
	if _, err := Ensure(run, "primary", c, c.Pins[0], r.rows); err != nil {
		t.Fatal(err)
	}
	var removed, created bool
	for _, args := range r.ran {
		removed = removed || slices.Contains(args, "delete")
		created = created || slices.Contains(args, "create")
	}
	if !removed || !created {
		t.Errorf("a mismatching row should be removed and made again; ran %v", r.ran)
	}
}

// A row that is there but is the wrong thing is reported rather than replaced,
// when the collection says it cannot be removed. The paid account's alias is
// that case: its address cannot be re-minted, so deleting it to make a better
// one is not a trade to make quietly.
func TestEnsureWillNotDeleteWhatItCannotRemake(t *testing.T) {
	c := Paid()[0]
	r := &recorder{rows: []map[string]any{{"name": PaidAlias, "type": "login"}}}
	_, err := Ensure(r.run, "paid", c, c.Pins[0], r.rows)
	if err == nil {
		t.Fatal("a row of the wrong type was accepted")
	}
	for _, want := range []string{"paid", PaidAlias, "deleting it"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not mention %q: %v", want, err)
		}
	}
	for _, args := range r.ran {
		if slices.Contains(args, "delete") || slices.Contains(args, "create") {
			t.Errorf("the paid account's own data was touched: %v", args)
		}
	}
}

func TestEnsureCarriesTheRunnersFailureUp(t *testing.T) {
	boom := errors.New("exit 2: not signed in")
	if _, err := Ensure((&recorder{fail: boom}).run, "primary", vaults(), vaults().Pins[0], nil); !errors.Is(err, boom) {
		t.Errorf("err = %v, want the runner's own failure", err)
	}
}

// Only what the suite made is swept, and a recurring event listed once per
// occurrence is removed once.
func TestSweepRemovesOnlyWhatTheSuiteMade(t *testing.T) {
	c := Collection{
		What: "event", List: []string{"calendar", "events", "list"}, Key: "title",
		IDKeys: []string{"calendar_id", "id"}, Remove: []string{"calendar", "events", "delete"},
	}
	r := &recorder{}
	removed := Sweep(r.run, "primary", c, []map[string]any{
		{"title": "Dentist", "calendar_id": "cal", "id": "one"},
		{"title": TestPrefix + "123-left-behind", "calendar_id": "cal", "id": "two"},
		{"title": TestPrefix + "123-series", "calendar_id": "cal", "id": "three"},
		{"title": TestPrefix + "123-series", "calendar_id": "cal", "id": "three"},
	})
	if !slices.Equal(removed, []string{TestPrefix + "123-left-behind", TestPrefix + "123-series"}) {
		t.Errorf("swept %v", removed)
	}
	if len(r.ran) != 2 {
		t.Errorf("a series listed twice should be removed once; ran %v", r.ran)
	}
	if PaidAlias != "" && strings.HasPrefix(PaidAlias, TestPrefix) {
		t.Errorf("the paid alias is named %q, which a sweep would delete", PaidAlias)
	}
}

// A collection with nothing to remove with is read-only, and a sweep over it
// would be a request that could only fail.
func TestSweepDoesNothingWithoutARemoveCommand(t *testing.T) {
	r := &recorder{}
	got := Sweep(r.run, "paid", Paid()[0], []map[string]any{{"name": TestPrefix + "1"}})
	if len(got) != 0 || len(r.ran) != 0 {
		t.Errorf("swept %v, ran %v", got, r.ran)
	}
}

// An event needs both its IDs to be addressed and Drive needs a path, which is
// why this is a join rather than a lookup.
func TestTargetIsTheReferenceTheCLITakes(t *testing.T) {
	event := Collection{IDKeys: []string{"calendar_id", "id"}}
	if got := event.Target(map[string]any{"calendar_id": "cal", "id": "evt"}, "Dentist"); got != "cal/evt" {
		t.Errorf("event target = %q", got)
	}
	drive := Collection{IDKeys: []string{"link_id"}, Parent: "/Documents"}
	if got := drive.Target(map[string]any{"link_id": "abc"}, "packing-list.txt"); got != "/Documents/packing-list.txt" {
		t.Errorf("drive target = %q", got)
	}
}

func TestPinIsFoundByTheNameItGoesBy(t *testing.T) {
	if _, ok := vaults().Pin("Personal"); !ok {
		t.Error("Personal is declared and was not found")
	}
	if _, ok := vaults().Pin("Nothing"); ok {
		t.Error("an undeclared pin was found")
	}
}

func TestFindMatchesOnTheCollectionsKey(t *testing.T) {
	rows := []map[string]any{{"name": "a"}, {"name": "b"}}
	if row, ok := Find(rows, "name", "b"); !ok || Str(row["name"]) != "b" {
		t.Errorf("Find = %v %v", row, ok)
	}
	if _, ok := Find(rows, "name", "c"); ok {
		t.Error("Find matched a row that is not there")
	}
}

// A whole number arrives from JSON as a float and has to read as itself, or
// every Fields comparison against a number fails.
func TestStrRendersAJSONValueForComparison(t *testing.T) {
	for _, tc := range []struct {
		in   any
		want string
	}{
		{nil, ""}, {"x", "x"}, {float64(1), "1"}, {float64(0), "0"},
		{float64(1.5), "1.5"}, {true, "true"}, {false, "false"},
	} {
		if got := Str(tc.in); got != tc.want {
			t.Errorf("Str(%#v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestAgreesOnEveryFieldNamed(t *testing.T) {
	row := map[string]any{"type": "alias", "status": float64(1)}
	if !agrees(row, map[string]string{"type": "alias", "status": "1"}) {
		t.Error("a matching row disagreed")
	}
	if agrees(row, map[string]string{"type": "login"}) {
		t.Error("a mismatching row agreed")
	}
	if !agrees(row, nil) {
		t.Error("a pin naming no fields is judged on its name alone")
	}
}
