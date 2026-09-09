package live

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/roman-16/proton-cli/tests/account"
)

// Addresses: the identity mail goes out under, and the signature that goes with
// it.

// primaryAddressID returns the account's first address ID.
func primaryAddressID(t *testing.T) string {
	t.Helper()
	raw := runOK(t, "--full-ids", "mail", "settings", "addresses", "list", "--output", "json")
	var env struct {
		Addresses []struct {
			ID    string `json:"id"`
			Email string `json:"email"`
		} `json:"addresses"`
	}
	if err := json.Unmarshal([]byte(raw), &env); err != nil || len(env.Addresses) == 0 {
		t.Fatalf("could not read the address list: %v\n%s", err, truncateOutput(raw))
	}
	for _, a := range env.Addresses {
		if a.Email == selfEmail() {
			return a.ID
		}
	}
	return env.Addresses[0].ID
}

func addressSignature(t *testing.T, addrID string) string {
	t.Helper()
	raw := runOK(t, "mail", "settings", "addresses", "get", "--output", "json", "--", addrID)
	var a struct {
		Signature string `json:"signature"`
	}
	if err := json.Unmarshal([]byte(raw), &a); err != nil {
		t.Fatalf("could not read the address: %v", err)
	}
	return a.Signature
}

func TestMailSettingsAddressesList(t *testing.T) {
	stdout := runOK(t, "mail", "settings", "addresses", "list")
	assertContains(t, stdout, "EMAIL")
	assertContains(t, stdout, selfEmail())
}

func TestMailSettingsAddressesGetByEmail(t *testing.T) {
	stdout := runOK(t, "mail", "settings", "addresses", "get", selfEmail())
	assertField(t, stdout, "Email:", selfEmail())
	assertContains(t, stdout, "Can Send:")
}

func TestMailSettingsAddressesUpdateSignature(t *testing.T) {
	addrID := primaryAddressID(t)
	original := addressSignature(t, addrID)
	restoreSignature(t, addrID, original)

	marker := testID() + "-signature"
	runOK(t, "mail", "settings", "addresses", "update", "--signature", marker, "--", addrID)
	assertContains(t, runOK(t, "mail", "settings", "addresses", "get", "--", addrID), marker)

	// Plain text is stored as HTML, so newlines survive as line breaks.
	runOK(t, "mail", "settings", "addresses", "update", "--signature", "line one\nline two", "--", addrID)
	if got := addressSignature(t, addrID); got != "line one<br>line two" {
		t.Errorf("stored signature = %q, want the newline turned into a line break", got)
	}

	runOK(t, "mail", "settings", "addresses", "update", "--clear-signature", "--", addrID)
	assertContains(t, runOK(t, "mail", "settings", "addresses", "get", "--", addrID), "(none)")
}

func restoreSignature(t *testing.T, addrID, original string) {
	t.Helper()
	cleanup(t, "Restore the address signature", func() error {
		args := []string{"mail", "settings", "addresses", "update", "--html", "--signature", original, "--", addrID}
		if strings.TrimSpace(original) == "" {
			args = []string{"mail", "settings", "addresses", "update", "--clear-signature", "--", addrID}
		}
		if _, stderr, code := run(t, args...); code != 0 {
			return fmt.Errorf("exit %d: %s", code, strings.TrimSpace(stderr))
		}
		return nil
	})
}

// Adding, disabling and deleting an address needs a paid Mail plan, so those act
// as the paid account - on the address kept for the suite and never on one
// somebody uses. Nothing here adds an address: Proton allows one deletion a
// year, so an address made per run would spend an allowance the account cannot
// get back.

