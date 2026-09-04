# Test Guidelines

## Two tiers, and one thing that separates them

**Does answering the question need Proton.** That is the whole boundary. It is binary, it never drifts, and it is the only thing that decides whether a run needs credentials, minutes and a metered allowance.

```bash
just test-fast    # everything decidable without Proton: seconds, no credentials, safe any time
just test         # that, then the live suite against all three accounts
```

Nothing else decides a tier. A subscription is a property of an *account*, not of a question, so the tests that need one act as the paid account like any other and run in the same pass. There is no build tag, no `-run` filter, no "this needs a plan" skip.

**An agent runs `just test-fast` and nothing else.** The live suite takes the best part of an hour, spends allowances Proton meters by the hour, and acts on real accounts - one of them somebody's own. When a change wants it, say so and why, and let the user decide.

## Layout

```
tests/
├── account/    which accounts exist, what their variables are called, whether they are configured
├── argv/       finding a command inside an argument list
├── fixture/    what an account holds for the suite to read
├── paid/       what may not be done to the paid account, and how to tell it came back
├── rules/      the rules the live suite is held to, read from its source
├── offline/    the real binary, no session, and the API pointed at a dead port
└── live/       the suite that needs Proton
```

Everything but `live/` runs in `just test-fast`, and everything but `live/` and `rules/` has ordinary unit tests beside it. That is the point of them being packages: a source-scanning guard that could not be unit-tested was silently wrong for a long time.

### Inside `live/`

The harness, one file per job:

| File | Contents |
| --- | --- |
| `main_test.go` | `TestMain`, the credential check, the password files, the sign-in |
| `run_test.go` | `runAs` and every named runner, the child environment, the 429 tripwire |
| `watch_test.go` | the commands that stay attached until told to stop |
| `fixture_test.go` | the accessors: `plainMail`, `mutableMail`, `alias`, `pinned` |
| `assert_test.go` | the assertions, `testID`, the reference-shape checks |
| `cleanup_test.go` | `cleanup`, `cleanupRun` and its per-account forms |
| `canary_test.go` | the photograph of the paid account, either side of the run |
| `trace_test.go` | the per-invocation trace `just test-report` reads |
| `response_test.go` | the response contract: rendering, streams, `--dry-run`, consent |

The tests, **one file per collection**, named the way the command is and the way the generated docs page is: `mail_messages_test.go`, `mail_filters_test.go`, `pass_vaults_test.go`, `calendar_events_test.go`. Somebody who ran `proton mail settings filters create --help` knows which file to open, and a new command has an obvious home. The group words are dropped - `mail_labels_test.go`, not `mail_settings_labels_test.go` - because the collection name is what distinguishes it.

## The three accounts

`tests/account` declares them. Each is as required as the others: an account is required because the binary holds tests that act as it, and a run that skipped them would report success for having done nothing.

| Profile | What it is for |
| --- | --- |
| `primary` | almost everything |
| `secondary` | the scenarios that genuinely need two Proton users - accepting a share, receiving mail, answering an invitation |
| `paid` | what Proton gates behind a subscription |

**Nine variables, all required.** `account.Required()` is the list, `account.Missing()` is what is unset, and the suite refuses to start rather than skipping what it cannot reach:

```
PROTON_CLI_TEST_PRIMARY_USER / _PASSWORD
PROTON_CLI_TEST_SECONDARY_USER / _PASSWORD / _SECOND_PASSWORD / _EXTRA_PASSWORD
PROTON_CLI_TEST_PAID_USER / _PASSWORD
PROTON_CLI_TEST_EXTERNAL_RECIPIENT
```

The **secondary account is in Proton's two-password mode** and its **Pass carries an extra password**, which is the only way the suite reaches either: the secret that opens its keys is not the one that signs it in. `TestAccountSettingsReportTwoPasswordMode` and `TestPassExtraPasswordProtectsTheSecondaryAccount` fail if the account is ever switched back, because the coverage would otherwise be gone with nothing saying so.

`PROTON_CLI_TEST_EXTERNAL_RECIPIENT` is a mailbox **outside Proton**, required for the reason the accounts are: encrypting to somebody with no Proton account, and emailing an invitation to an attendee with no Proton calendar, are branches no Proton account can enter. A fake `@example.com` address bounces and litters the inbox with MAILER-DAEMON returns, so it has to accept mail.

These are the harness's own variables, not the CLI's. `proton` takes an account from a signed-in profile, which `TestMain` establishes; the `PROTON_CLI_TEST_` prefix keeps them clear of anything the binary reads.

