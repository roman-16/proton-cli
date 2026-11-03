// Command changelog answers the two questions a release asks of CHANGELOG.md:
// which version the file declares, and what that version's release notes say.
//
// It is the whole of the release trigger. A version heading with no published
// release is a release request, so the file is validated in full before either
// answer is given: a malformed changelog fails here, in CI, rather than
// halfway through a publish that cannot be undone.
//
// Nothing is printed when the file asks for no release - a changelog with no
// version section yet, or whose newest version was yanked. That is a normal
// answer, not an error, and it is what makes a routine push a no-op.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/roman-16/proton-cli/internal/changelog"
)

func main() {
	notes := flag.Bool("notes", false, "Print the release notes body instead of the version")
	path := flag.String("path", "CHANGELOG.md", "Path to the changelog")
	flag.Parse()

	source, err := os.ReadFile(*path)
	if err != nil {
		fail(err)
	}
	document, err := changelog.Parse(*path, source)
	if err != nil {
		fail(err)
	}
	target, ok := document.Releasable()
	switch {
	case *notes && !ok:
		fail(fmt.Errorf("%s declares no version to release", *path))
	case *notes:
		fmt.Println(target.Body)
	case ok:
		fmt.Println(target.Version)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "changelog:", err)
	os.Exit(1)
}