// Every address comes back with what a person needs to tell one from another,
// and an address that cannot carry mail says so.
func TestMailSettingsAddressesReportEachKind(t *testing.T) {
	rows := runJSONArray(t, "mail", "settings", "addresses", "list")
	if len(rows) == 0 {
		t.Fatal("the account lists no addresses")
	}
	for _, row := range rows {
		a, _ := row.(map[string]interface{})
		if email, _ := a["email"].(string); !strings.Contains(email, "@") {
			t.Errorf("an address came back without an email address: %v", a["email"])
		}
		kind, ok := a["type"].(float64)
		if !ok || kind < addressOriginal || kind > addressExternal {
			t.Errorf("an address came back as a kind nothing names: %v", a["type"])
		}
		if _, ok := a["has_keys"].(bool); !ok {
			t.Errorf("an address does not say whether it holds a key: %v", a["has_keys"])
		}
	}
}

// The short-domain address is the account's own name on pm.me, and turning it
// on needs a paid Mail plan. The refusal names the address, which is what tells
// it from the one an account without the plan gets for any address at all.
func TestMailSettingsAddressesRefuseTheShortDomainWithoutAPaidPlan(t *testing.T) {
	before := len(runJSONArray(t, "mail", "settings", "addresses", "list"))
	username, _, _ := strings.Cut(selfEmail(), "@")

	_, stderr, code := run(t, "mail", "settings", "addresses", "create", username+"@pm.me")
	if code != 1 {
		t.Fatalf("turning on the short domain exited %d: %s", code, truncateOutput(stderr))
	}
	if !strings.Contains(stderr, "Turning on "+username+"@pm.me needs a paid Mail plan") {
		t.Errorf("the refusal does not name the short-domain address: %s", truncateOutput(stderr))
	}
	if after := len(runJSONArray(t, "mail", "settings", "addresses", "list")); after != before {
		t.Errorf("the account holds %d addresses, it held %d before a refused create", after, before)
	}
}

// A short-domain address that is already on is reported as the address it is,
// and nothing is set up a second time: an account has one for its lifetime.
func TestMailSettingsAddressesRefuseAShortDomainThatIsAlreadyOn(t *testing.T) {
	address := paidAddressOfKind(t, addressPremium)
	if address == "" {
		t.Fatal("the paid account has no short-domain address")
	}
	before := len(runJSONArrayPaid(t, "mail", "settings", "addresses", "list"))

	_, stderr, code := runPaid(t, "mail", "settings", "addresses", "create", address)
	if code != 4 {
		t.Fatalf("turning on a short domain that is on exited %d: %s", code, truncateOutput(stderr))
	}
	if !strings.Contains(stderr, address) {
		t.Errorf("the refusal does not name the address: %s", truncateOutput(stderr))
	}
	if after := len(runJSONArrayPaid(t, "mail", "settings", "addresses", "list")); after != before {
		t.Errorf("the account holds %d addresses, it held %d before a refused create", after, before)
	}
}

// The default address is the first one, so making one the default is a reorder.
// The account is put back in the order the run found it in.
func TestMailSettingsAddressesReorderMakesAnAddressTheDefault(t *testing.T) {
	address := paidForwarder(t)
	before := paidAddressOrder(t)
	if len(before) < 2 {
		t.Fatal("the paid account holds one address, so there is no order to set")
	}
	if before[0] == address {
		t.Fatalf("%s is already the default address, so a run before this one did not"+
			" put the order back", address)
	}

	runOKPaid(t, "mail", "settings", "addresses", "reorder", address)
	cleanup(t, "Restore the address order: proton --profile paid mail settings addresses reorder "+
		strings.Join(before, " "), func() error {
		if slices.Equal(paidAddressOrder(t), before) {
			return nil
		}
		args := append([]string{"mail", "settings", "addresses", "reorder"}, before...)
		if _, stderr, code, err := runAs(account.Paid, nil, args...); err != nil || code != 0 {
			return fmt.Errorf("exit %d: %v %s", code, err, strings.TrimSpace(stderr))
		}
		return nil
	})
	if got := paidAddressOrder(t); got[0] != address {
		t.Errorf("the default address is %s, want %s", got[0], address)
	}

	runOKPaid(t, append([]string{"mail", "settings", "addresses", "reorder"}, before...)...)
	if got := paidAddressOrder(t); !slices.Equal(got, before) {
		t.Errorf("the addresses came back in the order %v, want %v", got, before)
	}
}

