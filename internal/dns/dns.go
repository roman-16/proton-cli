// Package dns is what the owner of a custom domain has to put in its zone: the
// entries, in the shape a zone file writes them, and the verdict Proton reached
// on each of them.
//
// Mail and Pass both take a domain of the account's own and both hand back a
// handful of entries to add and a check per group, so the shape is declared once
// and the two services fill it in.
package dns

import (
	"strconv"
	"strings"

	"github.com/roman-16/proton-cli/internal/errs"
)

// Ok is the word a check reports when Proton can see what it expects.
const Ok = "ok"

// Apex is the host of an entry on the domain itself, as a zone file spells it.
const Apex = "@"

// Record is one entry to put in the domain's zone.
type Record struct {
	Type     string `json:"type"`
	Host     string `json:"host"`
	Value    string `json:"value"`
	Priority int    `json:"priority,omitempty"`
}

// Check is the entries that answer one check, and the verdict it reached.
type Check struct {
	Name    string   `json:"name"`
	Status  string   `json:"status"`
	Records []Record `json:"records"`
	// Found is what Proton read in the entries' place when the check failed,
	// for the services that say.
	Found []string `json:"found"`
}

// Text is the check's verdict with the entries it judges written under it,
// aligned with one another, and whatever Proton found instead last.
//
// A record field keeps a value whole however wide it is, which is what an entry
// needs: a DKIM value is long, exact, and copied by hand.
func (c Check) Text() string {
	rows := make([][]string, 0, len(c.Records))
	var widths []int
	for _, r := range c.Records {
		cells := r.cells()
		for len(widths) < len(cells) {
			widths = append(widths, 0)
		}
		for i, cell := range cells {
			if len(cell) > widths[i] {
				widths[i] = len(cell)
			}
		}
		rows = append(rows, cells)
	}
	var lines []string
	if c.Status != "" {
		lines = append(lines, c.Status)
	}
	for _, cells := range rows {
		var line strings.Builder
		for i, cell := range cells {
			line.WriteString(cell)
			line.WriteString(strings.Repeat(" ", widths[i]-len(cell)+2))
		}
		lines = append(lines, strings.TrimRight(line.String(), " "))
	}
	for _, found := range c.Found {
		lines = append(lines, "found: "+found)
	}
	return strings.Join(lines, "\n")
}

// cells is one entry as a zone file writes it: what to add, where, at what
// priority where a priority is part of it, and the value itself last.
func (r Record) cells() []string {
	if r.Priority > 0 {
		return []string{r.Type, r.Host, strconv.Itoa(r.Priority), r.Value}
	}
	return []string{r.Type, r.Host, r.Value}
}

// Failing names the checks that are not passing, in the order they are shown.
func Failing(checks []Check) []string {
	var out []string
	for _, c := range checks {
		if c.Status != Ok {
			out = append(out, c.Name)
		}
	}
	return out
}

// Name reads a domain as Proton files it, which is a bare name with no scheme,
// no address around it and no path after it. It is judged from the command
// line, so a malformed one costs nothing.
func Name(arg string) (string, error) {
	name := strings.TrimSpace(arg)
	bad := name == "" || !strings.Contains(name, ".") ||
		strings.ContainsAny(name, "@ \t/:") ||
		strings.HasPrefix(name, ".") || strings.HasSuffix(name, ".")
	if bad {
		return "", errs.Problemf("%q is not a domain name.", arg).
			Hint("write it on its own, as example.com")
	}
	return strings.ToLower(name), nil
}
