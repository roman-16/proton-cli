package mail

import (
	"context"
	"sort"
	"strconv"

	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/roman-16/proton-cli/internal/proton"
)

const (
	labelTypeLabel  = 1
	labelTypeFolder = 3
)

const rootFolder = 0

type Label struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Color  string `json:"color"`
	Type   int    `json:"type"`
	Path   string `json:"path,omitempty"`
	Parent string `json:"parent,omitempty"`
	Notify *bool  `json:"notify,omitempty"`
}

func (l Label) Notifies() bool { return l.Notify != nil && *l.Notify }

type LabelSpec struct {
	Name   string
	Color  string
	Parent *string
	Notify *bool
}

type rawLabel struct {
	ID, Name, Color, Path, ParentID   string
	Type, Order                       int
	Notify, Sticky, Expanded, Display int
}

func (s *Service) LabelsList(ctx context.Context) ([]Label, []Label, error) {
	labels, err := s.labelsOfType(ctx, labelTypeLabel)
	if err != nil {
		return nil, nil, err
	}
	folders, err := s.labelsOfType(ctx, labelTypeFolder)
	if err != nil {
		return nil, nil, err
	}
	return labels, folders, nil
}

func (s *Service) labelsOfType(ctx context.Context, t int) ([]Label, error) {
	rows, err := s.rawLabels(ctx, t)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Order < rows[j].Order })
	out := make([]Label, 0, len(rows))
	for _, l := range rows {
		row := Label{
			ID: l.ID, Name: l.Name, Color: l.Color, Type: l.Type,
			Path: l.Path, Parent: l.ParentID,
		}
		if t == labelTypeFolder {
			notify := l.Notify == 1
			row.Notify = &notify
		}
		out = append(out, row)
	}
	return Tree(out), nil
}

func (s *Service) rawLabels(ctx context.Context, t int) ([]rawLabel, error) {
	var r struct{ Labels []rawLabel }
	if err := s.C.Decode(ctx, proton.Request{
		Method: "GET", Path: "/core/v4/labels",
		Query: proton.Query("Type", strconv.Itoa(t)),
	}, &r); err != nil {
		return nil, err
	}
	return r.Labels, nil
}

func Tree(rows []Label) []Label {
	inside := make(map[string][]Label, len(rows))
	for _, row := range rows {
		inside[row.Parent] = append(inside[row.Parent], row)
	}
	out := make([]Label, 0, len(rows))
	placed := make(map[string]bool, len(rows))
	var walk func(parent string)
	walk = func(parent string) {
		for _, row := range inside[parent] {
			if placed[row.ID] {
				continue
			}
			placed[row.ID] = true
			out = append(out, row)
			walk(row.ID)
		}
	}
	walk("")
	for _, row := range rows {
		if !placed[row.ID] {
			out = append(out, row)
		}
	}
	return out
}

func (s *Service) LabelCreate(ctx context.Context, spec LabelSpec, isFolder bool) (string, error) {
	t := labelTypeLabel
	if isFolder {
		t = labelTypeFolder
	}
	body := map[string]any{"Name": spec.Name, "Color": spec.Color, "Type": t}
	if spec.Parent != nil && *spec.Parent != "" {
		body["ParentID"] = *spec.Parent
	}
	if spec.Notify != nil {
		body["Notify"] = boolInt(*spec.Notify)
	}
	var r struct{ Label struct{ ID string } }
	if err := s.C.Decode(ctx, proton.Request{Method: "POST", Path: "/core/v4/labels", Body: body}, &r); err != nil {
		return "", err
	}
	return r.Label.ID, nil
}

func (s *Service) LabelUpdate(ctx context.Context, id string, spec LabelSpec) error {
	cur, err := s.labelByID(ctx, id)
	if err != nil {
		return err
	}
	notify := cur.Notify
	if spec.Notify != nil {
		notify = boolInt(*spec.Notify)
	}
	parent := cur.ParentID
	if spec.Parent != nil {
		parent = *spec.Parent
	}
	body := map[string]any{
		"Name":     pick(spec.Name, cur.Name),
		"Color":    pick(spec.Color, cur.Color),
		"Notify":   notify,
		"Sticky":   cur.Sticky,
		"Expanded": cur.Expanded,
		"Display":  cur.Display,
	}
	if parent != "" {
		body["ParentID"] = parent
	}
	return s.C.Decode(ctx, proton.Request{Method: "PUT", Path: "/core/v4/labels/" + id, Body: body}, nil)
}

func (s *Service) LabelsReorder(ctx context.Context, ids []string, parent string, folder bool) error {
	body := map[string]any{"LabelIDs": ids, "Type": labelTypeLabel}
	if folder {
		body["Type"] = labelTypeFolder
		if parent == "" {
			body["ParentID"] = rootFolder
		} else {
			body["ParentID"] = parent
		}
	}
	return s.C.Decode(ctx, proton.Request{
		Method: "PUT", Path: "/core/v4/labels/order", Body: body,
	}, nil)
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func (s *Service) labelByID(ctx context.Context, id string) (rawLabel, error) {
	for _, t := range []int{labelTypeLabel, labelTypeFolder} {
		var r struct{ Labels []rawLabel }
		if err := s.C.Decode(ctx, proton.Request{
			Method: "GET", Path: "/core/v4/labels",
			Query: proton.Query("Type", strconv.Itoa(t)),
		}, &r); err != nil {
			return rawLabel{}, err
		}
		for _, l := range r.Labels {
			if l.ID == id {
				return l, nil
			}
		}
	}
	return rawLabel{}, &errs.NotFound{Kind: "folder or label", Ref: id}
}

func pick(want, current string) string {
	if want != "" {
		return want
	}
	return current
}

func (s *Service) LabelDelete(ctx context.Context, ids []string) error {
	return s.C.Decode(ctx, proton.Request{Method: "DELETE", Path: "/core/v4/labels", Body: map[string]any{"LabelIDs": ids}}, nil)
}
