package pass

import (
	"context"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/roman-16/proton-cli/internal/cli/kit"
	passsvc "github.com/roman-16/proton-cli/internal/service/pass"
	"github.com/roman-16/proton-cli/internal/ui"
	"github.com/roman-16/proton-cli/internal/units"
)

// Sharing a vault, and taking one somebody shared with you.
//
// A vault is opened by its share key and every item in it is sealed under that
// key, so sharing means handing over the key itself - every rotation of it,
// because an item made before the last rotation is still sealed under an older
// one. It goes out encrypted to their key and signed with yours.
//
// Proton keeps the people who accepted apart from the ones who have not
// answered. The question is the same either way - what may this address do with
// this vault - so the commands here put them back together: one record shows
// both, and update and remove act on whichever the address turns out to be.

func vaultsShareCmd() *cobra.Command {
	c := &cobra.Command{Use: "share", Short: "Who else can open a vault"}
	c.AddCommand(shareAddCmd(vaultTarget), shareGetCmd(vaultTarget),
		shareUpdateCmd(vaultTarget), shareRemoveCmd(vaultTarget))
	return c
}

// target is what a sharing command acts on: a whole vault, or one item in one.
//
// The two read identically - add, get, update, remove, the same arguments and
// the same --access - so they are one set of commands told which thing they are
// about, rather than two that would drift.
type target struct {
	noun string
	// resolve turns the REF into the share the thing lives in, the item within it
	// when there is one, and what to call it in a report.
	resolve func(c *kit.Invocation, ref string) (shareID, itemID, name string, err error)
	short   map[string]string
	long    map[string]string
}

var vaultTarget = target{
	noun: "vault",
	resolve: func(c *kit.Invocation, ref string) (string, string, string, error) {
		vault, err := vaultList(c).Find(c.Ctx, ref)
		if err != nil {
			return "", "", "", err
		}
		return vault.ShareID, "", vault.Name, nil
	},
	short: map[string]string{
		"add":    "Offer a vault to somebody",
		"get":    "Show who can open a vault",
		"update": "Change what somebody may do with a vault",
		"remove": "Take somebody's access to a vault away",
	},
	long: map[string]string{
		"add": "Offer a vault to somebody.\n\n" +
			"They are sent an invitation and see nothing until they take it. What is\n" +
			"sent is the key that opens the vault, encrypted to their key and signed\n" +
			"with yours - so it has to be another Proton account, because an address\n" +
			"Proton holds no keys for has nothing to encrypt to.",
		"get": "Show who can open a vault.\n\n" +
			"Members have accepted; the invited have not answered yet.",
		"update": "Change what somebody may do with a vault.\n\n" +
			"For a member, nothing is re-encrypted: the key they hold still opens the\n" +
			"vault, and only what they may do with it changes. Somebody who has not\n" +
			"answered yet holds nothing to change, so the offer is withdrawn and made\n" +
			"again at the new access - which sends them a fresh invitation.",
		"remove": "Take somebody's access to a vault away.\n\n" +
			"It withdraws an invitation nobody answered, or removes a member who did.\n" +
			"The vault is untouched; anything they already read they have read.",
	},
}

var itemTarget = target{
	noun: "item",
	resolve: func(c *kit.Invocation, ref string) (string, string, string, error) {
		shareID, itemID, err := resolveItem(c, ref)
		if err != nil {
			return "", "", "", err
		}
		it, err := c.App.Pass.ItemGet(c.Ctx, shareID, itemID)
		if err != nil {
			return shareID, itemID, "", err
		}
		return shareID, itemID, it.Name, nil
	},
	short: map[string]string{
		"add":    "Offer one item to somebody",
		"get":    "Show how an item is shared",
		"update": "Change what somebody may do with an item",
		"remove": "Take somebody's access to an item away",
	},
	long: map[string]string{
		"add": "Offer one item to somebody, leaving the vault around it alone.\n\n" +
			"What travels is the item's own key rather than the vault's, so they can\n" +
			"open that item and nothing else sealed under the same share.",
		"get": "Show how an item is shared: who holds it, who has been offered it,\n" +
			"and the links made for it.\n\n" +
			"A link's URL carries the key that opens the item, so this prints it in\n" +
			"full - as `links get` does, and as a listing never does.",
		"update": "Change what somebody may do with an item.\n\n" +
			"A member's access changes in place. Somebody who has not answered yet has\n" +
			"their offer withdrawn and made again at the new access, which sends them a\n" +
			"fresh invitation.",
		"remove": "Take somebody's access to an item away.\n\n" +
			"It withdraws an invitation nobody answered, or removes a member who did.",
	},
}

