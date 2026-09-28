package account

import (
	"context"
	"slices"

	"github.com/roman-16/proton-cli/internal/account/plan"
	"github.com/roman-16/proton-cli/internal/fetch"
	"github.com/roman-16/proton-cli/internal/proton"
)

const (
	ReasonDifferentAccount = "different-account"
	ReasonTooExpensive     = "too-expensive"
	ReasonMissingFeature   = "missing-feature"
	ReasonOtherService     = "other-service"
	ReasonMerge            = "merge"
	ReasonOther            = "other"
)

var DeletionReasons = []string{
	ReasonDifferentAccount, ReasonTooExpensive, ReasonMissingFeature,
	ReasonOtherService, ReasonMerge, ReasonOther,
}

var deletionReasonCodes = map[string]string{
	ReasonDifferentAccount: "DIFFERENT_ACCOUNT",
	ReasonTooExpensive:     "TOO_EXPENSIVE",
	ReasonMissingFeature:   "MISSING_FEATURE",
	ReasonOtherService:     "USE_OTHER_SERVICE",
	ReasonMerge:            "MERGE_ACCOUNT",
	ReasonOther:            "OTHER",
}

var familyPlans = []string{"duo2024", "family2022", "passfamily2024"}

const (
	visionaryPlan = "visionary2022"
	adminRole     = 2
)

type Deletion struct {
	Email        string
	FamilyMember bool
	Visionary    bool
}

func (s *Service) CanDelete(ctx context.Context) (*Deletion, error) {
	var (
		user struct {
			User struct {
				Email string
				Name  string
				Role  int
			}
		}
		held plan.Plan
	)
	if err := fetch.Together(ctx,
		func(ctx context.Context) error {
			_, err := s.C.Do(ctx, proton.Request{Method: "GET", Path: "/core/v4/users/delete"})
			return err
		},
		func(ctx context.Context) error {
			return s.C.Decode(ctx, proton.Request{Method: "GET", Path: "/core/v4/users"}, &user)
		},
		func(ctx context.Context) error {
			var err error
			held, err = plan.Read(ctx, s.C)
			return err
		},
	); err != nil {
		return nil, err
	}
	email := user.User.Email
	if email == "" {
		email = user.User.Name
	}
	return &Deletion{
		Email:        email,
		FamilyMember: slices.Contains(familyPlans, held.Name) && user.User.Role != adminRole,
		Visionary:    held.Name == visionaryPlan,
	}, nil
}

func (s *Service) Delete(ctx context.Context, d *Deletion, reason, message string) error {
	if d.FamilyMember {
		if _, err := s.C.Do(ctx, proton.Request{
			Method: "DELETE", Path: "/core/v4/organizations/membership",
		}); err != nil {
			return err
		}
	}
	body := map[string]string{"Reason": deletionReasonCodes[reason]}
	if reason != ReasonMerge {
		body["Feedback"] = message
	}
	_, err := s.C.Do(ctx, proton.Request{Method: "PUT", Path: "/core/v4/users/delete", Body: body})
	return err
}
