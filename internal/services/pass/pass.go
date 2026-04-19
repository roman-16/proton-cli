// Package pass provides Proton Pass vault, item and alias operations.
package pass

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	pgp "github.com/ProtonMail/gopenpgp/v2/crypto"
	"github.com/roman-16/proton-cli/internal/aead"
	"github.com/roman-16/proton-cli/internal/api"
	"github.com/roman-16/proton-cli/internal/keys"
	pb "github.com/roman-16/proton-cli/internal/proto"
	"google.golang.org/protobuf/proto"
)

// Service is the Pass domain service.
type Service struct{ C *api.Client }

// New constructs a pass service.
func New(c *api.Client) *Service { return &Service{C: c} }

// Vault is a decrypted Pass vault.
type Vault struct {
	ShareID     string `json:"share_id"`
	VaultID     string `json:"vault_id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Owner       bool   `json:"owner"`
	Shared      bool   `json:"shared"`
	Members     int    `json:"members"`
	AddressID   string `json:"address_id,omitempty"`
}

// Item is a decrypted Pass item.
type Item struct {
	ShareID    string   `json:"share_id"`
	ItemID     string   `json:"item_id"`
	Revision   int      `json:"revision"`
	State      int      `json:"state"`
	Type       string   `json:"type"`
	CreateTime int64    `json:"create_time,omitempty"`
	ModifyTime int64    `json:"modify_time,omitempty"`
	Name       string   `json:"name,omitempty"`
	Note       string   `json:"note,omitempty"`
	Username   string   `json:"username,omitempty"`
	Email      string   `json:"email,omitempty"`
	Password   string   `json:"password,omitempty"`
	TOTP       string   `json:"totp,omitempty"`
	URLs       []string `json:"urls,omitempty"`
	// Credit card
	Holder string `json:"holder,omitempty"`
	Number string `json:"number,omitempty"`
	Expiry string `json:"expiry,omitempty"`
	CVV    string `json:"cvv,omitempty"`
	PIN    string `json:"pin,omitempty"`
	// Wi-Fi
	SSID string `json:"ssid,omitempty"`

	raw *pb.Item
}

// shareKeys holds decrypted share keys keyed by rotation.
type shareKeys struct{ keys map[int][]byte }

func (sk *shareKeys) latest() ([]byte, int) {
	max := -1
	for r := range sk.keys {
		if r > max {
			max = r
		}
	}
	return sk.keys[max], max
}

// VaultsList returns all vaults (decrypted where possible).
func (s *Service) VaultsList(ctx context.Context, u *keys.Unlocked) ([]Vault, error) {
	shares, err := s.getShares(ctx)
	if err != nil {
		return nil, err
	}
	var out []Vault
	for _, raw := range shares {
		var sh struct {
			ShareID            string
			VaultID            string
			TargetType         int
			Owner              bool
			Shared             bool
			TargetMembers      int
			AddressID          string
			Content            string
			ContentKeyRotation int
		}
		if err := json.Unmarshal(raw, &sh); err != nil {
			continue
		}
		if sh.TargetType != 1 {
			continue
		}
		v := Vault{
			ShareID: sh.ShareID, VaultID: sh.VaultID,
			Owner: sh.Owner, Shared: sh.Shared,
			Members: sh.TargetMembers, AddressID: sh.AddressID,
		}
		if sh.Content != "" {
			sk, err := s.decryptShareKeys(ctx, sh.ShareID, u)
			if err == nil {
				if key, ok := sk.keys[sh.ContentKeyRotation]; ok {
					if vv, err := decryptVault(sh.Content, key); err == nil {
						v.Name = vv.Name
						v.Description = vv.Description
					}
				}
			}
		}
		out = append(out, v)
	}
	return out, nil
}

// VaultCreate creates a new vault.
func (s *Service) VaultCreate(ctx context.Context, u *keys.Unlocked, name string) ([]byte, error) {
	vault := &pb.Vault{Name: name}
	rawKey, err := aead.NewKey()
	if err != nil {
		return nil, err
	}
	msg := pgp.NewPlainMessage(rawKey)
	encKey, err := u.UserKR.Encrypt(msg, u.UserKR)
	if err != nil {
		return nil, err
	}
	encVaultKey := base64.StdEncoding.EncodeToString(encKey.GetBinary())
	pbBytes, err := proto.Marshal(vault)
	if err != nil {
		return nil, err
	}
	ct, err := aead.Encrypt(rawKey, pbBytes, []byte(aead.TagVaultContent))
	if err != nil {
		return nil, err
	}
	_, addrID, _, err := u.PrimaryAddrKR()
	if err != nil {
		return nil, err
	}
	resp, err := s.C.Do(ctx, api.Request{
		Method: "POST", Path: "/pass/v1/vault",
		Body: map[string]any{
			"AddressID":            addrID,
			"ContentFormatVersion": 1,
			"Content":              base64.StdEncoding.EncodeToString(ct),
			"EncryptedVaultKey":    encVaultKey,
		},
	})
	if err != nil {
		return nil, err
	}
	return resp.Body, nil
}

// VaultDelete deletes a vault by share ID.
func (s *Service) VaultDelete(ctx context.Context, shareID string) error {
	return s.C.Send(ctx, api.Request{Method: "DELETE", Path: "/pass/v1/vault/" + shareID}, nil)
}

// ResolveVault returns a shareID for a vault name or ID.
func (s *Service) ResolveVault(ctx context.Context, u *keys.Unlocked, nameOrID string) (string, error) {
	vaults, err := s.VaultsList(ctx, u)
	if err != nil {
		return "", err
	}
	if nameOrID == "" {
		if len(vaults) == 0 {
			return "", fmt.Errorf("no vaults found")
		}
		return vaults[0].ShareID, nil
	}
	for _, v := range vaults {
		if v.ShareID == nameOrID {
			return v.ShareID, nil
		}
	}
	for _, v := range vaults {
		if v.Name == nameOrID {
			return v.ShareID, nil
		}
	}
	return "", fmt.Errorf("vault %q not found", nameOrID)
}

// ItemsList returns all items across vaults, optionally filtered by vault.
func (s *Service) ItemsList(ctx context.Context, u *keys.Unlocked, vaultFilter string) ([]Item, error) {
	vaults, err := s.VaultsList(ctx, u)
	if err != nil {
		return nil, err
	}
	var out []Item
	for _, v := range vaults {
		if vaultFilter != "" && v.ShareID != vaultFilter && v.Name != vaultFilter {
			continue
		}
		sk, err := s.decryptShareKeys(ctx, v.ShareID, u)
		if err != nil {
			continue
		}
		items, err := s.fetchItems(ctx, v.ShareID, sk)
		if err != nil {
			continue
		}
		out = append(out, items...)
	}
	return out, nil
}

// ItemGet returns a single decrypted item.
func (s *Service) ItemGet(ctx context.Context, u *keys.Unlocked, shareID, itemID string) (*Item, error) {
	sk, err := s.decryptShareKeys(ctx, shareID, u)
	if err != nil {
		return nil, err
	}
	var r struct {
		Item struct {
			ItemID           string
			Revision         int
			State            int
			Content, ItemKey string
			KeyRotation      int
			CreateTime       int64
			ModifyTime       int64
		}
	}
	if err := s.C.Send(ctx, api.Request{Method: "GET", Path: fmt.Sprintf("/pass/v1/share/%s/item/%s", shareID, itemID)}, &r); err != nil {
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
	return out, nil
}

// ResolveItem returns a (shareID, itemID) for a literal pair or a search term.
func (s *Service) ResolveItem(ctx context.Context, u *keys.Unlocked, args []string) (string, string, error) {
	if len(args) == 2 {
		return args[0], args[1], nil
	}
	needle := strings.ToLower(args[0])
	items, err := s.ItemsList(ctx, u, "")
	if err != nil {
		return "", "", err
	}
	var matches []Item
	for _, it := range items {
		if strings.Contains(strings.ToLower(it.Name), needle) {
			matches = append(matches, it)
			continue
		}
		for _, u := range it.URLs {
			if strings.Contains(strings.ToLower(u), needle) {
				matches = append(matches, it)
				break
			}
		}
	}
	switch len(matches) {
	case 0:
		return "", "", fmt.Errorf("no item matching %q", args[0])
	case 1:
		return matches[0].ShareID, matches[0].ItemID, nil
	}
	lines := []string{fmt.Sprintf("ambiguous: %d items match %q:", len(matches), args[0])}
	for _, m := range matches {
		lines = append(lines, fmt.Sprintf("  %s  %s  (share %s, item %s)", m.Type, m.Name, m.ShareID, m.ItemID))
	}
	return "", "", fmt.Errorf("%s", strings.Join(lines, "\n"))
}

// NewItem describes a login/note/card item for ItemCreate.
type NewItem struct {
	Type                                       string // login|note|card
	Name, Username, Password, Email, URL, Note string
	Holder, Number, Expiry, CVV, PIN           string
}

// ItemCreate creates a new item in the given vault.
func (s *Service) ItemCreate(ctx context.Context, u *keys.Unlocked, shareID string, nc NewItem) ([]byte, error) {
	sk, err := s.decryptShareKeys(ctx, shareID, u)
	if err != nil {
		return nil, err
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
			ItemUsername: nc.Username,
			ItemEmail:    nc.Email,
			Password:     nc.Password,
			Urls:         urls,
		}}
	case "note":
		item.Content.Content = &pb.Content_Note{Note: &pb.ItemNote{}}
	case "card":
		item.Content.Content = &pb.Content_CreditCard{CreditCard: &pb.ItemCreditCard{
			CardholderName:     nc.Holder,
			Number:             nc.Number,
			ExpirationDate:     nc.Expiry,
			VerificationNumber: nc.CVV,
			Pin:                nc.PIN,
		}}
	default:
		return nil, fmt.Errorf("unsupported item type %q", nc.Type)
	}

	itemKey, err := aead.NewKey()
	if err != nil {
		return nil, err
	}
	pbBytes, err := proto.Marshal(item)
	if err != nil {
		return nil, err
	}
	ct, err := aead.Encrypt(itemKey, pbBytes, []byte(aead.TagItemContent))
	if err != nil {
		return nil, err
	}
	ek, err := aead.Encrypt(shareKey, itemKey, []byte(aead.TagItemKey))
	if err != nil {
		return nil, err
	}
	resp, err := s.C.Do(ctx, api.Request{
		Method: "POST", Path: "/pass/v1/share/" + shareID + "/item",
		Body: map[string]any{
			"Content":              base64.StdEncoding.EncodeToString(ct),
			"ContentFormatVersion": 7,
			"ItemKey":              base64.StdEncoding.EncodeToString(ek),
			"KeyRotation":          rotation,
		},
	})
	if err != nil {
		return nil, err
	}
	return resp.Body, nil
}

// ItemEdit applies a partial update to an existing item.
type Patch struct {
	Name, Username, Password, Email, URL, Note string
}

// ItemEdit updates an item.
func (s *Service) ItemEdit(ctx context.Context, u *keys.Unlocked, shareID, itemID string, patch Patch) error {
	sk, err := s.decryptShareKeys(ctx, shareID, u)
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
	if err := s.C.Send(ctx, api.Request{Method: "GET", Path: fmt.Sprintf("/pass/v1/share/%s/item/%s", shareID, itemID)}, &r); err != nil {
		return err
	}
	shareKey, ok := sk.keys[r.Item.KeyRotation]
	if !ok {
		return fmt.Errorf("no share key for rotation %d", r.Item.KeyRotation)
	}
	ikBytes, _ := base64.StdEncoding.DecodeString(r.Item.ItemKey)
	itemKey, err := aead.Decrypt(shareKey, ikBytes, []byte(aead.TagItemKey))
	if err != nil {
		return err
	}
	cBytes, _ := base64.StdEncoding.DecodeString(r.Item.Content)
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
	if it.Content != nil {
		if login, ok := it.Content.Content.(*pb.Content_Login); ok {
			if patch.Username != "" {
				login.Login.ItemUsername = patch.Username
			}
			if patch.Password != "" {
				login.Login.Password = patch.Password
			}
			if patch.Email != "" {
				login.Login.ItemEmail = patch.Email
			}
			if patch.URL != "" {
				login.Login.Urls = []string{patch.URL}
			}
		}
	}
	pbBytes, _ := proto.Marshal(&it)
	ct, err := aead.Encrypt(itemKey, pbBytes, []byte(aead.TagItemContent))
	if err != nil {
		return err
	}
	var latest struct {
		Key struct {
			Key         string
			KeyRotation int
		}
	}
	_ = s.C.Send(ctx, api.Request{Method: "GET", Path: fmt.Sprintf("/pass/v1/share/%s/item/%s/key/latest", shareID, itemID)}, &latest)
	return s.C.Send(ctx, api.Request{
		Method: "PUT", Path: fmt.Sprintf("/pass/v1/share/%s/item/%s", shareID, itemID),
		Body: map[string]any{
			"Content":              base64.StdEncoding.EncodeToString(ct),
			"ContentFormatVersion": 7,
			"KeyRotation":          latest.Key.KeyRotation,
			"LastRevision":         r.Item.Revision,
		},
	}, nil)
}

// ItemTrash moves an item to trash.
func (s *Service) ItemTrash(ctx context.Context, shareID, itemID string) error {
	rev, err := s.itemRevision(ctx, shareID, itemID)
	if err != nil {
		return err
	}
	return s.C.Send(ctx, api.Request{
		Method: "POST", Path: "/pass/v1/share/" + shareID + "/item/trash",
		Body: map[string]any{"Items": []map[string]any{{"ItemID": itemID, "Revision": rev}}},
	}, nil)
}

// ItemRestore moves an item out of trash.
func (s *Service) ItemRestore(ctx context.Context, shareID, itemID string) error {
	rev, err := s.itemRevision(ctx, shareID, itemID)
	if err != nil {
		return err
	}
	return s.C.Send(ctx, api.Request{
		Method: "POST", Path: "/pass/v1/share/" + shareID + "/item/untrash",
		Body: map[string]any{"Items": []map[string]any{{"ItemID": itemID, "Revision": rev}}},
	}, nil)
}

// ItemDelete permanently deletes an item (trashing first if needed).
func (s *Service) ItemDelete(ctx context.Context, shareID, itemID string) error {
	var r struct {
		Item struct {
			Revision int
			State    int
		}
	}
	if err := s.C.Send(ctx, api.Request{Method: "GET", Path: fmt.Sprintf("/pass/v1/share/%s/item/%s", shareID, itemID)}, &r); err != nil {
		return err
	}
	if r.Item.State != 2 {
		if err := s.ItemTrash(ctx, shareID, itemID); err != nil {
			return err
		}
		if err := s.C.Send(ctx, api.Request{Method: "GET", Path: fmt.Sprintf("/pass/v1/share/%s/item/%s", shareID, itemID)}, &r); err != nil {
			return err
		}
	}
	return s.C.Send(ctx, api.Request{
		Method: "DELETE", Path: "/pass/v1/share/" + shareID + "/item",
		Body: map[string]any{"Items": []map[string]any{{"ItemID": itemID, "Revision": r.Item.Revision}}},
	}, nil)
}

func (s *Service) itemRevision(ctx context.Context, shareID, itemID string) (int, error) {
	var r struct{ Item struct{ Revision int } }
	if err := s.C.Send(ctx, api.Request{Method: "GET", Path: fmt.Sprintf("/pass/v1/share/%s/item/%s", shareID, itemID)}, &r); err != nil {
		return 0, err
	}
	return r.Item.Revision, nil
}

// AliasOptions returns available alias suffixes + mailboxes for a vault.
type AliasSuffix struct {
	Suffix, SignedSuffix, Domain string
	IsPremium, IsCustom          bool
}

type AliasMailbox struct {
	ID    int
	Email string
}

// AliasOptions fetches alias options for the given vault.
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
	if err := s.C.Send(ctx, api.Request{Method: "GET", Path: "/pass/v1/share/" + shareID + "/alias/options"}, &r); err != nil {
		return nil, nil, err
	}
	var sx []AliasSuffix
	for _, s := range r.Options.Suffixes {
		sx = append(sx, AliasSuffix{Suffix: s.Suffix, SignedSuffix: s.SignedSuffix, Domain: s.Domain, IsPremium: s.IsPremium, IsCustom: s.IsCustom})
	}
	var mx []AliasMailbox
	for _, m := range r.Options.Mailboxes {
		mx = append(mx, AliasMailbox{ID: m.ID, Email: m.Email})
	}
	return sx, mx, nil
}

// AliasCreate creates a standalone alias item in a vault.
func (s *Service) AliasCreate(ctx context.Context, u *keys.Unlocked, shareID, prefix, suffix, mailbox, name string) ([]byte, error) {
	suffixes, mailboxes, err := s.AliasOptions(ctx, shareID)
	if err != nil {
		return nil, err
	}
	signed, err := pickSuffix(suffixes, suffix)
	if err != nil {
		return nil, err
	}
	mbox, err := pickMailbox(mailboxes, mailbox)
	if err != nil {
		return nil, err
	}
	sk, err := s.decryptShareKeys(ctx, shareID, u)
	if err != nil {
		return nil, err
	}
	shareKey, rotation := sk.latest()
	if name == "" {
		name = prefix
	}
	item := &pb.Item{Metadata: &pb.Metadata{Name: name}, Content: &pb.Content{Content: &pb.Content_Alias{Alias: &pb.ItemAlias{}}}}
	itemKey, err := aead.NewKey()
	if err != nil {
		return nil, err
	}
	pbBytes, _ := proto.Marshal(item)
	ct, err := aead.Encrypt(itemKey, pbBytes, []byte(aead.TagItemContent))
	if err != nil {
		return nil, err
	}
	ek, err := aead.Encrypt(shareKey, itemKey, []byte(aead.TagItemKey))
	if err != nil {
		return nil, err
	}
	resp, err := s.C.Do(ctx, api.Request{
		Method: "POST", Path: "/pass/v1/share/" + shareID + "/alias/custom",
		Body: map[string]any{
			"Prefix":       prefix,
			"SignedSuffix": signed,
			"MailboxIDs":   []int{mbox},
			"Item": map[string]any{
				"Content":              base64.StdEncoding.EncodeToString(ct),
				"ContentFormatVersion": 7,
				"ItemKey":              base64.StdEncoding.EncodeToString(ek),
				"KeyRotation":          rotation,
			},
		},
	})
	if err != nil {
		return nil, err
	}
	return resp.Body, nil
}

func (s *Service) getShares(ctx context.Context) ([]json.RawMessage, error) {
	var r struct{ Shares []json.RawMessage }
	if err := s.C.Send(ctx, api.Request{Method: "GET", Path: "/pass/v1/share"}, &r); err != nil {
		return nil, err
	}
	return r.Shares, nil
}

func (s *Service) decryptShareKeys(ctx context.Context, shareID string, u *keys.Unlocked) (*shareKeys, error) {
	var r struct {
		ShareKeys struct {
			Keys []json.RawMessage
		}
	}
	if err := s.C.Send(ctx, api.Request{Method: "GET", Path: "/pass/v1/share/" + shareID + "/key", Query: keys.Query("Page", "0")}, &r); err != nil {
		return nil, err
	}
	out := &shareKeys{keys: map[int][]byte{}}
	for _, raw := range r.ShareKeys.Keys {
		var k struct {
			Key         string
			KeyRotation int
		}
		if err := json.Unmarshal(raw, &k); err != nil {
			continue
		}
		kb, err := base64.StdEncoding.DecodeString(k.Key)
		if err != nil {
			continue
		}
		msg := pgp.NewPGPMessage(kb)
		dec, err := u.UserKR.Decrypt(msg, u.UserKR, pgp.GetUnixTime())
		if err != nil {
			continue
		}
		out.keys[k.KeyRotation] = dec.GetBinary()
	}
	if len(out.keys) == 0 {
		return nil, fmt.Errorf("failed to decrypt share keys for %s", shareID)
	}
	return out, nil
}

func (s *Service) fetchItems(ctx context.Context, shareID string, sk *shareKeys) ([]Item, error) {
	var out []Item
	var since string
	for {
		q := map[string]string{}
		if since != "" {
			q["Since"] = since
		}
		qv := keys.Query()
		for k, v := range q {
			qv.Set(k, v)
		}
		var r struct {
			Items struct {
				RevisionsData []json.RawMessage
				LastToken     string
			}
		}
		if err := s.C.Send(ctx, api.Request{Method: "GET", Path: "/pass/v1/share/" + shareID + "/item", Query: qv}, &r); err != nil {
			return nil, err
		}
		for _, raw := range r.Items.RevisionsData {
			var enc struct {
				ItemID           string
				Revision         int
				State            int
				Content, ItemKey string
				KeyRotation      int
				CreateTime       int64
				ModifyTime       int64
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
			out = append(out, *item)
		}
		if r.Items.LastToken == "" || len(r.Items.RevisionsData) == 0 {
			break
		}
		since = r.Items.LastToken
	}
	return out, nil
}

func decryptVault(encContent string, shareKey []byte) (*pb.Vault, error) {
	data, err := base64.StdEncoding.DecodeString(encContent)
	if err != nil {
		return nil, err
	}
	plain, err := aead.Decrypt(shareKey, data, []byte(aead.TagVaultContent))
	if err != nil {
		return nil, err
	}
	var v pb.Vault
	if err := proto.Unmarshal(plain, &v); err != nil {
		return nil, err
	}
	return &v, nil
}

func itemFromProto(it *pb.Item) *Item {
	item := &Item{raw: it, Type: itemTypeName(it)}
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
		return "credit_card"
	case *pb.Content_Identity:
		return "identity"
	case *pb.Content_SshKey:
		return "ssh_key"
	case *pb.Content_Wifi:
		return "wifi"
	case *pb.Content_Custom:
		return "custom"
	}
	return "unknown"
}

func pickSuffix(s []AliasSuffix, wanted string) (string, error) {
	if wanted == "" {
		if len(s) == 0 {
			return "", fmt.Errorf("no alias suffixes available")
		}
		return s[0].SignedSuffix, nil
	}
	for _, x := range s {
		if x.Suffix == wanted || strings.HasSuffix(x.Suffix, wanted) {
			return x.SignedSuffix, nil
		}
	}
	avail := make([]string, 0, len(s))
	for _, x := range s {
		avail = append(avail, x.Suffix)
	}
	return "", fmt.Errorf("suffix %q not found; available: %s", wanted, strings.Join(avail, ", "))
}

func pickMailbox(m []AliasMailbox, wanted string) (int, error) {
	if wanted == "" {
		if len(m) == 0 {
			return 0, fmt.Errorf("no mailboxes available")
		}
		return m[0].ID, nil
	}
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