**Signing in is the one thing here that can need a person.** Proton raises a CAPTCHA at login and only a browser answers one, so `just login` inherits your terminal and is the only thing in this repository that may wait. `go test` hands the test binary `/dev/null` on standard input, so nothing in `TestMain` can present one: a run that finds a working session pays one read, and a run that would have to sign in from scratch stops and says to run `just login`.

### The runner is the only way in

`runAs` builds the child environment from an **allowlist** rather than inheriting one. It is the single place a target account is chosen, so it is the single place the choice can be enforced or refused: whatever you happen to have exported, the binary under test sees a stated environment and can act only as the profile named there.

`run` / `runOK` / `runJSON` act as the primary; `runSecondary` / `runOKSecondary` / … as the second; `runPaid` / `runOKPaid` / … as the paid one. All of them are one implementation under three names, so which account a test acts as is stated at the call site rather than in a variable.

Two rules, both checked in `tests/rules`: nothing starts the binary itself, and nothing picks an account with `--profile`.

## The paid account

It is somebody's, so a run has to be **reversible**. Four things enforce that:

1. **Nothing seeds it.** `scripts/seed` acts on `account.Free()` and nothing else; `TestNothingSeedsThePaidAccount` reads its source.
2. **A test acts only on what it made.** It never lists the account's own calendars, vaults, messages or files and acts on what it finds. There is real data there, and a filter that matches more than it meant to is the mistake this rules out.
3. **Some commands are refused outright.** `paid.Restrictions()` names them and why, and `runAs` refuses before a process starts. `paid.FixtureOnly()` names the ones only the fixture may run. `tests/rules` checks that each one names a real command - a rule guarding a command nobody can type guards nothing and makes the list look more protective than it is.
4. **The account is photographed before and after.** Every listing, all four settings pages, the alias's contacts, the newest slice of the inbox. Anything left behind or missing fails the run and is named in a box. This is the one that catches a mistake nobody predicted; the other three are promises, and this checks them.

Only the newest slice of the inbox is looked at: the rest is somebody's real mail and none of the suite's business.

**What is allowed is what can be put back**, and the photograph is what decides the argument. The auto-reply looked reversible - `autoreply get` reads back everything `set` accepts - and is not: Proton keeps the last message even while it is off and offers no way to clear it, so a run leaves its own text in somebody's real settings for good. Turning it off restores the behaviour and not the state, and the photograph compares the state. So it is refused, and `PUT /mail/v4/settings/autoresponder` is a declared gap rather than a test. Lowering the version-history retention is refused for a blunter reason: Proton discards the revisions and nothing brings them back.

### The one fixture the suite reads and never makes

An alias address **cannot be un-minted**: deleting the item leaves the address spent for good. So `fixture.PaidAlias` names an alias somebody created once by hand, every run hangs contacts off that one and removes them again, and a run that cannot find it says what to run:

```
the paid account has no Pass alias called "proton-cli fixture", and the suite never makes one. Create it once:

    proton --profile paid pass aliases create --prefix protoncli --name "proton-cli fixture"
```

The name sits outside `fixture.TestPrefix` on purpose: `fixture.Sweep` deletes everything carrying that prefix, and this is the one fixture that must survive.

### Mail a run causes Proton to send

Sharing something makes Proton write to the owner when the other side answers, and no setting on this end turns that off. `runAs` counts those the moment it sees an invitation answered, and the run **sweeps its own notices**: mail from `no-reply@proton.me` matching one of `paid.Notices()` that arrived after the run began, moved to the **trash**, never deleted, because it is real mail. The sweep runs before the photograph, so what it clears is not then reported as something the run left behind - and anything it fails to clear stays, and the photograph names it.

Adding a test that makes Proton write to the account means adding its subject to `paid.Notices()`.

## One test per feature, on the account that can answer it

There are no plan-gated skips, and `tests/rules` fails on one. A skip in place of a test is a run that reports success for having done nothing - and it had the same feature tested twice, once in a test that always skipped and once in a test nobody ran.

**A skip is for something about the world that no run can arrange.** Two are left in the whole suite, both about Pass Monitor needing an address that has actually been in a breach. Everything else is a `t.Fatal`: a message that never arrived, an invitation that never came, a fixture that is not there. A test that quietly did nothing is a worse answer than a red one.

## The suite runs one test at a time

It is bound by waiting for Proton, so overlapping would be faster - but what gives out first is whatever the free plan meters hardest, and rate limiting arrives before any time is saved. So nothing calls `t.Parallel()`, and `TestNoTestAsksToRunInParallel` says so.

The one safeguard that remains is the one that does something: **a single 429 fails the run.** The client backs off and would very likely succeed, so the suite would otherwise pass and teach nobody anything. Proton's first sign of displeasure stops the run and says to give the account a few minutes.

## Fixtures, and the sending allowance

