# Agent Guidelines

## Project Context

This is an open-source CLI tool used by other people. All changes should consider:
- **Backwards compatibility** - consider impact on existing users when changing command syntax or flags, but don't let it block improvements
- **Cross-platform** - must work on Linux, macOS, and Windows (amd64 + arm64)
- **User-facing quality** - README, help text, and error messages should be clear and helpful
- **Distribution** - binaries are published as GitHub Releases via GoReleaser; users install by downloading a binary or via `go install`. A release is caused by `CHANGELOG.md`, see below

## Feature Scope

proton-cli mirrors what the **Proton web clients let a user do**, not every endpoint the API exposes. If an action isn't something a user can do in the official web UI, don't add it to the CLI - even when a backend endpoint for it exists. Web-client parity beats API completeness.

## The changelog is the release button

`CHANGELOG.md` is not documentation about releases, it is what causes them. A version section reaching `main` is a release request: CI passes, the Release workflow reads the newest section, and that version's tag, artifacts and release notes all follow from it. Nearly every merge adds no section and releases nothing.

- **There is no `[Unreleased]` section here, and adding one is not how to record a change.** The file gains a section only when a release is cut, written then from the commits since the last tag. Between releases it is not touched at all.
- **Never add a version section unless the user asked for a release.** There is nowhere to park an entry: a section is a release.
- Write the commit message so the entry can be written from it later: what moved on the surface a user touches, and what that means for them. The commit is where the reasoning is still fresh.
- An entry follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/): the six categories in the specification's order, one line per change, written for the person who types the commands. Internal work gets no entry.
- `scripts/changelog` parses the file and `just test-fast` fails on a malformed one: a category that is not one of the six, a version that skips a step, a section with nothing in it. `just notes` prints what the release page would say.

## The interface is a language, and it is declared

proton-cli has one grammar, one verb per idea, and one shape per response. All of it is declared in code and checked by `internal/cli/conformance_test.go`, which walks the whole command tree. **Read `internal/cli/kit/lang.go` before adding a command.**

- `proton <app> <collection> <verb> [TARGET…] [--flags]`. A group never acts.
- The program is `kit.Program`, `proton`, and every screen speaks it whichever name was typed. `kit.Alias`, `proton-cli`, is the second name each install channel links beside it; it names the project too, which is why the repo, the module, the packages, the release assets and `~/.config/proton-cli` all keep it. Never write either name as a literal.
- Verbs come from `kit.Verbs`. A word not in there is a word being invented.
- Argument names come from `kit.Placeholders`. `REF` is a full ID, a short ID, or a human handle; Drive uses `PATH` for things that exist in the tree.
- A flag name means exactly one thing CLI-wide. New shared flags go in `flagMeanings` in the conformance test, which fails if two commands disagree.
- Output goes through `internal/ui`: `kit.List`, `kit.Show`, `kit.Read`, `kit.Mutate`, `kit.Create`. Nothing outside `internal/ui` touches a process stream, and only `internal/app/credentials.go` reads a credential from a human.
- A command's `Short`, `Long`, flag usage and `examples.go` entry are its documentation: they are what `--help` shows and what the generated reference is built from. `internal/cli/help.go` renders every screen, and three of them are golden-tested. A `Long` says what would surprise the person running the command - a constraint, a default, a value list; the reasoning behind a design goes in `docs/about/why.md`, which a reader can skip.
- Mutations go through `kit.Mutate` or `kit.Create`, which is what makes `--dry-run` structural rather than remembered.
- Anything judgeable from the command line alone must be judged before the network: use `kit.Enum`, `kit.Color`, or cobra's `Args` and `MarkFlagRequired`.
- Selection uses `kit.Select`. Never write a second bulk-filter implementation.
- `internal/ui` has golden tests. Change a response and run `just golden`, then read the diff - it is the review.

## A failure has to leave a trace

Every run records what it did to `~/.config/proton-cli/logs/`, and `proton report` hands that to the maintainer. A user cannot be asked to reproduce a failure with a flag set, so the trace is written whether or not anything went wrong. **When you write code that can fail, decide what a stranger reading the log will need.**

Where to write a line: **anywhere information is about to be destroyed** - a loop carrying on past an error, an `err == nil` that quietly declines a result, a fallback that swallows why it fell back. A loop that discards the error at each `continue` and then reports "none of them worked" leaves nothing to log later; that failure is unfixable no matter what else is instrumented. `internal/cli/conformance_test.go` fails on an `if err != nil { continue }` under `internal/service` or `internal/account` that records nothing.

Which call to use:

| Situation | Use |
| --- | --- |
| Something is missing from the answer | `skip.Record(ctx, kind, ref, reason, err)` |
| Nothing is hidden - the caller refuses, or the gap is visible on screen | `slog.DebugContext`, plus a comment saying *recorded and not counted* and why |
| The run's own envelope: what was invoked, how it ended, a panic | `UI.Trace` |
| Anything during the work | `UI.Log`, or package-level `slog` |

Rules:

