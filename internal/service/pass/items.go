package pass

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/roman-16/proton-cli/internal/crypto/aead"
	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/roman-16/proton-cli/internal/fetch"
	"github.com/roman-16/proton-cli/internal/proton"
	"github.com/roman-16/proton-cli/internal/ref"
	pb "github.com/roman-16/proton-cli/internal/service/pass/proto"
	"google.golang.org/protobuf/proto"
)

// Item is what a listing knows about one item: what it is, where it lives, and
// the handles somebody addresses it by.
//
// It holds nothing that was locked away. A password, a card number, a private
// key and every hidden field are FullItem's, which is what `items get` reads and
// what says so in its own help - so a listing cannot spill a secret nobody asked
// for, whatever format it is printed in.
type Item struct {
	ShareID    string   `json:"share_id"`
	ItemID     string   `json:"item_id"`
	Revision   int      `json:"revision"`
	State      int      `json:"state"`
	Type       string   `json:"type"`
	CreateTime int64    `json:"create_time,omitempty"`
	ModifyTime int64    `json:"modify_time,omitempty"`
	Name       string   `json:"name,omitempty"`
	Username   string   `json:"username,omitempty"`
	Email      string   `json:"email,omitempty"`
	URLs       []string `json:"urls,omitempty"`

	// Shares is how many people hold this item on its own, which is what makes it
	// one of the things you have shared.
	Shares int `json:"shares,omitempty"`
	// Access is what this account may do with an item somebody shared with it,
	// and is empty for an item in a vault of your own.
	Access string `json:"access,omitempty"`

	// alias: the address and whether it is receiving. The route behind it takes a
	// request of its own, so it is read with the item rather than listed.
	Alias       string `json:"alias,omitempty"`
	AliasStatus string `json:"alias_status,omitempty"`
}

// FullItem is one item decrypted whole, secrets included.
type FullItem struct {
	Item
	Note       string `json:"note,omitempty"`
	Password   string `json:"password,omitempty"`
	TOTP       string `json:"totp,omitempty"`
	Holder     string `json:"holder,omitempty"`
	Number     string `json:"number,omitempty"`
	Expiry     string `json:"expiry,omitempty"`
	CVV        string `json:"cvv,omitempty"`
	PIN        string `json:"pin,omitempty"`
	SSID       string `json:"ssid,omitempty"`
	PublicKey  string `json:"public_key,omitempty"`
	PrivateKey string `json:"private_key,omitempty"`

	// Identity is what an identity item holds, keyed by the flag that sets it.
	// It is a map rather than thirty fields because Pass stores thirty and the
	// set is IdentityFields' to declare, not this struct's to repeat.
	Identity map[string]string `json:"identity,omitempty"`

	// the route behind an alias: where its mail arrives, what it sends as, and
	// what it has carried.
	AliasMailboxes   []string       `json:"alias_mailboxes,omitempty"`
	AliasDisplayName string         `json:"alias_display_name,omitempty"`
	AliasNote        string         `json:"alias_note,omitempty"`
	AliasActivity    *AliasActivity `json:"alias_activity,omitempty"`

	// extra custom fields (any item type)
	Fields []ItemField `json:"fields,omitempty"`

	raw *pb.Item
}

// aliasDisabled is the item flag Proton sets on an alias that has been switched
// off, so whether an alias is receiving is known from the item itself rather than
// from a request per address.
const aliasDisabled = 1 << 2

// aliasStatus is the word for an alias's switch. Only an alias has one.
func aliasStatus(kind string, flags int) string {
	switch {
	case kind != "alias":
		return ""
	case flags&aliasDisabled != 0:
		return "disabled"
	default:
		return "enabled"
	}
}

// ItemField is a custom extra field attached to an item.
type ItemField struct {
	Name string `json:"name"`
	// Section is the heading the field sits under, or "" for one that sits on
	// its own. Only the types whose editor offers headings can carry them.
	Section string `json:"section,omitempty"`
	Value   string `json:"value,omitempty"`
	Type    string `json:"type"`
}

// Ref renders the field the way --field accepts it.
func (f ItemField) Ref() string { return FieldRef(f.Section, f.Name) }

// ItemsList reads what is in every vault, or in the one named.
func (s *Service) ItemsList(ctx context.Context, vaultFilter string) ([]Item, error) {
	full, err := s.itemsFull(ctx, vaultFilter)
	if err != nil {
		return nil, err
	}
	out := make([]Item, 0, len(full))
	for _, it := range full {
		out = append(out, it.Item)
	}
	return out, nil
}

