package mail

import (
	"context"
	"slices"
	"strconv"
	"strings"

	"github.com/roman-16/proton-cli/internal/proton"
)

const labelTypeSystem = 4

var categoryOrder = []string{
	labelPrimary, labelSocial, labelPromotions, labelNewsletters, labelTransactions, labelUpdates,
}

type Category struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Shown  bool   `json:"shown"`
	Notify bool   `json:"notify"`

	stored rawLabel
}

func (c Category) Primary() bool { return c.ID == labelPrimary }

func IsPrimary(ref string) bool {
	ref = strings.TrimSpace(ref)
	return ref == labelPrimary || strings.EqualFold(ref, "primary")
}

func isCategory(id string) bool { return slices.Contains(categoryOrder, id) }

func (s *Service) Categories(ctx context.Context) ([]Category, error) {
	var r struct{ Labels []rawLabel }
	if err := s.C.Decode(ctx, proton.Request{
		Method: "GET", Path: "/core/v4/labels",
		Query: proton.Query("Type", strconv.Itoa(labelTypeSystem)),
	}, &r); err != nil {
		return nil, err
	}
	names := make(map[string]string, len(systemFolders))
	for name, id := range systemFolders {
		names[id] = name
	}
	out := make([]Category, 0, len(categoryOrder))
	for _, id := range categoryOrder {
		i := slices.IndexFunc(r.Labels, func(l rawLabel) bool { return l.ID == id })
		if i < 0 {
			continue
		}
		l := r.Labels[i]
		shown := l.Display == 1
		out = append(out, Category{
			ID: id, Name: names[id], Shown: shown, Notify: shown && l.Notify == 1, stored: l,
		})
	}
	return out, nil
}

func (s *Service) CategoryViewOn(ctx context.Context) (bool, error) {
	set, err := s.settings(ctx)
	if err != nil {
		return false, err
	}
	return set.categoryViewOn(), nil
}

func (s *Service) ShowCategory(ctx context.Context, c Category, shown bool) error {
	return s.putCategory(ctx, c, boolInt(shown), c.stored.Notify)
}

func (s *Service) NotifyCategory(ctx context.Context, c Category, notify bool) error {
	return s.putCategory(ctx, c, c.stored.Display, boolInt(notify))
}

func (s *Service) putCategory(ctx context.Context, c Category, display, notify int) error {
	return s.C.Decode(ctx, proton.Request{
		Method: "PUT", Path: "/core/v4/labels/" + c.ID,
		Body: map[string]any{
			"Name": c.stored.Name, "Color": c.stored.Color, "Display": display, "Notify": notify,
		},
	}, nil)
}
