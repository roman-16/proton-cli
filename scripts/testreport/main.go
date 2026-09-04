// Command testreport turns a suite trace into the numbers worth arguing about.
//
//	PROTON_CLI_TEST_TRACE=/tmp/trace.jsonl just test
//	go run ./scripts/testreport /tmp/trace.jsonl
//
// The suite's wall clock is spent almost entirely inside invocations of the
// binary, so what it costs is: how many invocations, and what each one waits for.
// An invocation waits for its requests, and if it makes them one after another it
// waits for all of them rather than for the slowest. That is what "overlap"
// reports: 1.0 is a chain, and a command with eight requests and an overlap of 1.0
// is paying eight round trips to learn what one could have told it.
//
// With PROTON_CLI_TEST_TRACE_REQUESTS set, --coverage instead prints every
// request the run made and Proton answered, as a method and a path template.
// That set is the suite's real reach into the live API: anything the CLI can send
// but the run never sent is somewhere Proton can change and no test will notice.
// A refused request does not count - an endpoint listed on the strength of a 401
// is a gap wearing the clothes of coverage.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"regexp"
	"slices"
	"sort"
	"strings"
)

type invocation struct {
	Profile  string       `json:"profile"`
	Args     []string     `json:"args"`
	Ms       int64        `json:"ms"`
	Exit     int          `json:"exit"`
	Requests []apiRequest `json:"requests"`
}

type apiRequest struct {
	Method           string `json:"method"`
	Path             string `json:"path"`
	Status           int    `json:"status"`
	Ms               int64  `json:"ms"`
	FinishedAtMicros int64  `json:"finished_at_micros"`
}

func main() {
	coverage := flag.Bool("coverage", false, "print the API surface the run reached instead of the timings")
	flag.Parse()
	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: testreport [--coverage] TRACE")
		os.Exit(1)
	}
	runs, err := read(flag.Arg(0))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if len(runs) == 0 {
		fmt.Fprintln(os.Stderr, "the trace is empty; set PROTON_CLI_TEST_TRACE before running the suite")
		os.Exit(1)
	}
	if *coverage {
		printCoverage(runs)
		return
	}
	printTimings(runs)
}

func read(path string) ([]invocation, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	var out []invocation
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for scanner.Scan() {
		var in invocation
		if err := json.Unmarshal(scanner.Bytes(), &in); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		out = append(out, in)
	}
	return out, scanner.Err()
}

// ── timings ──

type tally struct {
	name     string
	count    int
	ms       int64
	requests int
	overlap  float64
}

func printTimings(runs []invocation) {
	var total, requests int64
	for _, r := range runs {
		total += r.Ms
		requests += int64(len(r.Requests))
	}
	durations := make([]int64, 0, len(runs))
	for _, r := range runs {
		durations = append(durations, r.Ms)
	}
	slices.Sort(durations)

	fmt.Printf("invocations          %d\n", len(runs))
	fmt.Printf("inside the binary    %s\n", secs(total))
	fmt.Printf("mean / median        %.2fs / %.2fs\n",
		float64(total)/float64(len(runs))/1000, float64(durations[len(durations)/2])/1000)
	if requests > 0 {
		fmt.Printf("requests             %d (%.1f per invocation)\n", requests, float64(requests)/float64(len(runs)))
	}
	fmt.Printf("sends                %d\n", countSends(runs))
	if failed := countFailed(runs); failed > 0 {
		fmt.Printf("non-zero exits       %d\n", failed)
	}
	if limited := countRateLimited(runs); limited > 0 {
		fmt.Printf("RATE LIMITED         %d requests answered 429 - lower the concurrency\n", limited)
	}

	section("by app")
	printTallies(byKey(runs, func(r invocation) string { return field(r, 1) }), total, 20)

	commands := byKey(runs, func(r invocation) string { return field(r, 3) })
	section("by command")
	printTallies(commands, total, 20)

	if requests > 0 {
		section("chains worth flattening")
		fmt.Println("commands that ask for several things one after another, so they wait for all of them")
		var chains []tally
		for _, t := range commands {
			if t.requests/t.count >= 4 && t.overlap/float64(t.count) < 1.5 {
				chains = append(chains, t)
			}
		}
		if len(chains) == 0 {
			fmt.Println("none")
			return
		}
		printTallies(chains, total, 15)
	}
}

func printTallies(rows []tally, total int64, limit int) {
	fmt.Printf("%9s %6s %7s %6s %8s %8s  %s\n", "total", "share", "n", "mean", "requests", "overlap", "name")
	for i, t := range rows {
		if i == limit {
			break
		}
		reqs, overlap := "", ""
		if t.requests > 0 {
			reqs = fmt.Sprintf("%.1f", float64(t.requests)/float64(t.count))
			overlap = fmt.Sprintf("%.2fx", t.overlap/float64(t.count))
		}
		fmt.Printf("%8s %5.1f%% %7d %5.2fs %8s %8s  %s\n",
			secs(t.ms), 100*float64(t.ms)/float64(total), t.count,
			float64(t.ms)/float64(t.count)/1000, reqs, overlap, t.name)
	}
}