// itemsFull reads every item whole, which only a backup wants.
//
// The vaults are read at the same time and their items joined in the order the
// vaults came in, so the answer does not depend on which vault replied first. A
// vault that cannot be read is left out, as it was before.
func (s *Service) itemsFull(ctx context.Context, vaultFilter string) ([]FullItem, error) {
	vaults, err := s.VaultsList(ctx)
	if err != nil {
		return nil, err
	}
	wanted := make([]Vault, 0, len(vaults))
	for _, v := range vaults {
		if vaultFilter != "" && v.ShareID != vaultFilter && v.Name != vaultFilter {
			continue
		}
		wanted = append(wanted, v)
	}

	perVault := make([][]FullItem, len(wanted))
	fetches := make([]func(context.Context) error, len(wanted))
	for i, v := range wanted {
		fetches[i] = func(ctx context.Context) error {
			sk, err := s.decryptShareKeys(ctx, v.ShareID)
			if err != nil {
				return nil
			}
			items, err := s.fetchItems(ctx, v.ShareID, sk)
			if err != nil {
				return nil
			}
			perVault[i] = items
			return nil
		}
	}
	_ = fetch.Together(ctx, fetches...)

	var out []FullItem
	for _, items := range perVault {
		out = append(out, items...)
	}
	return out, nil
}

// SharedWithMe reads the items somebody shared with this account on their own.
//
// They are not in a vault of yours - the share points at the item and nothing
// else - so they are listed apart from `items list` and addressed by the ID this
// prints.
func (s *Service) SharedWithMe(ctx context.Context) ([]Item, error) {
	shares, err := s.shares(ctx)
	if err != nil {
		return nil, err
	}
	var wanted []Share
	for _, sh := range shares {
		if !sh.Vault() {
			wanted = append(wanted, sh)
		}
	}

	out := make([]Item, len(wanted))
	fetches := make([]func(context.Context) error, len(wanted))
	for i, sh := range wanted {
		fetches[i] = func(ctx context.Context) error {
			it, err := s.ItemGet(ctx, sh.ShareID, sh.TargetID)
			if err != nil {
				// An item whose content will not open is still an item you have
				// been given, and saying so is what lets somebody act on it.
				out[i] = Item{ShareID: sh.ShareID, ItemID: sh.TargetID, Access: sh.Access}
				return nil
			}
			it.Access = sh.Access
			out[i] = it.Item
			return nil
		}
	}
	_ = fetch.Together(ctx, fetches...)
	return out, nil
}

// SharedByMe reads the items you have shared on their own.
//
// Proton counts an item's shares on the item itself, so this costs no request a
// listing does not already make. A vault you share is not one of these: it is in
// `vaults list`, with the members it has.
func (s *Service) SharedByMe(ctx context.Context) ([]Item, error) {
	items, err := s.ItemsList(ctx, "")
	if err != nil {
		return nil, err
	}
	out := make([]Item, 0)
	for _, it := range items {
		if it.Shares > 0 {
			out = append(out, it)
		}
	}
	return out, nil
}

// ItemMove puts an item in another vault.
//
// The item is sealed under the key of the share it lives in, so moving it means
// handing Proton the same content sealed under the destination's key. It keeps
// its history and gets a new item ID, because in Pass an item is only unique
// with its share.
func (s *Service) ItemMove(ctx context.Context, shareID, itemID, toShareID string) (string, error) {
	if shareID == toShareID {
		return itemID, nil
	}
	keys, err := s.itemKeys(ctx, shareID, itemID)
	if err != nil {
		return "", err
	}
	destination, err := s.decryptShareKeys(ctx, toShareID)
	if err != nil {
		return "", err
	}
	destinationKey, rotation := destination.latest()

	rotations := make([]int, 0, len(keys))
	for r := range keys {
		rotations = append(rotations, r)
	}
	slices.Sort(rotations)

	sealed := make([]map[string]any, 0, len(rotations))
	for _, r := range rotations {
		enc, err := aead.Encrypt(destinationKey, keys[r], []byte(aead.TagItemKey))
		if err != nil {
			return "", err
		}
		sealed = append(sealed, map[string]any{
			"Key":         base64.StdEncoding.EncodeToString(enc),
			"KeyRotation": rotation,
		})
	}

	var r struct {
		Items []struct{ ItemID string }
	}
	if err := s.C.Decode(ctx, proton.Request{
		Method: "PUT", Path: "/pass/v1/share/" + shareID + "/item/share",
		Body: map[string]any{
			"ShareID": toShareID,
			"Items":   []map[string]any{{"ItemID": itemID, "ItemKeys": sealed}},
		},
	}, &r); err != nil {
		return "", err
	}
	if len(r.Items) == 0 {
		return "", fmt.Errorf("the move was accepted but named no item")
	}
	return r.Items[0].ItemID, nil
}

