#!/bin/sh
set -eu

# The help this script answers with is the first thing in it, so a reader who
# pipes it into a pager and a reader who asks for --help are shown one text. The
# script is meant to arrive through a pipe, where there is no file to read it
# back out of.
usage() {
	cat <<-'EOF'
		proton-cli installer.

		Downloads the latest proton-cli release for your OS/architecture from GitHub
		Releases, verifies its SHA-256 checksum, and installs it as `proton`, with
		`proton-cli` linked beside it. No Go toolchain and no package manager
		required.

		Usage:
		  curl -fsSL https://raw.githubusercontent.com/roman-16/proton-cli/main/scripts/install.sh | sh

		Options (after `... | sh -s --`):
		  --version <X.Y.Z>   Install a specific release (default: latest).
		  --install-dir <dir> Install into <dir> (default: ~/.local/bin).
		  --help              Show this help.

		Environment overrides:
		  PROTON_CLI_VERSION      Same as --version.
		  PROTON_CLI_INSTALL_DIR  Same as --install-dir.

		Windows: use the PowerShell installer instead:
		  irm https://raw.githubusercontent.com/roman-16/proton-cli/main/scripts/install.ps1 | iex
	EOF
}

REPO="roman-16/proton-cli"
# The project is proton-cli, which is what names the release asset; the program
# it installs is `proton`, and ALIAS is the second name it also answers to.
BIN="proton"
ALIAS="proton-cli"
VERSION="${PROTON_CLI_VERSION:-}"
INSTALL_DIR="${PROTON_CLI_INSTALL_DIR:-$HOME/.local/bin}"

main() {
	parse_args "$@"
	need_cmd uname
	need_cmd install
	need_downloader

	os="$(detect_os)"
	arch="$(detect_arch)"
	asset="${ALIAS}_${os}_${arch}"

	if [ -n "$VERSION" ]; then
		base="https://github.com/$REPO/releases/download/v${VERSION#v}"
	else
		base="https://github.com/$REPO/releases/latest/download"
	fi

	tmp="$(mktemp -d)"
	trap 'rm -rf "$tmp"' EXIT INT HUP TERM

	info "Downloading ${asset}${VERSION:+ (v${VERSION#v})}…"
	download "$base/$asset" "$tmp/$asset" ||
		die "download failed: $base/$asset (no prebuilt binary for $os/$arch in this release?)"
	download "$base/checksums.txt" "$tmp/checksums.txt" ||
		die "could not download checksums.txt from $base"

	verify_checksum "$tmp/$asset" "$tmp/checksums.txt" "$asset"
	success "Checksum verified"

	mkdir -p "$INSTALL_DIR"
	if ! failure="$(install -m 0755 "$tmp/$asset" "$INSTALL_DIR/$BIN" 2>&1)"; then
		die "could not install to $INSTALL_DIR:
      $failure
    Set --install-dir to install somewhere else."
	fi

	# The binary answering is the only proof the install worked: a checksum says
	# the bytes arrived, not that they run on this machine.
	if ! version="$("$INSTALL_DIR/$BIN" --version 2>&1)"; then
		die "$BIN was installed to $(tilde "$INSTALL_DIR/$BIN") but does not run on this machine:
      $version"
	fi

	# `--version` prints "proton version X.Y.Z"; the bare number reads better in a
	# sentence that already names the program.
	installed="$(printf '%s\n' "$version" | awk 'NR == 1 { print $NF }')"
	success "Installed ${BIN}${installed:+ $installed} → $(tilde "$INSTALL_DIR/$BIN")"

	# An install answers to both names, so the second one is part of installing
	# rather than an extra somebody has to know to ask for.
	if failure="$(ln -sf "$BIN" "$INSTALL_DIR/$ALIAS" 2>&1)"; then
		success "${ALIAS} → ${BIN}"
	else
		warn "$ALIAS was not linked beside $BIN, so only $BIN works here:
      $failure"
	fi

	check_path
	next_steps
}

# next_steps closes on what to do rather than on how to undo it. Somebody who has
# just run an install command is looking for the first command, not the last one.
next_steps() {
	printf '\n' >&2
	heading 'Next:'
	step "$BIN account login" 'sign in'
	step "$BIN --help" 'what it can do'
	step "$BIN completion $(current_shell)" 'tab completion'
	printf '\n' >&2
	info "Remove it again any time with: $BIN uninstall"
}

# current_shell names the shell to generate completions for, so the line can be
# pasted rather than adapted.
current_shell() {
	case "$(basename "${SHELL:-sh}")" in
	zsh) echo zsh ;;
	fish) echo fish ;;
	*) echo bash ;;
	esac
}

