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
	passAliasPrefix  string
	passAliasSuffix  string
	passAliasMailbox string

	passCreateAliasPrefix  string
	passCreateAliasSuffix  string
	passCreateAliasMailbox string
)

var passAliasCmd = &cobra.Command{
	Use:   "alias",
	Short: "Manage aliases",
}

var passAliasOptionsCmd = &cobra.Command{
	Use:   "options",
	Short: "List available alias suffixes and mailboxes",
	RunE:  runPassAliasOptions,
}

var passAliasCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create an alias",
	RunE:  runPassAliasCreate,
}

func init() {
	passAliasCreateCmd.Flags().StringVar(&passAliasPrefix, "prefix", "", "Alias prefix (the part before @)")
	passAliasCreateCmd.Flags().StringVar(&passAliasSuffix, "suffix", "", "Alias suffix (e.g. @passmail.net)")
	passAliasCreateCmd.Flags().StringVar(&passAliasMailbox, "mailbox", "", "Mailbox email to forward to")
	passAliasCreateCmd.Flags().StringVar(&passCreateName, "name", "", "Display name for the alias item")
	passAliasCreateCmd.Flags().StringVar(&passCreateVault, "vault", "", "Vault name or ID (default: first vault)")

	passCreateCmd.Flags().StringVar(&passCreateAliasPrefix, "alias-prefix", "", "Create alias with this prefix (login only)")
	passCreateCmd.Flags().StringVar(&passCreateAliasSuffix, "alias-suffix", "", "Alias suffix (e.g. @passmail.net)")
	passCreateCmd.Flags().StringVar(&passCreateAliasMailbox, "alias-mailbox", "", "Mailbox email to forward alias to")

	passAliasCmd.AddCommand(passAliasOptionsCmd, passAliasCreateCmd)
	passCmd.AddCommand(passAliasCmd)
}

func runPassAliasOptions(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	c, kr, err := getPassContext(ctx)
	if err != nil {
		return err
	}

	vaults, err := listDecryptedVaults(ctx, c, kr)
	if err != nil {
		return err
	}
	if len(vaults) == 0 {
		return fmt.Errorf("no vaults found")
	}

	suffixes, mailboxes, err := fetchAliasOptions(ctx, c, vaults[0].ShareID)
	if err != nil {
		return err
	}

	if flagJSON {
		result := map[string]interface{}{"Suffixes": suffixes, "Mailboxes": mailboxes}
		out, _ := json.MarshalIndent(result, "", "  ")
		_, _ = os.Stdout.Write(out)
		fmt.Println()
		return nil
	}

	fmt.Println("Suffixes:")
	for _, s := range suffixes {
		fmt.Printf("  %s\n", s.Suffix)
	}
	fmt.Println()
	fmt.Println("Mailboxes:")
	for _, m := range mailboxes {
		fmt.Printf("  %s (ID: %d)\n", m.Email, m.ID)
	}

	return nil
}

func runPassAliasCreate(cmd *cobra.Command, args []string) error {
	if passAliasPrefix == "" {
		return fmt.Errorf("--prefix is required")
	}

	ctx := context.Background()
	c, kr, err := getPassContext(ctx)
	if err != nil {
		return err
	}

	shareID, err := resolvePassVaultOrDefault(ctx, c, kr, passCreateVault)
	if err != nil {
		return err
	}

	return doCreateAlias(ctx, c, kr, shareID, passAliasPrefix, passAliasSuffix, passAliasMailbox, passCreateName)
}

// doCreateAlias performs the alias creation API call.
func doCreateAlias(ctx context.Context, c *client.Client, kr *crypto.KeyRings, shareID, prefix, suffix, mailbox, name string) error {
	suffixes, mailboxes, err := fetchAliasOptions(ctx, c, shareID)
	if err != nil {
		return err
	}

	signedSuffix, err := resolveAliasSuffix(suffixes, suffix)
	if err != nil {
		return err
	}

	mailboxID, err := resolveAliasMailbox(mailboxes, mailbox)
	if err != nil {
		return err
	}

	keys, err := decryptShareKeys(ctx, c, shareID, kr)
	if err != nil {
		return err
	}
	shareKey, keyRotation := keys.latestKey()

	if name == "" {
		name = prefix
	}

	item := &pb.Item{
		Metadata: &pb.Metadata{Name: name},
		Content:  &pb.Content{Content: &pb.Content_Alias{Alias: &pb.ItemAlias{}}},
	}

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
		"Prefix":       prefix,
		"SignedSuffix": signedSuffix,
		"MailboxIDs":   []int{mailboxID},
		"Item": map[string]interface{}{
			"Content":              encryptedContent,
			"ContentFormatVersion": 7,
			"ItemKey":              encryptedItemKey,
			"KeyRotation":          keyRotation,
		},
	}

	body, _ := json.Marshal(reqBody)
	resp, statusCode, err := c.Do(ctx, "POST", "/pass/v1/share/"+shareID+"/alias/custom", nil, string(body), "", "")
	if err != nil {
		return err
	}
	if statusCode >= 400 {
		return fmt.Errorf("create alias failed: %s", string(resp))
	}

	fmt.Fprintf(os.Stderr, "Alias created.\n")
	printJSON(resp)
	return nil
}

