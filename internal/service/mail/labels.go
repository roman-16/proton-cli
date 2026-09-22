package mail

import (
	"context"
	"sort"
	"strconv"

	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/roman-16/proton-cli/internal/proton"
)

// Proton label types: a label tags a message, a folder contains it.
const (
	labelTypeLabel  = 1
	labelTypeFolder = 3
)

// rootFolder is where a folder sits when it is not inside another one. Proton
// spells it as a number, so it can never be read as a folder's ID.
const rootFolder = 0

type Label struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
	Type  int    `json:"type"`
	Path  string `json:"path,omitempty"`
	// Parent is the folder this one is inside. It is empty for a folder at the
	// top level and for a label, which does not nest.
	Parent string `json:"parent,omitempty"`
	// Notify says whether mail landing in a folder is worth telling the reader
	// about. It is nil for a label, which is not a place mail lands: Proton
	// offers the switch on folders alone, so reporting one on a label would be
	// inventing a setting nobody can change.
	Notify *bool `json:"notify,omitempty"`
}

// Notifies reports whether this is somewhere a watch should report arrivals.
func (l Label) Notifies() bool { return l.Notify != nil && *l.Notify }

// LabelSpec is what a label or folder is made of. One type for both writes, so
// creating and updating cannot disagree about what the fields mean.
type LabelSpec struct {
	Name  string
	Color string
	// Parent is nil when the caller is not saying, which on an update leaves a
	// folder where it is. It points at the empty string for the top level.
	Parent *string
	// Notify is nil when the caller is not saying, which on an update leaves it
	// as it was.
	Notify *bool
}

// rawLabel is a label as Proton keeps it. Updating one replaces the whole
// record, so every field it accepts is read even though the CLI only changes
// three of them.
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

// labelsOfType answers in the order the account keeps, which is the order every
// Proton client shows. Order is a sequence per list - the labels are one list,
// and the folders inside each folder are another - so sorting by it and then
// laying the tree out puts each list in its own order.
func (s *Service) labelsOfType(ctx context.Context, t int) ([]Label, error) {
	var r struct{ Labels []rawLabel }
	if err := s.C.Decode(ctx, proton.Request{
		Method: "GET", Path: "/core/v4/labels",
		Query: proton.Query("Type", strconv.Itoa(t)),
	}, &r); err != nil {
		return nil, err
	}
	rows := r.Labels
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

// Tree lays folders out the way a client shows them: each folder followed by
// what is inside it. Siblings keep the order they arrive in, so whatever
// ordered the slice is what decides their sequence.
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
	// A folder the walk could not reach is kept rather than dropped: the answer
	// is the account's folders, not the ones this process could place.
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

// LabelUpdate changes a label or folder's name, color and/or parent.
//
// Proton replaces the whole record rather than patching it - a body without a
// Name is refused - so the current one is read and the changes are laid over it.
// Everything else travels back unchanged, which is what stops a recolour
// clearing whether a folder notifies or where it sits.
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
	// A folder at the top level is one with no parent named at all, which is how
	// a folder leaves the one it was inside.
	if parent != "" {
		body["ParentID"] = parent
	}
	return s.C.Decode(ctx, proton.Request{Method: "PUT", Path: "/core/v4/labels/" + id, Body: body}, nil)
}

// LabelsReorder writes the sequence Proton keeps one list in.
//
// A list is every label, or the folders inside one folder - the top level being
// a folder like any other here. The whole list travels rather than a move, so
// every member is named in the place it is to take.
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

// labelByID finds a label or a folder. They are one resource to Proton, listed
// apart only by type.
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

// pick is the change if one was asked for, else what is already there.
func pick(want, current string) string {
	if want != "" {
		return want
	}
	return current
}

func (s *Service) LabelDelete(ctx context.Context, ids []string) error {
	return s.C.Decode(ctx, proton.Request{Method: "DELETE", Path: "/core/v4/labels", Body: map[string]any{"LabelIDs": ids}}, nil)
}
