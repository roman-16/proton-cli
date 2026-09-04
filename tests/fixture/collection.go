package fixture

import (
	"path/filepath"
	"time"
)

// What the accounts hold for the suite to read.
//
// Both free accounts hold the same of it, so a test can act as either and the
// README panel is a photograph of it. The names read like somebody's account
// rather than like fixtures for that second reason: nothing here uses
// TestPrefix, which belongs to what the suite makes and clears up itself.
//
// Declaring it here rather than in either place that acts on it is what keeps
// the two in step - the seed and the suite bring about exactly the same thing,
// because both read this.

// Panel is what the README panel shows, in the order it shows it.
func Panel() []PanelMail { return panelMail }

// PanelMail is one of the panel's messages.
type PanelMail struct{ Subject, Body, Attach string }

var panelMail = []PanelMail{
	{"Invoice #2291 is ready", "Your invoice for this month is attached to your account.", ""},
	{"Your monthly security report", "No unusual sign-ins this month.", ""},
	{"Re: hiking weekend", "The north trail is open again - shall we take it?", "packing-list.txt"},
}

// Files the fixture uploads, and their contents. A caller writes them where it
// needs them: the seed into a directory it makes, a test into its own.
func Files() map[string]string { return files }

var files = map[string]string{
	"packing-list.txt": "Two tents, a stove, and the good map.\n",
	"trail-map.txt":    "The north trail is open again.\n",
	"panorama.jpg":     "",
}

