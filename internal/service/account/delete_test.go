package account

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"testing"
)

func deletable(role int, plan string) *answers {
	a := &answers{body: map[string]json.RawMessage{
		"GET /core/v4/users":         json.RawMessage(fmt.Sprintf(`{"User": {"Email": "jane@proton.me", "Role": %d}}`, role)),
		"GET /core/v4/organizations": json.RawMessage(fmt.Sprintf(`{"Organization": {"PlanName": %q}}`, plan)),
	}}
	return a
}

func sentPaths(a *answers) []string {
	var out []string
	for _, r := range a.sent {
		out = append(out, r.Method+" "+r.Path)
	}
	return out
}

func TestProtonIsAskedFirstWhetherTheAccountCanGo(t *testing.T) {
	a := deletable(2, "bundle2022")
	d, err := New(a, nil).CanDelete(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(sentPaths(a), "GET /core/v4/users/delete") {
		t.Errorf("asked %v, and never whether the account can be deleted", sentPaths(a))
	}
	if *d != (Deletion{Email: "jane@proton.me"}) {
		t.Errorf("deletion = %+v", *d)
	}
}

func TestAFamilyMemberLeavesThePlanBeforeTheAccountGoes(t *testing.T) {
	for _, tc := range []struct {
		role   int
		plan   string
		leaves bool
	}{
		{role: 1, plan: "family2022", leaves: true},
		{role: 1, plan: "duo2024", leaves: true},
		{role: 1, plan: "passfamily2024", leaves: true},
		{role: 2, plan: "family2022"},
		{role: 1, plan: "bundlepro2024"},
	} {
		a := deletable(tc.role, tc.plan)
		svc := New(a, nil)
		d, err := svc.CanDelete(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		a.sent = nil
		if err := svc.Delete(context.Background(), d, ReasonOther, "Moving everything to one provider"); err != nil {
			t.Fatal(err)
		}
		want := []string{"PUT /core/v4/users/delete"}
		if tc.leaves {
			want = append([]string{"DELETE /core/v4/organizations/membership"}, want...)
		}
		if got := sentPaths(a); !slices.Equal(got, want) {
			t.Errorf("role %d on %s sent %v, want %v", tc.role, tc.plan, got, want)
		}
	}
}

func TestTheReasonIsSentAsTheWebSendsIt(t *testing.T) {
	for _, tc := range []struct {
		reason string
		want   map[string]string
	}{
		{ReasonOtherService, map[string]string{"Reason": "USE_OTHER_SERVICE", "Feedback": "Moving everything to one provider"}},
		{ReasonMerge, map[string]string{"Reason": "MERGE_ACCOUNT"}},
	} {
		a := deletable(0, "")
		if err := New(a, nil).Delete(context.Background(), &Deletion{}, tc.reason, "Moving everything to one provider"); err != nil {
			t.Fatal(err)
		}
		body, _ := a.sent[len(a.sent)-1].Body.(map[string]string)
		if fmt.Sprint(body) != fmt.Sprint(tc.want) {
			t.Errorf("%s sent %v, want %v", tc.reason, body, tc.want)
		}
	}
	for _, reason := range DeletionReasons {
		if deletionReasonCodes[reason] == "" {
			t.Errorf("%s has no code Proton knows", reason)
		}
	}
}

func TestAVisionaryPlanIsNamedBeforeItIsLost(t *testing.T) {
	d, err := New(deletable(2, "visionary2022"), nil).CanDelete(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !d.Visionary {
		t.Error("a Visionary plan was not recognised")
	}
}
