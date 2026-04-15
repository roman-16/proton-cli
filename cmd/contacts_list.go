package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/roman-16/proton-cli/internal/crypto"
	"github.com/spf13/cobra"
)

var contactsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List contacts (decrypted)",
	RunE:  runContactsList,
}

var contactsGetCmd = &cobra.Command{
	Use:   "get {CONTACT_ID | NAME_OR_EMAIL}",
	Short: "Get a contact (decrypted). Pass an ID or search by name/email.",
	Args:  cobra.ExactArgs(1),
	RunE:  runContactsGet,
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

	userKR := kr.UserKR

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
		decrypted, err := crypto.DecryptContactCards(contact.Cards, userKR)
		if err != nil {
			decrypted = []string{"(decryption failed)"}
		}
		output = append(output, decryptedContact{ID: contact.ID, Cards: decrypted})
	}

	if flagJSON {
		out, _ := json.MarshalIndent(output, "", "  ")
		_, _ = os.Stdout.Write(out)
		fmt.Println()
		return nil
	}

	headers := []string{"ID", "NAME", "EMAIL", "PHONE"}
	var rows [][]string
	for _, c := range output {
		var name, email, phone string
		for _, card := range c.Cards {
			if n := parseICalField(card, "FN"); n != "" {
				name = n
			}
			if e := parseICalField(card, "EMAIL"); e != "" {
				email = e
			}
			if t := parseICalField(card, "TEL"); t != "" {
				phone = t
			}
		}
		rows = append(rows, []string{c.ID, name, email, phone})
	}

	printTable(headers, rows)
	return nil
}

func runContactsGet(cmd *cobra.Command, args []string) error {
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
		return err
	}

	if flagJSON {
		result := map[string]interface{}{"ID": res.Contact.ID, "Cards": decrypted}
		out, _ := json.MarshalIndent(result, "", "  ")
		_, _ = os.Stdout.Write(out)
		fmt.Println()
		return nil
	}

	fmt.Printf("ID: %s\n", res.Contact.ID)
	for _, card := range decrypted {
		if n := parseICalField(card, "FN"); n != "" {
			fmt.Printf("Name:  %s\n", n)
		}
		if e := parseICalField(card, "EMAIL"); e != "" {
			fmt.Printf("Email: %s\n", e)
		}
		if t := parseICalField(card, "TEL"); t != "" {
			fmt.Printf("Phone: %s\n", t)
		}
		if o := parseICalField(card, "ORG"); o != "" {
			fmt.Printf("Org:   %s\n", o)
		}
		if n := parseICalField(card, "NOTE"); n != "" {
			fmt.Printf("Note:  %s\n", n)
		}
	}
	return nil
}