func (s *Service) ItemGet(ctx context.Context, shareID, itemID string) (*FullItem, error) {
	sk, err := s.decryptShareKeys(ctx, shareID)
	if err != nil {
		return nil, err
	}
	var r struct {
		Item struct {
			ItemID           string
			Revision         int
			State            int
			Flags            int
			Content, ItemKey string
			KeyRotation      int
			CreateTime       int64
			ModifyTime       int64
			AliasEmail       string
		}
	}
	if err := s.C.Decode(ctx, proton.Request{Method: "GET", Path: fmt.Sprintf("/pass/v1/share/%s/item/%s", shareID, itemID)}, &r); err != nil {
		return nil, err
	}
	shareKey, ok := sk.keys[r.Item.KeyRotation]
	if !ok {
		return nil, fmt.Errorf("no share key for rotation %d", r.Item.KeyRotation)
	}
	ikBytes, err := base64.StdEncoding.DecodeString(r.Item.ItemKey)
	if err != nil {
		return nil, err
	}
	itemKey, err := aead.Decrypt(shareKey, ikBytes, []byte(aead.TagItemKey))
	if err != nil {
		return nil, err
	}
	cBytes, err := base64.StdEncoding.DecodeString(r.Item.Content)
	if err != nil {
		return nil, err
	}
	plain, err := aead.Decrypt(itemKey, cBytes, []byte(aead.TagItemContent))
	if err != nil {
		return nil, err
	}
	var it pb.Item
	if err := proto.Unmarshal(plain, &it); err != nil {
		return nil, err
	}
	out := itemFromProto(&it)
	out.ShareID = shareID
	out.ItemID = r.Item.ItemID
	out.Revision = r.Item.Revision
	out.State = r.Item.State
	out.CreateTime = r.Item.CreateTime
	out.ModifyTime = r.Item.ModifyTime
	out.Alias = r.Item.AliasEmail
	out.AliasStatus = aliasStatus(out.Type, r.Item.Flags)
	if out.Type != "alias" {
		return out, nil
	}
	// An address with nothing behind it is half an answer, so reading an alias
	// reads its route too: where its mail arrives, what it sends as, and what it
	// has carried. Pass asks the same question when it opens one.
	detail, err := s.AliasDetails(ctx, shareID, itemID)
	if err != nil {
		return nil, err
	}
	for _, m := range detail.Mailboxes {
		out.AliasMailboxes = append(out.AliasMailboxes, m.Email)
	}
	out.AliasDisplayName = detail.DisplayName
	out.AliasNote = detail.Note
	out.AliasActivity = &detail.Activity
	return out, nil
}

func (s *Service) ResolveItem(ctx context.Context, args []string) (string, string, error) {
	if len(args) == 2 {
		return args[0], args[1], nil
	}
	needle := strings.ToLower(args[0])
	items, err := s.ItemsList(ctx, "")
	if err != nil {
		return "", "", err
	}
	// An item somebody shared on its own is in no vault of yours, and a name is
	// the only handle it has: it is reachable here or not at all.
	shared, err := s.SharedWithMe(ctx)
	if err != nil {
		return "", "", err
	}
	items = append(items, shared...)
	// An exact item-ID match wins outright, so the ID printed by `items create`
	// round-trips as a single REF to get/edit/delete.
	for _, it := range items {
		if it.ItemID == args[0] {
			return it.ShareID, it.ItemID, nil
		}
	}
	var matches []Item
	for _, it := range items {
		if strings.Contains(strings.ToLower(it.Name), needle) {
			matches = append(matches, it)
			continue
		}
		if it.Alias != "" && strings.Contains(strings.ToLower(it.Alias), needle) {
			matches = append(matches, it)
			continue
		}
		for _, url := range it.URLs {
			if strings.Contains(strings.ToLower(url), needle) {
				matches = append(matches, it)
				break
			}
		}
	}
	it, err := ref.Pick("item", args[0], matches,
		func(i Item) string { return i.ItemID },
		func(i Item) string { return fmt.Sprintf("%s  %s  (share %s)", i.Type, i.Name, i.ShareID) })
	if err != nil {
		return "", "", err
	}
	return it.ShareID, it.ItemID, nil
}

type NewItem struct {
	Type                                       string
	Name, Username, Password, Email, URL, Note string
	TOTP                                       string
	Holder, Number, Expiry, CVV, PIN           string
	SSID, WifiSecurity                         string
	PrivateKey, PublicKey                      string
	// identity
	FullName, FirstName, LastName, PhoneNumber string
	Organization, JobTitle                     string
	StreetAddress, City, PostalCode, Country   string
	Birthdate, Website                         string
	// extra custom fields, each "NAME=VALUE"
	// ExtraFields are the custom fields the item carries beyond its type's own.
	ExtraFields ExtraFields
	// Identity is an identity item's fields, keyed by the flag that sets it.
	Identity map[string]string
}

