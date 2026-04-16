package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/roman-16/proton-cli/internal/client"
	"github.com/roman-16/proton-cli/internal/crypto"
	pb "github.com/roman-16/proton-cli/internal/proto"
	"github.com/spf13/cobra"
)

var (
	passCreateType     string
	passCreateName     string
	passCreateUsername string
	passCreatePassword string
	passCreateEmail    string
	passCreateURL      string
	passCreateNote     string
	passCreateVault    string

	// Credit card
	passCreateHolder string
	passCreateNumber string
	passCreateExpiry string
	passCreateCVV    string

	// Edit
	passEditName     string
	passEditUsername string
	passEditPassword string
	passEditEmail    string
	passEditURL      string
	passEditNote     string
)

var passCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create an item (login, note, card)",
	RunE:  runPassCreate,
}

var passEditCmd = &cobra.Command{
	Use:   "edit {SHARE_ID ITEM_ID | SEARCH}",
	Short: "Edit an item",
	Args:  cobra.RangeArgs(1, 2),
	RunE:  runPassEdit,
}

var passDeleteCmd = &cobra.Command{
	Use:   "delete {SHARE_ID ITEM_ID | SEARCH}",
	Short: "Permanently delete an item",
	Args:  cobra.RangeArgs(1, 2),
	RunE:  runPassDelete,
}

var passTrashCmd = &cobra.Command{
	Use:   "trash {SHARE_ID ITEM_ID | SEARCH}",
	Short: "Move an item to trash",
	Args:  cobra.RangeArgs(1, 2),
	RunE:  runPassTrash,
}

var passRestoreCmd = &cobra.Command{
	Use:   "restore SHARE_ID ITEM_ID",
	Short: "Restore an item from trash",
	Args:  cobra.ExactArgs(2),
	RunE:  runPassRestore,
}

func init() {
	passCreateCmd.Flags().StringVar(&passCreateType, "type", "login", "Item type (login, note, card)")
	passCreateCmd.Flags().StringVar(&passCreateName, "name", "", "Item name")
	passCreateCmd.Flags().StringVar(&passCreateUsername, "username", "", "Username (login)")
	passCreateCmd.Flags().StringVar(&passCreatePassword, "password", "", "Password (login)")
	passCreateCmd.Flags().StringVar(&passCreateEmail, "email", "", "Email (login)")
	passCreateCmd.Flags().StringVar(&passCreateURL, "url", "", "URL (login)")
	passCreateCmd.Flags().StringVar(&passCreateNote, "note", "", "Note")
	passCreateCmd.Flags().StringVar(&passCreateVault, "vault", "", "Vault name or ID (default: first vault)")

	passCreateCmd.Flags().StringVar(&passCreateHolder, "holder", "", "Cardholder name (card)")
	passCreateCmd.Flags().StringVar(&passCreateNumber, "number", "", "Card number (card)")
	passCreateCmd.Flags().StringVar(&passCreateExpiry, "expiry", "", "Expiry YYYY-MM (card)")
	passCreateCmd.Flags().StringVar(&passCreateCVV, "cvv", "", "CVV (card)")

	passEditCmd.Flags().StringVar(&passEditName, "name", "", "New name")
	passEditCmd.Flags().StringVar(&passEditUsername, "username", "", "New username")
	passEditCmd.Flags().StringVar(&passEditPassword, "password", "", "New password")
	passEditCmd.Flags().StringVar(&passEditEmail, "email", "", "New email")
	passEditCmd.Flags().StringVar(&passEditURL, "url", "", "New URL")
	passEditCmd.Flags().StringVar(&passEditNote, "note", "", "New note")
}

