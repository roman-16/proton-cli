# Test Guidelines

## Overview

All tests are **integration tests** that run the real `proton` binary against the live Proton API. There are no mocks - every test creates real data, verifies it, and cleans up.

Unit tests live alongside the code they test (e.g. `internal/mailtext/html_test.go`).

Faster suites run with **no credentials and no network**, and they are the ones to reach for first - `just test-fast` runs all in about a second:

- **unit** tests, colocated with their source
- **golden** tests in `internal/ui`, which pin the exact bytes of every response kind. Change how something looks and run `just golden`; the diff is the review.
- the **conformance** test in `internal/cli`, which walks the whole command tree and checks the interface's rules: one verb per idea, one meaning per flag, groups that never act, nothing outside `internal/ui` touching a process stream.
- the **offline** suite in `tests/offline`, which runs the real binary with no session and the API pointed at a dead port. Everything judgeable from the command line alone belongs there: a value outside a declared domain, a colour off Proton's palette, a reference shaped like nothing, an argument count. Each case costs about 5ms, against about 500ms and a set of credentials in the live suite.

An inconsistency is far more likely to be caught there than here.

## Running Tests

```bash
just test                             # the free accounts' integration tests, one at a time
just test-paid                        # the ones needing a paid plan (see below)
just test-all                         # both suites, one after the other
just test-one TestDriveItemsMove      # a single one
just test-report                      # where the time went, and how deep each request graph was
```

`just login` signs every account in, and `just seed` fills the two free ones, for working with them by hand.

**Signing in is the one thing here that can need a person.** Proton raises a CAPTCHA at login, a challenge belongs to the run it was issued to, and only a browser answers one - so `just login` inherits your terminal, and it is the only thing in this repository that may wait. `go test` hands the test binary `/dev/null` on standard input, so nothing inside `TestMain` can ever present one. A run that finds a working session pays one read for its own sign-in (`account login` does no SRP exchange while the saved session works); a run that would have to sign in from scratch stops and says to run `just login`.

The suite requires all six of `PROTON_CLI_TEST_PRIMARY_USER`, `PROTON_CLI_TEST_PRIMARY_PASSWORD`, `PROTON_CLI_TEST_SECONDARY_USER`, `PROTON_CLI_TEST_SECONDARY_PASSWORD`, `PROTON_CLI_TEST_SECONDARY_SECOND_PASSWORD` and `PROTON_CLI_TEST_SECONDARY_EXTRA_PASSWORD`.

`PROTON_CLI_TEST_PAID_USER` and `PROTON_CLI_TEST_PAID_PASSWORD` are required by **`just test-paid`, `just test-all` and `just coverage`**, and by nothing else. An account is required exactly where the binary holds tests for it: without the `paid` build tag there is no paid account compiled in, so demanding its credentials would demand something the run cannot use; with the tag every test acts on it, and a run that skipped them all would report success for having done nothing. `required` in `integration_test.go` says which, from `paidBuild`. `.env.example` lists all of them.

## The two accounts

The suite creates, mutates and deletes real data, so it runs on two accounts kept for that and nothing else. Most tests act as **`primary`**; the handful that genuinely need two Proton users bring in **`secondary`**.

The **secondary account is in Proton's two-password mode**, which is the only way the suite reaches that mode at all: the secret that opens its keys is not the one that signs it in, so `PROTON_CLI_TEST_SECONDARY_SECOND_PASSWORD` is as required as its password, and every run signs an account in through that path. `TestAccountSettingsReportTwoPasswordMode` fails if the account is ever switched back, because the coverage would then be gone with nothing saying so.

Its **Pass is protected with an extra password** for the same reason, so `PROTON_CLI_TEST_SECONDARY_EXTRA_PASSWORD` is required too, and `TestMain`'s sign-in hands it over. Proton grants the scope it buys for the life of the session and offers nothing that takes it back, so the exchange itself happens on the run that first meets it and never again - `TestPassExtraPasswordProtectsTheSecondaryAccount` checks the outcome every run and fails if the account is ever left without an extra password. Forcing a second exchange would mean a fresh session, and Proton answers an unattended sign-in with a CAPTCHA only a person can solve.

