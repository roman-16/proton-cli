package cmd

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/roman-16/proton-cli/internal/client"
	"github.com/roman-16/proton-cli/internal/crypto"
	"github.com/spf13/cobra"
)

var passCmd = &cobra.Command{
	Use:   "pass",
	Short: "Password manager operations",
}

var passVaultsCmd = &cobra.Command{
	Use:   "vaults",
	Short: "Manage vaults",
}

var (
	passVaultName string
	passListVault string
)

func init() {
	passVaultsCreateCmd.Flags().StringVar(&passVaultName, "name", "", "Vault name")

	passListCmd.Flags().StringVar(&passListVault, "vault", "", "Filter by vault name or ID")

	passVaultsCmd.AddCommand(passVaultsListCmd, passVaultsCreateCmd, passVaultsDeleteCmd)
	passCmd.AddCommand(passListCmd, passGetCmd, passCreateCmd, passEditCmd, passDeleteCmd, passTrashCmd, passRestoreCmd, passVaultsCmd)
	rootCmd.AddCommand(passCmd)
}

// passShareKeys holds decrypted share keys keyed by rotation.
type passShareKeys struct {
	keys map[int][]byte // rotation -> raw AES key
}

// getPassContext sets up auth and decrypts share keys for a vault.
func getPassContext(ctx context.Context) (*client.Client, *crypto.KeyRings, error) {
	c, err := getAuthenticatedClient(ctx)
	if err != nil {
		return nil, nil, err
	}

	password := getFlag(flagPassword, "PROTON_PASSWORD")
	kr, err := crypto.UnlockKeys(ctx, c, password)
	if err != nil {
		return nil, nil, err
	}

	return c, kr, nil
}

// decryptShareKeys fetches and decrypts all keys for a share.
func decryptShareKeys(ctx context.Context, c *client.Client, shareID string, userKR *crypto.KeyRings) (*passShareKeys, error) {
	rawKeys, err := crypto.GetPassShareKeys(ctx, c, shareID)
	if err != nil {
		return nil, err
	}

	keys := &passShareKeys{keys: make(map[int][]byte)}
	for _, raw := range rawKeys {
		var k struct {
			Key         string
			KeyRotation int
		}
		if err := json.Unmarshal(raw, &k); err != nil {
			continue
		}
		decrypted, err := crypto.OpenShareKey(k.Key, userKR.UserKR)
		if err != nil {
			continue
		}
		keys.keys[k.KeyRotation] = decrypted
	}

	if len(keys.keys) == 0 {
		return nil, fmt.Errorf("failed to decrypt any share keys for %s", shareID)
	}

	return keys, nil
}

// latestKey returns the share key with the highest rotation.
func (sk *passShareKeys) latestKey() ([]byte, int) {
	maxRot := -1
	for rot := range sk.keys {
		if rot > maxRot {
			maxRot = rot
		}
	}
	return sk.keys[maxRot], maxRot
}

// resolveVaultShareID resolves a vault name or ID to a share ID.
func resolveVaultShareID(ctx context.Context, c *client.Client, kr *crypto.KeyRings, nameOrID string) (string, error) {
	vaults, err := listDecryptedVaults(ctx, c, kr)
	if err != nil {
		return "", err
	}

	// Try exact ID match first.
	for _, v := range vaults {
		if v.ShareID == nameOrID {
			return v.ShareID, nil
		}
	}

	// Try name match.
	for _, v := range vaults {
		if v.Vault != nil && v.Vault.Name == nameOrID {
			return v.ShareID, nil
		}
	}

	return "", fmt.Errorf("vault %q not found", nameOrID)
}

// listDecryptedVaults fetches all vaults and decrypts their content.
func listDecryptedVaults(ctx context.Context, c *client.Client, kr *crypto.KeyRings) ([]crypto.PassVault, error) {
	rawShares, err := crypto.GetPassShares(ctx, c)
	if err != nil {
		return nil, err
	}

	var vaults []crypto.PassVault
	for _, raw := range rawShares {
		var share struct {
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
		if err := json.Unmarshal(raw, &share); err != nil {
			continue
		}

		// TargetType 1 = Vault, 2 = Item share
		if share.TargetType != 1 {
			continue
		}

		vault := crypto.PassVault{
			ShareID:   share.ShareID,
			VaultID:   share.VaultID,
			Owner:     share.Owner,
			Shared:    share.Shared,
			Members:   share.TargetMembers,
			AddressID: share.AddressID,
		}

		if share.Content != "" {
			keys, err := decryptShareKeys(ctx, c, share.ShareID, kr)
			if err == nil {
				if sk, ok := keys.keys[share.ContentKeyRotation]; ok {
					v, err := crypto.DecryptVaultContent(share.Content, sk)
					if err == nil {
						vault.Vault = v
					}
				}
			}
		}

		vaults = append(vaults, vault)
	}

	return vaults, nil
}

// itemTypeName returns a human-readable name for an item content type.
func itemTypeName(item *crypto.PassItem) string {
	if item.Item == nil || item.Item.Content == nil {
		return "unknown"
	}
	switch item.Item.Content.Content.(type) {
	case *crypto.ContentLogin:
		return "login"
	case *crypto.ContentNote:
		return "note"
	case *crypto.ContentAlias:
		return "alias"
	case *crypto.ContentCreditCard:
		return "credit_card"
	case *crypto.ContentIdentity:
		return "identity"
	case *crypto.ContentSshKey:
		return "ssh_key"
	case *crypto.ContentWifi:
		return "wifi"
	case *crypto.ContentCustom:
		return "custom"
	default:
		return "unknown"
	}
}
