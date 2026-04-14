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

var contactsCmd = &cobra.Command{
	Use:   "contacts",
	Short: "Contact operations (encrypted)",
}

var contactsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List contacts (decrypted)",
	RunE:  runContactsList,
}

var contactsGetCmd = &cobra.Command{
	Use:   "get CONTACT_ID",
	Short: "Get a contact (decrypted)",
	Args:  cobra.ExactArgs(1),
	RunE:  runContactsGet,
}

var (
	contactName  string
	contactEmail string
	contactPhone string
)

var contactsCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a contact",
	RunE:  runContactsCreate,
}

func init() {
	contactsCreateCmd.Flags().StringVar(&contactName, "name", "", "Contact name")
	contactsCreateCmd.Flags().StringVar(&contactEmail, "email", "", "Contact email")
	contactsCreateCmd.Flags().StringVar(&contactPhone, "phone", "", "Contact phone (optional)")

	contactsCmd.AddCommand(contactsListCmd, contactsGetCmd, contactsCreateCmd)
	rootCmd.AddCommand(contactsCmd)
}

func runContactsList(cmd *cobra.Command, args []string) error {
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

	addrKR, _, err := kr.FirstAddrKR()
	if err != nil {
		return err
	}

	// Fetch contacts with cards (export endpoint returns card data)
	body, _, err := c.Do(ctx, "GET", "/contacts/v4/contacts/export",
		map[string]string{"Page": "0", "PageSize": "50"}, "", "", "")
	if err != nil {
		return err
	}

	var res struct {
		Contacts []struct {
			ID    string
			Cards []map[string]interface{}
		}
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return err
	}

	type decryptedContact struct {
		ID    string   `json:"ID"`
		Cards []string `json:"Cards"`
	}

	var output []decryptedContact
	for _, contact := range res.Contacts {
		decrypted, err := crypto.DecryptContactCards(contact.Cards, addrKR)
		if err != nil {
			decrypted = []string{"(decryption failed)"}
		}
		output = append(output, decryptedContact{
			ID:    contact.ID,
			Cards: decrypted,
		})
	}

	out, _ := json.MarshalIndent(output, "", "  ")
	os.Stdout.Write(out)
	fmt.Println()
	return nil
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

	// Contacts use the USER key (not address key) for encryption/signing
	userKR := kr.UserKR

	// Build signed vCard (FN, UID, EMAIL)
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

	// Build encrypted vCard (phone, other fields)
	var encryptedVCard string
	if contactPhone != "" {
		encryptedVCard = fmt.Sprintf("BEGIN:VCARD\r\nVERSION:4.0\r\nTEL;PREF=1:%s\r\nEND:VCARD", contactPhone)
	}

	var cards []interface{}

	// Signed card (Type 2)
	signedCard, err := crypto.SignContactCard(signedVCard, userKR)
	if err != nil {
		return err
	}
	cards = append(cards, signedCard)

	// Encrypted+signed card (Type 3) if we have encrypted fields
	if encryptedVCard != "" {
		encCard, err := crypto.EncryptContactCard(encryptedVCard, userKR)
		if err != nil {
			return err
		}
		cards = append(cards, encCard)
	}

	reqBody := map[string]interface{}{
		"Contacts": []map[string]interface{}{
			{"Cards": cards},
		},
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

func runContactsGet(cmd *cobra.Command, args []string) error {
	contactID := args[0]

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

	addrKR, _, err := kr.FirstAddrKR()
	if err != nil {
		return err
	}

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

	decrypted, err := crypto.DecryptContactCards(res.Contact.Cards, addrKR)
	if err != nil {
		return err
	}

	result := map[string]interface{}{
		"ID":    res.Contact.ID,
		"Cards": decrypted,
	}

	out, _ := json.MarshalIndent(result, "", "  ")
	os.Stdout.Write(out)
	fmt.Println()
	return nil
}
