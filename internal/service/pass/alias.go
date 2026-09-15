package pass

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/roman-16/proton-cli/internal/crypto/aead"
	"github.com/roman-16/proton-cli/internal/proton"
	pb "github.com/roman-16/proton-cli/internal/service/pass/proto"
	"google.golang.org/protobuf/proto"
)

// An alias is two things at once: a Pass item, which `items` handles like any
// other, and a mail route, which is everything here - the address, the mailboxes
// it arrives in, the name it goes out under, and whether it is switched on.

type AliasSuffix struct {
	Suffix, SignedSuffix, Domain string
	IsPremium, IsCustom          bool
}

type AliasMailbox struct {
	ID    int
	Email string
}

// AliasActivity is what an alias has done lately. Proton counts the last
// fourteen days.
type AliasActivity struct {
	Forwarded int `json:"forwarded"`
	Replied   int `json:"replied"`
	Blocked   int `json:"blocked"`
}

// AliasDetail is the route behind an address: where its mail goes, what it sends
// as, and what it has carried lately.
type AliasDetail struct {
	Address     string
	Mailboxes   []AliasMailbox
	Available   []AliasMailbox
	DisplayName string
	// Note is SimpleLogin's own note, which aliases imported from there carry.
	Note string
	// Modify reports whether this account may change the route at all, which it
	// may not for an alias somebody else shared with it.
	Modify   bool
	Activity AliasActivity
}

func (s *Service) AliasDetails(ctx context.Context, shareID, itemID string) (*AliasDetail, error) {
	var r struct {
		Alias struct {
			Email     string
			Modify    bool
			Mailboxes []struct {
				ID    int
				Email string
			}
			AvailableMailboxes []struct {
				ID    int
				Email string
			}
			Name  string
			Note  string
			Stats struct {
				ForwardedEmails, RepliedEmails, BlockedEmails int
			}
		}
	}
	if err := s.C.Decode(ctx, proton.Request{
		Method: "GET", Path: fmt.Sprintf("/pass/v1/share/%s/alias/%s", shareID, itemID),
	}, &r); err != nil {
		return nil, err
	}
	boxes := func(in []struct {
		ID    int
		Email string
	}) []AliasMailbox {
		out := make([]AliasMailbox, 0, len(in))
		for _, m := range in {
			out = append(out, AliasMailbox{ID: m.ID, Email: m.Email})
		}
		return out
	}
	return &AliasDetail{
		Address: r.Alias.Email, Modify: r.Alias.Modify,
		Mailboxes: boxes(r.Alias.Mailboxes), Available: boxes(r.Alias.AvailableMailboxes),
		DisplayName: r.Alias.Name, Note: r.Alias.Note,
		Activity: AliasActivity{
			Forwarded: r.Alias.Stats.ForwardedEmails,
			Replied:   r.Alias.Stats.RepliedEmails,
			Blocked:   r.Alias.Stats.BlockedEmails,
		},
	}, nil
}

// AliasSetEnabled switches an alias on or off. A disabled alias keeps its address
// and stops receiving, which is the difference between retiring one and deleting
// it - deleting burns the address for good.
func (s *Service) AliasSetEnabled(ctx context.Context, shareID, itemID string, enabled bool) error {
	return s.C.Decode(ctx, proton.Request{
		Method: "PUT", Path: fmt.Sprintf("/pass/v1/share/%s/alias/%s/status", shareID, itemID),
		Body: map[string]any{"Enable": enabled},
	}, nil)
}

// AliasPatch is what an alias edit changes about the route.
type AliasPatch struct {
	// Mailboxes are the addresses mail to the alias should arrive in, named as
	// `settings domains list` lists them.
	Mailboxes []string
	// DisplayName is the name recipients see on mail sent from the alias.
	DisplayName string
}

func (p AliasPatch) empty() bool { return len(p.Mailboxes) == 0 && p.DisplayName == "" }

// AliasEdit changes where an alias forwards and what it sends as.
//
// Proton keeps the two behind endpoints of their own, so this is where one edit
// becomes the requests it takes - the same way Pass itself submits its one form.
func (s *Service) AliasEdit(ctx context.Context, shareID, itemID string, patch AliasPatch) error {
	if patch.empty() {
		return nil
	}
	if len(patch.Mailboxes) > 0 {
		detail, err := s.AliasDetails(ctx, shareID, itemID)
		if err != nil {
			return err
		}
		ids, err := pickMailboxes(detail.Available, patch.Mailboxes)
		if err != nil {
			return err
		}
		if err := s.C.Decode(ctx, proton.Request{
			Method: "POST", Path: fmt.Sprintf("/pass/v1/share/%s/alias/%s/mailbox", shareID, itemID),
			Body: map[string]any{"MailboxIDs": ids},
		}, nil); err != nil {
			return err
		}
	}
	if patch.DisplayName != "" {
		return s.C.Decode(ctx, proton.Request{
			Method: "PUT", Path: fmt.Sprintf("/pass/v1/share/%s/alias/%s/name", shareID, itemID),
			Body: map[string]any{"Name": patch.DisplayName},
		}, nil)
	}
	return nil
}