These are the harness's own variables, not the CLI's: proton takes an account from a signed-in profile, which `TestMain` establishes. The `PROTON_CLI_TEST_` prefix keeps them clear of anything the binary reads.

`TestMain` signs every required profile in before any test runs, each secret from a `0600` file of its own, and leaves those files in place for the rest of the run. The file is needed because a session cannot carry elevation: Proton re-authenticates over SRP for its guarded operations, `calendar settings calendars delete` among them, and that needs the password itself - the key blob sealed at login is a one-way derivation of it. `account login` is idempotent, so a run that reuses an existing session pays nothing.

`runAs` builds the child environment from an **allowlist** rather than inheriting one. It is the single place a target account is chosen, so it is the single place the choice can be enforced: whatever you happen to have exported, the binary under test sees a stated environment and can act only as the profile named there.

### Acting as the second account

Use it whenever a scenario genuinely needs two Proton users - accepting a share invitation, receiving and reading mail, or organizing a calendar invite the primary RSVPs to.

```go
func TestSecondAccountFoo(t *testing.T) {
    runOKSecondary(t, "mail", "settings", "addresses", "list")   // runs as the second account
    // ... primary and secondary interact ...
}
```

Run order matters: the *primary* invites or sends, then the *secondary* accepts or receives, then verify on whichever side the state landed. Register cleanup on **both** sides - a mutation made as the secondary needs `cleanupRunSecondary`.

An **external, non-Proton** recipient comes from `PROTON_CLI_TEST_EXTERNAL_RECIPIENT`, and the suite refuses to start without it. It is required for the reason the accounts are: encrypting to somebody with no Proton account, and emailing an invitation to an attendee with no Proton calendar, are branches the two test accounts cannot enter, so a run without it would pass having never tried them. Sending to a fake `@example.com` address instead bounces (nullMX) and litters the inbox with MAILER-DAEMON returns.

## Layout

```
tests/
├── integration_test.go      TestMain + helpers
├── lease_test.go            what two tests cannot both have, and the guards
├── paid_test.go             what a plan gates, and what may not be done to the paid account
├── paid_on_test.go          the paid tests themselves, behind the `paid` build tag
├── paid_off_test.go         what stands in for them when the tag is absent
├── trace_test.go            the per-invocation trace `just test-report` reads
├── fixture/                  what the seed puts on the account for the suite to read
├── offline/                  the real binary, no session, no network
├── settings_test.go         account / mail / calendar / drive settings
├── mail_test.go             messages, attachments, conversations, batch filters
├── mail_compose_test.go     drafts, reply, forward, sender selection, signatures
├── mail_export_test.go      .eml and mbox export, --eml import
├── mail_identity_test.go    addresses, display name, signature, auto-reply
├── drive_test.go            items, folders, trash, streaming, recursive, batch filters
├── calendar_test.go         calendars, events, scope-unlock delete
├── contacts_test.go         CRUD, REF resolution, exit codes
├── pass_test.go             vaults, items, alias, batch filters
├── account_test.go          account, session, profiles, account settings
├── contract_test.go         the response contract: envelopes, streams, exit
│                            codes, --dry-run, stdout=ID
├── profile_test.go          --profile / PROTON_PROFILE multi-account
├── short_ids_test.go        short-ID display and resolution
└── api_test.go              raw `api` escape hatch
```

`contract_test.go` is one file because the contract is one thing.

## How Tests Work

