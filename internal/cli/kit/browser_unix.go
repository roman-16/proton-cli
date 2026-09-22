//go:build !darwin && !windows

package kit

// opener names the freedesktop launcher, which consults $BROWSER before it
// consults the desktop's own association. On a machine with no desktop it is
// either absent or refuses, which is the answer ShowInBrowser wants.
func opener(url string) (string, []string) {
	return "xdg-open", []string{url}
}
