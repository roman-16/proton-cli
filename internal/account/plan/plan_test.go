package plan

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/roman-16/proton-cli/internal/proton"
)

// organization answers the one request Read makes with whatever it is told to.
type organization struct {
	body string
	err  error
}

func (o organization) Do(context.Context, proton.Request) (*proton.Response, error) {
	return nil, errors.New("organization only decodes")
}

func (o organization) Decode(_ context.Context, r proton.Request, out any) error {
	if r.Method != "GET" || r.Path != "/core/v4/organizations" {
		return errors.New("unexpected request " + r.Method + " " + r.Path)
	}
	if o.err != nil {
		return o.err
	}
	return json.Unmarshal([]byte(o.body), out)
}

func TestReadNamesThePlanAndWhatItAllows(t *testing.T) {
	got, err := Read(context.Background(), organization{
		body: `{"Code":1000,"Organization":{"PlanName":"bundle2022","MaxDomains":3,"MaxMembers":1}}`,
	})
	if err != nil {
		t.Fatalf("Read = %v", err)
	}
	if got != (Plan{Name: "bundle2022", MaxDomains: 3}) {
		t.Errorf("Read = %+v", got)
	}
}

// An account in no organization is an account on no plan, which is an answer
// rather than a failure to get one.
func TestReadTakesNoOrganizationForNoPlan(t *testing.T) {
	got, err := Read(context.Background(), organization{
		err: &proton.APIError{HTTPStatus: 422, Code: NoOrganization, Message: "not a member"},
	})
	if err != nil || got != (Plan{}) {
		t.Errorf("Read = %+v, %v; want no plan and no error", got, err)
	}
}

func TestReadPassesOnAnyOtherFailure(t *testing.T) {
	refusal := &proton.APIError{HTTPStatus: 500, Message: "Internal server error"}
	if _, err := Read(context.Background(), organization{err: refusal}); !errors.Is(err, error(refusal)) {
		t.Errorf("Read = %v, want Proton's own answer", err)
	}
}
