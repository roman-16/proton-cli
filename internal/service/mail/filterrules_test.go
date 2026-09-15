package mail

import (
	"encoding/json"
	"strings"
	"testing"
)

// The two views Proton stores have to say the same thing. Everything here is
// about that: a filter whose builder view disagrees with what runs would move
// the wrong mail and look right while doing it.

func TestTheTreeTestsWhatTheConditionSays(t *testing.T) {
	for _, tc := range []struct {
		name string
		cond FilterCondition
		want map[string]string
	}{
		{"a subject reads a header", FilterCondition{Field: "subject", Comparator: "contains", Value: "invoice"},
			map[string]string{"Type": `"Header"`, "Headers": `["Subject"]`, "Keys": `["invoice"]`, "Match": `{"Type":"Contains"}`}},
		{"a sender reads the envelope", FilterCondition{Field: "sender", Comparator: "is", Value: "a@b.c"},
			map[string]string{"Type": `"Address"`, "Headers": `["From"]`, "AddressPart": `{"Type":"All"}`}},
		{"a recipient reads every address it could have arrived on",
			FilterCondition{Field: "recipient", Comparator: "contains", Value: "team"},
			map[string]string{"Headers": `["To","Cc","Bcc"]`}},
		{"an attachment is only ever there or not",
			FilterCondition{Field: "attachments", Comparator: "contains"},
			map[string]string{"Type": `"Exists"`, "Headers": `["X-Attached"]`}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			test, _ := tc.cond.test()
			for field, want := range tc.want {
				if got := jsonOf(t, test[field]); got != want {
					t.Errorf("%s = %s, want %s", field, got, want)
				}
			}
		})
	}
}

// Sieve has no "begins with", so both ends of the range become a wildcard match.
// A * the user typed has to stop being a wildcard at that point, or the filter
// matches more mail than it was asked to.
func TestBeginsAndEndsBecomeAWildcardMatch(t *testing.T) {
	for _, tc := range []struct {
		comparator string
		value      string
		wantKey    string
	}{
		{"starts", "ALERT", `"ALERT*"`},
		{"ends", ".pdf", `"*.pdf"`},
		{"starts", "A*B", `"A\\\\*B*"`},
		{"contains", "A*B", `"A*B"`},
	} {
		t.Run(tc.comparator+" "+tc.value, func(t *testing.T) {
			test, _ := FilterCondition{Field: "subject", Comparator: tc.comparator, Value: tc.value}.test()
			keys := jsonOf(t, test["Keys"])
			if got := strings.TrimSuffix(strings.TrimPrefix(keys, "["), "]"); got != tc.wantKey {
				t.Errorf("key = %s, want %s", got, tc.wantKey)
			}
			wantMatch := `{"Type":"Matches"}`
			if tc.comparator == "contains" {
				wantMatch = `{"Type":"Contains"}`
			}
			if got := jsonOf(t, test["Match"]); got != wantMatch {
				t.Errorf("match = %s, want %s", got, wantMatch)
			}
		})
	}
}

// A value that looks like a Sieve variable must not be read as one, and the
// escape Proton uses only works if the node that defines it is also sent.
func TestADollarValueBringsTheNodeThatEscapesIt(t *testing.T) {
	rule := FilterRule{
		Conditions: []FilterCondition{{Field: "subject", Comparator: "contains", Value: "total ${1} due"}},
		Star:       true,
	}
	tree := jsonOf(t, rule.tree())
	if !strings.Contains(tree, `${dollar}{1}`) {
		t.Errorf("the value was not escaped:\n%s", tree)
	}
	if !strings.Contains(tree, `"Name":"dollar"`) {
		t.Errorf("nothing defines the dollar the escape refers to:\n%s", tree)
	}
	plain := FilterRule{
		Conditions: []FilterCondition{{Field: "subject", Comparator: "contains", Value: "total due"}},
		Star:       true,
	}
	if strings.Contains(jsonOf(t, plain.tree()), `"Type":"Set"`) {
		t.Error("a value with no dollar in it should not carry the escape node")
	}
}