func extraFieldToItem(section string, f *pb.ExtraField) ItemField {
	out := ItemField{Name: f.FieldName, Section: section, Type: "unknown"}
	switch c := f.Content.(type) {
	case *pb.ExtraField_Text:
		out.Value, out.Type = c.Text.Content, "text"
	case *pb.ExtraField_Hidden:
		out.Value, out.Type = c.Hidden.Content, "hidden"
	case *pb.ExtraField_Totp:
		out.Value, out.Type = c.Totp.TotpUri, "totp"
	}
	return out
}

// wifiSecurity maps a CLI security string to the protobuf enum; an unknown or
// empty value falls back to unspecified.
func wifiSecurity(s string) pb.WifiSecurity {
	switch strings.ToUpper(s) {
	case "WPA":
		return pb.WifiSecurity_WPA
	case "WPA2":
		return pb.WifiSecurity_WPA2
	case "WPA3":
		return pb.WifiSecurity_WPA3
	case "WEP":
		return pb.WifiSecurity_WEP
	default:
		return pb.WifiSecurity_UnspecifiedWifiSecurity
	}
}

func (s *Service) ItemCreate(ctx context.Context, shareID string, nc NewItem) (string, error) {
	sk, err := s.decryptShareKeys(ctx, shareID)
	if err != nil {
		return "", err
	}
	shareKey, rotation := sk.latest()

	item := &pb.Item{Metadata: &pb.Metadata{Name: nc.Name, Note: nc.Note}, Content: &pb.Content{}}
	switch nc.Type {
	case "login":
		urls := []string{}
		if nc.URL != "" {
			urls = append(urls, nc.URL)
		}
		item.Content.Content = &pb.Content_Login{Login: &pb.ItemLogin{
			ItemUsername: nc.Username, ItemEmail: nc.Email, Password: nc.Password, Urls: urls, TotpUri: nc.TOTP,
		}}
	case "note":
		item.Content.Content = &pb.Content_Note{Note: &pb.ItemNote{}}
	case "credit-card":
		item.Content.Content = &pb.Content_CreditCard{CreditCard: &pb.ItemCreditCard{
			CardholderName: nc.Holder, Number: nc.Number, ExpirationDate: nc.Expiry,
			VerificationNumber: nc.CVV, Pin: nc.PIN,
		}}
	case "wifi":
		item.Content.Content = &pb.Content_Wifi{Wifi: &pb.ItemWifi{
			Ssid: nc.SSID, Password: nc.Password, Security: wifiSecurity(nc.WifiSecurity),
		}}
	case "ssh-key":
		item.Content.Content = &pb.Content_SshKey{SshKey: &pb.ItemSSHKey{
			PrivateKey: nc.PrivateKey, PublicKey: nc.PublicKey,
		}}
	case "identity":
		item.Content.Content = &pb.Content_Identity{Identity: buildIdentity(nc.Identity)}
	case "custom":
		item.Content.Content = &pb.Content_Custom{Custom: &pb.ItemCustom{}}
	default:
		return "", fmt.Errorf("unsupported item type %q (supported: login, note, credit-card, wifi, ssh-key, identity, custom)", nc.Type)
	}
	fields, err := parseExtraFields(nc.ExtraFields)
	if err != nil {
		return "", err
	}
	loose, sections := split(fields)
	item.ExtraFields = loose
	if len(sections) > 0 && !setSections(item.Content, sections) {
		return "", fmt.Errorf("a %s item has no sections to put a field under", nc.Type)
	}

	return s.putItem(ctx, shareID, shareKey, rotation, item)
}

// contentFormatVersion is the version of the item protobuf this writes. It
// travels with every item, so a reader knows how to take the content apart, and
// an export says the same thing about the items it carries.
const contentFormatVersion = 7

// putItem seals one item under a fresh key of its own and stores it.
//
// Creating an item and reading one out of a backup differ only in where the
// protobuf came from, so they seal and send it the same way.
func (s *Service) putItem(ctx context.Context, shareID string, shareKey []byte, rotation int, item *pb.Item) (string, error) {
	itemKey, err := aead.NewKey()
	if err != nil {
		return "", err
	}
	pbBytes, err := proto.Marshal(item)
	if err != nil {
		return "", err
	}
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
		Method: "POST", Path: "/pass/v1/share/" + shareID + "/item",
		Body: map[string]any{
			"Content":              base64.StdEncoding.EncodeToString(ct),
			"ContentFormatVersion": contentFormatVersion,
			"ItemKey":              base64.StdEncoding.EncodeToString(ek),
			"KeyRotation":          rotation,
		},
	}, &r); err != nil {
		return "", err
	}
	return r.Item.ItemID, nil
}

type Patch struct {
	Scalars
	// Identity is an identity item's fields, keyed by the flag that sets it.
	Identity map[string]string
	// ExtraFields are custom fields to set, each NAME=VALUE or
	// SECTION/NAME=VALUE. One that names an existing field replaces its value;
	// the rest of them are left alone.
	ExtraFields ExtraFields
}