func runPassCreate(cmd *cobra.Command, args []string) error {
	if passCreateName == "" {
		return fmt.Errorf("--name is required")
	}

	ctx := context.Background()
	c, kr, err := getPassContext(ctx)
	if err != nil {
		return err
	}

	// Resolve vault
	var shareID string
	if passCreateVault != "" {
		shareID, err = resolveVaultShareID(ctx, c, kr, passCreateVault)
		if err != nil {
			return err
		}
	} else {
		vaults, err := listDecryptedVaults(ctx, c, kr)
		if err != nil {
			return err
		}
		if len(vaults) == 0 {
			return fmt.Errorf("no vaults found")
		}
		shareID = vaults[0].ShareID
	}

	keys, err := decryptShareKeys(ctx, c, shareID, kr)
	if err != nil {
		return err
	}
	shareKey, keyRotation := keys.latestKey()

	// Build protobuf item
	item := &pb.Item{
		Metadata: &pb.Metadata{
			Name: passCreateName,
			Note: passCreateNote,
		},
		Content: &pb.Content{},
	}

	switch passCreateType {
	case "login":
		var urls []string
		if passCreateURL != "" {
			urls = []string{passCreateURL}
		}
		item.Content.Content = &pb.Content_Login{
			Login: &pb.ItemLogin{
				ItemUsername: passCreateUsername,
				ItemEmail:    passCreateEmail,
				Password:     passCreatePassword,
				Urls:         urls,
			},
		}
	case "note":
		item.Content.Content = &pb.Content_Note{
			Note: &pb.ItemNote{},
		}
	case "card":
		item.Content.Content = &pb.Content_CreditCard{
			CreditCard: &pb.ItemCreditCard{
				CardholderName:     passCreateHolder,
				Number:             passCreateNumber,
				ExpirationDate:     passCreateExpiry,
				VerificationNumber: passCreateCVV,
			},
		}
	default:
		return fmt.Errorf("unsupported item type: %s (use login, note, or card)", passCreateType)
	}

	// If --alias-prefix is set, create login + alias in one call
	if passCreateAliasPrefix != "" {
		if passCreateType != "login" {
			return fmt.Errorf("--alias-prefix can only be used with --type login")
		}
		aliasName := passCreateName + " (alias)"
		aliasItem := &pb.Item{
			Metadata: &pb.Metadata{Name: aliasName},
			Content:  &pb.Content{Content: &pb.Content_Alias{Alias: &pb.ItemAlias{}}},
		}
		// Set the alias email on the login item
		suffixes, _, err := fetchAliasOptions(ctx, c, shareID)
		if err != nil {
			return err
		}
		for _, s := range suffixes {
			if passCreateAliasSuffix == "" || s.Suffix == passCreateAliasSuffix || strings.HasSuffix(s.Suffix, passCreateAliasSuffix) {
				aliasEmail := passCreateAliasPrefix + s.Suffix
				if login, ok := item.Content.Content.(*pb.Content_Login); ok {
					login.Login.ItemEmail = aliasEmail
				}
				break
			}
		}
		return doCreateLoginWithAlias(ctx, c, kr, shareID, item, aliasItem, passCreateAliasPrefix, passCreateAliasSuffix, passCreateAliasMailbox)
	}

	// Generate item key, encrypt content, encrypt item key
	itemKey, err := crypto.GenerateItemKey()
	if err != nil {
		return err
	}

	encryptedContent, err := crypto.EncryptItemContent(item, itemKey)
	if err != nil {
		return err
	}

	encryptedItemKey, err := crypto.EncryptItemKey(itemKey, shareKey)
	if err != nil {
		return err
	}

	reqBody := map[string]interface{}{
		"Content":              encryptedContent,
		"ContentFormatVersion": 7,
		"ItemKey":              encryptedItemKey,
		"KeyRotation":          keyRotation,
	}

	body, _ := json.Marshal(reqBody)
	resp, statusCode, err := c.Do(ctx, "POST", "/pass/v1/share/"+shareID+"/item", nil, string(body), "", "")
	if err != nil {
		return err
	}
	if statusCode >= 400 {
		return fmt.Errorf("create item failed: %s", string(resp))
	}

	fmt.Fprintf(os.Stderr, "Item created.\n")
	printJSON(resp)
	return nil
}

