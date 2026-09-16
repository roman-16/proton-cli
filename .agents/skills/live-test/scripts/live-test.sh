#!/usr/bin/env bash
set -euo pipefail

script=$(cd "$(dirname "$0")" && pwd)/$(basename "$0")
repo=$(cd "$(dirname "$script")/../../../.." && pwd)
session=proton-cli-live
state=/tmp/proton-cli-live
log=$state/log
plan=$state/plan

die() {
    printf '%s\n' "$*" >&2
    exit 1
}

plan_get() {
    sed --quiet "s/^$1=//p" "$plan"
}

names() {
    grep --no-filename --only-matching --extended-regexp '^func Test[A-Za-z0-9_]+' "$repo"/tests/live/*_test.go \
        | cut --characters=6- \
        | sort
}

matching() {
    names | grep --extended-regexp "${1:-.}" || true
}

descriptions() {
    awk '
        /^\/\// {
            text = substr($0, 4)
            comment = comment == "" ? text : comment " " text
            next
        }
        /^func Test[A-Za-z0-9_]+\(/ {
            name = $2
            sub(/\(.*/, "", name)
            sentence = comment
            sub("^" name " ", "", sentence)
            stop = index(sentence, ". ")
            if (stop > 0) sentence = substr(sentence, 1, stop - 1)
            sub(/\.$/, "", sentence)
            if (length(sentence) > 120) {
                sentence = substr(sentence, 1, 120)
                sub(/ [^ ]*$/, "", sentence)
                sentence = sentence " …"
            }
            print name "\t" sentence
            comment = ""
            next
        }
        { comment = "" }
    ' "$repo"/tests/live/*_test.go
}

multiplexer() {
    if [ -n "${ZELLIJ_SESSION_NAME:-}" ] && command -v zellij > /dev/null; then
        printf 'zellij\n'
    elif [ -n "${TMUX:-}" ] && command -v tmux > /dev/null; then
        printf 'tmux\n'
    elif command -v zellij > /dev/null; then
        printf 'zellij\n'
    elif command -v tmux > /dev/null; then
        printf 'tmux\n'
    else
        die "a run needs zellij or tmux, and this machine has neither"
    fi
}

session_exists() {
    case $1 in
        tmux) tmux -L "$session" has-session -t "$session" 2> /dev/null ;;
        zellij) zellij list-sessions --short 2> /dev/null | grep --quiet --line-regexp "$session" ;;
    esac
}

session_remove() {
    case $1 in
        tmux) tmux -L "$session" kill-server 2> /dev/null || true ;;
        zellij) zellij delete-session --force "$session" > /dev/null 2>&1 || true ;;
    esac
}

session_create() {
    case $1 in
        tmux) tmux -L "$session" new-session -d -s "$session" -c "$repo" bash "$script" pane ;;
        zellij) zellij attach --create-background "$session" -- bash "$script" pane > /dev/null 2>&1 ;;
    esac
}

attach_command() {
    case $1 in
        tmux) printf 'tmux -L %s attach\n' "$session" ;;
        zellij) printf 'zellij attach %s\n' "$session" ;;
    esac
}

over() {
    [ -f "$log" ] || return 1
    tail --lines=1 "$log" | grep --quiet --extended-regexp '^(EXIT [0-9]+|STOPPED)$'
}

in_flight() {
    [ -f "$plan" ] || return 1
    if over; then
        return 1
    fi
    session_exists "$(plan_get mux)"
}

duration() {
    local seconds=$1
    if [ "$seconds" -ge 3600 ]; then
        printf '%dh%02dm' $((seconds / 3600)) $((seconds % 3600 / 60))
    elif [ "$seconds" -ge 60 ]; then
        printf '%dm%02ds' $((seconds / 60)) $((seconds % 60))
    else
        printf '%ds' "$seconds"
    fi
}

counted() {
    if [ "$1" -eq 1 ]; then
        printf '%s %s' "$1" "$2"
    else
        printf '%s %ss' "$1" "$2"
    fi
}