// Scalars are the single-valued fields a patch can carry. They are their own
// struct so that Empty can compare them, which a struct holding a map cannot do.
type Scalars struct {
	Name, Username, Password, Email, URL, Note string
	TOTP                                       string
	Holder, Number, Expiry, CVV, PIN           string
	SSID, WifiSecurity                         string
	PrivateKey, PublicKey                      string
}

// Empty reports whether the patch changes nothing about the item, which is what
// an edit that only moved an alias's forwarding leaves behind.
func (p Patch) Empty() bool {
	return p.Scalars == Scalars{} && len(p.Identity) == 0 && p.ExtraFields.Empty()
}

func (s *Service) ItemEdit(ctx context.Context, shareID, itemID string, patch Patch) error {
	if patch.Empty() {
		return nil
	}
	sk, err := s.decryptShareKeys(ctx, shareID)
	if err != nil {
		return err
	}
	var r struct {
		Item struct {
			Revision         int
			Content, ItemKey string
			KeyRotation      int
		}
	}
	if err := s.C.Decode(ctx, proton.Request{Method: "GET", Path: fmt.Sprintf("/pass/v1/share/%s/item/%s", shareID, itemID)}, &r); err != nil {
		return err
	}
	shareKey, ok := sk.keys[r.Item.KeyRotation]
	if !ok {
		return fmt.Errorf("no share key for rotation %d", r.Item.KeyRotation)
	}
	ikBytes, err := base64.StdEncoding.DecodeString(r.Item.ItemKey)
	if err != nil {
		return fmt.Errorf("decode item key: %w", err)
	}
	itemKey, err := aead.Decrypt(shareKey, ikBytes, []byte(aead.TagItemKey))
	if err != nil {
		return err
	}
	cBytes, err := base64.StdEncoding.DecodeString(r.Item.Content)
	if err != nil {
		return fmt.Errorf("decode item content: %w", err)
	}
	plain, err := aead.Decrypt(itemKey, cBytes, []byte(aead.TagItemContent))
	if err != nil {
		return err
	}
	var it pb.Item
	if err := proto.Unmarshal(plain, &it); err != nil {
		return err
	}
	if it.Metadata == nil {
		it.Metadata = &pb.Metadata{}
	}
	if patch.Name != "" {
		it.Metadata.Name = patch.Name
	}
	if patch.Note != "" {
		it.Metadata.Note = patch.Note
	}
	if err := patchExtraFields(&it, patch); err != nil {
		return err
	}
	if it.Content != nil {
		switch content := it.Content.Content.(type) {
		case *pb.Content_Login:
			l := content.Login
			if patch.Username != "" {
				l.ItemUsername = patch.Username
			}
			if patch.Password != "" {
				l.Password = patch.Password
			}
			if patch.Email != "" {
				l.ItemEmail = patch.Email
			}
			if patch.URL != "" {
				l.Urls = []string{patch.URL}
			}
			if patch.TOTP != "" {
				l.TotpUri = patch.TOTP
			}
		case *pb.Content_CreditCard:
			cc := content.CreditCard
			if patch.Holder != "" {
				cc.CardholderName = patch.Holder
			}
			if patch.Number != "" {
				cc.Number = patch.Number
			}
			if patch.Expiry != "" {
				cc.ExpirationDate = patch.Expiry
			}
			if patch.CVV != "" {
				cc.VerificationNumber = patch.CVV
			}
			if patch.PIN != "" {
				cc.Pin = patch.PIN
			}
		case *pb.Content_Wifi:
			w := content.Wifi
			if patch.SSID != "" {
				w.Ssid = patch.SSID
			}
			if patch.Password != "" {
				w.Password = patch.Password
			}
			if patch.WifiSecurity != "" {
				w.Security = wifiSecurity(patch.WifiSecurity)
			}
		case *pb.Content_SshKey:
			k := content.SshKey
			if patch.PrivateKey != "" {
				k.PrivateKey = patch.PrivateKey
			}
			if patch.PublicKey != "" {
				k.PublicKey = patch.PublicKey
			}
		case *pb.Content_Identity:
			patchIdentity(content.Identity, patch.Identity)
		}
	}
	pbBytes, err := proto.Marshal(&it)
	if err != nil {
		return err
	}

	// The rotation sent has to be the rotation of the key the content was encrypted
	// with. Encrypting with one key and naming another labels the ciphertext with a
	// key that cannot open it, which is what happens when a rotation lands between
	// reading the item and writing it back.
	writeKey, rotation, err := s.latestItemKey(ctx, sk, shareID, itemID)
	if err != nil {
		return err
	}
	ct, err := aead.Encrypt(writeKey, pbBytes, []byte(aead.TagItemContent))
	if err != nil {
		return err
	}
	return s.C.Decode(ctx, proton.Request{
		Method: "PUT", Path: fmt.Sprintf("/pass/v1/share/%s/item/%s", shareID, itemID),
		Body: map[string]any{
			"Content":              base64.StdEncoding.EncodeToString(ct),
			"ContentFormatVersion": 7,
			"KeyRotation":          rotation,
			"LastRevision":         r.Item.Revision,
		},
	}, nil)
}

