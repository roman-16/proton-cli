// Command stats writes down the public counters that describe how proton-cli is
// obtained, because most of them are either cumulative or short-lived and a
// trend exists only where something records them.
//
// GitHub reports a release asset's downloads as a lifetime total, so a rate can
// only be had by subtracting one reading from the next, and a first run produces
// no rate at all. Its traffic endpoints are worse: they answer with the last
// fourteen days and discard everything older, so a day not captured inside that
// window is gone for good. Stars and forks are read as the repository's current
// totals and recorded a point a day, so their series grow forward from the first
// run. npm carries its own dated history and needs no recording.
//
// A source that cannot be reached leaves its last reading in place, so every
// source records when it was last read and the page can say which of its panels
// is standing still.
//
// The output is two files on the data branch. state.json is the last reading of
// every cumulative counter and exists only to be subtracted from. stats.json is
// what the documentation site fetches.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"golang.org/x/mod/semver"
)

const (
	owner = "roman-16"
	name  = "proton-cli"

	// The npm package is scoped, and an unscoped `proton-cli` exists on the
	// registry belonging to somebody else. Reading the wrong one reports a
	// stranger's downloads as this project's.
	npmScope = "@roman-16/proton-cli"

	// Repology answers 403 to a generic user agent and asks that automated
	// callers identify themselves with a way to be contacted.
	userAgent = "proton-cli-stats/1 (+https://github.com/roman-16/proton-cli)"
)

// npmPlatforms are the per-platform packages the scoped root pulls in through
// optionalDependencies. Exactly one of them is installed on any given machine,
// so counts that are near-equal across all six are registry mirrors rather than
// people, which is why npm is reported beside the download figures and never
// added into them.
var npmPlatforms = []string{
	"darwin-arm64", "darwin-x64", "linux-arm64", "linux-x64", "win32-arm64", "win32-x64",
}

func main() {
	dir := flag.String("dir", ".", "Directory holding state.json and stats.json")
	flag.Parse()

	if err := run(*dir); err != nil {
		fmt.Fprintln(os.Stderr, "stats:", err)
		os.Exit(1)
	}
}

func run(dir string) error {
	client := &api{token: token(), http: &http.Client{Timeout: 60 * time.Second}}
	now := time.Now().UTC()

	previous, err := read[state](filepath.Join(dir, "state.json"))
	if err != nil {
		return err
	}
	published, err := read[stats](filepath.Join(dir, "stats.json"))
	if err != nil {
		return err
	}

	releases, err := client.releases()
	if err != nil {
		return err
	}
	current := state{CapturedAt: now, Assets: counters(releases)}

	// Every panel starts as what was published last time, so a source that is
	// briefly unreachable leaves the page a stale reading rather than an empty
	// one, and only a fetch that succeeds overwrites anything.
	next := published
	next.UpdatedAt = now
	if next.ReadAt == nil {
		next.ReadAt = map[string]time.Time{}
	}
	if !previous.CapturedAt.IsZero() {
		next.Downloads = record(next.Downloads, spend(previous, current, releases))
	}

	for _, source := range []struct {
		key   string
		what  string
		fetch func() error
	}{
		{"traffic", "traffic", func() error {
			days, window, err := client.traffic(published.Traffic)
			if err != nil {
				return err
			}
			next.Traffic, next.Window = days, window
			return nil
		}},
		{"audience", "referrers and paths", func() error {
			referrers, paths, err := client.audience()
			if err != nil {
				return err
			}
			next.Referrers, next.Paths = referrers, paths
			return nil
		}},
		{"counts", "stars and forks", func() error {
			stars, forks, err := client.counts()
			if err != nil {
				return err
			}
			today := now.Format(time.DateOnly)
			next.Stars = mark(next.Stars, today, stars)
			next.Forks = mark(next.Forks, today, forks)
			return nil
		}},
		{"npm", "npm", func() error {
			days, platforms, err := client.npm(now)
			if err != nil {
				return err
			}
			next.Npm, next.NpmPlatforms = days, platforms
			return nil
		}},
		{"packaging", "packaging", func() error {
			all, err := client.packaging()
			if err != nil {
				return err
			}
			next.Packaging = all
			return nil
		}},
		{"aur", "the AUR", func() error {
			votes, err := client.aur()
			if err != nil {
				return err
			}
			next.Aur = votes
			return nil
		}},
	} {
		if err := source.fetch(); err != nil {
			if refused(err) {
				return err
			}
			fmt.Fprintf(os.Stderr, "stats: %s unavailable, keeping the last reading: %v\n",
				source.what, err)
			continue
		}
		next.ReadAt[source.key] = now
	}
	next.Totals = summarise(releases, next)
	next.lists()

	if err := write(filepath.Join(dir, "state.json"), current); err != nil {
		return err
	}
	return write(filepath.Join(dir, "stats.json"), next)
}

