package cmd

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"

	pgp "github.com/ProtonMail/gopenpgp/v2/crypto"
	"github.com/roman-16/proton-cli/internal/crypto"
	"github.com/spf13/cobra"
)

var mailCmd = &cobra.Command{
	Use:   "mail",
	Short: "Mail operations (encrypted)",
}

var mailReadCmd = &cobra.Command{
	Use:   "read MESSAGE_ID",
	Short: "Read a message (decrypted body)",
	Args:  cobra.ExactArgs(1),
	RunE:  runMailRead,
}

var (
	mailTo      string
	mailSubject string
	mailBody    string
)

var mailSendCmd = &cobra.Command{
	Use:   "send",
	Short: "Send a message",
	RunE:  runMailSend,
}

func init() {
	mailSendCmd.Flags().StringVar(&mailTo, "to", "", "Recipient email")
	mailSendCmd.Flags().StringVar(&mailSubject, "subject", "", "Subject")
	mailSendCmd.Flags().StringVar(&mailBody, "body", "", "Message body (plain text)")

	mailCmd.AddCommand(mailReadCmd, mailSendCmd)
	rootCmd.AddCommand(mailCmd)
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

	// Find the address key ring for this message
	addrKR, ok := kr.AddrKRs[res.Message.AddressID]
	if !ok {
		// Try first address
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
	os.Stdout.Write(out)
	fmt.Println()
	return nil
}

func runMailSend(cmd *cobra.Command, args []string) error {
	if mailTo == "" {
		return fmt.Errorf("--to is required")
	}
	if mailSubject == "" {
		return fmt.Errorf("--subject is required")
	}
	if mailBody == "" {
		return fmt.Errorf("--body is required")
	}

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

	addrKR, addrID, err := kr.FirstAddrKR()
	if err != nil {
		return err
	}

	senderEmail := kr.Addresses[0].Email

	// Encrypt draft body with address key
	plainMsg := pgp.NewPlainMessageFromString(mailBody)
	encDraft, err := addrKR.Encrypt(plainMsg, addrKR)
	if err != nil {
		return fmt.Errorf("failed to encrypt draft: %w", err)
	}
	armoredDraft, err := encDraft.GetArmored()
	if err != nil {
		return err
	}

	// Step 1: Create draft
	draftReq := map[string]interface{}{
		"Message": map[string]interface{}{
			"ToList":  []map[string]string{{"Address": mailTo, "Name": mailTo}},
			"CCList":  []interface{}{},
			"BCCList": []interface{}{},
			"Subject": mailSubject,
			"Sender":  map[string]string{"Address": senderEmail, "Name": ""},
			"Body":    armoredDraft,
			"MIMEType": "text/plain",
		},
	}

	draftBody, _ := json.Marshal(draftReq)
	draftResp, _, err := c.Do(ctx, "POST", "/mail/v4/messages", nil, string(draftBody), "", "")
	if err != nil {
		return err
	}

	var draftResult struct {
		Code    int
		Message struct {
			ID string
		}
	}
	if err := json.Unmarshal(draftResp, &draftResult); err != nil {
		return err
	}
	if draftResult.Code != 1000 {
		return fmt.Errorf("create draft failed: %s", string(draftResp))
	}

	messageID := draftResult.Message.ID

	// Clean up draft on failure
	cleanupDraft := func() {
		c.Do(ctx, "DELETE", "/mail/v4/messages/delete", nil,
			fmt.Sprintf(`{"IDs":["%s"]}`, messageID), "", "")
	}

	fmt.Fprintf(os.Stderr, "Sending...\n")

	// Step 2: Get recipient's public keys
	recipientKeysBody, _, err := c.Do(ctx, "GET", "/core/v4/keys/all",
		map[string]string{"Email": mailTo}, "", "", "")
	if err != nil {
		return err
	}

	var keysResult struct {
		Address struct {
			Keys []struct {
				PublicKey string
			}
		}
	}
	json.Unmarshal(recipientKeysBody, &keysResult)

	isInternal := len(keysResult.Address.Keys) > 0

	// Generate session key for the message
	sessionKey, err := pgp.GenerateSessionKey()
	if err != nil {
		return err
	}

	// Encrypt body with session key + sign with address key
	sendPlainMsg := pgp.NewPlainMessageFromString(mailBody)
	encBody, err := sessionKey.EncryptAndSign(sendPlainMsg, addrKR)
	if err != nil {
		return err
	}

	_ = addrID

	var packages []map[string]interface{}

	if isInternal {
		// Internal Proton recipient
		bodyKeyPacket, err := addrKR.EncryptSessionKey(sessionKey)
		if err != nil {
			return err
		}

		recKey, err := pgp.NewKeyFromArmored(keysResult.Address.Keys[0].PublicKey)
		if err != nil {
			return fmt.Errorf("failed to parse recipient key: %w", err)
		}
		recKR, err := pgp.NewKeyRing(recKey)
		if err != nil {
			return err
		}
		recKeyPacket, err := recKR.EncryptSessionKey(sessionKey)
		if err != nil {
			return err
		}

		packages = []map[string]interface{}{
			{
				"Addresses": map[string]interface{}{
					mailTo: map[string]interface{}{
						"Type":          1,
						"BodyKeyPacket": base64.StdEncoding.EncodeToString(recKeyPacket),
						"Signature":     0,
					},
				},
				"MIMEType":      "text/plain",
				"Type":          1,
				"Body":          base64.StdEncoding.EncodeToString(encBody),
				"BodyKeyPacket": base64.StdEncoding.EncodeToString(bodyKeyPacket),
			},
		}
	} else {
		// External recipient — SEND_CLEAR
		// Body is encrypted with session key, BodyKey has plaintext session key for server
		packages = []map[string]interface{}{
			{
				"Addresses": map[string]interface{}{
					mailTo: map[string]interface{}{
						"Type":      4, // SEND_CLEAR
						"Signature": 0,
					},
				},
				"MIMEType": "text/plain",
				"Type":     4, // SEND_CLEAR
				"Body":     base64.StdEncoding.EncodeToString(encBody),
				"BodyKey":  map[string]interface{}{"Key": base64.StdEncoding.EncodeToString(sessionKey.Key), "Algorithm": sessionKey.Algo},
			},
		}
	}

	sendReq := map[string]interface{}{
		"ExpirationTime":   nil,
		"AutoSaveContacts": 0,
		"Packages":         packages,
	}

	sendBody, _ := json.Marshal(sendReq)
	sendResp, statusCode, err := c.Do(ctx, "POST",
		"/mail/v4/messages/"+messageID,
		nil, string(sendBody), "", "")
	if err != nil {
		return err
	}

	if statusCode >= 400 {
		cleanupDraft()
		return fmt.Errorf("send failed: %s", string(sendResp))
	}

	fmt.Fprintf(os.Stderr, "Message sent.\n")
	return nil
}