func itemsShareCmd() *cobra.Command {
	c := &cobra.Command{Use: "share", Short: "Who else can open an item"}
	c.AddCommand(shareAddCmd(itemTarget), shareGetCmd(itemTarget),
		shareUpdateCmd(itemTarget), shareRemoveCmd(itemTarget))
	return c
}

func accessFlag() *kit.Enum {
	return &kit.Enum{
		Name: "access", Usage: "What they may do with it",
		Values: passsvc.VaultRoles(), Default: "viewer",
	}
}

func shareAddCmd(t target) *cobra.Command {
	access := accessFlag()
	c := &cobra.Command{
		Use:   "add REF EMAIL",
		Short: t.short["add"],
		Long:  t.long["add"],
		Args:  cobra.ExactArgs(2),
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			role, err := access.Value()
			if err != nil {
				return err
			}
			shareID, itemID, name, err := t.resolve(c, c.Args[0])
			if err != nil {
				return err
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Invited, Kind: "invitations", Count: 1, Name: c.Args[1],
				Detail: "to " + name,
			}, func() error {
				if itemID != "" {
					return c.App.Pass.ItemShare(c.Ctx, shareID, itemID, c.Args[1], role)
				}
				return c.App.Pass.VaultShare(c.Ctx, shareID, c.Args[1], role)
			})
		}),
	}
	access.Register(c)
	return c
}

func shareGetCmd(t target) *cobra.Command {
	return &cobra.Command{
		Use:   "get REF",
		Short: t.short["get"],
		Long:  t.long["get"],
		Args:  cobra.ExactArgs(1),
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			shareID, itemID, name, err := t.resolve(c, c.Args[0])
			if err != nil {
				return err
			}
			members, err := c.App.Pass.Members(c.Ctx, shareID, itemID)
			if err != nil {
				return err
			}
			invited, err := c.App.Pass.InvitesSent(c.Ctx, shareID, itemID)
			if err != nil {
				return err
			}
			fields := []ui.Field{{Label: "Name", Value: name}}
			for _, m := range members {
				who := m.Email + " (" + m.Access + ")"
				if m.Owner {
					who = m.Email + " (owner)"
				}
				fields = append(fields, ui.Field{Label: "Member", Value: who})
			}
			for _, i := range invited {
				fields = append(fields, ui.Field{
					Label: "Invited", Value: i.Email + " (" + i.Access + ", not yet accepted)",
				})
			}
			var links []passsvc.SecureLink
			if itemID != "" {
				if links, err = c.App.Pass.SecureLinksForItem(c.Ctx, shareID, itemID); err != nil {
					return err
				}
				for _, l := range links {
					fields = append(fields,
						ui.Field{Label: "Link", Value: l.URL},
						ui.Field{Label: "Link Expires", Value: units.Time(l.Expires)},
						ui.Field{Label: "Link Views", Value: reads(l)},
						ui.Field{Label: "Link ID", Value: l.LinkID, Ref: "pass links"},
					)
				}
			}
			if len(members) <= 1 && len(invited) == 0 && len(links) == 0 {
				fields = append(fields, ui.Field{Label: "Shared", Value: "no", Always: true})
			}
			if len(links) > 0 {
				c.Warn("Anyone with a link here can read the item until it expires.")
			}
			return kit.Show(c, ui.RecordSpec{
				Object: struct {
					Name    string               `json:"name"`
					Members []passsvc.Member     `json:"members"`
					Invited []passsvc.Invite     `json:"invited"`
					Links   []passsvc.SecureLink `json:"links"`
				}{name, members, invited, links},
				Fields: fields,
			})
		}),
	}
}

