package account

import (
	"context"

	"github.com/roman-16/proton-cli/internal/breach"
	"github.com/roman-16/proton-cli/internal/proton"
)

func (s *Service) SetSentinel(ctx context.Context, on bool) error {
	req := proton.Request{Method: "DELETE", Path: "/core/v4/settings/highsecurity", Body: map[string]any{}}
	if on {
		req = proton.Request{Method: "POST", Path: "/core/v4/settings/highsecurity", Body: map[string]any{}}
	}
	_, err := s.C.Do(ctx, req)
	return err
}

func (s *Service) SetSentinelEmails(ctx context.Context, on bool) error {
	req := proton.Request{
		Method: "DELETE", Path: "/core/v4/settings/highsecurity/summary/email", Body: map[string]any{},
	}
	if on {
		req = proton.Request{
			Method: "POST", Path: "/core/v4/settings/highsecurity/summary/email", Body: map[string]any{},
		}
	}
	_, err := s.C.Do(ctx, req)
	return err
}

func (s *Service) SetDarkWebMonitoring(ctx context.Context, on bool) error {
	req := proton.Request{Method: "DELETE", Path: "/core/v4/settings/breachalerts", Body: map[string]any{}}
	if on {
		req = proton.Request{Method: "POST", Path: "/core/v4/settings/breachalerts", Body: map[string]any{}}
	}
	_, err := s.C.Do(ctx, req)
	return err
}

func (s *Service) SetBreachEmails(ctx context.Context, on bool) error {
	_, err := s.C.Do(ctx, proton.Request{
		Method: "PATCH", Path: "/account/v1/breaches/email-notifications",
		Body: map[string]bool{"Enabled": on},
	})
	return err
}

func (s *Service) Breaches(ctx context.Context) (breach.Report, error) {
	var r breach.Answer
	if err := s.C.Decode(ctx, proton.Request{Method: "GET", Path: "/account/v4/breaches"}, &r); err != nil {
		return breach.Report{}, err
	}
	return r.Report(), nil
}

func (s *Service) SetBreachState(ctx context.Context, id, state string) error {
	_, err := s.C.Do(ctx, proton.Request{
		Method: "PUT", Path: "/account/v4/breaches/state",
		Body: map[string]any{"ID": id, "State": breach.Code(state)},
	})
	return err
}