tally() {
    awk '
        /^=== RUN   / {
            if ($3 !~ /\//) {
                started[$3] = NR
                current = $3
            }
            next
        }
        /^--- PASS: / { passed++; current = ""; next }
        /^--- SKIP: / { skipped++; current = ""; next }
        /^--- FAIL: / {
            failed++
            current = ""
            failures[++broken] = $3 " " started[$3] " " NR
            next
        }
        /^EXIT [0-9]+$/ { code = $2 }
        END {
            print "passed=" passed + 0
            print "failed=" failed + 0
            print "skipped=" skipped + 0
            print "running=" current
            print "code=" code
            for (i = 1; i <= broken; i++) print "failure=" failures[i]
        }
    ' "$log"
}

causes() {
    local code=$1 ran=$2 profile recorded
    if grep --quiet 'Proton rate-limited this run' "$log"; then
        printf 'Proton rate-limited this run - wait a few minutes; these failures are not bugs.\n'
    fi
    if grep --quiet 'could not sign' "$log"; then
        profile=$(sed --quiet 's/^could not sign \([a-z]*\) in.*/\1/p' "$log" | head --lines=1)
        printf 'the suite could not sign %s in - run just login, then start the run again.\n' "${profile:-an account}"
    fi
    if grep --quiet 'the live suite requires all of these' "$log"; then
        printf 'the run had no credentials - the nine PROTON_CLI_TEST_* variables have to be set in the shell that starts it.\n'
    fi
    if grep --quiet 'panic: test timed out' "$log"; then
        printf "hit the recipe's timeout after %s of %s tests\n" "$ran" "$(plan_get total)"
    fi
    if grep --quiet 'THE PAID ACCOUNT DID NOT COME BACK' "$log"; then
        printf 'the paid account did not come back as it was - read the box in the log\n'
    fi
    if [ "$ran" -eq 0 ]; then
        printf 'no test ran - the last 15 log lines:\n'
        grep --invert-match --extended-regexp '^(EXIT [0-9]+|STOPPED)$' "$log" | tail --lines=15 | sed 's/^/  /'
    fi
    case "$(plan_get recipe)" in
        coverage | coverage-one)
            recorded=$(sed --quiet 's#^ *tests/api-coverage.golden *| *\([0-9]*\).*#\1#p' "$log" | tail --lines=1)
            if [ "$code" != 0 ]; then
                printf 'api-coverage.golden untouched - the recipe records only a run that passed\n'
            elif [ -z "$recorded" ]; then
                printf 'api-coverage.golden: nothing the recording did not already hold\n'
            elif [ "$(plan_get recipe)" = coverage ]; then
                printf 'api-coverage.golden rewritten: %s changed\n' "$(counted "$recorded" line)"
            else
                printf 'api-coverage.golden: %s added\n' "$(counted "$recorded" line)"
            fi
            ;;
    esac
}

verdict() {
    local report headline line ran elapsed passed failed skipped running code
    report=$(tally)
    eval "$(printf '%s\n' "$report" | grep --invert-match '^failure=')"
    ran=$((passed + failed + skipped))
    elapsed=$(duration $(($(date +%s) - $(plan_get started))))
    if grep --quiet '^STOPPED$' "$log"; then
        headline=stopped
    else
        headline="finished · exit $code"
    fi
    line="$headline · $passed passed · $failed failed"
    if [ "$skipped" -gt 0 ]; then
        line="$line · $skipped skipped"
    fi
    printf '%s · of %s · %s\n' "$line" "$(plan_get total)" "$elapsed"
    printf '%s\n' "$report" | sed --quiet 's/^failure=//p' | while read -r name start stop; do
        printf '  FAIL %s · log lines %s-%s\n' "$name" "$start" "$stop"
    done
    causes "${code:-none}" "$ran"
}

cmd_tests() {
    local pattern=${1:-.} selected
    selected=$(matching "$pattern")
    [ -n "$selected" ] || die "no live test matches $pattern"
    descriptions | awk -F'\t' -v selected="$selected" '
        BEGIN {
            count = split(selected, list, "\n")
            for (i = 1; i <= count; i++) wanted[list[i]] = 1
        }
        $1 in wanted {
            rows[++found] = $0
            if (length($1) > width) width = length($1)
        }
        END {
            for (i = 1; i <= found; i++) {
                split(rows[i], field, "\t")
                printf "%-*s  %s\n", width, field[1], field[2]
            }
            printf "%d test%s match\n", found, found == 1 ? "" : "s"
        }
    '
}