// An order the account is already in is refused from the listing, so a reorder
// that would change nothing sends nothing.
func TestMailSettingsAddressesRefuseAReorderThatChangesNothing(t *testing.T) {
	_, stderr, code := run(t, "mail", "settings", "addresses", "reorder", selfEmail())
	if code != 1 || !strings.Contains(stderr, "already the default address") {
		t.Errorf("reordering the default address to the front exited %d: %s", code, truncateOutput(stderr))
	}
}

// Turning an address off and on again is the whole of what a run may do to one,
// and the fixture address is the only one it may do it to.
func TestMailSettingsAddressesTurnTheFixtureAddressOffAndOn(t *testing.T) {
	address := paidForwarder(t)

	runOKPaid(t, "mail", "settings", "addresses", "disable", address)
	// Only if it is still off: the test turns it back on itself, and enabling an
	// address that is already enabled is refused rather than ignored.
	cleanup(t, "Enable address: proton --profile paid mail settings addresses enable "+address,
		func() error {
			if addressStatusOf(t, address) == "active" {
				return nil
			}
			_, stderr, code, err := runAs(account.Paid, nil,
				"mail", "settings", "addresses", "enable", address)
			if err != nil || code != 0 {
				return fmt.Errorf("exit %d: %v %s", code, err, strings.TrimSpace(stderr))
			}
			return nil
		})
	assertAddressStatus(t, address, "disabled")

	// Proton sends from the first address, so an address that is off cannot be
	// the one it sends from. The row says so, and nothing is written.
	_, stderr, code := runPaid(t, "mail", "settings", "addresses", "reorder", address)
	if code != 1 || !strings.Contains(stderr, "cannot be the default") {
		t.Errorf("making a disabled address the default exited %d: %s", code, truncateOutput(stderr))
	}

	runOKPaid(t, "mail", "settings", "addresses", "enable", address)
	assertAddressStatus(t, address, "active")
}

// The two addresses Proton keeps are refused from the row, so neither reaches
// Proton: the account's first address and its short-domain one stay enabled
// whatever anybody types.
//
// It acts as the primary account rather than the paid one because the refusal is
// the CLI's, not Proton's - nothing is sent either way - and deleting an address
// is a command the paid account refuses outright, which would answer before the
// judgement under test could.
func TestMailSettingsAddressesRefuseTheOnesProtonKeeps(t *testing.T) {
	kept := map[string]float64{"first Proton": addressOriginal, "short-domain": addressPremium}
	for name, kind := range kept {
		address := ownAddressOfKind(t, kind)
		if address == "" && kind == addressOriginal {
			t.Fatal("the account lists no first Proton address")
		}
		if address == "" {
			continue
		}
		_, stderr, code := run(t, "mail", "settings", "addresses", "disable", address)
		if code != 1 || !strings.Contains(stderr, "cannot be disabled") {
			t.Errorf("disabling the %s address exited %d: %s", name, code, truncateOutput(stderr))
		}
		_, stderr, code = run(t, "mail", "settings", "addresses", "delete", address)
		if code != 1 || !strings.Contains(stderr, "cannot be deleted") {
			t.Errorf("deleting the %s address exited %d: %s", name, code, truncateOutput(stderr))
		}
	}
}

