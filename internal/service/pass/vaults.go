package pass

import (
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"
	"strconv"

	pgp "github.com/ProtonMail/gopenpgp/v2/crypto"
	"github.com/roman-16/proton-cli/internal/crypto/aead"
	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/roman-16/proton-cli/internal/fetch"
	"github.com/roman-16/proton-cli/internal/proton"
	pb "github.com/roman-16/proton-cli/internal/service/pass/proto"
	"google.golang.org/protobuf/proto"
)

type Vault struct {
	ShareID     string `json:"share_id"`
	VaultID     string `json:"vault_id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Owner       bool   `json:"owner"`
	Shared      bool   `json:"shared"`
	Members     int    `json:"members"`
	AddressID   string `json:"address_id,omitempty"`
	// Icon and Color are which of Pass's grid the vault picked, numbered from
	// one. Zero means it never chose.
	Icon  int `json:"icon,omitempty"`
	Color int `json:"color,omitempty"`
}

func (s *Service) VaultsList(ctx context.Context) ([]Vault, error) {
	all, err := s.shares(ctx)
	if err != nil {
		return nil, err
	}
	var shares []Share
	for _, sh := range all {
		if sh.Vault() {
			shares = append(shares, sh)
		}
	}

	// Each vault's name is encrypted with its own share's key, so every key is
	// asked for at the same time rather than one vault at a time. A share whose
	// keys cannot be read is reported without a name, as it was before, and the
	// answer is remembered so the second look costs nothing.
	fetches := make([]func(context.Context) error, 0, len(shares))
	for _, sh := range shares {
		if sh.Content == "" {
			continue
		}
		fetches = append(fetches, func(ctx context.Context) error {
			_, _ = s.decryptShareKeys(ctx, sh.ShareID)
			return nil
		})
	}
	_ = fetch.Together(ctx, fetches...)

	var out []Vault
	for _, sh := range shares {
		v := Vault{
			ShareID: sh.ShareID, VaultID: sh.VaultID,
			Owner: sh.Owner, Shared: sh.Shared,
			Members: sh.Members, AddressID: sh.AddressID,
		}
		if sh.Content != "" {
			if err := describe(ctx, s, sh, &v); err != nil {
				// Recorded and not counted. The row is on the screen with an empty
				// name, so nothing has gone missing from the answer - which is what
				// the tally is for. What is wrong is visible; why is in the log.
				slog.DebugContext(ctx, "pass: a vault's own record could not be read",
					"vault", sh.ShareID, "error", err)
			}
		}
		out = append(out, v)
	}
	return out, nil
}

// describe fills in what a vault's own encrypted record says about it: its
// name, its description and the icon and colour Proton shows it with.
func describe(ctx context.Context, s *Service, sh Share, v *Vault) error {
	sk, err := s.decryptShareKeys(ctx, sh.ShareID)
	if err != nil {
		return err
	}
	key, ok := sk.keys[sh.ContentKeyRotation]
	if !ok {
		return fmt.Errorf("no share key for rotation %d", sh.ContentKeyRotation)
	}
	vv, err := decryptVault(sh.Content, key)
	if err != nil {
		return err
	}
	v.Name = vv.Name
	v.Description = vv.Description
	if d := vv.GetDisplay(); d != nil {
		v.Icon = DisplayNumber(int(d.Icon))
		v.Color = DisplayNumber(int(d.Color))
	}
	return nil
}

func (s *Service) VaultCreate(ctx context.Context, name string) (string, error) {
	u, err := s.keys(ctx)
	if err != nil {
		return "", err
	}
	vault := &pb.Vault{Name: name}
	rawKey, err := aead.NewKey()
	if err != nil {
		return "", err
	}
	// Sealed to the primary user key alone, as Proton's own client seals it:
	// every key the account ever had has to open old secrets, but a new one
	// under a retired key is a secret whose reach is wider than its owner thinks.
	ownKey, err := u.PrimaryUserKey()
	if err != nil {
		return "", err
	}
	encKey, err := ownKey.Encrypt(pgp.NewPlainMessage(rawKey), ownKey)
	if err != nil {
		return "", err
	}
	encVaultKey := base64.StdEncoding.EncodeToString(encKey.GetBinary())
	pbBytes, err := proto.Marshal(vault)
	if err != nil {
		return "", err
	}
	ct, err := aead.Encrypt(rawKey, pbBytes, []byte(aead.TagVaultContent))
	if err != nil {
		return "", err
	}
	_, addr, err := u.PrimaryAddr()
	if err != nil {
		return "", err
	}
	var r struct{ Share struct{ ShareID string } }
	if err := s.C.Decode(ctx, proton.Request{
		Method: "POST", Path: "/pass/v1/vault",
		Body: map[string]any{
			"AddressID":            addr.ID,
			"ContentFormatVersion": 1,
			"Content":              base64.StdEncoding.EncodeToString(ct),
			"EncryptedVaultKey":    encVaultKey,
		},
	}, &r); err != nil {
		return "", err
	}
	return r.Share.ShareID, nil
}

func (s *Service) VaultDelete(ctx context.Context, shareID string) error {
	return s.C.Decode(ctx, proton.Request{Method: "DELETE", Path: "/pass/v1/vault/" + shareID}, nil)
}

// VaultEdit renames a vault, preserving its description and display settings by
// re-encrypting the existing vault content with the latest share key.
// VaultPatch changes only what it names, so setting an icon does not clear the
// description somebody wrote in the Pass app.
type VaultPatch struct {
	Name        *string
	Description *string
	Icon        *int
	Color       *int
}

// VaultIcons and VaultColors are the sets Pass offers, named the way a person
// would say them rather than by the proto's ICON1..ICON30.
//
// Proton's own picker shows a grid with no names at all, so the numbers are what
// there is; naming them one to thirty is the honest rendering of a choice that
// has no other vocabulary.
func VaultIcons() []string  { return numbered(30) }
func VaultColors() []string { return numbered(10) }

func numbered(n int) []string {
	out := make([]string, 0, n)
	for i := 1; i <= n; i++ {
		out = append(out, strconv.Itoa(i))
	}
	return out
}

// icon and colour numbers are offset by two in the proto: zero is unspecified
// and one is custom, so the first pickable one is two.
const displayOffset = 2

// DisplayValue turns the number a person writes into the enum Pass stores.
func DisplayValue(n int) int { return n + displayOffset - 1 }

// DisplayNumber is the inverse, and answers zero for a vault that never chose.
func DisplayNumber(v int) int {
	if v < displayOffset {
		return 0
	}
	return v - displayOffset + 1
}

func (s *Service) VaultEdit(ctx context.Context, shareID string, patch VaultPatch) error {
	shares, err := s.shares(ctx)
	if err != nil {
		return err
	}
	var content string
	var rotation int
	found := false
	for _, sh := range shares {
		if sh.ShareID == shareID && sh.Vault() {
			content, rotation, found = sh.Content, sh.ContentKeyRotation, true
			break
		}
	}
	if !found {
		return &errs.NotFound{Kind: "vault", Ref: shareID}
	}
	sk, err := s.decryptShareKeys(ctx, shareID)
	if err != nil {
		return err
	}
	shareKey, ok := sk.keys[rotation]
	if !ok {
		return fmt.Errorf("no share key for rotation %d", rotation)
	}
	pbBytes, err := patchedVault(content, shareKey, patch)
	if err != nil {
		return fmt.Errorf("edit vault %s: %w", shareID, err)
	}
	writeKey, writeRotation := sk.latest()
	if writeKey == nil {
		return fmt.Errorf("vault %s has no usable share key", shareID)
	}
	ct, err := aead.Encrypt(writeKey, pbBytes, []byte(aead.TagVaultContent))
	if err != nil {
		return err
	}
	return s.C.Decode(ctx, proton.Request{
		Method: "PUT", Path: "/pass/v1/vault/" + shareID,
		Body: map[string]any{
			"Content":              base64.StdEncoding.EncodeToString(ct),
			"ContentFormatVersion": 1,
			"KeyRotation":          writeRotation,
		},
	}, nil)
}

func (s *Service) ResolveVault(ctx context.Context, nameOrID string) (string, error) {
	vaults, err := s.VaultsList(ctx)
	if err != nil {
		return "", err
	}
	if nameOrID == "" {
		if len(vaults) == 0 {
			return "", &errs.NotFound{Kind: "vault"}
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
	return "", &errs.NotFound{Kind: "vault", Ref: nameOrID}
}

// renamedVault is the stored vault with a new name, and nothing else changed.
//
// It fails rather than substituting an empty vault when the existing content
// cannot be read: a rename that rebuilds the vault from nothing is a way of losing
// its description and every other field it holds.
func patchedVault(content string, shareKey []byte, patch VaultPatch) ([]byte, error) {
	if content == "" {
		return nil, fmt.Errorf("it has no stored content to change")
	}
	vault, err := decryptVault(content, shareKey)
	if err != nil {
		return nil, fmt.Errorf("its stored content could not be read: %w", err)
	}
	if patch.Name != nil {
		vault.Name = *patch.Name
	}
	if patch.Description != nil {
		vault.Description = *patch.Description
	}
	if patch.Icon != nil || patch.Color != nil {
		if vault.Display == nil {
			vault.Display = &pb.VaultDisplayPreferences{}
		}
		if patch.Icon != nil {
			vault.Display.Icon = pb.VaultIcon(DisplayValue(*patch.Icon))
		}
		if patch.Color != nil {
			vault.Display.Color = pb.VaultColor(DisplayValue(*patch.Color))
		}
	}
	return proto.Marshal(vault)
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
