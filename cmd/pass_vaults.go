package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/roman-16/proton-cli/internal/crypto"
	pb "github.com/roman-16/proton-cli/internal/proto"
	"github.com/spf13/cobra"
)

var passVaultsCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a vault",
	RunE:  runPassVaultsCreate,
}

var passVaultsDeleteCmd = &cobra.Command{
	Use:   "delete SHARE_ID",
	Short: "Delete a vault",
	Args:  cobra.ExactArgs(1),
	RunE:  runPassVaultsDelete,
}

func runPassVaultsCreate(cmd *cobra.Command, args []string) error {
	if passVaultName == "" {
		return fmt.Errorf("--name is required")
	}

	ctx := context.Background()
	c, kr, err := getPassContext(ctx)
	if err != nil {
		return err
	}

	vault := &pb.Vault{
		Name: passVaultName,
	}

	// Generate vault key and encrypt it with user's PGP key
	encryptedVaultKey, rawKey, err := crypto.CreateVaultKeys(kr.UserKR)
	if err != nil {
		return err
	}

	// Encrypt vault content with the new vault key
	encryptedContent, err := crypto.EncryptVaultContent(vault, rawKey)
	if err != nil {
		return err
	}

	_, addrID, err := kr.PrimaryAddrKR()
	if err != nil {
		return err
	}

	reqBody := map[string]interface{}{
		"AddressID":            addrID,
		"ContentFormatVersion": 1,
		"Content":              encryptedContent,
		"EncryptedVaultKey":    encryptedVaultKey,
	}

	body, _ := json.Marshal(reqBody)
	resp, statusCode, err := c.Do(ctx, "POST", "/pass/v1/vault", nil, string(body), "", "")
	if err != nil {
		return err
	}
	if statusCode >= 400 {
		return fmt.Errorf("create vault failed: %s", string(resp))
	}

	fmt.Fprintf(os.Stderr, "Vault created.\n")
	printJSON(resp)
	return nil
}

func runPassVaultsDelete(cmd *cobra.Command, args []string) error {
	shareID := args[0]

	ctx := context.Background()
	c, _, err := getPassContext(ctx)
	if err != nil {
		return err
	}

	resp, statusCode, err := c.Do(ctx, "DELETE", "/pass/v1/vault/"+shareID, nil, "", "", "")
	if err != nil {
		return err
	}
	if statusCode >= 400 {
		return fmt.Errorf("delete vault failed: %s", string(resp))
	}

	fmt.Fprintf(os.Stderr, "Vault deleted.\n")
	return nil
}
