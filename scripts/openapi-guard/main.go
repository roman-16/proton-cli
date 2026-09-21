// Command openapi-guard decides whether a regenerated openapi.yaml may replace
// the one in the tree.
//
// The weekly sync commits to main without anyone reading the diff, so this
// stands in for the reader. It refuses two things. A refactor upstream that the
// generator no longer follows leaves a spec that is still valid YAML and has
// lost most of what it documented, which is what the shrink limit catches. The
// narrower failure is a spec that stops documenting a request this CLI itself
// sends: the reference goes quiet about the very calls its readers make, and
// nothing about the file looks wrong. tests/api-coverage.golden, the recording
// of every request the live suite saw go out, is what names those.
//
// Only what the previous spec documented counts as lost. An endpoint no source
// has ever declared is a gap rather than a regression, and failing on one would
// block every sync until somebody taught the generator another source.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/goccy/go-yaml"
)

// A spec and a recording spell a placeholder differently - {shareID}, {id}, {n}
// - and mean the same route.
var placeholder = regexp.MustCompile(`\{[^}]*\}`)

var methods = map[string]bool{
	"delete": true,
	"get":    true,
	"head":   true,
	"patch":  true,
	"post":   true,
	"put":    true,
}

type spec struct {
	operations map[string]bool
	paths      int
	lines      int
}

type request struct {
	line string
	key  string
}

func main() {
	previous := flag.String("previous", "", "Path to the spec as it stood before the sync")
	current := flag.String("current", "", "Path to the regenerated spec")
	sent := flag.String("sent", "", "Path to the recording of every request the CLI sends")
	flag.Parse()

	if err := run(*previous, *current, *sent); err != nil {
		fmt.Fprintln(os.Stderr, "openapi-guard:", err)
		os.Exit(1)
	}
}

func run(previousPath, currentPath, sentPath string) error {
	if previousPath == "" || currentPath == "" || sentPath == "" {
		return errors.New("--previous, --current and --sent are all required")
	}

	previous, err := readSpec(previousPath)
	if err != nil {
		return err
	}
	current, err := readSpec(currentPath)
	if err != nil {
		return err
	}
	sent, err := readSent(sentPath)
	if err != nil {
		return err
	}

	fmt.Printf("paths: %d -> %d\n", previous.paths, current.paths)
	fmt.Printf("lines: %d -> %d\n", previous.lines, current.lines)

	problems := check(previous, current, sent)
	for _, problem := range problems {
		fmt.Println(problem)
	}
	if len(problems) > 0 {
		return errors.New("the regenerated spec was rejected")
	}
	return nil
}

func check(previous, current spec, sent []request) []string {
	var problems []string

	if shrank(previous.paths, current.paths) || shrank(previous.lines, current.lines) {
		problems = append(problems, fmt.Sprintf(
			"::error::regenerated spec shrank by more than 10%% (%d paths/%d lines -> %d paths/%d lines); refusing to commit",
			previous.paths, previous.lines, current.paths, current.lines))
	}

	var lost []string
	for _, r := range sent {
		if previous.operations[r.key] && !current.operations[r.key] {
			lost = append(lost, r.line)
		}
	}
	if len(lost) > 0 {
		report := []string{fmt.Sprintf(
			"::error::the regenerated spec no longer documents %d requests this CLI sends:", len(lost))}
		for _, line := range lost {
			report = append(report, "  "+line)
		}
		problems = append(problems, strings.Join(report, "\n")+"\nrefusing to commit")
	}

	return problems
}

func shrank(previous, current int) bool {
	return current*10 < previous*9
}

func readSpec(path string) (spec, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return spec{}, err
	}

	var document struct {
		Paths map[string]map[string]any `yaml:"paths"`
	}
	if err := yaml.Unmarshal(raw, &document); err != nil {
		return spec{}, fmt.Errorf("%s: %w", path, err)
	}

	parsed := spec{operations: map[string]bool{}, paths: len(document.Paths)}
	for route, operations := range document.Paths {
		for method := range operations {
			if methods[strings.ToLower(method)] {
				parsed.operations[operation(method, route)] = true
			}
		}
	}
	parsed.lines = strings.Count(string(raw), "\n")
	return parsed, nil
}

func readSent(path string) ([]request, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var requests []request
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		method, route, found := strings.Cut(line, " ")
		if !found {
			continue
		}
		requests = append(requests, request{line: line, key: operation(method, route)})
	}
	return requests, nil
}

func operation(method, route string) string {
	return strings.ToUpper(method) + " " + placeholder.ReplaceAllString(route, "{}")
}