# tilde shortens a path under the home directory, which is where the default
# install goes and where the full path says nothing extra.
tilde() {
	case "$1" in
	"$HOME"/*) printf '~%s' "${1#"$HOME"}" ;;
	*) printf '%s' "$1" ;;
	esac
}

parse_args() {
	while [ $# -gt 0 ]; do
		case "$1" in
		--version)
			[ $# -ge 2 ] || die "--version requires an argument"
			VERSION="$2"
			shift 2
			;;
		--version=*) VERSION="${1#*=}"; shift ;;
		--install-dir)
			[ $# -ge 2 ] || die "--install-dir requires an argument"
			INSTALL_DIR="$2"
			shift 2
			;;
		--install-dir=*) INSTALL_DIR="${1#*=}"; shift ;;
		-h | --help) usage; exit 0 ;;
		*) die "unknown option: $1 (see --help)" ;;
		esac
	done
}

detect_os() {
	case "$(uname -s)" in
	Linux) echo linux ;;
	Darwin) echo darwin ;;
	MINGW* | MSYS* | CYGWIN* | Windows_NT)
		die "Windows is not supported by this script. Use the PowerShell installer:
  irm https://raw.githubusercontent.com/$REPO/main/scripts/install.ps1 | iex
or install via winget: winget install Roman-16.ProtonCLI" ;;
	*) die "unsupported OS: $(uname -s)" ;;
	esac
}

detect_arch() {
	case "$(uname -m)" in
	x86_64 | amd64) echo amd64 ;;
	aarch64 | arm64) echo arm64 ;;
	*) die "unsupported architecture: $(uname -m)" ;;
	esac
}

verify_checksum() {
	file="$1"; sums="$2"; name="$3"
	expected="$(awk -v f="$name" '$2 == f { print $1; exit }' "$sums")"
	[ -n "$expected" ] || die "no checksum entry for $name in checksums.txt"

	if command -v sha256sum >/dev/null 2>&1; then
		actual="$(sha256sum "$file" | awk '{print $1}')"
	elif command -v shasum >/dev/null 2>&1; then
		actual="$(shasum -a 256 "$file" | awk '{print $1}')"
	elif command -v openssl >/dev/null 2>&1; then
		actual="$(openssl dgst -sha256 "$file" | awk '{print $NF}')"
	else
		die "no SHA-256 tool found (need sha256sum, shasum, or openssl)"
	fi

	[ "$expected" = "$actual" ] ||
		die "checksum mismatch for $name (expected $expected, got $actual)"
}

check_path() {
	case ":$PATH:" in
	*":$INSTALL_DIR:"*) ;;
	*)
		warn "$INSTALL_DIR is not on your PATH. Add it to your shell profile:"
		# shellcheck disable=SC2016 # $PATH is meant to stay literal for pasting.
		printf '\n    export PATH="%s:$PATH"\n\n' "$INSTALL_DIR" >&2
		;;
	esac
}

need_downloader() {
	if command -v curl >/dev/null 2>&1; then
		DL=curl
	elif command -v wget >/dev/null 2>&1; then
		DL=wget
	else
		die "need curl or wget"
	fi
}

download() {
	# download URL OUTFILE
	if [ "$DL" = curl ]; then
		curl -fsSL --proto '=https' --tlsv1.2 --retry 3 --connect-timeout 10 -o "$2" "$1"
	else
		wget -q --https-only --timeout=10 --tries=3 -O "$2" "$1"
	fi
}

need_cmd() { command -v "$1" >/dev/null 2>&1 || die "need '$1'"; }

# Colour follows the same rule the binary itself follows: a courtesy for a person
# at a terminal, and absent from anything captured. NO_COLOR is honoured here too
# (https://no-color.org), so one setting covers the install and everything after.
if [ -t 2 ] && [ -z "${NO_COLOR+x}" ] && [ "${TERM:-dumb}" != dumb ]; then
	C_GREEN="$(printf '\033[32m')"; C_YELLOW="$(printf '\033[33m')"; C_RED="$(printf '\033[31m')"
	C_DIM="$(printf '\033[2m')"; C_OFF="$(printf '\033[0m')"
else
	C_GREEN=""; C_YELLOW=""; C_RED=""; C_DIM=""; C_OFF=""
fi

info() { printf '  %s\n' "$1" >&2; }
heading() { printf '  %s\n' "$1" >&2; }
step() { printf '    %-32s %s%s%s\n' "$1" "$C_DIM" "$2" "$C_OFF" >&2; }
success() { printf '  %s✓%s %s\n' "$C_GREEN" "$C_OFF" "$1" >&2; }
warn() { printf '  %s!%s %s\n' "$C_YELLOW" "$C_OFF" "$1" >&2; }
die() { printf '  %s✗%s %s\n' "$C_RED" "$C_OFF" "$1" >&2; exit 1; }

main "$@"