// latestItemKey opens the item's newest key and returns it with its rotation, so a
// write re-encrypts under the key that is current rather than the one the revision
// happened to be stored with.
func (s *Service) latestItemKey(ctx context.Context, sk *shareKeys, shareID, itemID string) ([]byte, int, error) {
	var r struct {
		Key struct {
			Key         string
			KeyRotation int
		}
	}
	if err := s.C.Decode(ctx, proton.Request{
		Method: "GET", Path: fmt.Sprintf("/pass/v1/share/%s/item/%s/key/latest", shareID, itemID),
	}, &r); err != nil {
		return nil, 0, fmt.Errorf("get the item's latest key: %w", err)
	}
	shareKey, ok := sk.keys[r.Key.KeyRotation]
	if !ok {
		return nil, 0, fmt.Errorf("no share key for rotation %d", r.Key.KeyRotation)
	}
	encoded, err := base64.StdEncoding.DecodeString(r.Key.Key)
	if err != nil {
		return nil, 0, fmt.Errorf("decode the item's latest key: %w", err)
	}
	itemKey, err := aead.Decrypt(shareKey, encoded, []byte(aead.TagItemKey))
	if err != nil {
		return nil, 0, fmt.Errorf("open the item's latest key: %w", err)
	}
	return itemKey, r.Key.KeyRotation, nil
}

func (s *Service) ItemTrash(ctx context.Context, shareID, itemID string) error {
	rev, err := s.itemRevision(ctx, shareID, itemID)
	if err != nil {
		return err
	}
	return s.C.Decode(ctx, proton.Request{
		Method: "POST", Path: "/pass/v1/share/" + shareID + "/item/trash",
		Body: map[string]any{"Items": []map[string]any{{"ItemID": itemID, "Revision": rev}}},
	}, nil)
}

func (s *Service) ItemRestore(ctx context.Context, shareID, itemID string) error {
	rev, err := s.itemRevision(ctx, shareID, itemID)
	if err != nil {
		return err
	}
	return s.C.Decode(ctx, proton.Request{
		Method: "POST", Path: "/pass/v1/share/" + shareID + "/item/untrash",
		Body: map[string]any{"Items": []map[string]any{{"ItemID": itemID, "Revision": rev}}},
	}, nil)
}

// ItemDelete must trash an active item first; the API rejects deleting one
// that isn't already in the trash.
func (s *Service) ItemDelete(ctx context.Context, shareID, itemID string) error {
	var r struct {
		Item struct {
			Revision int
			State    int
		}
	}
	if err := s.C.Decode(ctx, proton.Request{Method: "GET", Path: fmt.Sprintf("/pass/v1/share/%s/item/%s", shareID, itemID)}, &r); err != nil {
		return err
	}
	if r.Item.State != 2 {
		if err := s.ItemTrash(ctx, shareID, itemID); err != nil {
			return err
		}
		if err := s.C.Decode(ctx, proton.Request{Method: "GET", Path: fmt.Sprintf("/pass/v1/share/%s/item/%s", shareID, itemID)}, &r); err != nil {
			return err
		}
	}
	return s.C.Decode(ctx, proton.Request{
		Method: "DELETE", Path: "/pass/v1/share/" + shareID + "/item",
		Body: map[string]any{"Items": []map[string]any{{"ItemID": itemID, "Revision": r.Item.Revision}}},
	}, nil)
}

func (s *Service) itemRevision(ctx context.Context, shareID, itemID string) (int, error) {
	var r struct{ Item struct{ Revision int } }
	if err := s.C.Decode(ctx, proton.Request{Method: "GET", Path: fmt.Sprintf("/pass/v1/share/%s/item/%s", shareID, itemID)}, &r); err != nil {
		return 0, err
	}
	return r.Item.Revision, nil
}