// state is the last reading of every counter that reports a lifetime total.
type state struct {
	CapturedAt time.Time      `json:"captured_at"`
	Assets     map[string]int `json:"assets"`
}

// lists keeps every collection a list in the written JSON.
//
// Go writes a slice that was never appended to as null, and a reader given a
// field its type calls a list is entitled to walk it rather than to test first
// whether it turned out to be nothing at all.
func (s *stats) lists() {
	if s.Downloads == nil {
		s.Downloads = []downloadDay{}
	}
	if s.Traffic == nil {
		s.Traffic = []trafficDay{}
	}
	if s.Stars == nil {
		s.Stars = []countDay{}
	}
	if s.Forks == nil {
		s.Forks = []countDay{}
	}
	if s.Npm == nil {
		s.Npm = []countDay{}
	}
	if s.NpmPlatforms == nil {
		s.NpmPlatforms = map[string]int{}
	}
	if s.Referrers == nil {
		s.Referrers = []source{}
	}
	if s.Paths == nil {
		s.Paths = []source{}
	}
	if s.Packaging == nil {
		s.Packaging = []distribution{}
	}
}

// stats is the whole of what the site fetches.
type stats struct {
	UpdatedAt    time.Time            `json:"updated_at"`
	ReadAt       map[string]time.Time `json:"read_at"`
	Totals       totals               `json:"totals"`
	Downloads    []downloadDay        `json:"downloads"`
	Traffic      []trafficDay         `json:"traffic"`
	Window       fortnight            `json:"traffic_window"`
	Stars        []countDay           `json:"stars"`
	Forks        []countDay           `json:"forks"`
	Npm          []countDay           `json:"npm"`
	NpmPlatforms map[string]int       `json:"npm_platforms"`
	Referrers    []source             `json:"referrers"`
	Paths        []source             `json:"paths"`
	Packaging    []distribution       `json:"packaging"`
	Aur          aur                  `json:"aur"`
}

// downloadDay is one interval's worth of release asset downloads, split by which
// artifact was fetched.
//
// Date is the day the interval began rather than the day it was written down,
// so a run that lands after midnight still describes the day it measured.
//
// Hours is the interval actually measured rather than an assumed twenty-four,
// because a run that does not happen merges two days into one row and a reader
// comparing it against its neighbours would otherwise see a spike that is only
// a missed cron.
type downloadDay struct {
	Date     string         `json:"date"`
	Hours    float64        `json:"hours"`
	Binary   int            `json:"binary"`
	Packaged int            `json:"packaged"`
	Platform map[string]int `json:"platform"`
	Version  map[string]int `json:"version"`
}

type trafficDay struct {
	Date     string `json:"date"`
	Clones   int    `json:"clones"`
	Cloners  int    `json:"cloners"`
	Views    int    `json:"views"`
	Visitors int    `json:"visitors"`
}

// fortnight is GitHub's own total for the window it reports, which is the only
// correct count of the people in it.
//
// Daily figures cannot be added up: somebody who cloned on three days is unique
// on each of them and counts three times in the sum. GitHub deduplicates across
// the whole window, so its number is smaller than the sum of the days and is
// the one worth showing.
type fortnight struct {
	Clones   int `json:"clones"`
	Cloners  int `json:"cloners"`
	Views    int `json:"views"`
	Visitors int `json:"visitors"`
}