// Free is what each of the two accounts kept for the suite holds. Anything
// missing is made by whoever asks for it first.
func Free(work string) []Collection {
	return []Collection{{
		What:   "label",
		List:   []string{"mail", "settings", "labels", "list"},
		Key:    "name",
		IDKeys: []string{"id"},
		Remove: []string{"mail", "settings", "labels", "delete"},
		Pins: []Pin{{
			ID:     "Newsletters",
			Fields: map[string]string{"color": "#8080FF"},
			Create: []string{"mail", "settings", "labels", "create", "--name", "Newsletters", "--color", "#8080FF"},
		}},
	}, {
		What:   "folder",
		List:   []string{"mail", "settings", "folders", "list"},
		Key:    "name",
		IDKeys: []string{"id"},
		Remove: []string{"mail", "settings", "folders", "delete"},
		Pins: []Pin{{
			ID:     "Projects",
			Fields: map[string]string{"color": "#3CBB3A"},
			Create: []string{"mail", "settings", "folders", "create", "--name", "Projects", "--color", "#3CBB3A"},
		}},
	}, {
		What:   "filter",
		List:   []string{"mail", "settings", "filters", "list"},
		Key:    "name",
		IDKeys: []string{"id"},
		Remove: []string{"mail", "settings", "filters", "delete"},
		// Disabled, and matching a word none of the fixture's mail carries. A free
		// account may hold one *active* filter, which the suite needs for itself,
		// and a filter that fired on the fixture's mail would file it out of the
		// inbox, which is where the panel looks.
		Pins: []Pin{{
			ID:     "Archive newsletters",
			Fields: map[string]string{"status": "0"},
			Create: []string{"mail", "settings", "filters", "create", "--name", "Archive newsletters", "--disabled",
				"--sieve", `require ["fileinto"]; if header :contains "Subject" "newsletter" { fileinto "Archive"; }`},
		}},
	}, {
		What:   "contact",
		List:   []string{"contacts", "list"},
		Key:    "name",
		IDKeys: []string{"id"},
		Remove: []string{"contacts", "delete"},
		Pins: []Pin{{
			ID:     "Anna Berger",
			Fields: map[string]string{"email": "anna@example.org"},
			Create: []string{"contacts", "create", "--name", "Anna Berger", "--email", "anna@example.org",
				"--phone", "+43 1 234567", "--organization", "Berger & Co"},
		}},
	}, {
		What:   "vault",
		List:   []string{"pass", "vaults", "list"},
		Key:    "name",
		IDKeys: []string{"share_id"},
		Remove: []string{"pass", "vaults", "delete"},
		Pins: []Pin{{
			ID:     "Personal",
			Create: []string{"pass", "vaults", "create", "--name", "Personal"},
		}},
	}, {
		What:   "pass item",
		List:   []string{"pass", "items", "list", "--vault", "Personal"},
		Key:    "name",
		IDKeys: []string{"item_id"},
		Remove: []string{"pass", "items", "delete"},
		Pins: []Pin{{
			ID:     "GitHub",
			Fields: map[string]string{"type": "login"},
			Create: []string{"pass", "items", "create", "--vault", "Personal", "--name", "GitHub",
				"--username", "roman", "--url", "github.com"},
			Secrets: map[string]string{"password": "correct-horse-battery"},
		}, {
			ID:     "Home Wi-Fi",
			Fields: map[string]string{"type": "wifi"},
			Create: []string{"pass", "items", "create", "--vault", "Personal", "--type", "wifi",
				"--name", "Home Wi-Fi", "--ssid", "Fritzbox", "--security", "WPA2"},
			Secrets: map[string]string{"password": "hunter2hunter2"},
		}, {
			ID:     "Door codes",
			Fields: map[string]string{"type": "note"},
			Create: []string{"pass", "items", "create", "--vault", "Personal", "--type", "note",
				"--name", "Door codes", "--note", "Front door: 1234"},
		}, {
			ID:     "Travel card",
			Fields: map[string]string{"type": "credit-card"},
			Create: []string{"pass", "items", "create", "--vault", "Personal", "--type", "credit-card",
				"--name", "Travel card", "--holder", "Anna Berger", "--expiry", "2030-12"},
			Secrets: map[string]string{"cvv": "123", "number": "4111111111111111"},
		}, {
			// An alias, because making one is what Proton meters hardest here -
			// a handful an hour, against five tests that each want one. Only the
			// test about creating an alias makes its own now; the rest read this.
			// Its address is Proton's to choose, so the name is what finds it.
			ID:     AliasName,
			Fields: map[string]string{"type": "alias"},
			Create: []string{"pass", "aliases", "create", "--vault", "Personal",
				"--name", AliasName, "--prefix", "newsletters"},
		}},
	}, {
		What:   "drive",
		List:   []string{"drive", "items", "list", "/"},
		Key:    "name",
		IDKeys: []string{"link_id"},
		Remove: []string{"drive", "items", "delete"},
		Parent: "/",
		Pins: []Pin{{
			ID:     "Documents",
			Fields: map[string]string{"type": "folder"},
			Create: []string{"drive", "items", "create", "/Documents"},
		}},
	}, {
		What:   "drive",
		List:   []string{"drive", "items", "list", "/Documents"},
		Key:    "name",
		IDKeys: []string{"link_id"},
		Remove: []string{"drive", "items", "delete"},
		Parent: "/Documents",
		Pins: []Pin{{
			ID:     "Trips",
			Fields: map[string]string{"type": "folder"},
			Create: []string{"drive", "items", "create", "/Documents/Trips"},
		}, {
			ID:     "packing-list.txt",
			Fields: map[string]string{"type": "file"},
			Create: []string{"drive", "items", "upload", filepath.Join(work, "packing-list.txt"), "/Documents"},
		}, {
			ID:     "panorama.jpg",
			Fields: map[string]string{"type": "file"},
			Create: []string{"drive", "items", "upload", filepath.Join(work, "panorama.jpg"), "/Documents"},
		}},
	}, {
		What:   "drive",
		List:   []string{"drive", "items", "list", "/Documents/Trips"},
		Key:    "name",
		IDKeys: []string{"link_id"},
		Remove: []string{"drive", "items", "delete"},
		Parent: "/Documents/Trips",
		Pins: []Pin{{
			ID:     "trail-map.txt",
			Fields: map[string]string{"type": "file"},
			Create: []string{"drive", "items", "upload", filepath.Join(work, "trail-map.txt"), "/Documents/Trips"},
		}},
	}, {
		What:   "event",
		List:   []string{"calendar", "events", "list", "--start", Today(), "--end", InDays(30)},
		Key:    "title",
		IDKeys: []string{"calendar_id", "id"},
		Remove: []string{"calendar", "events", "delete"},
		Pins: []Pin{{
			ID: "Dentist",
			Create: []string{"calendar", "events", "create", "--title", "Dentist",
				"--start", InDays(3) + "T10:00", "--duration", "1h",
				"--location", "Vienna", "--description", "Six-month check-up"},
		}, {
			ID: "Standup",
			Create: []string{"calendar", "events", "create", "--title", "Standup",
				"--start", InDays(3) + "T09:00", "--duration", "15m",
				"--rrule", "FREQ=WEEKLY;COUNT=5", "--remind", "15m"},
		}},
	}}
}

// Paid is what the paid account holds for the suite.
//
// One alias, made on the first run that needs it and never removed - which is
// the whole reason it is a fixture rather than something a test makes: an alias
// address cannot be un-minted, so a test that made its own would spend one of
// somebody's for every run. Declaring it here means the suite mints at most one,
// ever.
//
// There is no Remove, so a row that disagrees with this is reported rather than
// replaced. Deleting the account's alias to make a better one is not a trade the
// suite gets to make.
//
// The listing is `aliases list` rather than `items list` because it is unpaged:
// a real account can hold more items than a page, and a fixture that fell off
// the end of one would be minted again on every run.
func Paid() []Collection {
	return []Collection{{
		What:   "Pass alias",
		List:   []string{"pass", "aliases", "list"},
		Key:    "name",
		IDKeys: []string{"share_id", "item_id"},
		Pins: []Pin{{
			ID:     PaidAlias,
			Fields: map[string]string{"type": "alias"},
			Create: []string{"pass", "aliases", "create", "--prefix", PaidAliasPrefix, "--name", PaidAlias},
		}},
	}}
}

// Today and InDays date the fixture's events, and the window a listing of
// them uses. Both sides need the same answer.
func Today() string       { return time.Now().Format("2006-01-02") }
func InDays(n int) string { return time.Now().AddDate(0, 0, n).Format("2006-01-02") }
