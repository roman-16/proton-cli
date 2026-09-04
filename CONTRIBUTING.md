# Contributing

Thanks for helping out. Issues, ideas, and pull requests are all welcome.

## Scope

proton-cli mirrors what the Proton **web clients** let a user do. If an action isn't possible in the official web UI, it doesn't belong here, even when an API endpoint for it exists. Web-client parity beats API completeness.

## Getting set up

The repository uses [devbox](https://www.jetify.com/devbox) and [direnv](https://direnv.net/) to pin the toolchain:

```bash
git clone https://github.com/roman-16/proton-cli.git
cd proton-cli
direnv allow      # or: devbox shell
```

Without devbox you need Go 1.26 or newer, plus `actionlint`, `charm-freeze`, `golangci-lint`, `goreleaser`, `just`, `nixfmt`, `protoc`, `protoc-gen-go` and `shellcheck` for the tasks below.

## Everyday commands

`just --list` is the full set. The ones you'll reach for:

```bash
go build ./cmd/proton  # quick build (no CAPTCHA helper)
just build             # release-shaped binary
just run -- mail messages list
just lint              # format, regenerate, and check everything; run before every commit
just test-fast         # everything decidable without Proton
just docs              # regenerate the command reference from the tree
just golden            # update the pinned bytes of every response and help screen
just flake             # build the nix package, after a dependency bump
just snapshot          # every release artifact, without publishing
just demo              # regenerate the README demo images
just web-dev           # serve the documentation site while you edit the pages
just update            # move every dependency and tool to the latest version
```

`just lint` has to pass with no findings, and has to leave the tree clean. It formats Go and Nix, regenerates the command reference, builds the documentation site, and checks the workflows, the release configuration, the shell scripts and the Go, so a stale generated file fails the same way a lint finding does. CI runs the same recipe.

## Documentation

`docs/` is written twice over, on purpose.

**The guides are prose somebody wrote, task by task.** An app's guide is the `README.md` of its directory: `docs/mail/README.md` is the Mail guide, published at `/mail/`. The pages under `docs/using/`, `docs/help/` and `docs/about/` are written by hand too.

**The reference is generated** by `just docs` from the command tree. Inside an app's directory, every markdown file except `README.md` is generated, one per collection: `docs/mail/messages.md` documents `proton mail messages`, and `proton mail messages send` is published at `/mail/messages/#send`, so a URL reads like the command that leads to it.

Never edit a generated file. Change the command's `Short`, `Long`, flag usage or entry in `internal/cli/examples.go` and regenerate.

The two never share a file. A page that is half prose and half generated cannot be regenerated, which is how a generated section comes to be edited by hand and then silently overwritten.

### How to write it

**Write for the person running the command, not for the person reviewing the design.** A `Short` names the command. A `Long` says the thing that would otherwise surprise them: a constraint, a default, a value list, a way it can go wrong.

Why the design is that way is worth writing down, but it belongs in `docs/about/why.md`, which a reader can skip. A `Long` that argues its own case is read by everybody, on a help screen and again in the reference, whether they wanted it or not.

The same rules hold for the guides:

- One idea per sentence. Aim for about 15 words, and no more than 26.
- Put the answer first, the condition before the instruction, and the reason last or on the page for reasons.
- Headings are what a reader would type into search, not a phrase that reads well in sequence.
- A page's title, its sidebar label and its slug are the same words.

A help screen and a reference page agree about where a command is documented because `kit.Reference` answers for both. Adding a command therefore documents it in three places at once, and none of them by hand.

[The site](https://proton-cli.lerchster.dev) is `docs/` rendered. The pages stay plain markdown that reads on GitHub, and `web/scripts/import-docs.ts` derives what a site needs from what a page already has: the title from its heading, the description from its lead paragraph, and absolute links from the relative ones. So a page is edited in `docs/` and nowhere else, and a merge to `main` publishes it.

`just web` builds it the way CI does, which type-checks the site and fails on a link that goes nowhere. `just web-dev` serves it while you write.

The site is published from its own repository because GitHub gives a repository one Pages site, and this one's is [the APT repository](https://roman-16.github.io/proton-cli/). Keeping them apart is what stops a documentation change from reaching what `apt update` reads.

## Images

The terminal panel in the README is a recording of a real session, rendered with [freeze](https://github.com/charmbracelet/freeze). It records as `primary`, the same account the integration tests use, so it needs no credentials of its own:

```bash
just demo
```

[`scripts/terminal-demo/README.md`](scripts/terminal-demo/README.md) explains the pieces and the rules that keep the recording honest.

The card a link preview shows is `assets/og.svg`, hand-drawn, and `assets/og.png` is what it renders to - the only format the places a link gets pasted will draw. Edit the SVG and run `just og`; the site copies the PNG into place on every build. GitHub's own preview is a repository setting rather than a file, so the same PNG has to be uploaded there by hand.

## Tests

There are two tiers, and one thing separates them: **does answering the question need Proton.**

`just test-fast` is everything that does not - unit, golden, conformance, the rules the live suite is held to, and an offline suite that runs the real binary with no session and the API pointed at a dead port. No credentials, no network, about two seconds, safe to run any time:

```bash
just test-fast
```

`just test` is that, then the suite under `tests/live` - **integration tests against the live Proton API**. They create and delete real data on three accounts and take the best part of an hour.

```bash
just test-one TestMailMessagesList   # a single live test
just test                            # every test there is
```

Nothing else decides a tier. A subscription is a property of an account, not of a question, so the tests that need one act as the paid account like any other and run in the same pass. There is no separate recipe, no build tag and no "this needs a plan" skip.

**All nine variables are required**, and the suite refuses to start rather than skipping what it cannot reach:

```bash
export PROTON_CLI_TEST_PRIMARY_USER=primary@proton.me        # never your own account
export PROTON_CLI_TEST_PRIMARY_PASSWORD=...
export PROTON_CLI_TEST_SECONDARY_USER=secondary@proton.me
export PROTON_CLI_TEST_SECONDARY_PASSWORD=...
export PROTON_CLI_TEST_SECONDARY_SECOND_PASSWORD=...
export PROTON_CLI_TEST_SECONDARY_EXTRA_PASSWORD=...
export PROTON_CLI_TEST_PAID_USER=...                         # a real account, see below
export PROTON_CLI_TEST_PAID_PASSWORD=...
export PROTON_CLI_TEST_EXTERNAL_RECIPIENT=you@example.com    # a mailbox outside Proton
```

`just login` signs every account in - it needs a terminal, because Proton may ask for a CAPTCHA and only a person can answer one - and `just seed` fills the two free ones.

The **secondary** account carries the modes nothing else would reach: it is in Proton's two-password mode, and its Pass is protected with an extra password. `PROTON_CLI_TEST_EXTERNAL_RECIPIENT` is required for the same kind of reason - encrypting to somebody with no Proton account, and emailing an invitation to an attendee with no Proton calendar, are branches no Proton account can enter.

The **paid** account is the one exception to "never your own": Proton gates a good deal of what the web clients offer behind a subscription, and buying a second one to test with is not a reasonable thing to ask. So it is a real account, under one rule - **a run has to be reversible.** Nothing seeds it, a test acts only on what it made, a handful of commands are refused outright, and the account is photographed before and after so anything left behind fails the run and is named.

It needs no setup. The one thing that outlives a run is a single Pass alias, which the first run makes and every run after reuses - an alias address cannot be un-minted, so the suite spends one for the life of the account rather than one per run, and `tests/rules` fails on a test that tries to make its own.

Where a feature cannot be exercised reversibly, it is not exercised: the auto-reply and single-item Pass sharing are declared gaps in `internal/cli/coverage_test.go` rather than tests.

Never point the primary or secondary at an account you care about. Credentials can go in a local `.env` file (see `.env.example`), which the devbox shell loads automatically.

Unit test files are named after the file they test (`size.go` → `size_test.go`). The live suite is one file per collection, named the way the command is: `mail_messages_test.go`, `pass_vaults_test.go`.

## Project layout

| Path | Contents |
| --- | --- |
| `cmd/proton/`, `cmd/proton-cli/` | Entry points, one per name the binary answers to |
| `internal/cli/` | Cobra command tree, help screens, flags, exit codes |
| `internal/cli/kit/` | The declared language: verbs, placeholders, shared flags, reference URLs |
| `internal/service/` | Per-product logic (mail, drive, calendar, contacts, pass) |
| `internal/proton/` | API client, request plumbing, error types |
| `internal/crypto/`, `internal/account/` | Key handling, SRP login, sessions |
| `internal/ui/` | Output formatting: tables, records, documents, JSON, YAML, progress |
| `tests/live/` | Live-API integration tests, one file per collection |
| `tests/` | Everything the suite is built from, and the rules it is held to - all of it decidable without Proton |
| `scripts/` | Command-reference and OpenAPI generators, installers, release helpers, README demo |
| `assets/` | Logo and the generated README demo images |
| `docs/` | User documentation: guides by hand, the command reference generated |
| `CHANGELOG.md` | What each version changed for the people using it, and the thing that releases it |

## Working with Proton's API

Proton's web client is the reference for endpoints, payload shapes, and crypto flows:

```bash
cd /tmp && git clone --depth 1 https://github.com/ProtonMail/WebClients.git
```

`openapi.yaml` in the repository root is generated from that source and covers roughly 740 endpoints. Regenerate it with:

```bash
just openapi
```

A weekly workflow does the same thing and commits when upstream changes. See [`scripts/README.md`](scripts/README.md).

## Pull requests

- Keep the change focused, and match the surrounding style.
- Run `just lint` and `just test-fast`.
- Add or adjust integration tests when you touch behaviour that they cover.
- Update the guides in `docs/` and, when it's user-facing, the README. Run `just docs` rather than editing a generated reference page.
- Leave [`CHANGELOG.md`](CHANGELOG.md) alone; its entries are written when a release is cut, from the commits that went into it. Write the commit message so the entry can be written from it.
- Commit messages follow [Conventional Commits](https://www.conventionalcommits.org/) (`feat:`, `fix:`, `docs:`, `build:`, …). Release notes are not generated from them: what a release says is written by hand in `CHANGELOG.md`, because a commit documents a step in the source and an entry documents a difference someone can feel.

## Releases

[`CHANGELOG.md`](CHANGELOG.md) is the release button. Add a version section to it in [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) form and merge it to `main`; that is the whole of it. The version, the tag and the release notes all come from the one file the change was reviewed in, so shipping is a decision made once, in a diff, rather than a version typed into a form afterwards.

The section is written when the release is cut, from the commits since the last tag, so there is no `[Unreleased]` heading accumulating between releases and a merge that is not a release leaves the file untouched. That puts the whole of a release in one reviewable diff, and it puts the burden on commit messages, which is where the reasoning is while it is still fresh.

The **Release** workflow runs when CI passes on `main`, reads the newest version section, and stops there when a release for it is already published - which is what nearly every merge does, in seconds. Otherwise it tags and hands the section to GoReleaser as the release notes. The tag is pushed last on purpose: it is fetched by users, resolved by `go install`, and the version GoReleaser derives, so nothing that outlives a failed run happens until everything that can fail has passed. GoReleaser then builds every target, publishes the GitHub release, and updates the APT repository, AUR, Homebrew tap, winget, and npm.

Because the file decides and not the run, a release that fails partway through is finished by re-running the workflow: an existing tag is reused and its own commit released rather than whatever `main` has become, and a release already published is left alone. The file is held to its format by `just test-fast`, which is also what keeps the button safe: versions move one step at a time, so after 2.2.3 the file may say 2.2.4, 2.3.0 or 3.0.0 and nothing else, and a `[YANKED]` section is never republished.

`just notes` prints the version and the notes the current file would publish. `just snapshot` runs the same GoReleaser pipeline locally without publishing, so a packaging mistake surfaces before the tag rather than after it.

## Security

Please don't file security issues publicly. [`SECURITY.md`](SECURITY.md) has the private reporting channels.
