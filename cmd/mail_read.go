package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/roman-16/proton-cli/internal/crypto"
	"github.com/spf13/cobra"
)

var mailReadCmd = &cobra.Command{
	Use:   "read MESSAGE_ID",
	Short: "Read a message (decrypted body)",
	Args:  cobra.ExactArgs(1),
	RunE:  runMailRead,
}

func runMailRead(cmd *cobra.Command, args []string) error {
	messageID := args[0]

	ctx := context.Background()
	c, err := getAuthenticatedClient(ctx)
	if err != nil {
		return err
	}

	password := getFlag(flagPassword, "PROTON_PASSWORD")
	kr, err := crypto.UnlockKeys(ctx, c, password)
	if err != nil {
		return err
	}

	body, _, err := c.Do(ctx, "GET", "/mail/v4/messages/"+messageID, nil, "", "", "")
	if err != nil {
		return err
	}

	var res struct {
		Message struct {
			ID        string
			Subject   string
			Sender    map[string]interface{}
			ToList    []map[string]interface{}
			Body      string
			AddressID string
		}
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return err
	}

	addrKR, ok := kr.AddrKRs[res.Message.AddressID]
	if !ok {
		var err error
		addrKR, _, err = kr.FirstAddrKR()
		if err != nil {
			return err
		}
	}

	decryptedBody, err := crypto.DecryptMessageBody(res.Message.Body, addrKR)
	if err != nil {
		decryptedBody = "(decryption failed: " + err.Error() + ")"
	}

	result := map[string]interface{}{
		"ID":            res.Message.ID,
		"Subject":       res.Message.Subject,
		"Sender":        res.Message.Sender,
		"ToList":        res.Message.ToList,
		"DecryptedBody": decryptedBody,
	}

	out, _ := json.MarshalIndent(result, "", "  ")
	_, _ = os.Stdout.Write(out)
	fmt.Println()
	return nil
}
