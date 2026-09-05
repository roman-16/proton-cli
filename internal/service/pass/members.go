package pass

import (
	"context"
	"fmt"
	"sort"

	"github.com/roman-16/proton-cli/internal/proton"
)

// Who else can open a vault or an item, once they have said yes.
//
// An invitation and a membership are two states of one question - what may this
// address do with this thing - so Proton keeps them in separate places and this
// package puts them back together: Members are the people who accepted, and
// InvitesSent the ones who have not answered.

// Member is somebody who holds a share of yours.
type Member struct {
	// ShareID is their share, not yours. It is what a role change or a removal
	// addresses, and it is the ID Proton wants for an ownership transfer.
	ShareID string `json:"share_id"`
	Email   string `json:"email"`
	Name    string `json:"name,omitempty"`
	Access  string `json:"access"`
	Owner   bool   `json:"owner"`
}

// Members lists who holds a share of the vault, or of one item in it.
//
// Proton keeps an item's members apart from the vault's, so which of the two is
// asked for depends on whether an item is named: a vault's members are everyone
// who can open all of it, and an item's are the people given that item alone.
func (s *Service) Members(ctx context.Context, shareID, itemID string) ([]Member, error) {
	path := "/pass/v1/share/" + shareID + "/user"
	if itemID != "" {
		path += "/item/" + itemID
	}
	// Proton pages this by the token the previous answer ended with.
	var since string
	out, err := proton.All(ctx, func(ctx context.Context, _ int) ([]Member, bool, error) {
		q := proton.Query()
		if since != "" {
			q.Set("Since", since)
		}
		var r struct {
			Shares []struct {
				ShareID     string
				UserName    string
				UserEmail   string
				Owner       bool
				ShareRoleID string
			}
			LastToken string
		}
		if err := s.C.Decode(ctx, proton.Request{Method: "GET", Path: path, Query: q}, &r); err != nil {
			return nil, false, err
		}
		page := make([]Member, 0, len(r.Shares))
		for _, m := range r.Shares {
			access := roleWord(m.ShareRoleID)
			// Proton records the owner with whatever role the share was made
			// with; what they may do is everything, so that is what is reported.
			if m.Owner {
				access = roleWords[roleManager]
			}
			page = append(page, Member{
				ShareID: m.ShareID, Email: m.UserEmail, Name: m.UserName,
				Access: access, Owner: m.Owner,
			})
		}
		since = r.LastToken
		return page, r.LastToken != "" && len(r.Shares) > 0, nil
	})
	if err != nil {
		return nil, err
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Owner != out[j].Owner {
			return out[i].Owner
		}
		return out[i].Email < out[j].Email
	})
	return out, nil
}

// MemberSetAccess changes what a member may do. Nothing is re-encrypted: the key
// they hold still opens the share, and only the permission moves.
func (s *Service) MemberSetAccess(ctx context.Context, shareID, memberShareID, access string) error {
	role, err := roleFor(access)
	if err != nil {
		return err
	}
	return s.C.Decode(ctx, proton.Request{
		Method: "PUT", Path: fmt.Sprintf("/pass/v1/share/%s/user/%s", shareID, memberShareID),
		Body: map[string]any{"ShareRoleID": role},
	}, nil)
}

// MemberRemove takes somebody's access away. Their share is deleted; what it
// pointed at is untouched.
func (s *Service) MemberRemove(ctx context.Context, shareID, memberShareID string) error {
	return s.C.Decode(ctx, proton.Request{
		Method: "DELETE", Path: fmt.Sprintf("/pass/v1/share/%s/user/%s", shareID, memberShareID),
	}, nil)
}

// VaultTransfer makes one of the vault's members its owner.
//
// Only an owner can hand a vault over, and afterwards they are a manager of it
// like anybody else - so this is the one change to a vault that the person making
// it cannot undo alone.
func (s *Service) VaultTransfer(ctx context.Context, shareID, memberShareID string) error {
	return s.C.Decode(ctx, proton.Request{
		Method: "PUT", Path: "/pass/v1/vault/" + shareID + "/owner",
		Body: map[string]any{"NewOwnerShareID": memberShareID},
	}, nil)
}
