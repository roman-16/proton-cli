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

var mailAttachmentsCmd = &cobra.Command{
	Use:   "attachments",
	Short: "Manage message attachments",
}

var mailAttachmentsListCmd = &cobra.Command{
	Use:   "list MESSAGE_ID",
	Short: "List attachments of a message",
	Args:  cobra.ExactArgs(1),
	RunE:  runMailAttachmentsList,
}

var mailAttachmentsDownloadCmd = &cobra.Command{
	Use:   "download MESSAGE_ID ATTACHMENT_ID [OUTPUT_PATH]",
	Short: "Download and decrypt an attachment",
	Args:  cobra.RangeArgs(2, 3),
	RunE:  runMailAttachmentsDownload,
}

func runMailAttachmentsList(cmd *cobra.Command, args []string) error {
	messageID := args[0]

	ctx := context.Background()
	c, err := getAuthenticatedClient(ctx)
	if err != nil {
		return err
	}

	body, _, err := c.Do(ctx, "GET", "/mail/v4/messages/"+messageID, nil, "", "", "")
	if err != nil {
		return err
	}

	var res struct {
		Message struct {
			Attachments []struct {
				ID       string
				Name     string
				Size     int64
				MIMEType string
			}
		}
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return err
	}

	if flagJSON {
		out, _ := json.MarshalIndent(res.Message.Attachments, "", "  ")
		_, _ = os.Stdout.Write(out)
		fmt.Println()
		return nil
	}

	headers := []string{"ID", "NAME", "SIZE", "TYPE"}
	var rows [][]string
	for _, att := range res.Message.Attachments {
		rows = append(rows, []string{att.ID, att.Name, formatSize(att.Size), att.MIMEType})
	}

	printTable(headers, rows)
	return nil
}

func runMailAttachmentsDownload(cmd *cobra.Command, args []string) error {
	messageID := args[0]
	attachmentID := args[1]

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

	// Get message to find AddressID and attachment KeyPackets
	msgBody, _, err := c.Do(ctx, "GET", "/mail/v4/messages/"+messageID, nil, "", "", "")
	if err != nil {
		return err
	}

	var msgRes struct {
		Message struct {
			AddressID   string
			Attachments []struct {
				ID         string
				Name       string
				KeyPackets string
			}
		}
	}
	if err := json.Unmarshal(msgBody, &msgRes); err != nil {
		return err
	}

	var keyPackets, attName string
	for _, att := range msgRes.Message.Attachments {
		if att.ID == attachmentID {
			keyPackets = att.KeyPackets
			attName = att.Name
			break
		}
	}
	if keyPackets == "" {
		return fmt.Errorf("attachment %s not found in message %s", attachmentID, messageID)
	}

	addrKR, ok := kr.AddrKRs[msgRes.Message.AddressID]
	if !ok {
		var err error
		addrKR, _, err = kr.FirstAddrKR()
		if err != nil {
			return err
		}
	}

	attBody, _, err := c.Do(ctx, "GET", "/mail/v4/attachments/"+attachmentID, nil, "", "", "")
	if err != nil {
		return err
	}

	kpBytes, err := base64.StdEncoding.DecodeString(keyPackets)
	if err != nil {
		return fmt.Errorf("failed to decode key packets: %w", err)
	}

	splitMsg := pgp.NewPGPSplitMessage(kpBytes, attBody)
	pgpMsg := splitMsg.GetPGPMessage()
	dec, err := addrKR.Decrypt(pgpMsg, nil, 0)
	if err != nil {
		return fmt.Errorf("failed to decrypt attachment: %w", err)
	}

	outputPath := attName
	if len(args) >= 3 {
		outputPath = args[2]
	}
	if err := os.WriteFile(outputPath, dec.GetBinary(), 0644); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "Downloaded %s (%d bytes)\n", outputPath, len(dec.GetBinary()))
	return nil
}
