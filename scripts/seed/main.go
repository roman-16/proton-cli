// Command seed brings the two test accounts to the state the integration suite
// and the README panel both expect.
//
//	go run ./scripts/seed                     # both accounts
//	go run ./scripts/seed --profile primary   # one
//	go run ./scripts/seed --stage             # and make the panel's mail the only unread
//
// It assumes the accounts are signed in; `just login` is what does that.
//
// Every datum is judged against the fixture before it is touched: absent ones
// are made, wrong ones are replaced. Presence alone is not enough - a message
// filed into Archive by a filter, or a label of the wrong colour, would pass a
// presence check and fail an assertion somewhere far away.
//
// The suite guards whole assertions with a skip when a collection is empty - no
// contacts, no vaults, no calendars - which is what this data is for.
package main

import (
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/roman-16/proton-cli/tests/account"
	"github.com/roman-16/proton-cli/tests/fixture"
)

// accounts are the two kept for this suite and nothing else, which are the only
// ones anything here may fill with test data.
//
// The seed does not sign them in. Signing in can need a person - Proton raises a
// CAPTCHA at login, and only a browser answers one - so it is `just login`'s
// job, and this assumes it was done. A session already in place is what every
// command below runs on.
var accounts = account.Free()

func main() {
	var only, stage = flag.String("profile", "", "act on one profile"), flag.Bool("stage", false, "make the panel's mail the only unread")
	flag.Parse()

	if err := requireCredentials(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	work, err := os.MkdirTemp("", "proton-cli-seed-*")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer func() { _ = os.RemoveAll(work) }()
	if err := writeFiles(work); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := writePasswordFiles(work); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	r := &report{}
	var wg sync.WaitGroup
	for _, a := range accounts {
		if *only != "" && *only != a.Profile {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			address := os.Getenv(a.User)
			// The calendar is named before anything else, because the events pinned
			// below are pinned in the calendar that name refers to.
			r.calendar(a.Profile)
			// Then everything the account holds, in lanes: a folder has to exist
			// before the file in it, so collections of the same thing keep their
			// order, while a label and a vault have nothing to do with each other.
			lanes := lanesOf(fixture.Free(work), func(c fixture.Collection) { r.reconcile(a.Profile, c) })
			lanes = append(lanes,
				func() { r.photos(a.Profile, work) },
				func() {
					if *stage && a.Profile == "primary" {
						r.stage(a.Profile, address, work)
						return
					}
					r.mail(a.Profile, address, work)
				},
			)
			runLanes(lanes)
			// Last, because a sweep puts things in the trash it then empties.
			r.empty(a.Profile)
		}()
	}
	wg.Wait()
	if *only == "" {
		r.across(work)
	}

	switch {
	case len(r.failures) > 0:
		fmt.Printf("made %d, replaced %d, swept %d, %d could not be seeded: %s\n",
			r.made, r.remade, r.swept, len(r.failures), strings.Join(r.failures, ", "))
		os.Exit(1)
	case r.made+r.remade+r.swept == 0:
		fmt.Println("already seeded")
	default:
		fmt.Printf("made %d, replaced %d, swept %d\n", r.made, r.remade, r.swept)
	}
}

// lanesOf groups collections by the thing they hold. Two collections of the same
// thing are reconciled in order, because that is where a dependency between them
// can exist - a folder and the file inside it are both "drive".
func lanesOf(cs []fixture.Collection, reconcile func(fixture.Collection)) []func() {
	var order []string
	by := map[string][]fixture.Collection{}
	for _, c := range cs {
		if _, seen := by[c.What]; !seen {
			order = append(order, c.What)
		}
		by[c.What] = append(by[c.What], c)
	}
	lanes := make([]func(), 0, len(order))
	for _, what := range order {
		lanes = append(lanes, func() {
			for _, c := range by[what] {
				reconcile(c)
			}
		})
	}
	return lanes
}

// seedJobs bounds how much of one account is seeded at once, so filling two
// accounts asks no more of Proton than running the suite does.
const seedJobs = 4

func runLanes(lanes []func()) {
	sem := make(chan struct{}, seedJobs)
	var wg sync.WaitGroup
	for _, lane := range lanes {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			lane()
		}()
	}
	wg.Wait()
}

func requireCredentials() error {
	var missing []string
	for _, a := range accounts {
		for _, v := range a.Secrets() {
			if os.Getenv(v) == "" {
				missing = append(missing, v)
			}
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("set %s\n(the primary and secondary accounts - the suite and this both create and delete real data)",
			strings.Join(missing, " "))
	}
	return nil
}

func writeFiles(work string) error {
	for name, body := range fixture.Files() {
		if body == "" {
			// A file with some bulk, so a listing shows a size worth reading.
			body = strings.Repeat("proton-cli\n", 4000)
		}
		if err := os.WriteFile(filepath.Join(work, name), []byte(body), 0o600); err != nil {
			return err
		}
	}
	return writePhotos(work)
}

// writePNG draws one image. Generated rather than checked in, so the repository
// carries no binaries.
func writePNG(path string, shade color.RGBA, w, h int) error {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(img, img.Bounds(), &image.Uniform{C: shade}, image.Point{}, draw.Src)
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := png.Encode(f, img); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// writePhotos draws the photos the library is topped up with, and the image the
// suite's inline-attachment fixture embeds. Each one is visibly different from
// the last.
func writePhotos(work string) error {
	shades := []color.RGBA{{R: 0x1b, G: 0x10, B: 0x33, A: 0xff}, {R: 0x6d, G: 0x4a, B: 0xff, A: 0xff}, {R: 0x35, G: 0xb1, B: 0x91, A: 0xff}}
	if err := writePNG(filepath.Join(work, fixture.Attachments.Inline), shades[1], 8, 8); err != nil {
		return err
	}
	for i := 0; i < photoCount; i++ {
		if err := writePNG(filepath.Join(work, fmt.Sprintf("photo-%d.png", i+1)), shades[i%len(shades)], 240, 160); err != nil {
			return err
		}
	}
	return nil
}

// across seeds what only exists between the two accounts: what the sharing,
// cross-account delivery and invitation tests look for.
func (r *report) across(work string) {
	from, to := os.Getenv(accounts[0].User), os.Getenv(accounts[1].User)
	fmt.Println("between the two")

	found, err := fixture.Rows(seedRunner, "secondary", "mail", "messages", "list", "--subject", "Trip photos", "--folder", "inbox", "--page-size", "1")
	switch {
	case err != nil:
		r.fail("mail: primary -> secondary", err)
	case len(found) == 0:
		r.make("primary", "mail: primary -> secondary",
			[]string{"mail", "messages", "send", "--to", to, "--subject", "Trip photos",
				"--body", "Sending the ones that came out.", "--attach", filepath.Join(work, "packing-list.txt")})
	}

	if _, err := run("primary", "drive", "items", "share", "get", "/Documents"); err != nil {
		r.make("primary", "drive: /Documents shared with the secondary",
			[]string{"drive", "items", "share", "add", "/Documents", to, "--edit", "--message", "Have a look"})
	}

	events, err := fixture.Rows(seedRunner, "secondary", "calendar", "events", "list", "--start", fixture.Today(), "--end", fixture.InDays(30))
	switch {
	case err != nil:
		r.fail("calendar: invitation", err)
	default:
		if _, ok := fixture.Find(events, "title", "Quarterly sync"); !ok {
			r.make("secondary", "calendar: invitation awaiting a response",
				[]string{"calendar", "events", "create", "--title", "Quarterly sync",
					"--start", fixture.InDays(5) + "T14:00", "--duration", "30m", "--attendee", from})
		}
	}
}