type countDay struct {
	Date  string `json:"date"`
	Count int    `json:"count"`
}

type source struct {
	Name    string `json:"name"`
	Count   int    `json:"count"`
	Uniques int    `json:"uniques"`
}

// distribution is one place that carries proton-cli, and the version it has.
type distribution struct {
	Repository string `json:"repository"`
	Version    string `json:"version"`
}

type aur struct {
	Votes      int     `json:"votes"`
	Popularity float64 `json:"popularity"`
}

type totals struct {
	Downloads int    `json:"downloads"`
	Stars     int    `json:"stars"`
	Forks     int    `json:"forks"`
	Latest    string `json:"latest"`
}

// kind is what fetching a given asset says about how proton-cli was obtained.
type kind int

const (
	// ignored is checksums.txt, which accompanies an acquisition rather than
	// being one, and anything else a release carries that nobody installs.
	ignored kind = iota
	// binary is a bare executable. Both install scripts, the self-updater and a
	// browser pointed at the same URL fetch exactly this file, and nothing
	// GitHub publishes tells those apart.
	binary
	// packaged is the versioned artifact: what the AUR, Homebrew and winget
	// build their packages on, and what a hand download of a release takes.
	packaged
)

var packagedExtensions = []string{".apk", ".deb", ".rpm", ".tar.gz", ".zip"}

// classify reports what an asset's name says about it, given the version of the
// release carrying it. Both families are matched for what they are, so an asset
// this does not recognise is counted as nothing rather than as a download.
func classify(asset, version string) kind {
	rest, named := strings.CutPrefix(asset, name+"_")
	if !named {
		return ignored
	}
	rest = strings.TrimPrefix(rest, version+"_")
	for _, extension := range packagedExtensions {
		if strings.HasSuffix(rest, extension) {
			return packaged
		}
	}
	if strings.HasSuffix(rest, ".exe") || !strings.Contains(rest, ".") {
		return binary
	}
	return ignored
}

// platform is the operating system and architecture an asset was built for, as
// the single token `linux_amd64`.
func platform(asset, version string) string {
	trimmed := strings.TrimPrefix(asset, name+"_")
	trimmed = strings.TrimPrefix(trimmed, version+"_")
	trimmed = strings.TrimSuffix(trimmed, ".exe")
	for _, extension := range packagedExtensions {
		trimmed = strings.TrimSuffix(trimmed, extension)
	}
	return trimmed
}

type asset struct {
	Name      string `json:"name"`
	Downloads int    `json:"download_count"`
}

type release struct {
	Tag        string  `json:"tag_name"`
	Draft      bool    `json:"draft"`
	Prerelease bool    `json:"prerelease"`
	Assets     []asset `json:"assets"`
}

// version is the release's tag without the leading v, which is how goreleaser
// writes it into an asset's name.
func (r release) version() string { return strings.TrimPrefix(r.Tag, "v") }

// counters flattens every release's assets into one map keyed by tag and asset,
// which is the shape a later run subtracts from.
func counters(releases []release) map[string]int {
	assets := map[string]int{}
	for _, r := range releases {
		for _, a := range r.Assets {
			assets[r.Tag+"/"+a.Name] = a.Downloads
		}
	}
	return assets
}

// spend turns two readings of the counters into the interval between them.
//
// A counter never decreases on its own, so a fall means an asset was replaced
// or a release deleted and the interval is credited nothing rather than a
// negative. An asset seen for the first time contributes its whole count,
// because it was created inside the interval.
//
// The split is by which artifact was fetched, because that is as far as the
// counters can tell one acquisition from another. checksums.txt accompanies a
// scripted fetch of the executable rather than being one, and is counted as
// nothing, since counting it would count those fetches twice.
func spend(previous, current state, releases []release) downloadDay {
	day := downloadDay{
		Date:     previous.CapturedAt.Format(time.DateOnly),
		Hours:    current.CapturedAt.Sub(previous.CapturedAt).Hours(),
		Platform: map[string]int{},
		Version:  map[string]int{},
	}

	for _, r := range releases {
		for _, a := range r.Assets {
			delta := a.Downloads - previous.Assets[r.Tag+"/"+a.Name]
			if delta <= 0 {
				continue
			}
			switch classify(a.Name, r.version()) {
			case binary:
				day.Binary += delta
			case packaged:
				day.Packaged += delta
			default:
				continue
			}
			day.Platform[platform(a.Name, r.version())] += delta
			day.Version[r.version()] += delta
		}
	}
	return day
}