1. `TestMain` in `integration_test.go` builds the binary once into a temp directory.
2. Each test calls the binary as a subprocess via `run()` / `runOK()` / `runJSON()`.
3. `TestMain` signs every required profile in and seeds nothing. What the account has to hold is brought about by the test that reads it.
4. Each invocation names its profile through `PROTON_PROFILE` in a scrubbed child environment, and the session is reused across invocations.
5. Tests run **one at a time**, and every one of them calls `t.Parallel()`. What two tests cannot both have at once is declared and leased - see below.

## Running one at a time

The suite is bound by waiting for Proton, not by doing anything, so tests overlap. Proton absorbs a good deal of that: sixteen concurrent invocations were measured to cost the same wall time as one.

What decides the setting is not the client but the account, and **what gives out first is whichever endpoint the free plan meters hardest**:

- `PUT /calendar/v1/{id}/events/sync`, because the free plan allows few calendars and so every calendar test writes to the same one. At four, a full run failed on it three times out of three.
- Making an alias, which is the tightest of them. Several tests need one of their own, and Proton allows few per hour, so any overlap at all has the later ones refused.

Raising it means either a paid account with room to spare, or a lease that makes the tests competing for one endpoint take turns.

The safeguards, in the order they bite:

- **A single 429 fails the run.** The client backs off and would very likely succeed, so the suite would otherwise pass and teach nobody anything. Proton's first sign of displeasure stops the run and says to lower the concurrency.
- **Sends never overlap.** `runAs` takes the `sending` lease for any command that puts a message on the wire, so no test has to remember. It is held for the send and not for the wait after it.
- **The client holds at most 8 requests in flight**, and refreshes a session once per process however many requests discover it expired together.
- **Raise `parallel` in the justfile one step at a time**, only after a full run shows no rate limiting, and never past eight. The calendar sync and alias endpoints are what give out first, so that is where a run that was raised too far will fail.

### Leases: what two tests cannot both have

Almost nothing needs a lease. Each test makes its own labels, folders, events and items under its own `testID()` and asserts on those. Two things do:

- an account has exactly **one** of some things - its settings, an address's signature, the auto-reply;
- the free plan allows only a few **calendars, vaults, labels, mail folders and filters**, and the fixture already holds one of each, so a test that makes one takes the spare slot;
- a few tests identify their own work by **comparing a listing before and after**, which another test's work would appear in (photos have no name in a listing, so there is no other way).

Those are named in `lease_test.go` and taken with `lease(t, ...)`, which holds them for the test and releases them after. This is what `t.Parallel()` alone cannot say: two tests that exclude each other but nobody else.

**Both rules are checked, not remembered.** `TestEveryTestThatTouchesSharedStateLeasesIt` reads every test's source and fails if it touches something shared without leasing it; `TestEveryTestRunsInParallelUnlessItSaysWhy` fails on a test that is neither parallel nor listed in `serialTests` with a reason. A test that runs alone gets the whole account to itself for free, because Go finishes every non-parallel test before any parallel one resumes - which is exactly what the one test that rewrites the shared ID cache file needs.

When a new conflict appears - and it will appear as a test failing somewhere it has nothing to do with, like `Number of calendars exceeded limit` - the fix is one line in the vocabulary and one `lease` call, and the guard finds every test that needs it.

## What a run costs, and how to know

A run is spent almost entirely **inside invocations of the binary** - measured, 100% of the wall clock, with no idle time between them. So there are exactly three ways to make the suite faster: fewer invocations, cheaper invocations, or invocations that overlap.

An invocation's cost is the **depth of its request graph**, not the number of assertions in the test. A command that asks Proton for eight things one after another waits for all eight; one that asks for them together waits for the slowest. Do not guess which is which - measure it:

```bash
just test-report                    # per command: invocations, time, requests, overlap
just test-report TestCalendar       # or one slice of it
just coverage                       # re-record which of Proton's API the suite reaches
```

`overlap` is 1.0 for a strict chain and higher when requests were made together. The report ends with **chains worth flattening**: commands with several requests and an overlap near 1.0. That list is the work, in order.

