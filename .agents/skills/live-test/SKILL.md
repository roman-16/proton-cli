---
name: live-test
description: Runs proton-cli's live suite the one way an agent may - works out which live tests the working tree and the conversation point at, asks with the questionnaire (full test, full coverage, and the matching subset run when the context names tests), starts the pick in a detached zellij or tmux session, reports one progress line a minute, then prints the verdict and removes the session. Use when the user asks for a live run, a test run or a coverage run, wants a change confirmed against Proton, or names this skill. Not for `just test-fast`, which any agent runs directly.
---

# Live test

A live run signs three accounts in, acts on real data and spends an allowance Proton meters by the hour. The questionnaire is what makes it the user's decision, so it comes before the network, always, and it names every test the run will touch.

`scripts/live-test.sh` does the mechanics. From the repository root:

```bash
.agents/skills/live-test/scripts/live-test.sh tests PATTERN     # what a pattern matches, and what each test proves
.agents/skills/live-test/scripts/live-test.sh start RECIPE [PATTERN]
.agents/skills/live-test/scripts/live-test.sh check             # one line: where the run is, or the verdict
.agents/skills/live-test/scripts/live-test.sh stop              # abort, keep the log
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

```bash
.agents/skills/live-test/scripts/live-test.sh start coverage-one 'TestContactsMatchingPinStillDelivers|TestPassExportAndImportRoundTrip'
```

Then, once a minute until it says `finished` or `stopped`:

```bash
sleep 60 && .agents/skills/live-test/scripts/live-test.sh check
```

Each check is one line and needs no comment from you: `23% · 26 passed · 2 failed · 28 of 121 · running TestDriveItemsUpload · last output 8s ago · 4m31s elapsed`. Say nothing between checks. A question from the user gets a one-line answer, then the polling resumes.

**Stuck:** the same test `running` with `last output` past five minutes. Say which test and how long, and offer `stop`. The user can watch the run themselves at any time - `start` prints the `tail --follow` command and the attach command for their multiplexer.

## 4. Report, then stop

`check` prints the verdict, tears the session down and leaves the log at `/tmp/proton-cli-live/log`:

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
- Touch a multiplexer session other than `proton-cli-live`.
- Start a second run while one is going.
