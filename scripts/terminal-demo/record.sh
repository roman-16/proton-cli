#!/usr/bin/env bash
# Records the README demo by running proton for real and writing the
# session, ANSI and all, to stdout. `just demo` pipes that into freeze.
#
#   just demo
#
# `just demo` seeds the account first, so the session has something to show. The
# only thing this script changes is the file it uploads, which it deletes
# again afterwards - the cleanup panel is a dry run, so it removes nothing.
set -euo pipefail

cd "$(dirname "$0")/../.."

# The recording runs as `primary`, the account the integration suite uses, which
# `just demo` has already signed in and staged. The profile is exported rather
# than passed as a flag, so the recorded commands stay free of demo plumbing.
export PROTON_PROFILE=primary
bin=${PROTON_CLI:-./proton}

# A fixed window keeps the rendered image identical across machines.
stty columns 84 rows 40 2>/dev/null || true

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
printf 'The north trail is open again.\n' >"$work/trail-map.txt"

# Signing in unlocked the key hierarchy, so the transcript opens on a command
# rather than on an authentication notice and no panel pauses halfway through.

# Only the prompt marker is colored, and it names its colour the way the CLI
# does, so the panel's theme decides its shade along with everything else.
prompt() { printf '\033[35m$\033[39m %s\n' "$*"; }

prompt "proton mail messages list --unread --limit 3"
"$bin" mail messages list --unread --limit 3 || true
printf '\n'

prompt "proton drive items upload trail-map.txt /Documents"
"$bin" drive items upload "$work/trail-map.txt" /Documents || true
printf '\n'

prompt "proton calendar events get Dentist"
"$bin" calendar events get Dentist || true
printf '\n'

prompt "proton pass items list --vault Personal"
"$bin" pass items list --vault Personal || true

# --yes because a permanent removal asks first, and there is nobody here to
# answer: this runs inside the pty the recording is captured from, with its
# output discarded, so the question would never be seen.
"$bin" --yes drive items delete /Documents/trail-map.txt >/dev/null 2>&1 || true