// An address on a domain the account cannot use is refused, and the refusal
// names what it could have been. Nothing is created, so this is the one test
// that sends the create every run.
func TestMailSettingsAddressesRefuseADomainTheAccountCannotUse(t *testing.T) {
	before := len(runJSONArrayPaid(t, "mail", "settings", "addresses", "list"))

	_, stderr, code := runPaid(t, "mail", "settings", "addresses", "create",
		testID()+"@nowhere.invalid")
	if code != 1 {
		t.Fatalf("an address on an impossible domain exited %d: %s", code, truncateOutput(stderr))
	}
	if !strings.Contains(stderr, "cannot add an address on") {
		t.Errorf("the refusal does not name the domain as the problem: %s", truncateOutput(stderr))
	}
	if after := len(runJSONArrayPaid(t, "mail", "settings", "addresses", "list")); after != before {
		t.Errorf("the account holds %d addresses, it held %d before a refused create", after, before)
	}
}

// An address that already has a key is not made twice, and saying so is the
// answer rather than a second address.
func TestMailSettingsAddressesRefuseOneThatIsAlreadyThere(t *testing.T) {
	address := paidForwarder(t)

	_, stderr, code := runPaid(t, "mail", "settings", "addresses", "create", address)
	if code != 4 {
		t.Fatalf("creating an address that exists exited %d: %s", code, truncateOutput(stderr))
	}
	if !strings.Contains(stderr, address) {
		t.Errorf("the refusal does not name the address: %s", truncateOutput(stderr))
	}
}

// An email address is read before anything is sent, so a malformed one costs no
// request. tests/offline holds the same judgement without an account; this is
// the one that proves the command judges it too.
func TestMailSettingsAddressesRefuseSomethingThatIsNotAnAddress(t *testing.T) {
	_, stderr, code := runPaid(t, "mail", "settings", "addresses", "create", "work")
	if code != 1 || !strings.Contains(stderr, "not an email address") {
		t.Errorf("creating \"work\" exited %d: %s", code, truncateOutput(stderr))
	}
}

// The kinds of address Proton distinguishes, as a machine format reports them.
// The text output names them; a listing carries Proton's own numbers.
const (
	addressOriginal     = 1
	addressCustomDomain = 3
	addressPremium      = 4
	addressExternal     = 5
)

// ownAddressOfKind is the primary account's address of one kind, or "" when it
// has none: an account without a short domain has no premium address to refuse.
func ownAddressOfKind(t *testing.T, kind float64) string {
	t.Helper()
	for _, row := range runJSONArray(t, "mail", "settings", "addresses", "list") {
		a, _ := row.(map[string]interface{})
		if got, _ := a["type"].(float64); got == kind {
			email, _ := a["email"].(string)
			return email
		}
	}
	return ""
}

// paidAddressOfKind is the same on the paid account.
func paidAddressOfKind(t *testing.T, kind float64) string {
	t.Helper()
	for _, row := range runJSONArrayPaid(t, "mail", "settings", "addresses", "list") {
		a, _ := row.(map[string]interface{})
		if got, _ := a["type"].(float64); got == kind {
			email, _ := a["email"].(string)
			return email
		}
	}
	return ""
}

// paidAddressOrder is the paid account's addresses as it keeps them, the
// default first.
func paidAddressOrder(t *testing.T) []string {
	t.Helper()
	var order []string
	for _, row := range runJSONArrayPaid(t, "mail", "settings", "addresses", "list") {
		a, _ := row.(map[string]interface{})
		email, _ := a["email"].(string)
		order = append(order, email)
	}
	return order
}

func assertAddressStatus(t *testing.T, address, want string) {
	t.Helper()
	if got := addressStatusOf(t, address); got != want {
		t.Errorf("%s is %s, want %s", address, got, want)
	}
}

func addressStatusOf(t *testing.T, address string) string {
	t.Helper()
	row := runJSONPaid(t, "mail", "settings", "addresses", "get", address)
	status, _ := row["status"].(float64)
	return statusName(status)
}

// statusName reads Proton's own number for whether an address is on, which is
// what a record carries in a machine format.
func statusName(status float64) string {
	if status == 1 {
		return "active"
	}
	return "disabled"
}