- **Declare every attribute name in `redact.Fields`** with a policy. Conformance checks both directions: an undeclared name fails, and so does a declared name nothing writes. Reuse a name before adding one.
- **Log shape, not content.** Counts, a reason from a closed set, an endpoint, a status, a duration, a boolean about the account. Never a subject, filename, search term, secret, or a flag's *value* - those have no name to be written under. An address, ID or path gets a stand-in from `internal/redact`, which is the only thing that decides this and decides it above every destination.
- **Make the line distinguish causes.** "It failed" earns nothing. Which of the several ways it fails, and the counts around it, is the whole value. A fact that changes the advice is worth one request once the failure has already happened.
- **`skip.Record` counts as well as logs**, so `kit.List` warns that a listing is short and the envelope carries `skipped`. Never hand-roll that: a listing that under-reports and exits `0` is a wrong answer presented as a right one. Kinds and reasons are declared in `internal/skip`; `skip.Hides` marks the kinds whose loss takes their contents too.
- **`--log-level` is the screen only.** The file is always at debug. Never lower a record's level to keep it off the screen - use `UI.Trace`.
- **Phrase user errors with `errs.Problemf`; leave internal failures bare.** `kit.Run` tags an unphrased error out of a command body as exit `7` and invites a report, so a bare `fmt.Errorf` about a mistyped flag reads as "report this", and a polished sentence over a broken key hierarchy hides a real bug.

## Quality Gates

After making code changes, run these in order. Stop on the first failure and fix it before continuing.

**The live suites are the user's to run, never the agent's.** `just test` and `just test-paid` take minutes, spend allowances Proton meters by the hour, and act on real accounts. An agent runs **`just test-fast`** and nothing else. When a change wants one of the live suites - a new command, anything touching a request, a fix that only the live API can confirm - say so, say which one and why, and let the user decide. Never start one to find out.

1. **Tests** - `just test-fast` is the gate an agent runs: unit, golden, conformance and offline, no credentials, about a second. `just test` adds the live suite against the two accounts one test at a time, and `just test-paid` the ones needing a subscription; both are the user's. A live run fails on the first sign of Proton rate-limiting rather than pressing on, so a failure that says so means wait, not fix the code.
2. **Lint** - **always run `just lint`** and fix everything before considering the work done. It formats Go and Nix and regenerates the command reference, then checks the workflows with `actionlint`, the release configuration with `goreleaser check`, the shell scripts with `shellcheck`, and the Go with `golangci-lint` (CGO-free, so no C compiler needed). CI runs the same recipe and then fails on anything it rewrote, so leaving a generated file stale is the same failure as leaving a finding.
3. **Build** - `just build` produces the release-shaped binary; it needs the toolchain from `devbox shell`.
4. **Nix, when `go.mod` moved** - `just flake` builds the flake package from the working tree. `vendorHash` in `flake.nix` goes stale on every dependency change and nothing else catches it; the recipe prints the hash to paste in.
5. **Packaging, when the release surface moved** - `just snapshot` builds every artifact a tag would, without publishing. Run it after touching `.goreleaser.yaml`, the completions or the embedded helpers.
6. **Coverage, when the CLI's requests moved** - `just coverage` re-records which of Proton's API the live suite reaches. It runs both live suites, so it is the user's too. `just test-fast` fails when the CLI can send a request no test has ever sent, which is how an agent finds out it needs one; ask for the re-record and read the diff together. See [Testing](#testing).

## Testing

Tests are **integration tests** that run against the live Proton API. They run on the primary and secondary test accounts, and require `PROTON_CLI_TEST_PRIMARY_USER`, `PROTON_CLI_TEST_PRIMARY_PASSWORD`, `PROTON_CLI_TEST_SECONDARY_USER`, `PROTON_CLI_TEST_SECONDARY_PASSWORD`, and the secondary's `PROTON_CLI_TEST_SECONDARY_SECOND_PASSWORD` and `PROTON_CLI_TEST_SECONDARY_EXTRA_PASSWORD` - that account carries two-password mode and a Pass extra password so the suite reaches both.

- **`just test-fast` is always safe** - no API, no credentials, about a second to run, and the only one an agent starts
- **`just test` and `just test-paid` are the user's**, not the agent's: minutes each, against real accounts, spending allowances that are metered by the hour. An agent that wants one asks for it and says why.
- **What limits how often you can run it is the sending allowance, not the clock.** A run sends about seventeen messages and these are free accounts, which Proton caps at fifty an hour and a hundred and fifty a day. Two runs back to back are fine; four in an hour will start failing on the quota, and those failures look like bugs but are not. When in doubt, wait rather than debug.
- **Single tests are cheaper** (`just test-one TestName`) when verifying one change
- **`just test-report`** says where the time went and how deep each command's request graph was
- **`just docs`** regenerates the command reference from the tree; `just lint` runs it too, and CI fails on the diff. Inside an app's directory every markdown file except `README.md` is generated, one per collection - never edit one. Change the command's `Short`, `Long`, flag usage or `examples.go` entry
- **Unit test file naming**: name a unit test file after the source file it tests, with `_test.go` appended (e.g. `size.go` → `size_test.go`) - never after a symbol or after a file that doesn't exist. The integration tests under `tests/` are the exception: they are grouped by feature area.

## Reference Source

The Proton WebClients TypeScript source is available at `/tmp/proton-cli-WebClients/` (cloned from https://github.com/ProtonMail/WebClients). Use it as the primary reference for:

- API endpoint signatures, request/response shapes (`packages/shared/lib/api/`)
- Encryption flows and key handling (`packages/shared/lib/keys/`, `packages/crypto/`)
- How the web client calls endpoints (parameter names, types, ordering)
- Constants and enums (`packages/shared/lib/constants.ts`, etc.)

If the clone is missing or stale, run:
```bash
cd /tmp && git clone --depth 1 --branch main https://github.com/ProtonMail/WebClients.git proton-cli-WebClients
```
