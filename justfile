[doc("Build the release-shaped binary")]
build:
    go build -o proton ./cmd/proton

[doc("Remove everything generated: the binary, the completions, the release output")]
clean:
    rm --recursive --force completions dist proton

[doc("Re-record the README panels by running a real session, then render them")]
demo: build
    #!/usr/bin/env bash
    set -euo pipefail
    ansi=/tmp/proton-cli-demo.ansi
    go run ./scripts/seed --profile primary --stage
    script --quiet --command "bash scripts/terminal-demo/record.sh" --return /dev/null \
        | sed --expression 's/\r$//' --expression 's/.*\r//' > "$ansi"
    # The recording carries colour names, since that is all the CLI emits; each
    # panel resolves them against one of Proton's own themes before freeze draws it.
    render() {
        themed=/tmp/proton-cli-demo-$1.ansi
        bash scripts/terminal-demo/theme.sh "$2" < "$ansi" > "$themed"
        freeze "$themed" --config "scripts/terminal-demo/$1.json" --output "assets/demo-$1.svg" < /dev/null
        sed --in-place "s|\(<g font-family=[^>]*\)fill=\"[^\"]*\"|\1fill=\"$3\"|" "assets/demo-$1.svg"
    }
    render dark carbon "#FFFFFF"
    render light snow "#0C0C14"

[doc("Regenerate the command reference from the command tree")]
docs:
    go run ./scripts/gendocs

[doc("Record today's reading of the public counters the Stats page charts")]
stats dir=".":
    go run ./scripts/stats --dir {{ dir }}

[doc("Build the nix package from the working tree, which a flake only sees once tracked")]
flake:
    #!/usr/bin/env bash
    set -euo pipefail
    work=$(just worktree)
    trap 'rm --recursive --force "$work"' EXIT
    cd "$work"
    nix build --print-build-logs

[doc("Update the golden files that pin every response's and every help screen's exact bytes")]
golden:
    go test ./internal/ui/ ./internal/cli/ -update -count=1

