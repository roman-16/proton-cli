package mail

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/roman-16/proton-cli/internal/proton"
)

// Who reaches the inbox, and who does not.
//
// Proton calls these incoming defaults: a standing decision about one sender or
// one domain, applied before the spam filter forms an opinion. The web client
// splits them across three lists - block, spam, allow - but they are one record
// with a location on it, so they are one collection here with a verb per
// destination.

// Sender destinations, as Proton numbers them.
const (
	senderInbox   = 0
	senderSpam    = 4
	senderBlocked = 14
)

// SenderRule is a standing decision about who may reach you.
type SenderRule struct {
	ID string `json:"id"`
	// Email and Domain are alternatives: a rule names one address or one whole
	// domain, never both.
	Email  string `json:"email,omitempty"`
	Domain string `json:"domain,omitempty"`
	// Goes is where their mail lands: inbox, spam or blocked.
	Goes string `json:"goes"`
	Time int64  `json:"create_time,omitempty"`
}

func senderDestination(location int) string {
	switch location {
	case senderInbox:
		return "inbox"
	case senderSpam:
		return "spam"
	case senderBlocked:
		return "blocked"
	}
	return fmt.Sprintf("%d", location)
}

// SenderDestinations are the words the verbs map onto, for a flag's declared
// domain and for the help.
var SenderDestinations = map[string]int{
	"inbox": senderInbox, "spam": senderSpam, "blocked": senderBlocked,
}

// sendersPageSize is how many rules one request asks for.
const sendersPageSize = 100

func (s *Service) SendersList(ctx context.Context) ([]SenderRule, error) {
	return proton.All(ctx, func(ctx context.Context, page int) ([]SenderRule, bool, error) {
		var r struct {
			IncomingDefaults []struct {
				ID, Email, Domain string
				Location          int
				Time              int64
			}
		}
		q := url.Values{}
		q.Set("Page", fmt.Sprintf("%d", page))
		q.Set("PageSize", fmt.Sprintf("%d", sendersPageSize))
		if err := s.C.Decode(ctx, proton.Request{
			Method: "GET", Path: "/mail/v4/incomingdefaults", Query: q,
		}, &r); err != nil {
			return nil, false, err
		}
		out := make([]SenderRule, 0, len(r.IncomingDefaults))
		for _, d := range r.IncomingDefaults {
			out = append(out, SenderRule{
				ID: d.ID, Email: d.Email, Domain: d.Domain,
				Goes: senderDestination(d.Location), Time: d.Time,
			})
		}
		return out, proton.Full(out, sendersPageSize), nil
	})
}

// SenderSet files a sender or a domain to a destination.
//
// Overwrite is always on, because a second decision about the same address is a
// change of mind rather than a conflict, and asking somebody to delete the old
// rule first would be a step with no question behind it.
func (s *Service) SenderSet(ctx context.Context, target string, location int) error {
	body := map[string]any{"Location": location}
	if strings.HasPrefix(target, "@") {
		body["Domain"] = strings.TrimPrefix(target, "@")
	} else {
		body["Email"] = target
	}
	q := url.Values{}
	q.Set("Overwrite", "1")
	return s.C.Decode(ctx, proton.Request{
		Method: "POST", Path: "/mail/v4/incomingdefaults", Query: q, Body: body,
	}, nil)
}

// SenderForget removes the standing decision, so the spam filter is free to form
// its own opinion again.
func (s *Service) SenderForget(ctx context.Context, targets []string) error {
	rules, err := s.SendersList(ctx)
	if err != nil {
		return err
	}
	var ids []string
	for _, want := range targets {
		found := false
		for _, r := range rules {
			if strings.EqualFold(r.Email, want) ||
				(r.Domain != "" && strings.EqualFold("@"+r.Domain, want)) {
				ids = append(ids, r.ID)
				found = true
				break
			}
		}
		if !found {
			return &errs.NotFound{Kind: "sender rule", Ref: want}
		}
	}
	return s.C.Decode(ctx, proton.Request{
		Method: "PUT", Path: "/mail/v4/incomingdefaults/delete",
		Body: map[string]any{"IDs": ids},
	}, nil)
}