// record merges an interval into the series, adding to the row already standing
// for that date so that two runs on one day describe one day.
func record(days []downloadDay, day downloadDay) []downloadDay {
	for i, existing := range days {
		if existing.Date != day.Date {
			continue
		}
		day.Hours += existing.Hours
		day.Binary += existing.Binary
		day.Packaged += existing.Packaged
		for k, v := range existing.Platform {
			day.Platform[k] += v
		}
		for k, v := range existing.Version {
			day.Version[k] += v
		}
		days[i] = day
		return days
	}
	return append(days, day)
}

// latest is the newest version anybody can install, which a draft is not and a
// prerelease is not: no install channel offers either.
func latest(releases []release) string {
	newest := ""
	for _, r := range releases {
		if r.Draft || r.Prerelease {
			continue
		}
		if semver.Compare("v"+r.version(), "v"+newest) > 0 {
			newest = r.version()
		}
	}
	return newest
}

func summarise(releases []release, s stats) totals {
	t := totals{Latest: latest(releases)}
	for _, r := range releases {
		for _, a := range r.Assets {
			if classify(a.Name, r.version()) != ignored {
				t.Downloads += a.Downloads
			}
		}
	}
	if len(s.Stars) > 0 {
		t.Stars = s.Stars[len(s.Stars)-1].Count
	}
	if len(s.Forks) > 0 {
		t.Forks = s.Forks[len(s.Forks)-1].Count
	}
	return t
}

// api reaches the public endpoints this reads. The token is needed only by the
// traffic endpoints, which GitHub refuses to an installation token however its
// permissions are declared.
type api struct {
	token string
	http  *http.Client
}

func token() string {
	for _, key := range []string{"STATS_TOKEN", "GH_TOKEN", "GITHUB_TOKEN"} {
		if value := os.Getenv(key); value != "" {
			return value
		}
	}
	return ""
}

// get reads one JSON document.
func (a *api) get(address string, into any) error {
	request, err := http.NewRequest(http.MethodGet, address, nil)
	if err != nil {
		return err
	}
	request.Header.Set("User-Agent", userAgent)
	if strings.HasPrefix(address, "https://api.github.com/") {
		request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
		if a.token != "" {
			request.Header.Set("Authorization", "Bearer "+a.token)
		}
	}

	response, err := a.http.Do(request)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(response.Body, 32<<20))
	if err != nil {
		return err
	}
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: %s: %s", address, response.Status, strings.TrimSpace(string(body)))
	}
	return json.Unmarshal(body, into)
}

// github reads a paginated collection whole. Every collection here is small
// enough that the page count is a handful.
func github[T any](a *api, path string) ([]T, error) {
	var all []T
	for page := 1; page <= 50; page++ {
		address := fmt.Sprintf("https://api.github.com/repos/%s/%s/%s?per_page=100&page=%d",
			owner, name, path, page)
		var batch []T
		if err := a.get(address, &batch); err != nil {
			return nil, err
		}
		all = append(all, batch...)
		if len(batch) < 100 {
			return all, nil
		}
	}
	return all, nil
}

func (a *api) releases() ([]release, error) { return github[release](a, "releases") }

