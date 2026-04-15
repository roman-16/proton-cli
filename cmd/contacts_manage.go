package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/roman-16/proton-cli/internal/crypto"
	"github.com/spf13/cobra"
)

var contactsCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a contact",
	RunE:  runContactsCreate,
}

var contactsDeleteCmd = &cobra.Command{
	Use:   "delete {CONTACT_ID | NAME_OR_EMAIL}...",
	Short: "Delete contacts",
	Args:  cobra.MinimumNArgs(1),
	RunE:  runContactsDelete,
}

var contactsUpdateCmd = &cobra.Command{
	Use:   "update {CONTACT_ID | NAME_OR_EMAIL}",
	Short: "Update a contact",
	Args:  cobra.ExactArgs(1),
	RunE:  runContactsUpdate,
}

func runContactsCreate(cmd *cobra.Command, args []string) error {
	if contactName == "" && contactEmail == "" {
		return fmt.Errorf("--name or --email is required")
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

	userKR := kr.UserKR

	uid := fmt.Sprintf("proton-cli-%d", time.Now().UnixNano())
	displayName := contactName
	if displayName == "" {
		displayName = contactEmail
	}

	signedVCard := fmt.Sprintf("BEGIN:VCARD\r\nVERSION:4.0\r\nFN:%s\r\nUID:%s\r\n", displayName, uid)
	if contactEmail != "" {
		signedVCard += fmt.Sprintf("item1.EMAIL;PREF=1:%s\r\n", contactEmail)
	}
	signedVCard += "END:VCARD"

	var encryptedVCard string
	if contactPhone != "" {
		encryptedVCard = fmt.Sprintf("BEGIN:VCARD\r\nVERSION:4.0\r\nTEL;PREF=1:%s\r\nEND:VCARD", contactPhone)
	}

	var cards []interface{}

	signedCard, err := crypto.SignContactCard(signedVCard, userKR)
	if err != nil {
		return err
	}
	cards = append(cards, signedCard)

	if encryptedVCard != "" {
		encCard, err := crypto.EncryptContactCard(encryptedVCard, userKR)
		if err != nil {
			return err
		}
		cards = append(cards, encCard)
	}

	reqBody := map[string]interface{}{
		"Contacts":  []map[string]interface{}{{"Cards": cards}},
		"Overwrite": 0,
		"Labels":    0,
	}

	body, _ := json.Marshal(reqBody)
	resp, statusCode, err := c.Do(ctx, "POST", "/contacts/v4/contacts", nil, string(body), "", "")
	if err != nil {
		return err
	}
	if statusCode >= 400 {
		fmt.Fprintf(os.Stderr, "Error: %s\n", string(resp))
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "Contact created.\n")
	printJSON(resp)
	return nil
}

func runContactsDelete(cmd *cobra.Command, args []string) error {
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

	userKR := kr.UserKR

	var ids []string
	for _, arg := range args {
		if isLikelyID(arg) {
			ids = append(ids, arg)
		} else {
			id, err := resolveContactID(ctx, c, userKR, arg)
			if err != nil {
				return err
			}
			ids = append(ids, id)
		}
	}

	reqBody := map[string]interface{}{"IDs": ids}
	body, _ := json.Marshal(reqBody)
	resp, statusCode, err := c.Do(ctx, "PUT", "/contacts/v4/contacts/delete", nil, string(body), "", "")
	if err != nil {
		return err
	}
	if statusCode >= 400 {
		return fmt.Errorf("delete failed: %s", string(resp))
	}

	fmt.Fprintf(os.Stderr, "Deleted %d contact(s).\n", len(ids))
	return nil
}

func runContactsUpdate(cmd *cobra.Command, args []string) error {
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

	userKR := kr.UserKR

	contactID := args[0]
	if !isLikelyID(contactID) {
		resolved, err := resolveContactID(ctx, c, userKR, contactID)
		if err != nil {
			return err
		}
		contactID = resolved
	}

	// Fetch existing contact
	body, _, err := c.Do(ctx, "GET", "/contacts/v4/contacts/"+contactID, nil, "", "", "")
	if err != nil {
		return err
	}

	var res struct {
		Contact struct {
			ID    string
			Cards []map[string]interface{}
		}
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return err
	}

	decrypted, err := crypto.DecryptContactCards(res.Contact.Cards, userKR)
	if err != nil {
		return fmt.Errorf("failed to decrypt contact: %w", err)
	}

	currentName, currentEmail, currentPhone, currentUID := "", "", "", ""
	for _, card := range decrypted {
		if n := parseICalField(card, "FN"); n != "" {
			currentName = n
		}
		if e := parseICalField(card, "EMAIL"); e != "" {
			currentEmail = e
		}
		if t := parseICalField(card, "TEL"); t != "" {
			currentPhone = t
		}
		if u := parseICalField(card, "UID"); u != "" {
			currentUID = u
		}
	}

	if contactUpdateName != "" {
		currentName = contactUpdateName
	}
	if contactUpdateEmail != "" {
		currentEmail = contactUpdateEmail
	}
	if contactUpdatePhone != "" {
		currentPhone = contactUpdatePhone
	}
	if currentUID == "" {
		currentUID = fmt.Sprintf("proton-cli-%d", time.Now().UnixNano())
	}

	displayName := currentName
	if displayName == "" {
		displayName = currentEmail
	}

	signedVCard := fmt.Sprintf("BEGIN:VCARD\r\nVERSION:4.0\r\nFN:%s\r\nUID:%s\r\n", displayName, currentUID)
	if currentEmail != "" {
		signedVCard += fmt.Sprintf("item1.EMAIL;PREF=1:%s\r\n", currentEmail)
	}
	signedVCard += "END:VCARD"

	var cards []interface{}

	signedCard, err := crypto.SignContactCard(signedVCard, userKR)
	if err != nil {
		return err
	}
	cards = append(cards, signedCard)

	if currentPhone != "" {
		encVCard := fmt.Sprintf("BEGIN:VCARD\r\nVERSION:4.0\r\nTEL;PREF=1:%s\r\nEND:VCARD", currentPhone)
		encCard, err := crypto.EncryptContactCard(encVCard, userKR)
		if err != nil {
			return err
		}
		cards = append(cards, encCard)
	}

	reqBody := map[string]interface{}{"Cards": cards}
	updateBody, _ := json.Marshal(reqBody)
	resp, statusCode, err := c.Do(ctx, "PUT", "/contacts/v4/contacts/"+contactID, nil, string(updateBody), "", "")
	if err != nil {
		return err
	}
	if statusCode >= 400 {
		return fmt.Errorf("update failed: %s", string(resp))
	}

	fmt.Fprintf(os.Stderr, "Contact updated.\n")
	return nil
}