### The guard rail

The live suite is the only thing that would notice Proton changing an answer, and it can only notice for requests it actually makes. So both halves of that are written down:

- **what the suite reaches** - `tests/api-coverage.golden`, recorded from a real run by `just coverage`;
- **what the CLI can send** - read from the source by `TestEveryRequestTheCLICanSendIsOneTheSuiteSends`, which needs no account and so runs in `just test-fast` on every push.

A request the CLI can send that the golden does not hold **fails the build**. There are two ways out, and both have to be argued in the test's own file: `unreachable` for what no run can do (revoking the session it is running on, a paid feature these accounts do not have), and `untested` for a gap somebody chose to leave, which is reported on every run rather than passing quietly. That list is something to shorten.

So an optimisation cannot quietly narrow the live surface: removing the last test that reaches an endpoint turns the build red. When a change legitimately stops the CLI sending something, `just coverage` re-records it and the diff is the review.

## Writing a Test

Follow **Arrange → Act → Assert**. Every test that creates data must register cleanup **immediately after creation**, before any assertion that might fail.

```go
func TestDriveItemsFoo(t *testing.T) {
    // Arrange
    folder := "/" + testID() + "-foo"
    runOK(t, "drive", "folders", "create", folder)
    cleanupRun(t, fmt.Sprintf("Delete: proton drive items delete %s", folder),
        "drive", "items", "delete", folder)

    // Act
    stdout := runOK(t, "drive", "items", "list", folder)

    // Assert
    assertContains(t, stdout, "expected-string")
}
```

## Cleanup Rules

- **Always register cleanup**, even for tests about deletion - the test might fail before reaching the delete step.
- Use `cleanupRun()` for CLI commands, `cleanup()` for custom functions - both are `t.Cleanup`-based (per-test).
- **Never clean up a seeded fixture.** The account holds it between runs on purpose; deleting it makes the next run send it again and spend the allowance this exists to protect.
- `t.Cleanup()` guarantees cleanup runs even on test failure.
- Cleanup failures print a loud box with a copy-pasteable command the user can run manually:

  ```
  ╔══════════════════════════════════════════════════════════════╗
  ║  ⚠️  CLEANUP FAILED - MANUAL ACTION REQUIRED                 ║
  ╠══════════════════════════════════════════════════════════════╣
  ║  Delete folder: proton drive items delete /test-xxx      ║
  ║  Error: exit 1: ...                                          ║
  ╚══════════════════════════════════════════════════════════════╝
  ```

## Helpers

| Helper | Purpose |
|---|---|
| `run(t, args...)` | Run binary, return stdout/stderr/exitCode |
| `runOK(t, args...)` | Run binary, fail test on non-zero exit, return stdout |
| `runOKStderr(t, args...)` | Same as `runOK` but also returns stderr |
| `runWithStdin(t, stdin, args...)` | Run with a custom stdin reader |
| `runJSON(t, args...)` | Adds `--output json`, parses stdout as JSON **object** |
| `runJSONArray(t, args...)` | Adds `--output json`, unwraps the collection envelope, returns the rows |
| `testID()` | Unique `proton-cli-test-{ms}-{rand}` prefix |
| `cleanupRun(t, desc, args...)` | Register cleanup that runs the CLI |
| `cleanup(t, desc, func)` | Register cleanup with a custom function |
| `assertContains(t, stdout, substr)` | Assert stdout contains substring |
| `assertNotContains(t, stdout, substr)` | Assert stdout does not contain substring |
| `assertField(t, stdout, field, expected)` | Assert `Key: Value` line matches |
| `runArgs(stdin, args...)` | `t`-free runner (stdout, stderr, code, err); used by fixtures and suite cleanup |
| `sendTestMail(t, subject)` | Send a mail to self, register per-test cleanup, return inbox ID. **Only when the send itself is the subject** |
| `plainMail(t)` / `quotedMail(t)` / `sharedAttachment(t)` / `mutableMail(t)` | Seeded mail fixtures (see the sending-allowance section) |
| `waitFor(timeout, interval, check)` | Poll `check` (checks first, then sleeps) until true or timeout |
| `messageIDInFolder(folder, subject)` | `t`-free: first message ID in a folder matching subject, or `""` |
| `selfEmail()` | The primary account's address |
| `looksLikeID(s)` | Heuristic: Proton base64 IDs end in `==` |
| `secondaryEmail()` | The second account's address |
| `runSecondary` / `runOKSecondary` / `runJSONSecondary` / `runJSONArraySecondary` / `cleanupRunSecondary` | The same runners, as the second account |
| `lease(t, ...)` | Take exclusive use of shared state for this test (see Leases) |
| `externalRecipient(t)` | A non-Proton recipient; the suite refuses to start without one |

