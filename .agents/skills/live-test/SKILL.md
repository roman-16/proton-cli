---
name: live-test
description: Runs proton-cli's live suite the one way an agent may - works out which live tests the working tree and the conversation point at, asks with the questionnaire (full test, full coverage, and the matching subset run when the context names tests), then hands the pick to one script call that runs it in a detached zellij or tmux session, prints a progress line a minute, and returns with the verdict the moment it ends. Use when the user asks for a live run, a test run or a coverage run, wants a change confirmed against Proton, or names this skill. Not for `just test-fast`, which any agent runs directly.
---

# Live test

A live run signs three accounts in, acts on real data and spends an allowance Proton meters by the hour. The questionnaire is what makes it the user's decision, so it comes before the network, always, and it names every test the run will touch.

`scripts/live-test.sh` does the mechanics. From the repository root:

```bash
.agents/skills/live-test/scripts/live-test.sh tests PATTERN         # what a pattern matches, and what each test proves
.agents/skills/live-test/scripts/live-test.sh run RECIPE [PATTERN]  # start it, report every minute, return with the verdict
.agents/skills/live-test/scripts/live-test.sh watch                 # rejoin a run that is already going
.agents/skills/live-test/scripts/live-test.sh check                 # one line: where it is, or the verdict
.agents/skills/live-test/scripts/live-test.sh stop                  # end it, keep the log
```

The run inherits the shell that starts it, so the nine `PROTON_CLI_TEST_*` variables and the devbox toolchain have to be in it.

## 1. Work out which tests the change wants

Read `git status --short`, `git --no-pager diff -U0` and what the conversation has been about. Map what moved to test names:

| What changed | Tests |
| --- | --- |
| `tests/live/<app>_<collection>_test.go` | the test functions the diff touches, plus any test calling a helper it touches |
| `internal/cli/<app>/<collection>.go` | `Test<App><Collection>` |
| `internal/service/<app>/…` | `Test<App>` |
| `internal/cli/kit`, the client, the key hierarchy, the harness (`main_test.go`, `run_test.go`, `fixture_test.go`, …) | the whole suite, and nothing smaller |
| a new command | its live test, which is written first |

A test that does not exist yet is a test to write, never a reason to run everything. Confirm the selection before asking:

```bash
.agents/skills/live-test/scripts/live-test.sh tests 'TestContactsMatchingPinStillDelivers|TestPassExportAndImportRoundTrip'
```

It prints one line per match with the first sentence of that test's doc comment, then the count. A match list past about 140 tests is a full run in disguise: offer the full pair, or split it by collection.

## 2. Ask

One `questionnaire` question. **Full test** and **Full coverage** are always there. **Test run** and **Coverage run** come first and only when the context named tests:

| Label | Description |
| --- | --- |
| Test run | `just test-one 'PATTERN'` - the N above, records nothing |
| Coverage run | `just coverage-one 'PATTERN'` - the N above, and adds what they reached to `tests/api-coverage.golden` |
| Full test | `just test` - all N, about half an hour, records nothing |
| Full coverage | `just coverage` - all N, and rewrites `tests/api-coverage.golden` from what they reached |

The prompt carries the list: one line per test, the name and what it proves about this change, then the pattern itself on its own line. With no subset to offer, the prompt says why in one line - nothing in the working tree points at a live test, or the change sits under every command and only a full run answers it.

Leave `allowOther` on. Free text is a correction to the selection: apply it and ask again. Nothing starts without a picked option.

## 3. Run it

One call, and it lasts as long as the run does. Make it bare, with the bash tool's timeout set to one hour (3600 seconds):

```bash
.agents/skills/live-test/scripts/live-test.sh run coverage-one 'TestContactsMatchingPinStillDelivers|TestPassExportAndImportRoundTrip'
```

Bare means no pipe and no redirect - not `| tail`, not `| grep`, not `> file`. The progress lines are for the user to watch, and a pipe holds every one of them back until the run ends, which for the full suite is forty minutes of nothing on screen. One hour is what the full suite needs with room to spare; a shorter timeout aborts the call partway, and a longer one holds nothing.

```
started just coverage-one (2 tests) in zellij session proton-cli-live
watch: tail --follow /tmp/proton-cli-live/log · or attach: zellij attach proton-cli-live
50% · 1 passed · 0 failed · 1 of 2 · running TestPassExportAndImportRoundTrip · last output 4s ago · 1m00s elapsed
finished · exit 0 · 2 passed · 0 failed · of 2 · 1m48s
log: /tmp/proton-cli-live/log · session proton-cli-live removed
```

A line a minute, the verdict the moment the run ends, and nothing owed from you in between. A run that finishes inside the first minute prints no progress line at all.

The run lives in its own session, so an interrupted call costs nothing: `watch` rejoins it, prints where it is and follows it the same way. `check` answers in one line without waiting, and `stop` ends the run and keeps the log.

**Stuck:** after five silent minutes the script says so beside the progress line and names the test. The user can end it with `stop` from their own terminal, or attach with the command the first line printed.

## 4. Report, then stop

The call's last lines are the verdict; the session is already gone and the log stays at `/tmp/proton-cli-live/log`:

```
finished · exit 1 · 3 passed · 1 failed · of 4 · 2m41s
  FAIL TestPassExportAndImportRoundTrip · log lines 188-241
api-coverage.golden untouched - the recipe records only a run that passed
log: /tmp/proton-cli-live/log · session proton-cli-live removed
```

Pass on what it says in a line or two, and let the cause line decide the advice:

| Verdict says | What it means for the user |
| --- | --- |
| `Proton rate-limited this run` | wait a few minutes; those failures are not bugs and nothing wants fixing |
| `could not sign … in` | they run `just login` once, then the run again |
| `hit the recipe's timeout` | the pattern was too big or a test hung; name where it stopped |
| `the paid account did not come back` | somebody's real account holds something the run left; the box in the log says what |
| `api-coverage.golden: N lines added` | read that diff with them; it is committed with the change |

Then stop. Reading the failures and fixing them is the next thing the user asks for, and `/tmp/proton-cli-live/log` is where the output is.

## Never

- Start a run without a picked option in the questionnaire.
- Run `just test`, `just coverage`, `just test-one`, `just coverage-one` or `go test ./tests/live/…` any other way.
- Poll `check` in a loop. `run` and `watch` already report once a minute.
- Pipe or redirect the script's output. Every line it prints is the user's to see as it is printed.
- Give the `run` or `watch` call a timeout other than one hour.
- Touch a multiplexer session other than `proton-cli-live`.
- Start a second run while one is going.