func runPassEdit(cmd *cobra.Command, args []string) error {
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

	// Fetch current item
	body, _, err := c.Do(ctx, "GET", fmt.Sprintf("/pass/v1/share/%s/item/%s", shareID, itemID), nil, "", "", "")
	if err != nil {
		return err
	}

	var res struct {
		Item struct {
			ItemID      string
			Revision    int
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

	itemKeyBytes, err := crypto.DecryptItemKey(res.Item.ItemKey, shareKey)
	if err != nil {
		return err
	}

	item, err := crypto.DecryptItemContent(res.Item.Content, itemKeyBytes)
	if err != nil {
		return err
	}

	// Apply edits
	if item.Metadata == nil {
		item.Metadata = &pb.Metadata{}
	}
	if passEditName != "" {
		item.Metadata.Name = passEditName
	}
	if passEditNote != "" {
		item.Metadata.Note = passEditNote
	}

	if item.Content != nil {
		if login, ok := item.Content.Content.(*pb.Content_Login); ok {
			if passEditUsername != "" {
				login.Login.ItemUsername = passEditUsername
			}
			if passEditPassword != "" {
				login.Login.Password = passEditPassword
			}
			if passEditEmail != "" {
				login.Login.ItemEmail = passEditEmail
			}
			if passEditURL != "" {
				login.Login.Urls = []string{passEditURL}
			}
		}
	}

	// Re-encrypt with same item key
	encryptedContent, err := crypto.EncryptItemContent(item, itemKeyBytes)
	if err != nil {
		return err
	}

	// Fetch latest item key for the request
	latestKeyResp, _, err := c.Do(ctx, "GET", fmt.Sprintf("/pass/v1/share/%s/item/%s/key/latest", shareID, itemID), nil, "", "", "")
	if err != nil {
		return err
	}
	var latestKey struct {
		Key struct {
			Key         string
			KeyRotation int
		}
	}
	_ = json.Unmarshal(latestKeyResp, &latestKey)

	reqBody := map[string]interface{}{
		"Content":              encryptedContent,
		"ContentFormatVersion": 7,
		"KeyRotation":          latestKey.Key.KeyRotation,
		"LastRevision":         res.Item.Revision,
	}

	updateBody, _ := json.Marshal(reqBody)
	resp, statusCode, err := c.Do(ctx, "PUT", fmt.Sprintf("/pass/v1/share/%s/item/%s", shareID, itemID), nil, string(updateBody), "", "")
	if err != nil {
		return err
	}
	if statusCode >= 400 {
		return fmt.Errorf("edit item failed: %s", string(resp))
	}

	fmt.Fprintf(os.Stderr, "Item updated.\n")
	return nil
}

func runPassDelete(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	c, kr, err := getPassContext(ctx)
	if err != nil {
		return err
	}

	shareID, itemID, err := resolvePassItemArgs(ctx, c, kr, args)
	if err != nil {
		return err
	}

	// Get revision for the request
	body, _, err := c.Do(ctx, "GET", fmt.Sprintf("/pass/v1/share/%s/item/%s", shareID, itemID), nil, "", "", "")
	if err != nil {
		return err
	}
	var res struct {
		Item struct {
			Revision int
			State    int
		}
	}
	_ = json.Unmarshal(body, &res)

	// Trash first if not already trashed (State 2 = trashed)
	if res.Item.State != 2 {
		trashReq := map[string]interface{}{
			"Items": []map[string]interface{}{
				{"ItemID": itemID, "Revision": res.Item.Revision},
			},
		}
		trashBody, _ := json.Marshal(trashReq)
		resp, statusCode, err := c.Do(ctx, "POST", "/pass/v1/share/"+shareID+"/item/trash", nil, string(trashBody), "", "")
		if err != nil {
			return err
		}
		if statusCode >= 400 {
			return fmt.Errorf("trash failed: %s", string(resp))
		}
		// Re-fetch to get updated revision
		body, _, err = c.Do(ctx, "GET", fmt.Sprintf("/pass/v1/share/%s/item/%s", shareID, itemID), nil, "", "", "")
		if err != nil {
			return err
		}
		_ = json.Unmarshal(body, &res)
	}

	reqBody := map[string]interface{}{
		"Items": []map[string]interface{}{
			{"ItemID": itemID, "Revision": res.Item.Revision},
		},
	}
	delBody, _ := json.Marshal(reqBody)
	resp, statusCode, err := c.Do(ctx, "DELETE", "/pass/v1/share/"+shareID+"/item", nil, string(delBody), "", "")
	if err != nil {
		return err
	}
	if statusCode >= 400 {
		return fmt.Errorf("delete failed: %s", string(resp))
	}

	fmt.Fprintf(os.Stderr, "Item deleted.\n")
	return nil
}

func runPassTrash(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	c, kr, err := getPassContext(ctx)
	if err != nil {
		return err
	}

	shareID, itemID, err := resolvePassItemArgs(ctx, c, kr, args)
	if err != nil {
		return err
	}

	body, _, err := c.Do(ctx, "GET", fmt.Sprintf("/pass/v1/share/%s/item/%s", shareID, itemID), nil, "", "", "")
	if err != nil {
		return err
	}
	var res struct {
		Item struct{ Revision int }
	}
	_ = json.Unmarshal(body, &res)

	reqBody := map[string]interface{}{
		"Items": []map[string]interface{}{
			{"ItemID": itemID, "Revision": res.Item.Revision},
		},
	}
	trashBody, _ := json.Marshal(reqBody)
	resp, statusCode, err := c.Do(ctx, "POST", "/pass/v1/share/"+shareID+"/item/trash", nil, string(trashBody), "", "")
	if err != nil {
		return err
	}
	if statusCode >= 400 {
		return fmt.Errorf("trash failed: %s", string(resp))
	}

	fmt.Fprintf(os.Stderr, "Item trashed.\n")
	return nil
}

func runPassRestore(cmd *cobra.Command, args []string) error {
	shareID := args[0]
	itemID := args[1]

	ctx := context.Background()
	c, _, err := getPassContext(ctx)
	if err != nil {
		return err
	}

	body, _, err := c.Do(ctx, "GET", fmt.Sprintf("/pass/v1/share/%s/item/%s", shareID, itemID), nil, "", "", "")
	if err != nil {
		return err
	}
	var res struct {
		Item struct{ Revision int }
	}
	_ = json.Unmarshal(body, &res)

	reqBody := map[string]interface{}{
		"Items": []map[string]interface{}{
			{"ItemID": itemID, "Revision": res.Item.Revision},
		},
	}
	restoreBody, _ := json.Marshal(reqBody)
	resp, statusCode, err := c.Do(ctx, "POST", "/pass/v1/share/"+shareID+"/item/untrash", nil, string(restoreBody), "", "")
	if err != nil {
		return err
	}
	if statusCode >= 400 {
		return fmt.Errorf("restore failed: %s", string(resp))
	}

	fmt.Fprintf(os.Stderr, "Item restored.\n")
	return nil
}

// resolvePassItemArgs resolves 1 or 2 args into shareID + itemID.
func resolvePassItemArgs(ctx context.Context, c *client.Client, kr *crypto.KeyRings, args []string) (string, string, error) {
	if len(args) == 2 {
		return args[0], args[1], nil
	}

	found, err := searchPassItem(ctx, c, kr, args[0])
	if err != nil {
		return "", "", err
	}
	return found.ShareID, found.ItemID, nil
}
