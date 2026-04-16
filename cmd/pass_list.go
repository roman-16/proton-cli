package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/roman-16/proton-cli/internal/client"
	"github.com/roman-16/proton-cli/internal/crypto"
	"github.com/spf13/cobra"
)

var passVaultsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List vaults",
	RunE:  runPassVaultsList,
}

var passListCmd = &cobra.Command{
	Use:   "list",
	Short: "List items across all vaults",
	RunE:  runPassList,
}

func runPassVaultsList(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	c, kr, err := getPassContext(ctx)
	if err != nil {
		return err
	}

	vaults, err := listDecryptedVaults(ctx, c, kr)
	if err != nil {
		return err
	}

	if flagJSON {
		out, _ := json.MarshalIndent(vaults, "", "  ")
		_, _ = os.Stdout.Write(out)
		fmt.Println()
		return nil
	}

	headers := []string{"SHARE_ID", "NAME", "ITEMS", "OWNER", "SHARED"}
	var rows [][]string
	for _, v := range vaults {
		name := "(encrypted)"
		if v.Vault != nil {
			name = v.Vault.Name
		}
		owner := "no"
		if v.Owner {
			owner = "yes"
		}
		shared := "no"
		if v.Shared {
			shared = "yes"
		}
		rows = append(rows, []string{v.ShareID, name, fmt.Sprintf("%d", v.Members), owner, shared})
	}

	printTable(headers, rows)
	return nil
}

func runPassList(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	c, kr, err := getPassContext(ctx)
	if err != nil {
		return err
	}

	vaults, err := listDecryptedVaults(ctx, c, kr)
	if err != nil {
		return err
	}

	type itemOutput struct {
		ShareID  string
		Vault    string
		ItemID   string
		Type     string
		Name     string
		Username string
		Email    string
	}

	var allItems []itemOutput
	for _, vault := range vaults {
		if passListVault != "" {
			match := vault.ShareID == passListVault
			if !match && vault.Vault != nil {
				match = vault.Vault.Name == passListVault
			}
			if !match {
				continue
			}
		}

		keys, err := decryptShareKeys(ctx, c, vault.ShareID, kr)
		if err != nil {
			continue
		}

		items, err := fetchAndDecryptItems(ctx, c, vault.ShareID, keys)
		if err != nil {
			continue
		}

		vaultName := "(encrypted)"
		if vault.Vault != nil {
			vaultName = vault.Vault.Name
		}

		for _, item := range items {
			out := itemOutput{
				ShareID: vault.ShareID,
				Vault:   vaultName,
				ItemID:  item.ItemID,
				Type:    itemTypeName(&item),
			}
			if item.Item != nil && item.Item.Metadata != nil {
				out.Name = item.Item.Metadata.Name
			}
			if item.Item != nil && item.Item.Content != nil {
				if login, ok := item.Item.Content.Content.(*crypto.ContentLogin); ok {
					out.Username = login.Login.ItemUsername
					if out.Username == "" {
						out.Username = login.Login.ItemEmail
					}
					out.Email = login.Login.ItemEmail
				}
			}
			allItems = append(allItems, out)
		}
	}

	if flagJSON {
		out, _ := json.MarshalIndent(allItems, "", "  ")
		_, _ = os.Stdout.Write(out)
		fmt.Println()
		return nil
	}

	headers := []string{"VAULT", "TYPE", "NAME", "USERNAME", "SHARE_ID", "ITEM_ID"}
	var rows [][]string
	for _, item := range allItems {
		username := item.Username
		if username == "" {
			username = item.Email
		}
		rows = append(rows, []string{item.Vault, item.Type, item.Name, username, item.ShareID, item.ItemID})
	}

	printTable(headers, rows)
	fmt.Fprintf(os.Stderr, "\n%d item(s)\n", len(allItems))
	return nil
}

// fetchAndDecryptItems fetches all items from a share and decrypts them.
// Uses cursor-based pagination (Since token) to get all items.
func fetchAndDecryptItems(ctx context.Context, c *client.Client, shareID string, keys *passShareKeys) ([]crypto.PassItem, error) {
	var allItems []crypto.PassItem
	var since string

	for {
		query := map[string]string{}
		if since != "" {
			query["Since"] = since
		}

		body, _, err := c.Do(ctx, "GET", "/pass/v1/share/"+shareID+"/item", query, "", "", "")
		if err != nil {
			return nil, err
		}

		var res struct {
			Items struct {
				RevisionsData []json.RawMessage
				LastToken     string
			}
		}
		if err := json.Unmarshal(body, &res); err != nil {
			return nil, err
		}

		for _, raw := range res.Items.RevisionsData {
			var enc struct {
				ItemID      string
				Revision    int
				State       int
				Content     string
				ItemKey     string
				KeyRotation int
			}
			if err := json.Unmarshal(raw, &enc); err != nil {
				continue
			}

			// State 1 = active, 2 = trashed
			if enc.State != 1 {
				continue
			}

			shareKey, ok := keys.keys[enc.KeyRotation]
			if !ok {
				continue
			}

			itemKey, err := crypto.DecryptItemKey(enc.ItemKey, shareKey)
			if err != nil {
				continue
			}

			item, err := crypto.DecryptItemContent(enc.Content, itemKey)
			if err != nil {
				continue
			}

			allItems = append(allItems, crypto.PassItem{
				ItemID:   enc.ItemID,
				Revision: enc.Revision,
				State:    enc.State,
				ShareID:  shareID,
				Metadata: item.Metadata,
				Content:  item.Content,
				Item:     item,
			})
		}

		if res.Items.LastToken == "" || len(res.Items.RevisionsData) == 0 {
			break
		}
		since = res.Items.LastToken
	}

	return allItems, nil
}
