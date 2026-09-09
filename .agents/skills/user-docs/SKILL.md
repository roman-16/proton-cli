---
name: user-docs
description: Writes and revises proton-cli's user-facing documentation - the README, the hand-written pages under docs/, and the Short, Long and examples that --help and the generated reference are built from - so it states what the CLI does and refuses, keeps one home per topic, and carries no mechanism, rationale or Proton internals. Use when adding or changing a page in docs/, the README, a command's help text or its examples, or when reviewing documentation in a pull request.
---

# User docs

Write for the person who has proton open in another window and wants to get something done, or who is deciding whether to install it. Both want the contract: what a command does, what it refuses, what to do instead. Neither wants to know how it works inside or why it was built that way.

## What counts as user docs

| Surface | Where it is written | Published as |
| --- | --- | --- |
| README | `README.md` | The repository front page |
| Guides and cross-cutting pages | `docs/**/README.md`, `docs/*.md`, `docs/using/`, `docs/help/`, `docs/about/faq.md`, `docs/about/security.md` | The site, one page per file |
| Reference | `Short`, `Long`, flag usage and `internal/cli/examples.go` | `--help`, and `docs/<app>/<collection>.md` after `just docs` |
| The agent skill | `internal/cli/skill.md` | `proton skill` |

Not user docs, and not held to this skill: `CONTRIBUTING.md`, `AGENTS.md`, `tests/`, `CHANGELOG.md`.

Inside an app's directory every markdown file except `README.md` is generated. Never edit one; change the `Short`, `Long`, flag usage or example and run `just docs`.

## Pick the page kind first

Each kind serves one need and may contain only what serves it.

| Kind | Page | Contains | Never contains |
| --- | --- | --- | --- |
| Tutorial | `docs/first-commands.md` | One safe path from install to a useful command, every step shown | Options, alternatives, reference tables |
| Guide | `docs/<app>/README.md`, `docs/using/*.md` | Tasks, one heading each, in the order people meet them: the command, then the constraint that would surprise them | The full flag list (link the reference), reasons |
| Reference | `Long`, flag usage, examples | Every flag, its values, its default, how it fails; two or more examples | Instruction, argument, anything true of another command too |
| Troubleshooting | `docs/help/troubleshooting.md` | A symptom as the heading, the message as the reader sees it, then the fix | Explanations of what went on inside |
| Limits | `docs/help/limits.md` | A command a web client has and `proton` does not, and what to do instead | Why it is missing, or a gap that is not `proton`'s to close |
| FAQ | `docs/about/faq.md` | Questions somebody asks before installing, answered in a paragraph with a link | Anything a guide owns |
| Security | `docs/about/security.md` | What leaves the machine, what is on disk, how to reduce risk | Cryptographic design, key hierarchies |

A new page is rare. Before creating one, find the page that owns the topic (next section); the new material almost always belongs in it.

## One home per topic

A fact written twice drifts; the exit-code tables once disagreed. A topic has one section that owns it, and every other page links to that section with one sentence that names the concern and the owner. Never restate.

| Topic | Owner |
| --- | --- |
| Installing, updating, uninstalling, the once-a-day update notice, installing completion | `docs/install.md` |
| What Tab completes | `docs/using/naming.md` › Tab completion |
| IDs, short IDs, handles, paths | `docs/using/naming.md` |
| Selecting many things, `--limit`, `--scope` | `docs/using/filters.md` |
| `--dry-run`, what asks first, the confirmation policy, `deny` | `docs/using/confirmations.md` |
| Response shapes, JSON, colour, exit codes and what to do about each | `docs/using/output.md` |
| Pipelines, cron, systemd, what to know before automating | `docs/using/scripting.md` |
| The config file, environment variables, files on disk, global flags, turning the log off | `docs/using/settings.md` |
| Signing in, profiles and several accounts, sessions on other devices, commands that ask for the password again | `docs/account/README.md` |
| `proton report`, what the diagnostic log holds and never holds | `docs/help/troubleshooting.md` › Reporting a bug |
| CAPTCHA, rate limits, a session that expired | `docs/help/troubleshooting.md` |
| Search lag | `docs/mail/README.md` › Search |
| What leaves the machine, what protects the session file | `docs/about/security.md` |
| The grammar, the verbs, flags that mean one thing everywhere, the three spellings of none | `docs/commands.md` |

A linking sentence looks like this and no longer:

```markdown
A few commands ask for your password even when you are signed in; they are listed in [Account](../account/README.md#commands-that-ask-for-the-password-again).
```

## What never goes in

Delete these on sight, in a page you are writing and in one you are editing:

- **Mechanism.** How many requests something costs, what is fetched page by page, batches, indexes, types, key hierarchies, what a compiler or a test guarantees.
- **Rationale.** Any clause after "because", "which is why", "so that", "the alternative was". State the constraint; drop the argument. `A filter that matches a folder and the files inside it selects the folder alone.` needs no second half.
- **Proton internals.** "Proton serves 150 rows", "Proton guards this endpoint", "as in the web client", "Proton's own wordlist". Keep the sentence only if it tells the reader what to do: `Only another Proton account can be invited.`
- **History.** "used to", "no longer", "now also", "previously". Write what is.
- **A gap that is not the tool's.** The docs cover what `proton` can and cannot do beside the web clients, and nothing else. Something no CLI is in a position to do - a workflow that lives in a browser session, an action only the phone app performs, a decision Proton makes about an account - is not a limit, not a caveat and not a note. It gets no row in Limits and no sentence anywhere. Where `proton` refuses it, the refusal on screen is the whole of what the reader is told.
- **Self-reference and filler.** "This page describes", "note that", "simply", "easily", "just", "please".
- **The author's voice.** No "we". The reader is "you"; the tool is `proton`.

A reason survives only when it changes what the reader does, and then as a fact about the tool: `A deny is not a security boundary. Anything running as you can edit the config file that declares it.`

## Sentences

- Second person. Imperative for an instruction: `Pass --zone Europe/Vienna to be sure.`
- Present tense, active voice. The subject is the reader or the tool.
- About 15 words, never more than 26. One idea per sentence; a second idea is a second sentence or a list.
- Condition before instruction: `With no terminal to ask, pass --password-file.`
- One term per concept, the project's terms: profile, session, `REF`, short ID, app, collection, verb, folder and label (a message has one folder and any number of labels), vault and item, occurrence and series. Never rename one mid-page.
- A numbered list only when order matters; bullets otherwise; items parallel in form.
- Tables for anything with more than two attributes: name, values, default, meaning.
- Headings are what a reader would type into a search box: `Sign in without a terminal`, not `Going headless`. Sentence case. A page's H1, sidebar label and slug are the same words.
- The answer first. A page's lead paragraph says what the page lets the reader do, in one or two sentences.
- Hyphens only. No em-dashes, no en-dashes.

## Commands and output

- A line the reader will copy goes in a `bash` block with no prompt:

  ```bash
  proton mail messages list --unread --folder all
  ```

- A transcript that shows output goes in a `console` block, `$ ` before each command, output as the build prints it:

  ```console
  $ proton account get
  Email:       you@proton.me
  Profile:     default
  Session:     valid
  ```

- Output shown is output the build prints. Take it from a real run or from the golden files under `internal/ui/testdata/`; never write it from memory.
- Placeholders are the names in `kit.Placeholders`, in capitals: `REF`, `PATH`, `EMAIL`. A value the reader replaces in a copyable line is a realistic example instead: `alice@proton.me`, `/Documents/report.pdf`, `Invoice #2291`.
- Full flag names in every example; a single-letter form only where the page is about the short forms.
- Never a real secret, token or address. A password comes from `--password-file` or `--password-stdin` in every example, as it does in the tool.
- Every invocation must exist. `TestEveryCommandTheDocsShowExists` resolves each one, including inline spans that start with an app name, against the command tree and fails on a command or flag that is not there.
- Show the smallest example that does the task, then the one variation people reach for. Everything else is the reference's job.

## Help text

- `Short` names the command in under 50 characters, active present tense, no full stop: `Rename a vault, or change how it looks`.
- `Long` opens with the `Short` as a sentence, then says only what would surprise the reader: a constraint, a default, a value list, a way it fails. Two to four sentences. No argument for the design, no mention of Proton's servers.
- Flag usage is a fragment starting with a capital, no full stop: `Folder or label to look in (default: inbox)`.
- Every leaf has examples in `internal/cli/examples.go`: one basic, one showing the flag people reach for, more only when a flag is easy to misread.
- After changing any of them: `just docs`, and `just golden` if one of the three golden help screens moved.

## Procedure

1. Name the reader's task, and pick the page kind for it.
2. Find the owner page for the topic. Read the whole page before touching it.
3. Write the section: the command, then the constraint, then a link to whatever another page owns.
4. Strike every clause the "What never goes in" list names, in what you wrote and in the paragraphs around it.
5. Take every transcript from a run or a golden file.
6. Run `just test-fast` for the docs test and `just lint`, which regenerates the reference and fails the site build on a link that goes nowhere.

## Checklist

- Does every sentence state what proton does, refuses or requires - never how, never why?
- Is each topic on the page one that this page owns, or a one-sentence link to the page that does?
- Would a reader arriving from a search engine understand this page without the one before it?
- Does every heading read like a search query?
- Is every command real, every flag spelled in full, every transcript from a run?
- Is there any "because", "used to", "simply", "we", or a dash that is not a hyphen?
- Did `just test-fast` and `just lint` pass?