## The sending allowance, and the fixtures that protect it

The accounts are on the free plan, which allows **50 messages an hour and 150 a day**, counted per recipient. A run sends about 19. That, not the wall clock, is what decides how often the suite can be run: a suite that sends 30 can be run once an hour however fast it is.

So a message is only sent when the sending is the thing being tested. Everything that merely needs *a* message of some shape reads one the account already holds.

### Fixtures are made when something asks

`tests/fixture` declares what the accounts hold and how to bring each one about. Nothing is made up front: the accessor a test calls finds its fixture, makes it if the account has not got it, and remembers it for the rest of the run.

That is what makes a run cost only what it uses. `just test-one TestMailMessagesList` never touches Pass, Drive or Calendar; a run that reads no mail sends none. And a fixture that cannot be made - Proton meters making an alias to a handful an hour - fails the tests that need it rather than every test there is.

`just seed` asks for all of it at once, through the same declaration, for putting an account in shape by hand. It is the only thing that sweeps collections a run never looks at; a run sweeps what it lists, which costs nothing because it was listing anyway.

**Only what the suite never changes may be remembered.** A listing held from before another test changed the thing is a false pass, which is worse than a slow run. A test that mutates its fixture leases it and puts it back - see [Leases](#leases-what-two-tests-cannot-both-have).

### Seeded fixtures (read-only tests)

| Fixture accessor | What it gives you | Use for |
|---|---|---|
| `plainMail(t)` | `(msgID, convID, subject)` - a delivered self-mail with a plain body (no quote markers, no attachments); its body contains its subject | reading, formats, body-only, redirects, summaries, search-hit |
| `quotedMail(t)` | `(msgID, subject)` - body carries the canonical `On <date>, <name> <addr> wrote:` reply block | strip-quotes assertions |
| `sharedAttachment(t)` (aka `findMessageWithAttachment(t)`) | `(msgID, attID, attName)` - a delivered mail carrying one regular attachment | attachment list/download/footer tests |
| `sharedMixedAttachment(t)` (aka `findMessageWithMixedAttachments(t)`) | `msgID` - the same message, which also carries an **inline** image | inline-vs-attachment disposition filter tests |
| `mutableMail(t)` | `msgID` of a message this test may change and change back | mark / star / move / trash round-trips |

The lookups happen at most once per run under `sync.OnceValues`, and are safe to call from parallel tests. `mutableMail` hands out one message from a pool, so two tests running together never change the same one; the state is put back if the test failed before it could.

**Rule of thumb:**

- **Read-only** test (never mutates its message) → use a seeded fixture.
- **Mutating** test (mark / star / move / trash) → `mutableMail(t)`.
- A test of the **send path itself** (attachments, inline images, HTML, scheduled, expiring, encrypted-for-outside, `--eml`, cross-account) → send your own. Each distinct send shape keeps exactly one such test: that is what makes a change to Proton's send path fail something.
- A test whose subject is a **flag on the parent** (`reply` setting `IsReplied`) → send your own, because a seeded parent would already be flagged and the assertion would pass for the wrong reason.

```go
func TestMailMessagesGetBodyOnly(t *testing.T) {
    msgID, _, subject := plainMail(t)    // seeded, no send, no delivery wait
    stdout := runOK(t, "mail", "messages", "get", "--body-only", msgID)
    assertContains(t, stdout, subject)
}
```

Adding a fixture means declaring it in `tests/fixture`. The first run that asks for it makes it.

### Polling for delivery

Use `waitFor(timeout, interval, check)` - it checks **before** the first sleep, so an already-true condition is free. Never write a `for { time.Sleep(2*time.Second); ... }` loop. `messageIDInFolder(folder, subject)` and `conversationIDOf(msgID)` are `t`-free lookups that pair well with `waitFor`.

## Conventions the tests rely on

These are stable CLI guarantees that tests verify:

### stdout = new ID on create

Every create command writes **just the new ID** (one line, no JSON, no trailing text) to stdout and `✓ …` to stderr:

```go
stdout := runOK(t, "mail", "settings", "labels", "create", "--name", name, "--color", "#8080FF")
id := strings.TrimSpace(stdout)
// id is a bare 88-char Proton ID; stderr carried the human message.
```

This makes shell capture work: `ID=$(proton ... create ...)`.

### Exit codes

| Code | Meaning |
|---|---|
| 0 | success |
| 1 | user error (bad flag, missing arg, invalid input) |
| 2 | auth |
| 3 | not-found (REF matched no resource) |
| 4 | conflict / ambiguous (REF matched multiple resources) |
| 5 | network / 5xx |
| 130 | cancelled via Ctrl+C |

### Output format

`--output text|json|yaml` (default `text`).

Every collection comes out as an envelope keyed by its plural noun, always with a `count`, so one consumer reads any list:

```json
{ "messages": [ … ], "count": 3, "total": 47, "page": 0, "page_size": 3, "has_more": true }
```

`runJSONArray` unwraps it and cross-checks the count. Keys are `snake_case`, enumerated values are names rather than Proton's numbers (`"type": "file"`, not `"type": 2`), and IDs are never shortened. A mutation emits a result object rather than a bare ID, so `--output json` always means JSON.

### REF arguments

Every command that takes an ID also accepts a substring search term. Ambiguous matches return exit 4 with candidates listed on stderr.

Two-ID references are one slash-separated token: `pass items get SHARE_ID/ITEM_ID`, `calendar events get CALENDAR_ID/EVENT_ID`. Short IDs work on both halves.

**Drive addresses items by `PATH`**, because that is what Proton resolves and what a person means. Things with no place in the tree - trashed items, photos, albums - are addressed by the `REF` their list showed.

### Local validation precedes the network

Anything judgeable from the command line alone must fail without a session: an unknown setting key, a value outside a declared domain, a colour off Proton's palette, a missing required flag, a selection that names nothing. A test for one of those belongs in `tests/offline`, where it costs 5ms and no credentials, and should assert the whole accepted domain appears in the message.

This holds because **no step asserts that an account exists, and none of them unlocks its keys**. Both requirements belong to the request: the client holds the session one (`SetSessionGuard`), and a service holds the key one, asking for the hierarchy where it decrypts rather than before the command body runs. So a command judges what it can judge and only then finds out whether anyone is signed in. One place keeps the requirement earlier, on purpose: **a dry run of a mutation that reaches Proton**, because a preview is a claim about what the command would do, and without an account it would not do it. The two commands that change this machine instead - `update` and `uninstall` - declare `kit.OnThisMachine` and preview signed out, because that is what they would have done.

So a check a command makes for itself - a missing `--name`, a prefix an alias cannot do without - is answered from the command line even for commands that decrypt, and its test belongs in `tests/offline`.

## Cobra and Positional IDs

Proton IDs are Base64URL-encoded, so about one in sixty-four begins with `-` and argv would hand it to the flag parser. `preprocessArgs` in `internal/cli/dashids.go` inserts `--` ahead of it, and it decides by shape alone: a dash-leading token is a reference when every part of it is a complete ID (22, 32, or ≥60 ending `==`). Nothing shorter has to be judged, because a **short ID never begins with a dash** - `ref.Shorten` starts the eight characters after any leading dashes.

So `--` is never needed in a test, for a full ID or a short one.

**Put flags before the positionals.** Everything after the inserted `--` is positional, so a flag written after a leading-dash *full* ID arrives as an argument and the command fails with "accepts N arg(s)" - `rewrapFlagError` catches that and explains it. Whether it happens depends on which ID the account handed out, so writing the flags last makes a test fail roughly one run in sixty rather than never:

```go
runOK(t, "mail", "messages", "attachments", "download", "--dest-dir", dir, msgID)   // always works
runOK(t, "mail", "messages", "attachments", "download", msgID, "--dest-dir", dir)   // works until the ID starts with '-'
```

`runJSON` and `runJSONArray` put `--output json` in front for the same reason, so they are safe with any reference.

### `cleanupRun` descriptions

The copy-pasteable commands surfaced on cleanup failure do not need `--`; the user can paste them as-is even if the ID starts with a dash.

## Naming

- Test artifacts use the `proton-cli-test-{ms}-{rand}-{purpose}` prefix (from `testID()`).
- This makes them identifiable in the Proton UI if cleanup ever fails.
- Never use short or common names that could collide with real data.

## The paid account

Proton gates a good deal of what the web clients offer behind a subscription, and the two accounts above are free ones. Buying a second subscription to test with is not a reasonable thing to ask, so the paid tests run against **an account somebody actually uses** - under rules that make a run reversible rather than merely careful.

```bash
just test-paid          # the paid tests alone
just test-all           # the free suite first, then these
```

Those two recipes are the only things that compile the paid tests. `just test` does not, which is the point of the tag rather than a habit.

```go
func TestPaidCalendarSharing(t *testing.T) {
    t.Parallel()
    requirePaid(t)
    // create something, use it, delete it
}
```

Five rules, in the order they bite:

1. **`just test` cannot reach it.** The paid tests live behind the `paid` build tag, so an ordinary run does not compile them, does not sign the account in and does not read it. This is stronger than a skip, which is one edit away from not skipping.
2. **Nothing seeds it.** `scripts/seed` knows the two free accounts and no others; `TestNothingSeedsThePaidAccount` reads its source and fails if that changes.
3. **A test acts only on what it made.** It does not list the account's own calendars, vaults, messages or files and act on what it finds. There is real data there, and a filter that matches more than it meant to is the mistake this rules out.
4. **Some commands are refused outright.** `offLimitsOnPaid` names them and why - an auto-reply that answers real mail, a setting whose previous value cannot be read back, an address that cannot be recreated, emptying a folder that had things in it before the run. `runAs` enforces it: the one place a target account is chosen is the one place the choice can be refused, so a test cannot opt out by forgetting. `TestEveryPaidRestrictionSaysWhy` fails on a rule with no reason written out.
5. **The account is photographed before and after.** Calendars, vaults, Pass items, labels, folders, filters, addresses, contacts, the root of the drive, the newest of the inbox, and all four settings pages. Anything left behind or missing after the run fails it and is named in a box. This is the one that catches a mistake nobody predicted - the other four are promises, and this checks them.

Only the newest slice of the inbox is looked at: the rest is somebody's real mail and none of the suite's business.

### Mail a run causes Proton to send

Sharing something makes Proton write to the owner when the other side answers, and no setting on this end turns that off. So a run **sweeps its own notices**: mail from `no-reply@proton.me`, matching one of the subjects in `noticesProtonSends`, that arrived after the run began. It is moved to the **trash**, never deleted, because it is real mail.

The sweep runs before the photograph, so what it clears is not then reported as something the run left behind - and anything it fails to clear stays, and the photograph names it. Adding a test that makes Proton write to the account means adding its subject to that list.

Sending is allowed: a paid test may send from that address like any other.

A run refuses to start if an account's user and password look swapped - an address in the password variable and something that is not one in the user variable. Signing in with them the wrong way round is a failed login attempt against a real account, and failed attempts are what Proton counts before it starts demanding a CAPTCHA.

The runners mirror the free ones - `runPaid`, `runOKPaid`, `runJSONPaid`, `cleanupRunPaid` - and live in `paid_on_test.go` with the tests. Add the shapes a new test needs alongside it rather than up front.

`just lint` runs `golangci-lint` twice, once with the tag, because the pass without it does not compile these files at all - a helper used only here would otherwise look unused, and a mistake in here would never be caught.

**A paid test's name has to contain `Paid`.** `just test-paid` selects them with `-run Paid`, so one named anything else would never run and would look like it had passed. `TestEveryPaidTestSaysSoInItsName` reads the file and enforces it.

The canary earns its own two tests: one that it photographed something (an empty photograph compares equal to another empty one and would pass forever), and one that the comparison notices an item appearing, disappearing, or turning up twice.

## Paid-plan features

Even with a paid account configured, most tests still run on the free ones, and a test that reaches a gated feature there skips rather than fails.

Which features those are is declared in **`paid_test.go`**, not recognised test by test:

```go
stdout, stderr, code := run(t, "contacts", "groups", "create", "--name", gname, "--color", "#8080FF")
skipIfPlanRefuses(t, contactGroups, code, stderr)
```

| Feature | Declared as | How Proton refuses |
| --- | --- | --- |
| Contact groups | `contactGroups` | HTTP 401, Proton code `2027` |
| Auto-reply | `autoReplySchedule` | a message naming "upgrade", "paid" or "subscription" |

A refusal recognised by whatever substring happened to be in one error message is a skip that stops working the day Proton rewords it, and the test then fails somewhere unrelated to what it was about. So a gate is matched **by code where there is one**, and by words only where there is not - the sentence is Proton's to reword, the code is not.

`skipIfPlanRefuses` also takes the exit code, so a command that succeeded is never mistaken for one that was refused: a message that happens to contain the word "upgrade" is not a refusal. That judgement is made in one place.

Adding a gated feature means adding a `gate` to `paid_test.go` and calling `skipIfPlanRefuses`, never writing a new `strings.Contains` in a test.

Auto-reply is worth a note: Proton refuses it with 9100, the same code it uses for a missing scope, so the CLI elevates the session first and only then hears the real reason. That is why `mail settings autoreply set` carries the credential flags even though the answer is a subscription and not a password.

## Known Limitations

- `calendar settings calendars delete` hits an endpoint Proton guards behind an elevated session. Nothing in the command arranges that: the client elevates when the server asks, using the password `runAs` supplies through `--password-file`, and drops the scope again.
- `drive trash list` and `drive trash empty` cover every volume the account has, photos included, so a test that empties the trash empties the photo library's too. `driveTrash` is leased for exactly that reason.
- Proton only allows specific hex colors for labels, folders, calendars and contact groups (e.g. `#8080FF`, `#3CBB3A`) - see `ACCENT_COLORS` in the WebClients source. The CLI refuses anything else locally, before a request.
- `just test-report` says where the time went. The report ends with the request chains still worth flattening; `drive items upload` and `pass items delete` are genuinely deep and the rest is measured.
- Mail-delivery latency is inherent: a self-mail's inbox copy lands a few seconds after send. Only the tests whose subject is the send pay it.
- `TestDriveItemsUploadManyBlocks` uploads 44 MiB and takes about 20 seconds. It is the only test that makes the CLI ask for more than one batch of block links, so it is the only one that would notice if Proton lowered the number of links a single request may ask for. It stays.