[doc("Fix and format everything fixable, then lint with no findings allowed")]
lint:
    gofmt -w .
    nixfmt flake.nix
    just docs
    just web
    actionlint
    goreleaser check
    shellcheck scripts/*.sh scripts/terminal-demo/*.sh
    golangci-lint run ./...
    GOOS=windows go build ./...
    GOOS=darwin go build ./...

[doc("Sign every test account in, answering Proton's CAPTCHA if it asks. Needs a terminal")]
login *args: build
    go run ./scripts/login {{ args }}

[doc("Print the version and the release notes the current CHANGELOG.md would publish")]
notes:
    go run ./scripts/changelog
    go run ./scripts/changelog --notes

[doc("Re-render the social card that a link preview shows, from its SVG source")]
og:
    resvg assets/og.svg assets/og.png --width 1200 --height 630 \
        --use-fonts-dir "$DEVBOX_PACKAGES_DIR/share/fonts" \
        --font-family "JetBrains Mono" --monospace-family "JetBrains Mono"

[doc("Regenerate openapi.yaml from the WebClients TypeScript source")]
openapi:
    cd scripts && bun install --frozen-lockfile && bun run generate-openapi

[doc("Build the documentation site, which type-checks it and validates every link")]
web:
    cd web && bun install --frozen-lockfile && bun run check && bun run build

[doc("Serve the documentation site, rebuilding as the pages change")]
web-dev:
    cd web && bun install --frozen-lockfile && bun run dev

[doc("Regenerate the Pass protobuf bindings")]
proto:
    protoc --proto_path=internal/service/pass/proto/protos \
        --go_out=internal/service/pass/proto --go_opt=paths=source_relative \
        internal/service/pass/proto/protos/item-v1.proto \
        internal/service/pass/proto/protos/vault-v1.proto

[doc("Build and run")]
run *args:
    go run ./cmd/proton {{ args }}

[doc("Fill the test accounts with the data the suite expects (the suite runs this too)")]
seed *args: build
    go run ./scripts/seed {{ args }}

[doc("Build every release artifact without publishing, the way a tag would")]
snapshot: build
    #!/usr/bin/env bash
    set -euo pipefail
    export AUR_KEY="${AUR_KEY:-unused}"
    export TAP_WINGET_TOKEN="${TAP_WINGET_TOKEN:-unused}"
    goreleaser release --snapshot --clean --skip=publish

# There are two tiers, and one thing separates them: does answering the question
# need Proton.
#
# `test-fast` is every package but the live suite - unit, golden, conformance,
# the rules the suite is held to, and the offline suite that runs the real binary
# with no session and the API pointed at a dead port. No credentials, no network,
# a couple of seconds. `test` is that, then the live suite against all three
# accounts.
#
# Nothing else decides a tier. A subscription is a property of an account, not of
# a question, so the tests that need one act as the paid account like any other
# and run in the same pass.
# The one package that needs an account, and the vendored Go inside the
# documentation site's node_modules, which is nobody's to test.
notFast := "/tests/live$|/node_modules/"

# The live suite runs one test at a time. It is bound by waiting for Proton, so
# overlapping would be faster - but what gives out first is whatever the free
# plan meters hardest, and rate limiting arrives before any time is saved. The
# timeout says how long a run may take before something is wrong, not how long it
# takes: several hundred tests that each wait on Proton in turn is a good half
# hour of honest work.
[doc("Every test there is: the fast ones, then the live suite against all three accounts")]
test: test-fast
    go test ./tests/live/ -v -count=1 -timeout 45m -parallel 1 -shuffle=on

[doc("Everything decidable without Proton: unit, golden, conformance, rules, offline")]
test-fast:
    #!/usr/bin/env bash
    set -euo pipefail
    go test $(go list ./... | grep --invert-match --extended-regexp '{{ notFast }}') -count=1

[doc("Run a single test (or a `|`-separated regex of test names)")]
test-one pattern:
    go test ./tests/live/ -v -count=1 -run '{{ pattern }}' -timeout 10m

[doc("Report what the live suite spent its time on, and how deep each command's request graph was")]
test-report *pattern=".":
    #!/usr/bin/env bash
    set -euo pipefail
    trace="${PROTON_CLI_TEST_TRACE:-/tmp/proton-cli-trace.jsonl}"
    PROTON_CLI_TEST_TRACE="$trace" PROTON_CLI_TEST_TRACE_REQUESTS=1 \
        go test ./tests/live/ -v -count=1 -run '{{ pattern }}' -timeout 45m -parallel 1 || true
    go run ./scripts/testreport "$trace"

[doc("Record which of Proton's API the live suite reaches, for the check that no change quietly narrows it")]
coverage:
    #!/usr/bin/env bash
    set -euo pipefail
    trace="${PROTON_CLI_TEST_TRACE:-/tmp/proton-cli-trace.jsonl}"
    PROTON_CLI_TEST_TRACE="$trace" PROTON_CLI_TEST_TRACE_REQUESTS=1 \
        go test ./tests/live/ -v -count=1 -timeout 45m -parallel 1
    go run ./scripts/testreport --coverage "$trace" > tests/api-coverage.golden
    git --no-pager diff --stat tests/api-coverage.golden || true

[doc("Move every dependency and tool to the latest version")]
update:
    go get -u ./...
    go mod tidy
    just vendor-hash
    cd scripts && bun update
    devbox update
    just lint
    just test-fast
    just flake

[doc("Recompute the vendorHash in flake.nix, which every change to go.mod invalidates")]
vendor-hash:
    #!/usr/bin/env bash
    set -euo pipefail
    work=$(just worktree)
    trap 'rm --recursive --force "$work"' EXIT
    sed --in-place 's|vendorHash = "[^"]*"|vendorHash = "sha256-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="|' "$work/flake.nix"
    git -C "$work" add --all
    log=$(cd "$work" && nix build --print-build-logs 2>&1 || true)
    hash=$(printf '%s\n' "$log" | sed --quiet 's/^ *got: *//p' | tail -1)
    if [ -z "$hash" ]; then
        printf '%s\n' "$log" >&2
        exit 1
    fi
    sed --in-place "s|vendorHash = \"[^\"]*\"|vendorHash = \"$hash\"|" flake.nix
    printf 'vendorHash = %s\n' "$hash"

[private]
worktree:
    #!/usr/bin/env bash
    set -euo pipefail
    work=$(mktemp --directory)
    # What is packed is what is on disk, so a file deleted but not yet staged is
    # absent from the copy rather than fatal to it: the point of this is to build
    # the working tree, and the index is only how its paths are enumerated.
    git ls-files --cached --others --exclude-standard -z \
        | while IFS= read -r -d '' path; do [ -e "$path" ] && printf '%s\0' "$path"; done \
        | tar --create --null --files-from=- \
        | tar --extract --directory="$work"
    git -C "$work" init --quiet
    git -C "$work" add --all
    printf '%s\n' "$work"