// The comment is how the builder recognises its own work: reading a filter back
// means matching it against the tests below it.
func TestTheCommentNamesTheOperatorAndEveryComparator(t *testing.T) {
	rule := FilterRule{
		MatchAny: true,
		Conditions: []FilterCondition{
			{Field: "sender", Comparator: "is", Value: "a@b.c", Negate: true},
			{Field: "attachments", Comparator: "contains"},
		},
		Star: true,
	}
	want := "/**\r\n * @type or\r\n * @comparator !is\r\n * @comparator default\r\n */"
	got := comparatorComment(rule.statement(), []string{
		rule.Conditions[0].commentWord(), rule.Conditions[1].commentWord(),
	})["Text"]
	if got != want {
		t.Errorf("comment =\n%q\nwant\n%q", got, want)
	}
}

// Negation wraps the test rather than changing it, which is the only way Sieve
// has of saying it.
func TestNegationWrapsTheTest(t *testing.T) {
	rule := FilterRule{
		Conditions: []FilterCondition{{Field: "subject", Comparator: "contains", Value: "x", Negate: true}},
		MarkRead:   true,
	}
	if !strings.Contains(jsonOf(t, rule.tree()), `"Type":"Not"`) {
		t.Error("a negated condition is not wrapped in Not")
	}
}

// The folder goes in front of the labels: Proton takes one list for both and
// tells them apart by which of yours the name belongs to.
func TestTheFolderLeadsTheLabels(t *testing.T) {
	rule := FilterRule{MoveTo: "Archive", Labels: []string{"Receipts", "Tax"}}
	if got, want := jsonOf(t, rule.fileInto()), `["Archive","Receipts","Tax"]`; got != want {
		t.Errorf("fileInto = %s, want %s", got, want)
	}
	if got, want := jsonOf(t, FilterRule{Labels: []string{"Receipts"}}.fileInto()), `["Receipts"]`; got != want {
		t.Errorf("with no folder, fileInto = %s, want %s", got, want)
	}
}

// Everything a rule says has to reach the tree, because the tree is all Proton
// is sent and all it keeps.
func TestEveryPartOfARuleReachesTheTree(t *testing.T) {
	tree := jsonOf(t, FilterRule{
		MatchAny:   true,
		Conditions: []FilterCondition{{Field: "sender", Comparator: "ends", Value: "@acme.com", Negate: true}},
		MoveTo:     "Archive",
		Labels:     []string{"Receipts"},
		MarkRead:   true,
		Star:       true,
	}.tree())
	for _, want := range []struct{ text, why string }{
		{`"Type":"AnyOf"`, "--match any"},
		{`"Type":"Not"`, "a negated condition"},
		{`"Type":"Address"`, "a sender condition"},
		{`"*@acme.com"`, "ends becoming a wildcard match"},
		{`"Name":"Archive"`, "--into"},
		{`"Name":"Receipts"`, "--label"},
		{`\\Seen`, "--mark-read"},
		{`\\Flagged`, "--star"},
		{`@comparator !ends`, "the comment the builder reads back"},
	} {
		if !strings.Contains(tree, want.text) {
			t.Errorf("%s is missing from the tree (%s):\n%s", want.why, want.text, tree)
		}
	}
}

// Every filter carries the check that keeps it off mail Proton already binned.
func TestAFilterNeverRunsOnSpam(t *testing.T) {
	tree := jsonOf(t, FilterRule{
		Conditions: []FilterCondition{{Field: "subject", Comparator: "is", Value: "x"}},
		Star:       true,
	}.tree())
	for _, want := range []string{"vnd.proton.spam-threshold", `"Type":"SpamTest"`, `"Type":"Return"`} {
		if !strings.Contains(tree, want) {
			t.Errorf("the spam check is missing %s", want)
		}
	}
}

