package selfmanage

import (
	"os"
	"path/filepath"
	"strings"
)

// Kind classifies how a proton binary was installed, which decides whether
// it may replace itself in place.
type Kind int

const (
	// KindStandalone is a curl-script install or a manually downloaded binary:
	// eligible for in-place self-update.
	KindStandalone Kind = iota
	// KindNix is an install from the immutable Nix store.
	KindNix
	// KindHomebrew is a Homebrew install.
	KindHomebrew
	// KindNpm is an npm install.
	KindNpm
	// KindWinget is a winget install.
	KindWinget
)

// String names the install channel, for the diagnostic log and the report: how
// a binary arrived decides what fixing it looks like, so a reader has to be
// told which of these they are looking at.
func (k Kind) String() string {
	switch k {
	case KindNix:
		return "nix"
	case KindHomebrew:
		return "homebrew"
	case KindNpm:
		return "npm"
	case KindWinget:
		return "winget"
	}
	return "standalone"
}

// Source is how the running binary arrived, asking the operating system where
// it is. A machine that will not say answers "unknown", which is itself worth
// knowing.
func Source() string {
	exe, err := os.Executable()
	if err != nil {
		return "unknown"
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return Classify(exe).String()
}

// Classify reports how proton was installed, given the resolved path of the
// running executable (symlinks already evaluated).
//
// It positively identifies only the package managers that install into a
// user-writable location (Homebrew, npm, winget) plus the immutable Nix store,
// because those are the ones an in-place swap could silently corrupt. System
// package managers (pacman/AUR, apt, dnf, apk) install into root-owned paths,
// so a self-update simply fails to write there and is refused by the caller's
// permission check - no path heuristic needed.
func Classify(exePath string) Kind {
	// Normalise Windows separators so the markers match regardless of the OS
	// the path came from (os.Executable yields backslashes on Windows).
	p := strings.ReplaceAll(exePath, `\`, "/")
	switch {
	case strings.Contains(p, "/nix/store/"):
		return KindNix
	case strings.Contains(p, "/Cellar/"), strings.Contains(p, "/Caskroom/"), strings.Contains(p, "/.linuxbrew/"):
		return KindHomebrew
	case strings.Contains(p, "/node_modules/"):
		return KindNpm
	case strings.Contains(strings.ToLower(p), "/winget/"):
		return KindWinget
	default:
		return KindStandalone
	}
}