func (s *Service) AliasOptions(ctx context.Context, shareID string) ([]AliasSuffix, []AliasMailbox, error) {
	var r struct {
		Options struct {
			Suffixes []struct {
				Suffix, SignedSuffix, Domain string
				IsPremium, IsCustom          bool
			}
			Mailboxes []struct {
				ID    int
				Email string
			}
		}
	}
	if err := s.C.Decode(ctx, proton.Request{Method: "GET", Path: "/pass/v1/share/" + shareID + "/alias/options"}, &r); err != nil {
		return nil, nil, err
	}
	var sx []AliasSuffix
	for _, x := range r.Options.Suffixes {
		sx = append(sx, AliasSuffix{Suffix: x.Suffix, SignedSuffix: x.SignedSuffix, Domain: x.Domain, IsPremium: x.IsPremium, IsCustom: x.IsCustom})
	}
	var mx []AliasMailbox
	for _, m := range r.Options.Mailboxes {
		mx = append(mx, AliasMailbox{ID: m.ID, Email: m.Email})
	}
	return sx, mx, nil
}

// AliasPlan is the address an alias will have, worked out before it is made.
//
// Proton decides the tail: the suffixes it offers are invented on the spot,
// each signed, and whichever gets used must be sent back with the signature it
// came with. So the address is knowable in advance, but only by
// asking - which is also what makes a suffix nobody offered a refusal that
// arrives before anything exists.
type AliasPlan struct {
	// Address is what mail sent to the alias will be addressed to.
	Address string

	prefix    string
	signed    string
	mailboxes []int
}

// PlanAlias works out the address, the suffix to sign it with, and the mailboxes
// it forwards to, without making anything.
func (s *Service) PlanAlias(ctx context.Context, shareID, prefix, suffix string, mailboxes []string) (*AliasPlan, error) {
	suffixes, boxes, err := s.AliasOptions(ctx, shareID)
	if err != nil {
		return nil, err
	}
	chosen, err := pickSuffix(suffixes, suffix)
	if err != nil {
		return nil, err
	}
	ids, err := pickMailboxes(boxes, mailboxes)
	if err != nil {
		return nil, err
	}
	return &AliasPlan{
		Address: prefix + chosen.Suffix,
		prefix:  prefix, signed: chosen.SignedSuffix, mailboxes: ids,
	}, nil
}

// AliasCreate makes the alias the plan describes and returns the new item's ID.
func (s *Service) AliasCreate(ctx context.Context, shareID string, plan *AliasPlan, name string) (string, error) {
	sk, err := s.decryptShareKeys(ctx, shareID)
	if err != nil {
		return "", err
	}
	shareKey, rotation := sk.latest()
	item := &pb.Item{Metadata: &pb.Metadata{Name: name}, Content: &pb.Content{Content: &pb.Content_Alias{Alias: &pb.ItemAlias{}}}}
	itemKey, err := aead.NewKey()
	if err != nil {
		return "", err
	}
	pbBytes, _ := proto.Marshal(item)
	ct, err := aead.Encrypt(itemKey, pbBytes, []byte(aead.TagItemContent))
	if err != nil {
		return "", err
	}
	ek, err := aead.Encrypt(shareKey, itemKey, []byte(aead.TagItemKey))
	if err != nil {
		return "", err
	}
	var r struct{ Item struct{ ItemID string } }
	if err := s.C.Decode(ctx, proton.Request{
		Method: "POST", Path: "/pass/v1/share/" + shareID + "/alias/custom",
		Body: map[string]any{
			"Prefix":       plan.prefix,
			"SignedSuffix": plan.signed,
			"MailboxIDs":   plan.mailboxes,
			"Item": map[string]any{
				"Content":              base64.StdEncoding.EncodeToString(ct),
				"ContentFormatVersion": 7,
				"ItemKey":              base64.StdEncoding.EncodeToString(ek),
				"KeyRotation":          rotation,
			},
		},
	}, &r); err != nil {
		return "", err
	}
	return r.Item.ItemID, nil
}

func pickSuffix(s []AliasSuffix, wanted string) (AliasSuffix, error) {
	if wanted == "" {
		if len(s) == 0 {
			return AliasSuffix{}, fmt.Errorf("no alias suffixes available")
		}
		return s[0], nil
	}
	for _, x := range s {
		if x.Suffix == wanted || strings.HasSuffix(x.Suffix, wanted) {
			return x, nil
		}
	}
	// The domains, not the suffixes: Proton mints the word in front of a suffix
	// afresh every time it is asked, so naming the full form back would be
	// telling somebody to use a value that has already stopped working.
	avail := make([]string, 0, len(s))
	seen := make(map[string]bool, len(s))
	for _, x := range s {
		if !seen[x.Domain] {
			seen[x.Domain] = true
			avail = append(avail, x.Domain)
		}
	}
	return AliasSuffix{}, fmt.Errorf("no alias domain matching %q; available: %s",
		wanted, strings.Join(avail, ", "))
}

// pickMailboxes resolves the mailboxes an alias should arrive in. Naming none
// means the first Proton offers, which is what its own client fills in.
func pickMailboxes(m []AliasMailbox, wanted []string) ([]int, error) {
	if len(m) == 0 {
		return nil, fmt.Errorf("no mailboxes available")
	}
	if len(wanted) == 0 {
		return []int{m[0].ID}, nil
	}
	ids := make([]int, 0, len(wanted))
	for _, w := range wanted {
		id, err := pickMailbox(m, w)
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func pickMailbox(m []AliasMailbox, wanted string) (int, error) {
	for _, x := range m {
		if x.Email == wanted || strings.Contains(x.Email, wanted) {
			return x.ID, nil
		}
	}
	avail := make([]string, 0, len(m))
	for _, x := range m {
		avail = append(avail, x.Email)
	}
	return 0, fmt.Errorf("mailbox %q not found; available: %s", wanted, strings.Join(avail, ", "))
}