func jsonOf(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// A rule shown in words has to be one that, typed back in, gives the same filter.
// The trip goes through JSON because that is what the wire does to it, and it is
// where a []string stops being one.
func TestARuleSurvivesTheTripThroughProton(t *testing.T) {
	for _, rule := range []FilterRule{
		{Conditions: []FilterCondition{{Field: "subject", Comparator: "contains", Value: "invoice"}}, MarkRead: true},
		{Conditions: []FilterCondition{{Field: "sender", Comparator: "is", Value: "a@b.c", Negate: true}}, Star: true},
		{Conditions: []FilterCondition{{Field: "recipient", Comparator: "starts", Value: "team+"}}, Labels: []string{"Work"}},
		{Conditions: []FilterCondition{{Field: "subject", Comparator: "ends", Value: ".pdf"}}, Labels: []string{"Archive"}},
		{Conditions: []FilterCondition{{Field: "subject", Comparator: "matches", Value: "re: *"}}, MarkRead: true},
		{Conditions: []FilterCondition{{Field: "attachments", Comparator: "contains"}}, Star: true},
		{Conditions: []FilterCondition{{Field: "subject", Comparator: "starts", Value: "a*b"}}, Star: true},
		{
			MatchAny: true,
			Conditions: []FilterCondition{
				{Field: "subject", Comparator: "contains", Value: "one"},
				{Field: "attachments", Comparator: "contains", Negate: true},
			},
			Labels: []string{"Archive", "Receipts"}, MarkRead: true, Star: true,
		},
	} {
		t.Run(rule.Conditions[0].Field+" "+rule.Conditions[0].Comparator, func(t *testing.T) {
			var tree []any
			if err := json.Unmarshal([]byte(jsonOf(t, rule.tree())), &tree); err != nil {
				t.Fatal(err)
			}
			got, ok := RuleOf(tree)
			if !ok {
				t.Fatalf("a rule this package generated was not readable back:\n%s", jsonOf(t, rule.tree()))
			}
			// The folder and the labels are one list in the tree, so what went in
			// as MoveTo comes back at the front of Labels. The filter is the same.
			want := rule
			want.Labels = append([]string{}, rule.fileInto()...)
			want.MoveTo = ""
			if jsonOf(t, got) != jsonOf(t, want) {
				t.Errorf("round trip changed the rule:\n got %s\nwant %s", jsonOf(t, got), jsonOf(t, want))
			}
			// And the rule that came back has to generate the same tree.
			if jsonOf(t, got.tree()) != jsonOf(t, rule.tree()) {
				t.Errorf("the rule read back builds a different filter:\n got %s\nwant %s",
					jsonOf(t, got.tree()), jsonOf(t, rule.tree()))
			}
		})
	}
}

// A script somebody wrote by hand is not a rule, and saying it is would show
// them words that do not describe their filter.
func TestATreeThatIsNotARuleIsNotReadAsOne(t *testing.T) {
	for _, tc := range []struct {
		name string
		tree string
	}{
		{"nothing at all", `[]`},
		{"no test", `[{"Type":"Require","List":["fileinto"]}]`},
		{"a header this grammar has no word for",
			`[{"Type":"If","If":{"Type":"AllOf","Tests":[{"Type":"Header","Headers":["X-Spam"],"Keys":["1"],"Match":{"Type":"Is"}}]},"Then":[{"Type":"Keep"}]}]`},
		{"an action this grammar has no word for",
			`[{"Type":"If","If":{"Type":"AllOf","Tests":[{"Type":"Header","Headers":["Subject"],"Keys":["x"],"Match":{"Type":"Is"}}]},"Then":[{"Type":"Discard"}]}]`},
		{"a flag this grammar has no word for",
			`[{"Type":"If","If":{"Type":"AllOf","Tests":[{"Type":"Header","Headers":["Subject"],"Keys":["x"],"Match":{"Type":"Is"}}]},"Then":[{"Type":"AddFlag","Flags":["\\Answered"]}]}]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var tree []any
			if err := json.Unmarshal([]byte(tc.tree), &tree); err != nil {
				t.Fatal(err)
			}
			if rule, ok := RuleOf(tree); ok {
				t.Errorf("read as a rule when it is not one: %s", jsonOf(t, rule))
			}
		})
	}
}