func (s *Service) fetchItems(ctx context.Context, shareID string, sk *shareKeys) ([]FullItem, error) {
	var out []FullItem
	var since string
	for {
		qv := proton.Query()
		if since != "" {
			qv.Set("Since", since)
		}
		var r struct {
			Items struct {
				RevisionsData []json.RawMessage
				LastToken     string
			}
		}
		if err := s.C.Decode(ctx, proton.Request{Method: "GET", Path: "/pass/v1/share/" + shareID + "/item", Query: qv}, &r); err != nil {
			return nil, err
		}
		for _, raw := range r.Items.RevisionsData {
			var enc struct {
				ItemID           string
				Revision         int
				State            int
				Flags            int
				Content, ItemKey string
				KeyRotation      int
				CreateTime       int64
				ModifyTime       int64
				AliasEmail       string
				ShareCount       int
			}
			if err := json.Unmarshal(raw, &enc); err != nil {
				continue
			}
			if enc.State != 1 {
				continue
			}
			shareKey, ok := sk.keys[enc.KeyRotation]
			if !ok {
				continue
			}
			ikBytes, err := base64.StdEncoding.DecodeString(enc.ItemKey)
			if err != nil {
				continue
			}
			itemKey, err := aead.Decrypt(shareKey, ikBytes, []byte(aead.TagItemKey))
			if err != nil {
				continue
			}
			cBytes, err := base64.StdEncoding.DecodeString(enc.Content)
			if err != nil {
				continue
			}
			plain, err := aead.Decrypt(itemKey, cBytes, []byte(aead.TagItemContent))
			if err != nil {
				continue
			}
			var it pb.Item
			if err := proto.Unmarshal(plain, &it); err != nil {
				continue
			}
			item := itemFromProto(&it)

			item.ShareID = shareID
			item.ItemID = enc.ItemID
			item.Revision = enc.Revision
			item.State = enc.State
			item.CreateTime = enc.CreateTime
			item.ModifyTime = enc.ModifyTime
			item.Alias = enc.AliasEmail
			item.AliasStatus = aliasStatus(item.Type, enc.Flags)
			item.Shares = enc.ShareCount
			out = append(out, *item)
		}
		if r.Items.LastToken == "" || len(r.Items.RevisionsData) == 0 {
			break
		}
		since = r.Items.LastToken
	}
	return out, nil
}

func itemFromProto(it *pb.Item) *FullItem {
	item := &FullItem{raw: it}
	item.Type = itemTypeName(it)
	if it.Metadata != nil {
		item.Name = it.Metadata.Name
		item.Note = it.Metadata.Note
	}
	if it.Content == nil || it.Content.Content == nil {
		return item
	}
	switch c := it.Content.Content.(type) {
	case *pb.Content_Login:
		item.Username = c.Login.ItemUsername
		item.Email = c.Login.ItemEmail
		item.Password = c.Login.Password
		item.TOTP = c.Login.TotpUri
		item.URLs = c.Login.Urls
	case *pb.Content_CreditCard:
		item.Holder = c.CreditCard.CardholderName
		item.Number = c.CreditCard.Number
		item.Expiry = c.CreditCard.ExpirationDate
		item.CVV = c.CreditCard.VerificationNumber
		item.PIN = c.CreditCard.Pin
	case *pb.Content_Wifi:
		item.SSID = c.Wifi.Ssid
		item.Password = c.Wifi.Password
	case *pb.Content_SshKey:
		item.PrivateKey = c.SshKey.PrivateKey
		item.PublicKey = c.SshKey.PublicKey
	case *pb.Content_Identity:
		item.Identity = readIdentity(c.Identity)
	}
	for _, f := range it.ExtraFields {
		item.Fields = append(item.Fields, extraFieldToItem("", f))
	}
	for _, s := range sectionsOf(it.Content) {
		for _, f := range s.GetSectionFields() {
			item.Fields = append(item.Fields, extraFieldToItem(s.GetSectionName(), f))
		}
	}
	return item
}

func itemTypeName(it *pb.Item) string {
	if it.Content == nil || it.Content.Content == nil {
		return "unknown"
	}
	switch it.Content.Content.(type) {
	case *pb.Content_Login:
		return "login"
	case *pb.Content_Note:
		return "note"
	case *pb.Content_Alias:
		return "alias"
	case *pb.Content_CreditCard:
		return "credit-card"
	case *pb.Content_Identity:
		return "identity"
	case *pb.Content_SshKey:
		return "ssh-key"
	case *pb.Content_Wifi:
		return "wifi"
	case *pb.Content_Custom:
		return "custom"
	}
	return "unknown"
}

// ── pinning ──

// ItemPin puts an item at the top of the list, and ItemUnpin takes it back down.
//
// Pinning carries no content, so nothing is encrypted or re-encrypted: it is the
// vault recording that one of its items is wanted often.
func (s *Service) ItemPin(ctx context.Context, shareID, itemID string, pinned bool) error {
	method := "POST"
	if !pinned {
		method = "DELETE"
	}
	return s.C.Decode(ctx, proton.Request{
		Method: method,
		Path:   fmt.Sprintf("/pass/v1/share/%s/item/%s/pin", shareID, itemID),
	}, nil)
}

// ── history ──

// Revision is one earlier state of an item.
//
// Pass keeps every edit, so a password changed by mistake is recoverable by
// reading what it was. The content is decrypted the same way the current item is,
// because a revision is the item as it stood.
type Revision struct {
	Revision   int   `json:"revision"`
	CreateTime int64 `json:"create_time,omitempty"`
	ModifyTime int64 `json:"modify_time,omitempty"`
	// Item is what the revision was, without what it kept: reading one of those
	// back is `revisions get`, one revision at a time.
	Item *Item `json:"item"`
}

