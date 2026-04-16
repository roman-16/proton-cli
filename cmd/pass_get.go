package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/roman-16/proton-cli/internal/client"
	"github.com/roman-16/proton-cli/internal/crypto"
	"github.com/spf13/cobra"
)

var passGetCmd = &cobra.Command{
	Use:   "get {SHARE_ID ITEM_ID | SEARCH}",
	Short: "Get a Pass item (decrypted). Pass two IDs or search by name/URL.",
	Args:  cobra.RangeArgs(1, 2),
	RunE:  runPassGet,
}

func runPassGet(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	c, kr, err := getPassContext(ctx)
	if err != nil {
		return err
	}

	var shareID, itemID string

	if len(args) == 2 {
		shareID = args[0]
		itemID = args[1]
	} else {
		found, err := searchPassItem(ctx, c, kr, args[0])
		if err != nil {
			return err
		}
		shareID = found.ShareID
		itemID = found.ItemID
	}

	keys, err := decryptShareKeys(ctx, c, shareID, kr)
	if err != nil {
		return err
	}

	body, _, err := c.Do(ctx, "GET", fmt.Sprintf("/pass/v1/share/%s/item/%s", shareID, itemID), nil, "", "", "")
	if err != nil {
		return err
	}

	var res struct {
		Item struct {
			ItemID      string
			Revision    int
			State       int
			Content     string
			ItemKey     string
			KeyRotation int
		}
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return err
	}

	shareKey, ok := keys.keys[res.Item.KeyRotation]
	if !ok {
		return fmt.Errorf("no share key for rotation %d", res.Item.KeyRotation)
	}

	itemKey, err := crypto.DecryptItemKey(res.Item.ItemKey, shareKey)
	if err != nil {
		return err
	}

	item, err := crypto.DecryptItemContent(res.Item.Content, itemKey)
	if err != nil {
		return err
	}

	if flagJSON {
		result := map[string]interface{}{
			"ShareID":  shareID,
			"ItemID":   res.Item.ItemID,
			"Revision": res.Item.Revision,
			"Item":     itemToMap(item),
		}
		out, _ := json.MarshalIndent(result, "", "  ")
		_, _ = os.Stdout.Write(out)
		fmt.Println()
		return nil
	}

	if item.Metadata != nil {
		fmt.Printf("Name:     %s\n", item.Metadata.Name)
		if item.Metadata.Note != "" {
			fmt.Printf("Note:     %s\n", item.Metadata.Note)
		}
	}

	if item.Content != nil {
		switch ct := item.Content.Content.(type) {
		case *crypto.ContentLogin:
			if ct.Login.ItemUsername != "" {
				fmt.Printf("Username: %s\n", ct.Login.ItemUsername)
			}
			if ct.Login.ItemEmail != "" {
				fmt.Printf("Email:    %s\n", ct.Login.ItemEmail)
			}
			if ct.Login.Password != "" {
				fmt.Printf("Password: %s\n", ct.Login.Password)
			}
			if ct.Login.TotpUri != "" {
				fmt.Printf("TOTP:     %s\n", ct.Login.TotpUri)
			}
			for _, url := range ct.Login.Urls {
				fmt.Printf("URL:      %s\n", url)
			}
		case *crypto.ContentCreditCard:
			fmt.Printf("Holder:   %s\n", ct.CreditCard.CardholderName)
			fmt.Printf("Number:   %s\n", ct.CreditCard.Number)
			fmt.Printf("Expiry:   %s\n", ct.CreditCard.ExpirationDate)
			if ct.CreditCard.VerificationNumber != "" {
				fmt.Printf("CVV:      %s\n", ct.CreditCard.VerificationNumber)
			}
			if ct.CreditCard.Pin != "" {
				fmt.Printf("PIN:      %s\n", ct.CreditCard.Pin)
			}
		case *crypto.ContentIdentity:
			if ct.Identity.FullName != "" {
				fmt.Printf("Name:     %s\n", ct.Identity.FullName)
			}
			if ct.Identity.Email != "" {
				fmt.Printf("Email:    %s\n", ct.Identity.Email)
			}
			if ct.Identity.PhoneNumber != "" {
				fmt.Printf("Phone:    %s\n", ct.Identity.PhoneNumber)
			}
			if ct.Identity.Organization != "" {
				fmt.Printf("Org:      %s\n", ct.Identity.Organization)
			}
		case *crypto.ContentSshKey:
			if ct.SshKey.PublicKey != "" {
				fmt.Printf("Pub Key:  %s\n", ct.SshKey.PublicKey)
			}
		case *crypto.ContentWifi:
			fmt.Printf("SSID:     %s\n", ct.Wifi.Ssid)
			if ct.Wifi.Password != "" {
				fmt.Printf("Password: %s\n", ct.Wifi.Password)
			}
		}
	}

	fmt.Printf("Type:     %s\n", itemTypeName(&crypto.PassItem{Item: item}))
	fmt.Printf("ID:       %s\n", res.Item.ItemID)
	fmt.Printf("Share:    %s\n", shareID)

	return nil
}

func searchPassItem(ctx context.Context, c *client.Client, kr *crypto.KeyRings, search string) (*crypto.PassItem, error) {
	vaults, err := listDecryptedVaults(ctx, c, kr)
	if err != nil {
		return nil, err
	}

	type match struct {
		item  crypto.PassItem
		name  string
		vault string
	}
	var matches []match
	searchLower := strings.ToLower(search)

	for _, vault := range vaults {
		keys, err := decryptShareKeys(ctx, c, vault.ShareID, kr)
		if err != nil {
			continue
		}

		items, err := fetchAndDecryptItems(ctx, c, vault.ShareID, keys)
		if err != nil {
			continue
		}

		vaultName := ""
		if vault.Vault != nil {
			vaultName = vault.Vault.Name
		}

		for _, item := range items {
			nameMatch := false
			name := ""
			if item.Item != nil && item.Item.Metadata != nil {
				name = item.Item.Metadata.Name
				if strings.Contains(strings.ToLower(name), searchLower) {
					nameMatch = true
				}
			}
			if !nameMatch && item.Item != nil && item.Item.Content != nil {
				if login, ok := item.Item.Content.Content.(*crypto.ContentLogin); ok {
					for _, url := range login.Login.Urls {
						if strings.Contains(strings.ToLower(url), searchLower) {
							nameMatch = true
							break
						}
					}
				}
			}
			if nameMatch {
				matches = append(matches, match{item: item, name: name, vault: vaultName})
			}
		}
	}

	if len(matches) == 0 {
		return nil, fmt.Errorf("no item matching %q found", search)
	}
	if len(matches) > 1 {
		fmt.Fprintf(os.Stderr, "Multiple matches for %q:\n", search)
		for _, m := range matches {
			fmt.Fprintf(os.Stderr, "  %s  %s  (vault: %s, share: %s, item: %s)\n",
				itemTypeName(&m.item), m.name, m.vault, m.item.ShareID, m.item.ItemID)
		}
		return nil, fmt.Errorf("ambiguous: %d items match %q, use SHARE_ID ITEM_ID", len(matches), search)
	}

	fmt.Fprintf(os.Stderr, "Found: %s\n", matches[0].name)
	return &matches[0].item, nil
}

func itemToMap(item interface{}) map[string]interface{} {
	data, err := json.Marshal(item)
	if err != nil {
		return nil
	}
	var m map[string]interface{}
	_ = json.Unmarshal(data, &m)
	return m
}