// traffic merges GitHub's fourteen-day window into the series it already has.
//
// Every call answers with the whole window, so a row is replaced rather than
// added to: today's bucket is still being written and corrects itself tomorrow,
// and a gap shorter than a fortnight fills itself in without any intervention.
func (a *api) traffic(existing []trafficDay) ([]trafficDay, fortnight, error) {
	type bucket struct {
		Timestamp string `json:"timestamp"`
		Count     int    `json:"count"`
		Uniques   int    `json:"uniques"`
	}
	var views struct {
		Count   int      `json:"count"`
		Uniques int      `json:"uniques"`
		Views   []bucket `json:"views"`
	}
	var clones struct {
		Count   int      `json:"count"`
		Uniques int      `json:"uniques"`
		Clones  []bucket `json:"clones"`
	}
	base := fmt.Sprintf("https://api.github.com/repos/%s/%s/traffic/", owner, name)
	if err := a.get(base+"views", &views); err != nil {
		return nil, fortnight{}, explain(err)
	}
	if err := a.get(base+"clones", &clones); err != nil {
		return nil, fortnight{}, explain(err)
	}
	window := fortnight{
		Clones: clones.Count, Cloners: clones.Uniques,
		Views: views.Count, Visitors: views.Uniques,
	}

	days := map[string]trafficDay{}
	for _, day := range existing {
		days[day.Date] = day
	}
	for _, v := range views.Views {
		date := v.Timestamp[:10]
		day := days[date]
		day.Date, day.Views, day.Visitors = date, v.Count, v.Uniques
		days[date] = day
	}
	for _, c := range clones.Clones {
		date := c.Timestamp[:10]
		day := days[date]
		day.Date, day.Clones, day.Cloners = date, c.Count, c.Uniques
		days[date] = day
	}
	return sorted(days), window, nil
}

// refused reports a credential the traffic endpoints will not accept, which is
// the one failure that must stop the run rather than be carried over.
//
// Those endpoints require push access, and the token a workflow is handed by
// default does not have it however its permissions block is written. A run that
// shrugged this off would look healthy while GitHub deleted, a fortnight at a
// time, the only record of where anybody came from.
func refused(err error) bool {
	return strings.Contains(err.Error(), "traffic/") &&
		(strings.Contains(err.Error(), "403") || strings.Contains(err.Error(), "401"))
}

// explain turns a refusal into the sentence that fixes it.
func explain(err error) error {
	if !refused(err) {
		return err
	}
	return fmt.Errorf("%w\n\nThe traffic endpoints need a personal access token with "+
		"classic `repo` scope or fine-grained `Administration: read`, in STATS_TOKEN. "+
		"The default GITHUB_TOKEN cannot read them, whatever its permissions say. "+
		"Every day this fails is a day of traffic GitHub deletes and nothing recovers", err)
}

func (a *api) audience() (referrers, paths []source, err error) {
	base := fmt.Sprintf("https://api.github.com/repos/%s/%s/traffic/popular/", owner, name)
	type entry struct {
		Referrer string `json:"referrer"`
		Path     string `json:"path"`
		Count    int    `json:"count"`
		Uniques  int    `json:"uniques"`
	}

	var from []entry
	if err := a.get(base+"referrers", &from); err != nil {
		return nil, nil, explain(err)
	}
	for _, e := range from {
		referrers = append(referrers, source{Name: e.Referrer, Count: e.Count, Uniques: e.Uniques})
	}

	var read []entry
	if err := a.get(base+"paths", &read); err != nil {
		return nil, nil, explain(err)
	}
	for _, e := range read {
		paths = append(paths, source{Name: e.Path, Count: e.Count, Uniques: e.Uniques})
	}
	return referrers, paths, nil
}

// counts is the repository's own totals of stars and forks.
//
// They are read as a current value rather than as a history, because GitHub
// will not list who starred a repository to a token holding less than write
// access to its contents - which is a great deal more than a counter is worth
// granting a job that only reads. So both are recorded the way downloads are,
// a point at a time, and their series grow forward from the first run.
func (a *api) counts() (stars, forks int, err error) {
	var repository struct {
		Stars int `json:"stargazers_count"`
		Forks int `json:"forks_count"`
	}
	address := fmt.Sprintf("https://api.github.com/repos/%s/%s", owner, name)
	if err := a.get(address, &repository); err != nil {
		return 0, 0, err
	}
	return repository.Stars, repository.Forks, nil
}

// mark places a reading on a date, replacing the one already there so that two
// runs in a day leave the day with its latest value rather than two of them.
func mark(series []countDay, date string, count int) []countDay {
	for i, point := range series {
		if point.Date == date {
			series[i].Count = count
			return series
		}
	}
	series = append(series, countDay{Date: date, Count: count})
	sort.Slice(series, func(i, j int) bool { return series[i].Date < series[j].Date })
	return series
}

