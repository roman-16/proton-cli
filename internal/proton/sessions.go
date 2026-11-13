package proton

import "context"

// Session lifecycle, mirroring the "Sessions" page of Proton's account settings
// (packages/components/containers/sessions/) and the calls behind it
// (packages/shared/lib/api/auth.ts: querySessions, revokeSession,
// revokeOtherSessions).

type Session struct {
	UID        string `json:"uid"`
	LocalID    int    `json:"local_id"`
	ClientID   string `json:"client_id"`
	MemberID   string `json:"member_id,omitempty"`
	CreateTime int64  `json:"create_time"`
	Current    bool   `json:"current"`
}

func (c *Client) Sessions(ctx context.Context) ([]Session, error) {
	var r struct {
		Sessions []struct {
			UID        string
			LocalID    int
			ClientID   string
			MemberID   string
			CreateTime int64
		}
	}
	if err := c.Decode(ctx, Request{Method: "GET", Path: "/auth/v4/sessions"}, &r); err != nil {
		return nil, err
	}
	mine, _, _ := c.Tokens()
	out := make([]Session, 0, len(r.Sessions))
	for _, s := range r.Sessions {
		out = append(out, Session{
			UID: s.UID, LocalID: s.LocalID, ClientID: s.ClientID,
			MemberID: s.MemberID, CreateTime: s.CreateTime,
			Current: s.UID == mine,
		})
	}
	return out, nil
}

// Revoke ends the session the client is signed in with, as signing out of a
// Proton app does (packages/shared/lib/api/auth.ts: revoke). Revoking it by UID
// instead asks for the account password.
func (c *Client) Revoke(ctx context.Context) error {
	return c.Decode(ctx, Request{Method: "DELETE", Path: "/core/v4/auth"}, nil)
}

func (c *Client) RevokeSession(ctx context.Context, uid string) error {
	return c.Decode(ctx, Request{Method: "DELETE", Path: "/auth/v4/sessions/" + uid}, nil)
}

func (c *Client) RevokeOtherSessions(ctx context.Context) error {
	return c.Decode(ctx, Request{Method: "DELETE", Path: "/auth/v4/sessions"}, nil)
}
