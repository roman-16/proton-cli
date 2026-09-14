// Command twofa rewrites the list of domains that offer a time-based code.
//
// The source is 2fa.directory, which publishes the sites it has catalogued as
// supporting TOTP. Proton Pass reads the same list for the same check, so a
// verdict here is the one Pass reaches, and the two adjustments below are theirs
// too: proton.me is left out because an account's own two-factor is not a site
// login, and google.com is put in because the directory files it under a name
// the domain match would miss.
//
// The file it writes is committed. A binary that fetched the list at run time
// would answer differently on two machines on the same day and say nothing about
// it, and would need the network for a check whose whole point is that it does
// not.
package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"
)

const source = "https://api.2fa.directory/v3/totp.json"

// added and dropped are the two adjustments Proton makes to the published list.
var (
	added   = []string{"google.com"}
	dropped = map[string]bool{"proton.me": true}
)

func main() {
	domains, err := fetch()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if len(domains) < 500 {
		fmt.Fprintf(os.Stderr, "only %d domains came back; refusing to shorten the list\n", len(domains))
		os.Exit(1)
	}
	path := destination()
	if err := os.WriteFile(path, []byte(strings.Join(domains, "\n")+"\n"), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("%d domains written to %s\n", len(domains), path)
}

// fetch reads the directory and reduces it to the sorted set of domains.
//
// An entry is a two-element array of the site's name and its details, and only
// the domain is wanted; anything shaped otherwise is upstream's business and is
// passed over rather than failing the run.
func fetch() ([]string, error) {
	client := &http.Client{Timeout: 60 * time.Second}
	req, err := http.NewRequest(http.MethodGet, source, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s answered %s", source, res.Status)
	}

	var entries []json.RawMessage
	if err := json.NewDecoder(res.Body).Decode(&entries); err != nil {
		return nil, err
	}

	seen := map[string]bool{}
	for _, raw := range entries {
		var entry []json.RawMessage
		if err := json.Unmarshal(raw, &entry); err != nil || len(entry) < 2 {
			continue
		}
		var details struct{ Domain string }
		if err := json.Unmarshal(entry[1], &details); err != nil || details.Domain == "" {
			continue
		}
		seen[strings.ToLower(details.Domain)] = true
	}
	for _, d := range added {
		seen[d] = true
	}

	out := make([]string, 0, len(seen))
	for d := range seen {
		if !dropped[d] {
			out = append(out, d)
		}
	}
	slices.Sort(out)
	return out, nil
}

// destination is the embedded list, found from this file rather than from the
// working directory, so the recipe runs from anywhere.
func destination() string {
	_, here, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(here), "..", "..", "internal", "twofa", "domains.txt")
}