func byKey(runs []invocation, key func(invocation) string) []tally {
	index := map[string]*tally{}
	for _, r := range runs {
		k := key(r)
		t, ok := index[k]
		if !ok {
			t = &tally{name: k}
			index[k] = t
		}
		t.count++
		t.ms += r.Ms
		t.requests += len(r.Requests)
		t.overlap += overlapOf(r.Requests)
	}
	out := make([]tally, 0, len(index))
	for _, t := range index {
		out = append(out, *t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ms > out[j].ms })
	return out
}

// overlapOf is how many requests were in flight on average while any were: the
// time spent on requests divided by the time they spanned. A chain is 1.0.
func overlapOf(requests []apiRequest) float64 {
	if len(requests) == 0 {
		return 0
	}
	var spent, first, last int64
	first = -1
	for _, q := range requests {
		spent += q.Ms
		start := q.FinishedAtMicros - q.Ms*1000
		if first < 0 || start < first {
			first = start
		}
		if q.FinishedAtMicros > last {
			last = q.FinishedAtMicros
		}
	}
	span := float64(last-first) / 1000
	if span <= 0 {
		return float64(len(requests))
	}
	return float64(spent) / span
}

// field names an invocation by its first n words that name the command rather
// than what it acts on: flags, the values they take, paths and identifiers all
// belong to one call and would make every call its own row.
func field(r invocation, n int) string {
	var words []string
	for _, a := range r.Args {
		switch {
		case strings.HasPrefix(a, "-"), strings.HasPrefix(a, "/"),
			a == "json", a == "yaml", a == "text", opaque.MatchString(a):
			continue
		}
		words = append(words, a)
		if len(words) == n {
			break
		}
	}
	if len(words) == 0 {
		return "(none)"
	}
	return strings.Join(words, " ")
}

func countSends(runs []invocation) int {
	n := 0
	for _, r := range runs {
		if slices.Contains(r.Args, "send") && !slices.Contains(r.Args, "--dry-run") {
			n++
		}
	}
	return n
}

func countFailed(runs []invocation) int {
	n := 0
	for _, r := range runs {
		if r.Exit != 0 {
			n++
		}
	}
	return n
}

func countRateLimited(runs []invocation) int {
	n := 0
	for _, r := range runs {
		for _, q := range r.Requests {
			if q.Status == 429 {
				n++
			}
		}
	}
	return n
}

func secs(ms int64) string { return fmt.Sprintf("%.0fs", float64(ms)/1000) }

func section(title string) { fmt.Printf("\n── %s ──\n", title) }

// ── coverage ──

// opaque matches a path segment that is a Proton identifier rather than a name:
// base64url of some length, or a number. Templating them is what makes two runs
// comparable.
var opaque = regexp.MustCompile(`^[A-Za-z0-9_=-]{20,}$`)

// words are path segments that are as long as an ID but are part of the endpoint.
// Guessing from the characters cannot work: an invitation ID is twenty-two of
// them and about one in a hundred is all letters, so the words are named instead.
var words = map[string]bool{"checkAvailableHashes": true}

var numeric = regexp.MustCompile(`^[0-9]+$`)

func printCoverage(runs []invocation) {
	seen := map[string]bool{}
	for _, r := range runs {
		// What the raw `api` command sent is what a test asked for by hand. Its
		// whole purpose is to reach something no command models, so it proves
		// nothing about the surface the CLI itself covers.
		if field(r, 1) == "api" {
			continue
		}
		for _, q := range r.Requests {
			// A request Proton refused is not one the suite reaches. Recording it
			// would let an endpoint sit in the golden on the strength of an error,
			// which is the opposite of what the golden is for.
			if q.Status < 200 || q.Status > 299 {
				continue
			}
			seen[q.Method+" "+template(q.Path)] = true
		}
	}
	if len(seen) == 0 {
		fmt.Fprintln(os.Stderr, "the trace holds no requests; set PROTON_CLI_TEST_TRACE_REQUESTS too")
		os.Exit(1)
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	for _, k := range out {
		fmt.Println(k)
	}
}

func template(path string) string {
	segments := strings.Split(path, "/")
	for i, s := range segments {
		switch {
		case opaque.MatchString(s) && !words[s]:
			segments[i] = "{id}"
		case numeric.MatchString(s):
			segments[i] = "{n}"
		}
	}
	return strings.Join(segments, "/")
}