cmd_start() {
    local recipe=${1:-} pattern=${2:-} selected total mux
    case $recipe in
        coverage | test)
            [ -z "$pattern" ] || die "$recipe runs every test and takes no pattern"
            ;;
        coverage-one | test-one)
            [ -n "$pattern" ] || die "$recipe needs a pattern"
            ;;
        *)
            die "recipe is one of: coverage, coverage-one, test, test-one"
            ;;
    esac
    selected=$(matching "${pattern:-.}")
    [ -n "$selected" ] || die "no live test matches $pattern - nothing to run"
    total=$(printf '%s\n' "$selected" | awk 'END { print NR }')
    ! in_flight || die "a run is already going - check it, or stop it"

    mux=$(multiplexer)
    session_remove "$mux"
    rm --recursive --force "$state"
    mkdir --parents "$state"
    : > "$log"
    {
        printf 'mux=%s\n' "$mux"
        printf 'pattern=%s\n' "$pattern"
        printf 'recipe=%s\n' "$recipe"
        printf 'started=%s\n' "$(date +%s)"
        printf 'total=%s\n' "$total"
    } > "$plan"
    session_create "$mux"

    printf 'started just %s (%s) in %s session %s\n' "$recipe" "$(counted "$total" test)" "$mux" "$session"
    printf 'watch: tail --follow %s · or attach: %s\n' "$log" "$(attach_command "$mux")"
}

cmd_pane() {
    local recipe pattern code
    recipe=$(plan_get recipe)
    pattern=$(plan_get pattern)
    cd "$repo"
    set +e
    if [ -n "$pattern" ]; then
        just "$recipe" "$pattern" 2>&1 | tee --append "$log"
    else
        just "$recipe" 2>&1 | tee --append "$log"
    fi
    code=${PIPESTATUS[0]}
    set -e
    printf 'EXIT %s\n' "$code" >> "$log"
}

cmd_check() {
    [ -f "$plan" ] || die "no run started"
    local total passed failed skipped running code ran percent elapsed idle line mux
    total=$(plan_get total)
    if over; then
        mux=$(plan_get mux)
        if session_exists "$mux"; then
            session_remove "$mux"
            verdict
            printf 'log: %s · session %s removed\n' "$log" "$session"
        else
            verdict
            printf 'log: %s · session already removed\n' "$log"
        fi
        return
    fi
    eval "$(tally | grep --invert-match '^failure=')"
    ran=$((passed + failed + skipped))
    percent=$((ran * 100 / total))
    elapsed=$(duration $(($(date +%s) - $(plan_get started))))
    idle=$(duration $(($(date +%s) - $(stat --format=%Y "$log"))))
    if [ "$ran" -eq 0 ] && [ -z "$running" ]; then
        printf '0%% · 0 of %s · preparing (build, sign-in, paid photograph) · %s elapsed\n' "$total" "$elapsed"
        return
    fi
    line="$percent% · $passed passed · $failed failed"
    if [ "$skipped" -gt 0 ]; then
        line="$line · $skipped skipped"
    fi
    line="$line · $ran of $total"
    if [ -n "$running" ]; then
        line="$line · running $running · last output $idle ago"
    else
        line="$line · finishing (paid photograph, coverage merge)"
    fi
    printf '%s · %s elapsed\n' "$line" "$elapsed"
}

cmd_stop() {
    [ -f "$plan" ] || die "no run started"
    in_flight || die "no run in progress"
    session_remove "$(plan_get mux)"
    printf 'STOPPED\n' >> "$log"
    verdict
    printf 'log: %s · session %s removed\n' "$log" "$session"
}

case ${1:-} in
    check) cmd_check ;;
    pane) cmd_pane ;;
    start) cmd_start "${2:-}" "${3:-}" ;;
    stop) cmd_stop ;;
    tests) cmd_tests "${2:-}" ;;
    *) die "usage: live-test.sh tests PATTERN | start RECIPE [PATTERN] | check | stop" ;;
esac