// ItemHistory reads what an item used to be, newest first.
func (s *Service) ItemHistory(ctx context.Context, shareID, itemID string) ([]Revision, error) {
	full, err := s.itemHistory(ctx, shareID, itemID)
	if err != nil {
		return nil, err
	}
	out := make([]Revision, 0, len(full))
	for _, rev := range full {
		row := Revision{Revision: rev.Revision, CreateTime: rev.CreateTime, ModifyTime: rev.ModifyTime}
		if rev.Item != nil {
			row.Item = &rev.Item.Item
		}
		out = append(out, row)
	}
	return out, nil
}

// RevisionGet reads one earlier state whole.
//
// It is the command for reading a password an item used to have, so it decrypts
// that revision and nothing else: the history beside it stays a list of what
// changed and when.
func (s *Service) RevisionGet(ctx context.Context, shareID, itemID string, revision int) (*FullItem, error) {
	history, err := s.itemHistory(ctx, shareID, itemID)
	if err != nil {
		return nil, err
	}
	for _, rev := range history {
		if rev.Revision != revision {
			continue
		}
		if rev.Item == nil {
			return nil, fmt.Errorf("revision %d was written under a key this account no longer holds", revision)
		}
		return rev.Item, nil
	}
	return nil, &errs.NotFound{Kind: "revision", Ref: strconv.Itoa(revision)}
}

// fullRevision is one earlier state with its content still in it.
type fullRevision struct {
	Revision   int
	CreateTime int64
	ModifyTime int64
	Item       *FullItem
}

func (s *Service) itemHistory(ctx context.Context, shareID, itemID string) ([]fullRevision, error) {
	sk, err := s.decryptShareKeys(ctx, shareID)
	if err != nil {
		return nil, err
	}
	var r struct {
		Revisions struct {
			RevisionsData []struct {
				ItemID           string
				Revision         int
				State            int
				Flags            int
				Content, ItemKey string
				KeyRotation      int
				CreateTime       int64
				ModifyTime       int64
				AliasEmail       string
			}
		}
	}
	q := proton.Query("PageSize", "50")
	if err := s.C.Decode(ctx, proton.Request{
		Method: "GET",
		Path:   fmt.Sprintf("/pass/v1/share/%s/item/%s/revision", shareID, itemID),
		Query:  q,
	}, &r); err != nil {
		return nil, err
	}
	out := make([]fullRevision, 0, len(r.Revisions.RevisionsData))
	for _, rev := range r.Revisions.RevisionsData {
		shareKey, ok := sk.keys[rev.KeyRotation]
		if !ok {
			continue
		}
		item, err := decodeItem(shareKey, rev.Content, rev.ItemKey)
		if err != nil {
			// A revision written under a key this account no longer holds is
			// still part of the history, so it is reported by its number rather
			// than dropped.
			out = append(out, fullRevision{
				Revision: rev.Revision, CreateTime: rev.CreateTime, ModifyTime: rev.ModifyTime,
			})
			continue
		}
		item.ShareID, item.ItemID = shareID, rev.ItemID
		item.Revision, item.State = rev.Revision, rev.State
		item.CreateTime, item.ModifyTime = rev.CreateTime, rev.ModifyTime
		item.Alias = rev.AliasEmail
		item.AliasStatus = aliasStatus(item.Type, rev.Flags)
		out = append(out, fullRevision{
			Revision: rev.Revision, CreateTime: rev.CreateTime,
			ModifyTime: rev.ModifyTime, Item: item,
		})
	}
	// Newest first, which is the order somebody looking for "what did it used to
	// be" reads in. Sorted rather than reversed, so the answer does not depend on
	// which way round Proton happened to send the page.
	slices.SortFunc(out, func(a, b fullRevision) int { return b.Revision - a.Revision })
	return out, nil
}

// decodeItem unwraps one item's content with the share key that sealed it. The
// current item and every revision of it are stored the same way, so both read
// through here.
func decodeItem(shareKey []byte, content, itemKey string) (*FullItem, error) {
	ikBytes, err := base64.StdEncoding.DecodeString(itemKey)
	if err != nil {
		return nil, err
	}
	key, err := aead.Decrypt(shareKey, ikBytes, []byte(aead.TagItemKey))
	if err != nil {
		return nil, err
	}
	cBytes, err := base64.StdEncoding.DecodeString(content)
	if err != nil {
		return nil, err
	}
	plain, err := aead.Decrypt(key, cBytes, []byte(aead.TagItemContent))
	if err != nil {
		return nil, err
	}
	var it pb.Item
	if err := proto.Unmarshal(plain, &it); err != nil {
		return nil, err
	}
	return itemFromProto(&it), nil
}