// doCreateLoginWithAlias creates a login item and alias in one API call.
func doCreateLoginWithAlias(ctx context.Context, c *client.Client, kr *crypto.KeyRings, shareID string, loginItem, aliasItem *pb.Item, prefix, suffix, mailbox string) error {
	suffixes, mailboxes, err := fetchAliasOptions(ctx, c, shareID)
	if err != nil {
		return err
	}

	signedSuffix, err := resolveAliasSuffix(suffixes, suffix)
	if err != nil {
		return err
	}

	mailboxID, err := resolveAliasMailbox(mailboxes, mailbox)
	if err != nil {
		return err
	}

	keys, err := decryptShareKeys(ctx, c, shareID, kr)
	if err != nil {
		return err
	}
	shareKey, keyRotation := keys.latestKey()

	// Encrypt login item
	loginKey, err := crypto.GenerateItemKey()
	if err != nil {
		return err
	}
	encLoginContent, err := crypto.EncryptItemContent(loginItem, loginKey)
	if err != nil {
		return err
	}
	encLoginKey, err := crypto.EncryptItemKey(loginKey, shareKey)
	if err != nil {
		return err
	}

	// Encrypt alias item
	aliasKey, err := crypto.GenerateItemKey()
	if err != nil {
		return err
	}
	encAliasContent, err := crypto.EncryptItemContent(aliasItem, aliasKey)
	if err != nil {
		return err
	}
	encAliasKey, err := crypto.EncryptItemKey(aliasKey, shareKey)
	if err != nil {
		return err
	}

	reqBody := map[string]interface{}{
		"Item": map[string]interface{}{
			"Content":              encLoginContent,
			"ContentFormatVersion": 7,
			"ItemKey":              encLoginKey,
			"KeyRotation":          keyRotation,
		},
		"Alias": map[string]interface{}{
			"Prefix":       prefix,
			"SignedSuffix": signedSuffix,
			"MailboxIDs":   []int{mailboxID},
			"Item": map[string]interface{}{
				"Content":              encAliasContent,
				"ContentFormatVersion": 7,
				"ItemKey":              encAliasKey,
				"KeyRotation":          keyRotation,
			},
		},
	}

	body, _ := json.Marshal(reqBody)
	resp, statusCode, err := c.Do(ctx, "POST", "/pass/v1/share/"+shareID+"/item/with_alias", nil, string(body), "", "")
	if err != nil {
		return err
	}
	if statusCode >= 400 {
		return fmt.Errorf("create login with alias failed: %s", string(resp))
	}

	fmt.Fprintf(os.Stderr, "Login with alias created.\n")
	printJSON(resp)
	return nil
}

// resolvePassVaultOrDefault resolves a vault name/ID or returns the first vault.
func resolvePassVaultOrDefault(ctx context.Context, c *client.Client, kr *crypto.KeyRings, nameOrID string) (string, error) {
	if nameOrID != "" {
		return resolveVaultShareID(ctx, c, kr, nameOrID)
	}
	vaults, err := listDecryptedVaults(ctx, c, kr)
	if err != nil {
		return "", err
	}
	if len(vaults) == 0 {
		return "", fmt.Errorf("no vaults found")
	}
	return vaults[0].ShareID, nil
}

type aliasSuffix struct {
	Suffix       string
	SignedSuffix string
	IsPremium    bool
	IsCustom     bool
	Domain       string
}

type aliasMailbox struct {
	ID    int
	Email string
}

func fetchAliasOptions(ctx context.Context, c *client.Client, shareID string) ([]aliasSuffix, []aliasMailbox, error) {
	body, _, err := c.Do(ctx, "GET", "/pass/v1/share/"+shareID+"/alias/options", nil, "", "", "")
	if err != nil {
		return nil, nil, err
	}

	var res struct {
		Options struct {
			Suffixes []struct {
				Suffix       string
				SignedSuffix string
				IsPremium    bool
				IsCustom     bool
				Domain       string
			}
			Mailboxes []struct {
				ID    int
				Email string
			}
		}
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return nil, nil, err
	}

	var suffixes []aliasSuffix
	for _, s := range res.Options.Suffixes {
		suffixes = append(suffixes, aliasSuffix{
			Suffix:       s.Suffix,
			SignedSuffix: s.SignedSuffix,
			IsPremium:    s.IsPremium,
			IsCustom:     s.IsCustom,
			Domain:       s.Domain,
		})
	}

	var mailboxes []aliasMailbox
	for _, m := range res.Options.Mailboxes {
		mailboxes = append(mailboxes, aliasMailbox{ID: m.ID, Email: m.Email})
	}

	return suffixes, mailboxes, nil
}

func resolveAliasSuffix(suffixes []aliasSuffix, wanted string) (string, error) {
	if wanted == "" {
		if len(suffixes) == 0 {
			return "", fmt.Errorf("no alias suffixes available")
		}
		return suffixes[0].SignedSuffix, nil
	}

	for _, s := range suffixes {
		if s.Suffix == wanted || strings.HasSuffix(s.Suffix, wanted) {
			return s.SignedSuffix, nil
		}
	}

	available := make([]string, len(suffixes))
	for i, s := range suffixes {
		available[i] = s.Suffix
	}
	return "", fmt.Errorf("suffix %q not found; available: %s", wanted, strings.Join(available, ", "))
}

func resolveAliasMailbox(mailboxes []aliasMailbox, wanted string) (int, error) {
	if wanted == "" {
		if len(mailboxes) == 0 {
			return 0, fmt.Errorf("no mailboxes available")
		}
		return mailboxes[0].ID, nil
	}

	for _, m := range mailboxes {
		if m.Email == wanted || strings.Contains(m.Email, wanted) {
			return m.ID, nil
		}
	}

	available := make([]string, len(mailboxes))
	for i, m := range mailboxes {
		available[i] = m.Email
	}
	return 0, fmt.Errorf("mailbox %q not found; available: %s", wanted, strings.Join(available, ", "))
}