// npm reads the registry's own daily history, which is retroactive and needs no
// recording, plus the per-platform packages that show why the figure cannot be
// read as installs.
func (a *api) npm(now time.Time) ([]countDay, map[string]int, error) {
	type window struct {
		Downloads []struct {
			Day       string `json:"day"`
			Downloads int    `json:"downloads"`
		} `json:"downloads"`
	}

	var daily []countDay
	// The range endpoint answers at most a year at a time, so a project older
	// than that is read a year per request.
	for year := 2026; year <= now.Year(); year++ {
		from := fmt.Sprintf("%d-01-01", year)
		to := fmt.Sprintf("%d-12-31", year)
		if year == now.Year() {
			to = now.Format(time.DateOnly)
		}
		var got window
		address := fmt.Sprintf("https://api.npmjs.org/downloads/range/%s:%s/%s",
			from, to, url.PathEscape(npmScope))
		if err := a.get(address, &got); err != nil {
			return nil, nil, err
		}
		for _, d := range got.Downloads {
			daily = append(daily, countDay{Date: d.Day, Count: d.Downloads})
		}
	}
	daily = counted(daily)

	platforms := map[string]int{}
	for _, p := range npmPlatforms {
		var point struct {
			Downloads int `json:"downloads"`
		}
		address := "https://api.npmjs.org/downloads/point/last-month/" +
			url.PathEscape(npmScope+"-"+p)
		if err := a.get(address, &point); err != nil {
			return nil, nil, err
		}
		platforms[p] = point.Downloads
	}
	return daily, platforms, nil
}

// counted drops the days the registry has not answered for yet. It reports a
// day as zero until it has counted it, so a trailing zero is a day still being
// tallied rather than a day nobody fetched anything.
func counted(daily []countDay) []countDay {
	for len(daily) > 0 && daily[len(daily)-1].Count == 0 {
		daily = daily[:len(daily)-1]
	}
	return daily
}

// packaging is which distributions carry proton-cli and which version each of
// them has. Whether a version is behind is settled against the newest release
// GitHub has published, which Repology's own idea of newest cannot answer for:
// it is a crawl, and can be older than the release it would be judging.
func (a *api) packaging() ([]distribution, error) {
	var projects []struct {
		Repository string `json:"repo"`
		Version    string `json:"version"`
	}
	if err := a.get("https://repology.org/api/v1/project/"+name, &projects); err != nil {
		return nil, err
	}
	var all []distribution
	for _, p := range projects {
		all = append(all, distribution{Repository: p.Repository, Version: p.Version})
	}
	sort.Slice(all, func(i, j int) bool { return all[i].Repository < all[j].Repository })
	return all, nil
}

func (a *api) aur() (aur, error) {
	var response struct {
		Results []struct {
			Votes      int     `json:"NumVotes"`
			Popularity float64 `json:"Popularity"`
		} `json:"results"`
	}
	address := "https://aur.archlinux.org/rpc/v5/info?arg[]=" + name + "-bin"
	if err := a.get(address, &response); err != nil {
		return aur{}, err
	}
	if len(response.Results) == 0 {
		return aur{}, nil
	}
	return aur{Votes: response.Results[0].Votes, Popularity: response.Results[0].Popularity}, nil
}

func sorted(days map[string]trafficDay) []trafficDay {
	all := make([]trafficDay, 0, len(days))
	for _, day := range days {
		all = append(all, day)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].Date < all[j].Date })
	return all
}

// read returns the zero value for a file that is not there, which is what makes
// a first run behave like any other.
func read[T any](path string) (T, error) {
	var value T
	source, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return value, nil
	}
	if err != nil {
		return value, err
	}
	return value, json.Unmarshal(source, &value)
}

// write emits the file with sorted keys and a trailing newline, so a day that
// changed one counter shows as one changed line and the branch's history reads
// as the record it is.
func write(path string, value any) error {
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, append(encoded, '\n'), 0o644)
}
