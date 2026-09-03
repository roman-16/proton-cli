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
    # The paid tests are behind a build tag, so the pass above does not compile
    # them. Without this they are the one part of the tree nothing lints.
    golangci-lint run --build-tags=paid ./...

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

# How many live tests run at once.
#
# One: what limits this is not the client but what the free plan meters, and
# making an alias is the tightest of them. Several tests need one of their own
# and Proton allows few per hour, so anything above this has them refused.
# Raise it one step at a time and only after a full run shows no rate limiting -
# the suite fails on the first sign of it - and never past eight.
parallel := "1"

# The timeouts say how long a run may take before something is wrong, not how long
# it takes, so they are sized to the setting above rather than to an ambition.
# One at a time, the suite is several hundred tests that each wait on Proton in
# turn, which is twenty minutes of honest work - a ten-minute limit timed every
# run out mid-suite and read as a hang. Raising `parallel` is what makes it quick
# again; until then the limit has to clear the serial cost, and one test still
# answers in seconds.
[doc("The live-API suite against the two free test accounts, and everything that needs no account")]
test: test-fast
    go test ./tests/ -v -count=1 -timeout 30m -parallel {{ parallel }} -shuffle=on

[doc("The tests that need a paid plan, against the account in PROTON_CLI_TEST_PAID_*")]
test-paid: test-fast
    #!/usr/bin/env bash
    set -euo pipefail
    # A separate recipe, and a build tag, because this is the only thing here
    # that touches an account somebody depends on. `just test` cannot reach it:
    # without the tag those tests are not compiled in at all.
    #
    # Only the paid tests: the free suite has its own recipe and running it here
    # would spend the sending allowance for nothing. Every test that needs the
    # account says Paid in its name, which TestEveryPaidTestSaysSoInItsName
    # enforces so the filter cannot quietly miss one.
    #
    # One at a time, because there is no second paid account to spread the load
    # over and nothing here is worth racing.
    go test ./tests/ -tags=paid -v -count=1 -timeout 20m -parallel 1 -run Paid

# Both suites, which is two recipes rather than one because they run against
# different accounts under different rules. This is the only place they are
# named together: `test` still cannot reach the paid account, since without the
# build tag those tests are not compiled in at all, and that is the property
# worth keeping. Asking for them here is opting in, out loud.
[doc("Every test there is: the free suite, then the paid one")]
test-all: test
    just test-paid

[doc("Unit, golden, conformance and offline tests: no API, no credentials, seconds not minutes")]
test-fast:
    go test ./cmd/... ./internal/... ./scripts/... ./tests/offline/ -count=1

[doc("Run a single test (or a `|`-separated regex of test names)")]
test-one pattern:
    go test ./tests/ -v -count=1 -run '{{ pattern }}' -timeout 5m

[doc("Report what the live suite spent its time on, and how deep each command's request graph was")]
test-report *pattern=".":
    #!/usr/bin/env bash
    set -euo pipefail
    trace="${PROTON_CLI_TEST_TRACE:-/tmp/proton-cli-trace.jsonl}"
    PROTON_CLI_TEST_TRACE="$trace" PROTON_CLI_TEST_TRACE_REQUESTS=1 \
        go test ./tests/ -v -count=1 -run '{{ pattern }}' -timeout 20m -parallel {{ parallel }} || true
    go run ./scripts/testreport "$trace"

[doc("Record which of Proton's API the live suite reaches, for the check that no change quietly narrows it")]
coverage:
    #!/usr/bin/env bash
    set -euo pipefail
    trace="${PROTON_CLI_TEST_TRACE:-/tmp/proton-cli-trace.jsonl}"
    PROTON_CLI_TEST_TRACE="$trace" PROTON_CLI_TEST_TRACE_REQUESTS=1 \
        go test ./tests/ -v -count=1 -timeout 30m -parallel {{ parallel }}
    # Both suites, because the golden is what every request the CLI can send has
    # to appear in, and some of them only a paid plan can reach. Without a paid
    # account the free half is recorded on its own and the paid endpoints show up
    # as gaps, which is the truth for that machine.
    traces="$trace"
    if [ -n "${PROTON_CLI_TEST_PAID_USER:-}" ]; then
        paid="${trace%.jsonl}-paid.jsonl"
        PROTON_CLI_TEST_TRACE="$paid" PROTON_CLI_TEST_TRACE_REQUESTS=1 \
            go test ./tests/ -tags=paid -v -count=1 -timeout 20m -parallel 1 -run Paid
        traces="$traces $paid"
    else
        echo "no paid account configured; recording the free suite only" >&2
    fi
    # shellcheck disable=SC2086
    cat $traces > "${trace%.jsonl}-all.jsonl"
    go run ./scripts/testreport --coverage "${trace%.jsonl}-all.jsonl" > tests/api-coverage.golden
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
