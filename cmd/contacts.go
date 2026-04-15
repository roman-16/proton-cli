package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	pgp "github.com/ProtonMail/gopenpgp/v2/crypto"
	"github.com/roman-16/proton-cli/internal/client"
	"github.com/roman-16/proton-cli/internal/crypto"
	"github.com/spf13/cobra"
)

var contactsCmd = &cobra.Command{
	Use:   "contacts",
	Short: "Contact operations",
}

var (
	contactName  string
	contactEmail string
	contactPhone string

	contactUpdateName  string
	contactUpdateEmail string
	contactUpdatePhone string
)

func init() {
	contactsCreateCmd.Flags().StringVar(&contactName, "name", "", "Contact name")
	contactsCreateCmd.Flags().StringVar(&contactEmail, "email", "", "Contact email")
	contactsCreateCmd.Flags().StringVar(&contactPhone, "phone", "", "Contact phone (optional)")

	contactsUpdateCmd.Flags().StringVar(&contactUpdateName, "name", "", "New name")
	contactsUpdateCmd.Flags().StringVar(&contactUpdateEmail, "email", "", "New email")
	contactsUpdateCmd.Flags().StringVar(&contactUpdatePhone, "phone", "", "New phone")

	contactsCmd.AddCommand(contactsListCmd, contactsGetCmd, contactsCreateCmd, contactsDeleteCmd, contactsUpdateCmd)
	rootCmd.AddCommand(contactsCmd)
}

// resolveContactID searches contacts by name or email, returns the contact ID.
func resolveContactID(ctx context.Context, cl *client.Client, addrKR *pgp.KeyRing, search string) (string, error) {
	type match struct{ id, name, email string }
	var matches []match

	for page := 0; ; page++ {
		body, _, err := cl.Do(ctx, "GET", "/contacts/v4/contacts/export",
			map[string]string{"Page": fmt.Sprintf("%d", page), "PageSize": "50"}, "", "", "")
		if err != nil {
			return "", err
		}

		var res struct {
			Contacts []struct {
				ID    string
				Cards []map[string]interface{}
			}
		}
		if err := json.Unmarshal(body, &res); err != nil {
			return "", err
		}
		if len(res.Contacts) == 0 {
			break
		}

		for _, contact := range res.Contacts {
			decrypted, err := crypto.DecryptContactCards(contact.Cards, addrKR)
			if err != nil {
				continue
			}
			var name, email string
			for _, card := range decrypted {
				if n := parseICalField(card, "FN"); n != "" {
					name = n
				}
				if e := parseICalField(card, "EMAIL"); e != "" {
					email = e
				}
			}
			searchLower := strings.ToLower(search)
			if strings.Contains(strings.ToLower(name), searchLower) ||
				strings.Contains(strings.ToLower(email), searchLower) {
				matches = append(matches, match{id: contact.ID, name: name, email: email})
			}
		}

		if len(res.Contacts) < 50 {
			break
		}
	}

	if len(matches) == 0 {
		return "", fmt.Errorf("no contact matching %q found", search)
	}
	if len(matches) > 1 {
		fmt.Fprintf(os.Stderr, "Multiple matches for %q:\n", search)
		for _, m := range matches {
			fmt.Fprintf(os.Stderr, "  %s  %s  (%s)\n", m.id, m.name, m.email)
		}
		return "", fmt.Errorf("ambiguous: %d contacts match %q, use the full ID", len(matches), search)
	}

	fmt.Fprintf(os.Stderr, "Found: %s <%s>\n", matches[0].name, matches[0].email)
	return matches[0].id, nil
}