func shareUpdateCmd(t target) *cobra.Command {
	access := accessFlag()
	c := &cobra.Command{
		Use:   "update REF EMAIL",
		Short: t.short["update"],
		Long:  t.long["update"],
		Args:  cobra.ExactArgs(2),
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			role, err := access.Value()
			if err != nil {
				return err
			}
			shareID, itemID, name, err := t.resolve(c, c.Args[0])
			if err != nil {
				return err
			}
			held, err := whoHolds(c, shareID, itemID, name, c.Args[1])
			if err != nil {
				return err
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Updated, Kind: "shares", Count: 1, Name: held.email(),
				Detail: "on " + name,
			}, func() error {
				if held.member != nil {
					return c.App.Pass.MemberSetAccess(c.Ctx, shareID, held.member.ShareID, role)
				}
				// Somebody who has not answered holds no share to change, so the
				// offer is withdrawn and made again at the access asked for.
				if err := c.App.Pass.InviteRevoke(c.Ctx, shareID, held.invite.ID); err != nil {
					return err
				}
				if itemID != "" {
					return c.App.Pass.ItemShare(c.Ctx, shareID, itemID, held.email(), role)
				}
				return c.App.Pass.VaultShare(c.Ctx, shareID, held.email(), role)
			})
		}),
	}
	access.Register(c)
	return c
}

func shareRemoveCmd(t target) *cobra.Command {
	return &cobra.Command{
		Use:   "remove REF EMAIL",
		Short: t.short["remove"],
		Long:  t.long["remove"],
		Args:  cobra.ExactArgs(2),
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			shareID, itemID, name, err := t.resolve(c, c.Args[0])
			if err != nil {
				return err
			}
			held, err := whoHolds(c, shareID, itemID, name, c.Args[1])
			if err != nil {
				return err
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Removed, Kind: "shares", Count: 1, Name: held.email(),
				Detail: "from " + name,
			}, func() error {
				if held.member != nil {
					return c.App.Pass.MemberRemove(c.Ctx, shareID, held.member.ShareID)
				}
				return c.App.Pass.InviteRevoke(c.Ctx, shareID, held.invite.ID)
			})
		}),
	}
}

// held is whoever an address turned out to be: somebody who accepted, or an
// offer nobody has answered.
type held struct {
	member *passsvc.Member
	invite *passsvc.Invite
}

func (h held) email() string {
	if h.member != nil {
		return h.member.Email
	}
	return h.invite.Email
}

// whoHolds finds the person an address names, whether they have accepted or not.
//
// The address somebody was invited as and the address Proton knows them by are
// not always the same one: an account signs in as whatever address it likes and
// is a member under its primary Proton one. So a miss lists who is actually
// there, which is the only way somebody could know what to type instead.
func whoHolds(c *kit.Invocation, shareID, itemID, name, email string) (held, error) {
	members, err := c.App.Pass.Members(c.Ctx, shareID, itemID)
	if err != nil {
		return held{}, err
	}
	for _, m := range members {
		if strings.EqualFold(m.Email, email) {
			return held{member: &m}, nil
		}
	}
	invites, err := c.App.Pass.InvitesSent(c.Ctx, shareID, itemID)
	if err != nil {
		return held{}, err
	}
	for _, i := range invites {
		if strings.EqualFold(i.Email, email) {
			return held{invite: &i}, nil
		}
	}

	addresses := make([]string, 0, len(members)+len(invites))
	for _, m := range members {
		addresses = append(addresses, m.Email)
	}
	for _, i := range invites {
		addresses = append(addresses, i.Email+" (not yet accepted)")
	}
	fail := kit.Fail("Nobody at %s holds %s.", email, name).Exit(3)
	if len(addresses) == 0 {
		return held{}, fail.Hint("it is not shared with anybody")
	}
	return held{}, fail.Hint(addresses...)
}

// ── handing a vault over ──

func vaultsTransferCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "transfer REF EMAIL",
		Short: "Make somebody else the owner of a vault",
		Long: "Make somebody else the owner of a vault.\n\n" +
			"They have to be a member already, and only the owner can hand a vault over.\n\n" +
			"Afterwards you are a manager like anybody else. This is the one change to a\n" +
			"vault you cannot undo on your own.",
		Args: cobra.ExactArgs(2),
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			vault, err := vaultList(c).Find(c.Ctx, c.Args[0])
			if err != nil {
				return err
			}
			held, err := whoHolds(c, vault.ShareID, "", vault.Name, c.Args[1])
			if err != nil {
				return err
			}
			if held.member == nil {
				return kit.Fail("%s has been offered %s and has not taken it yet.",
					held.email(), vault.Name).
					Hint("a vault can only be handed to somebody who accepted it").Exit(3)
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Transferred, Kind: "vaults", Count: 1, Name: vault.Name,
				Detail: "to " + held.member.Email, IDs: []string{vault.ShareID},
			}, func() error {
				return c.App.Pass.VaultTransfer(c.Ctx, vault.ShareID, held.member.ShareID)
			})
		}),
	}
}