The free accounts allow **50 messages an hour and 150 a day**, counted per recipient. That, not the wall clock, is what decides how often the suite can be run. So a message is only sent when the sending is the thing being tested; everything that merely needs *a* message of some shape reads one the account already holds.

`tests/fixture` declares what an account holds. Nothing is made up front: the accessor a test calls finds its fixture, makes it if the account has not got it, and remembers it for the rest of the run. That is what makes a run cost only what it uses - `just test-one TestMailMessagesList` never touches Pass, Drive or Calendar.

| Accessor | What it gives you | Use for |
|---|---|---|
| `plainMail(t)` | `(msgID, convID, subject)` - a delivered self-mail, plain body, no attachments, body contains its subject | reading, formats, body-only, redirects, summaries, search hits |
| `quotedMail(t)` | `(msgID, subject)` - body carries the canonical `On <date>, <name> <addr> wrote:` block | strip-quotes assertions |
| `attachedMail(t)` | `(msgID, convID, attID, attName)` - one regular attachment **and** one inline image | attachments, and telling the two dispositions apart |
| `mutableMail(t)` | `msgID` of a message this test may change and change back | mark / star / move / trash round-trips |
| `alias(t)` | `(ref, address)` - the primary account's Pass alias | anything that reads an alias |
| `paidAlias(t)` | the same on the paid account | alias contacts |
| `pinned(t, profile, what, name)` | one row of a declared collection | a vault, a label, a folder in Drive |

**Rule of thumb:**

- **Read-only** → a fixture.
- **Mutating** → `mutableMail(t)`, or make your own and clean it up.
- A test of the **send path itself** (attachments, inline images, HTML, scheduled, expiring, encrypted-for-outside, `--eml`, cross-account) → send your own. Each distinct send shape keeps exactly one such test: that is what makes a change to Proton's send path fail something.
- A test whose subject is a **flag on the parent** (`reply` setting `IsReplied`) → send your own, because a fixture parent would already be flagged and the assertion would pass for the wrong reason.

`just seed` asks for all of it at once, through the same declaration, for putting a free account in shape by hand. It is the only thing that sweeps collections a run never looks at; a run sweeps what it lists, which costs nothing because it was listing anyway.

## Writing a test

**Arrange → Act → Assert.** Register cleanup **immediately after creation**, before any assertion that might fail - including in the tests about deletion, which might fail before reaching the delete step.

```go
func TestDriveItemsFoo(t *testing.T) {
    folder := "/" + testID() + "-foo"
    runOK(t, "drive", "items", "create", folder)
    cleanupRun(t, "Delete: proton drive items delete "+folder,
        "drive", "items", "delete", folder)

    stdout := runOK(t, "drive", "items", "list", folder)

    assertContains(t, stdout, "expected-string")
}
```

- `cleanupRun` for a CLI command, `cleanup` for anything else. Both are `t.Cleanup`, so they run on failure.
- **Never clean up a fixture.** The account holds it between runs on purpose; deleting it makes the next run make it again and spend the allowance it exists to protect.
- Finding the thing already gone - exit 3 - is the job done, not a failure.
- A cleanup that could not do its job prints a box with a copy-pasteable command.

Artifacts use the `proton-cli-test-{ms}-{rand}-{purpose}` prefix from `testID()`, so they are identifiable in the Proton UI and a sweep can find what an interrupted run left behind. Never a short or common name that could collide with real data.

## Local validation precedes the network

Anything judgeable from the command line alone must fail **without a session**, and its test belongs in `tests/offline`, where it costs 5 ms and no credentials against 500 ms and a set of them here. An unknown setting key, a value outside a declared domain, a colour off Proton's palette, a missing required flag, a file that holds no events, a selection that names nothing.

This holds because **no step asserts that an account exists, and none of them unlocks its keys**. Both requirements belong to the request: the client holds the session one (`SetSessionGuard`) and a service holds the key one, asking for the hierarchy where it decrypts. So a command judges what it can judge and only then finds out whether anyone is signed in.

One place keeps the requirement earlier, on purpose: **a dry run of a mutation that reaches Proton**, because a preview is a claim about what the command would do, and without an account it would not do it. So `drive items delete --all` and `mail settings filters reorder` exit `2` offline and their tests stay live. The two commands that change this machine instead - `update` and `uninstall` - declare `kit.OnThisMachine` and preview signed out, because that is what they would have done.

## What a run costs, and how to know

A run is spent almost entirely **inside invocations of the binary** - measured, 100% of the wall clock. So there are two ways to make it faster: fewer invocations, or cheaper ones. An invocation's cost is the **depth of its request graph**, not the number of assertions. Do not guess - measure:

```bash
just test-report                 # per command: invocations, time, requests, overlap
just test-report TestCalendar    # or one slice of it
just coverage                    # re-record which of Proton's API the suite reaches
```

`overlap` is 1.0 for a strict chain and higher when requests were made together. The report ends with **chains worth flattening**: commands with several requests and an overlap near 1.0. That list is the work, in order.

### The guard rail

The live suite is the only thing that would notice Proton changing an answer, and only for requests it actually makes. So both halves are written down:

- **what the suite reaches** - `tests/api-coverage.golden`, recorded by `just coverage` from a real run. Only requests Proton **answered** count: an endpoint listed on the strength of a 401 is a gap wearing the clothes of coverage.
- **what the CLI can send** - read from the source by `TestEveryRequestTheCLICanSendIsOneTheSuiteSends`, which needs no account and runs in `just test-fast` on every push.

A request the CLI can send that the golden does not hold **fails the build**. Two ways out, both argued in that test's own file: `unreachable` for what no run could do, and `untested` for a gap somebody chose to leave, reported on every run rather than passing quietly. "The accounts do not have the plan for it" is no longer one of them.

The honest limit: a path built in a variable is invisible to the extractor, which is why the whole `kit.Settings` write path escapes it. Write the path in the request literal.

## Conventions the tests rely on

### stdout = the new reference

Every create command writes **just the new reference** - one line, no JSON, no trailing text - to stdout and `✓ …` to stderr, so `ID=$(proton ... create ...)` works. `assertBareID` and `assertBarePairRef` check it where the thing is made.

### Exit codes

| Code | Meaning |
|---|---|
| 0 | success |
| 1 | user error (bad flag, missing arg, invalid input) |
| 2 | auth |
| 3 | not-found (REF matched no resource) |
| 4 | conflict / ambiguous (REF matched multiple resources) |
| 5 | network / 5xx |
| 6 | refused by the confirmation policy |
| 130 | cancelled via Ctrl+C |

### Output format

`--output text|json|yaml`, default `text`. Every collection comes out as an envelope keyed by its plural noun, always with a `count`:

```json
{ "messages": [ … ], "count": 3, "total": 47, "page": 0, "page_size": 3, "has_more": true }
```

`runJSONArray` unwraps it and cross-checks the count. Keys are `snake_case`, enumerated values are names rather than Proton's numbers (`"type": "file"`, not `"type": 2`), and IDs are never shortened. A mutation emits a result object rather than a bare ID, so `--output json` always means JSON.

### REF arguments

Every command that takes an ID also accepts a substring search term; an ambiguous match is exit 4 with the candidates on stderr. Two-ID references are one slash-separated token - `pass items get SHARE_ID/ITEM_ID`, `calendar events get CALENDAR_ID/EVENT_ID` - and short IDs work on both halves.

**Drive addresses items by `PATH`**, because that is what Proton resolves and what a person means. Things with no place in the tree - trashed items, photos, albums - are addressed by the `REF` their list showed.

### Flags before the positionals

Proton IDs are Base64URL, so about one in sixty-four begins with `-`. `preprocessArgs` inserts `--` ahead of it, which makes everything after it positional - so a flag written *after* a leading-dash full ID arrives as an argument and the command fails with "accepts N arg(s)". Whether it happens depends on which ID the account handed out, so writing the flags last makes a test fail roughly one run in sixty rather than never:

```go
runOK(t, "mail", "messages", "attachments", "download", "--dest-dir", dir, msgID)   // always works
runOK(t, "mail", "messages", "attachments", "download", msgID, "--dest-dir", dir)   // until the ID starts with '-'
```

`runJSON`, `runJSONArray` and the consenting runners all put their flags in front for the same reason, so `--` is never needed in a test - for a full ID or a short one. The copy-pasteable commands in a `cleanupRun` description do not need it either.

## Known limitations

- `calendar settings calendars delete` hits an endpoint Proton guards behind an elevated session. Nothing in the command arranges that: the client elevates when the server asks, using the password `runAs` supplies through `--password-file`, and drops the scope again.
- `drive trash list` and `drive trash empty` cover every volume the account has, photos included, so a test that empties the trash empties the photo library's too.
- Proton allows only its own accent colours for labels, folders, calendars and contact groups (`#8080FF`, `#3CBB3A`, …) - see `ACCENT_COLORS` in the WebClients source. The CLI refuses anything else locally, before a request.
- Mail-delivery latency is inherent: a self-mail's inbox copy lands a few seconds after the send. Only the tests whose subject is the send pay it.
- `TestDriveItemsUploadManyBlocks` uploads 44 MiB and takes about 20 seconds. It is the only test that makes the CLI ask for more than one batch of block links, so it is the only one that would notice if Proton lowered the number a single request may ask for. It stays.