// ── the other side ──

func invitationsCmd() *cobra.Command {
	c := &cobra.Command{Use: "invitations", Short: "What other people have offered you"}
	c.AddCommand(invitationsListCmd(), invitationsAcceptCmd(), invitationsDeclineCmd())
	return c
}

func receivedList(c *kit.Invocation) *kit.Lookup[passsvc.Invite] {
	return &kit.Lookup[passsvc.Invite]{
		Kind: "invitation",
		Load: func(ctx context.Context) ([]passsvc.Invite, error) {
			return c.App.Pass.InvitesReceived(ctx)
		},
		ID:     func(i passsvc.Invite) string { return i.ID },
		Handle: func(i passsvc.Invite) string { return i.Vault },
	}
}

func receivedColumns() []ui.Column[passsvc.Invite] {
	return []ui.Column[passsvc.Invite]{
		{Header: "ID", ID: true, Cell: func(i passsvc.Invite) string { return i.ID }},
		{Header: "KIND", Cell: func(i passsvc.Invite) string { return i.Kind() }},
		{Header: "VAULT", Flex: true, Handle: true, Cell: func(i passsvc.Invite) string { return i.Vault }},
		{Header: "FROM", Cell: func(i passsvc.Invite) string { return i.Inviter }},
		{Header: "ACCESS", Cell: func(i passsvc.Invite) string { return i.Access }},
		{Header: "ITEMS", Right: true, Cell: func(i passsvc.Invite) string {
			if i.Items == 0 {
				return ""
			}
			return strconv.Itoa(i.Items)
		}},
	}
}

func invitationsListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List what other people have offered you",
		Long: "List what other people have offered you.\n\n" +
			"For a vault, you can read its name and how much is in it before accepting.\n" +
			"Its contents stay sealed until you do.\n\n" +
			"An item offered on its own shows no preview at all.",
		Args: cobra.NoArgs,
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			rows, err := c.App.Pass.InvitesReceived(c.Ctx)
			if err != nil {
				return err
			}
			return kit.List(c, ui.TableSpec[passsvc.Invite]{
				Noun: "invitations", Columns: receivedColumns(), Total: len(rows), Page: ui.Unpaged,
			}, rows)
		}),
	}
}

func invitationsAcceptCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "accept REF...",
		Short: "Take what somebody offered you",
		Long: "Take what somebody offered you.\n\n" +
			"The keys arrive encrypted to the address the offer was sent to, and are\n" +
			"re-encrypted to your own. A vault then behaves like any other of yours.\n\n" +
			"An item accepted on its own is in no vault of yours, so `shared list` is\n" +
			"where it appears.",
		Args: cobra.MinimumNArgs(1),
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			return answerInvites(c, ui.Accepted, c.App.Pass.InviteAccept)
		}),
	}
}

func invitationsDeclineCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "decline REF...",
		Short: "Turn down what somebody offered you",
		Args:  cobra.MinimumNArgs(1),
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			return answerInvites(c, ui.Declined, c.App.Pass.InviteReject)
		}),
	}
}

// answerInvites says yes or no to the ones named.
func answerInvites(c *kit.Invocation, action ui.Action, answer func(context.Context, string) error) error {
	sel, err := kit.SelectFrom(c, "invitations", receivedColumns(), receivedList(c))
	if err != nil {
		return err
	}
	return kit.Mutate(c, ui.ResultSpec{
		Action: action, Kind: "invitations", Count: sel.Len(), IDs: sel.IDs,
		Name: kit.Sole(sel.Rows, func(i passsvc.Invite) string { return i.Vault }),
	}, func() error {
		for _, id := range sel.IDs {
			if err := answer(c.Ctx, id); err != nil {
				return err
			}
		}
		return nil
	})
}
