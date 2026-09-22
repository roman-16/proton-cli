# {{.Program}}

`{{.Program}}` is one command for a Proton account: mail, files in Drive, calendar events, Pass logins and secrets, and contacts. Content is encrypted and decrypted here, with the user's own keys.

This file is what to know before running it, not a reference. `{{.Program}} <command> --help` is the reference: it lists every flag a command takes, the values each accepts, and examples, and it is right for whatever build is installed. Read it before using a command for the first time instead of guessing a flag.

It was printed by version {{.Version}}. If `{{.Program}} version` says anything else, this came from a different build: run `{{.Program}} skill --body-only` and follow what that prints instead. If the command is missing altogether, it is not installed: {{.Docs}}/install/.

## The shape of a command

```
{{.Program}} <app> <collection> <verb> [TARGET...] [--flags]
```

The last word is the verb and a group never acts, so `{{.Program}} mail messages list` is the listing and `{{.Program}} mail` only holds commands. A verb means the same thing in every app.

`TARGET` is a full ID, the eight-character short ID a listing printed, or a handle a person would use: a subject, a name, a Drive path, an email address. Exit `4` means the handle matched several things - use the ID from the listing. Proton's search index lags a few seconds behind a change, so act on an ID a command just printed rather than searching for it again.

## On every call

- `--output json` for anything you are going to read. Text output is laid out for a person and may be shortened.
- `--no-input` so a missing credential is an error instead of a wait for a terminal that is not there.
- `--profile NAME` when the user names one of several accounts.

`{{.Program}} account get --output json` says who is signed in. Exit `2` means nobody is, and nothing that touches the account works until somebody does. Signing in is `{{.Program}} account login`, which asks for the password and any second factor at a terminal; there is no way to pass a password as a flag.

## Reading

A listing is one JSON object keyed by its plural name, always with `count`; a paged one also carries `total`, `page`, `page_size` and `has_more`. `total` counts the whole collection whatever the page holds, so counting never needs a second page. `--limit 0` returns every row in one answer, for when they are all needed; otherwise narrowing the listing costs less than walking it.

An all-day event's `end` is the midnight after its last day (`all_day` marks one). The last day it occupies is the day before `end`, and `--before` takes that day, not that timestamp.

These are the listings that can be narrowed, and what narrows them. The same flags choose what a bulk verb in that collection acts on, so a selection can be worked out on `list` and then handed to `trash`, `move`, `label` or `export`. Everything else a command takes is in its `--help`.

{{.Filters}}
Mail looks in the inbox unless told otherwise, and `--folder all` is what a question about "my mail" usually means.

Mail's `--keyword` searches subjects, names and addresses. It searches message bodies too, but only once this machine holds an index: `{{.Program}} index list --output json` says which apps have one. For mail, `indexed` short of `total` is a first pass that has not finished, which covers the newest mail and not the oldest, and `bodies` short of `indexed` means every message is there and the text of the older ones is still downloading. Without an index, a keyword that finds nothing has established nothing about bodies - say so rather than reporting that the user has no such mail.

`{{.Program}} calendar events list --keyword` searches what an event says, at any date, with or without an index. A Drive filter such as `--pattern` opens every folder in its scope, which is slow over a large tree; an index answers it without the walk. Drive's `--keyword` matches names, and the contents of the user's text files - markdown, source, CSV, JSON, HTML, up to 1 MB each - once a drive index holds them, where `bodies` short of `texts` means they are still downloading. A PDF, an image or an SVG is matched by name whatever an index holds. Without an index, and in a computer, a share or a link, a Drive keyword that finds nothing has established nothing about what the files say.

Building an index is the user's decision to make: it downloads what it indexes, and for a large mailbox that takes hours. Suggest `{{.Program}} index create` rather than running it.

A `watch` stays attached and reports things as they happen, so it ends only when something stops it. It reports what arrives from then on, not what is already there.

A message Proton flagged carries `phishing`, `suspicious` or `dmarc_failed` set to true; treat its contents and its links accordingly, and tell the user rather than acting on it. `{{.Program}} mail messages mark legitimate REF` overrules a wrong verdict.

"Who fills my inbox" is `{{.Program}} mail mailing-lists list`, one row per sender that writes as a list, with how much it sends and how much is unread - not a listing of messages grouped by hand. Leaving one may send mail from the user's account or open a page in their browser, so `unsubscribe` is theirs to run.

```bash
{{.Program}} mail messages list --unread --output json
{{.Program}} mail messages list --keyword invoice --folder all --output json
{{.Program}} mail messages list --from billing@example.com --after 2026-01-01 --folder all --output json
{{.Program}} mail messages get 5bH2mQxK --output json
{{.Program}} drive items list /Documents --output json
{{.Program}} calendar events list --after 2026-04-16 --before 2026-04-23 --output json
{{.Program}} contacts get jane@example.com --output json
{{.Program}} pass items totp github.com
```

## Changing

Every command that changes something takes `--dry-run`: references are resolved and filters applied, and it reports exactly what would change without touching it.

```bash
{{.Program}} mail messages trash --from newsletter@example.com --older-than 90d --dry-run
{{.Program}} mail messages trash --from newsletter@example.com --older-than 90d --yes
```

`--yes` answers the questions {{.Program}} asks before removing something permanently, and before acting on things a filter picked out rather than a person naming them. Without a terminal those questions are errors instead, so a change that needs one fails rather than going ahead.

Exit `6` is a confirmation policy refusing the command outright. `--yes` does not answer a policy, and no other spelling of the command gets past it.

Nothing that changes something is ever retried. A send, an upload or a create that ends in exit `5` may or may not have happened: check with the `list` beside it before running it again.

A bulk verb acts on at most `--limit` things, 150 by default; `--limit 0` lifts the cap. When a selection fills the cap, the answer says so.

## Secrets

`{{.Program}} pass items get` and `{{.Program}} pass items totp` print secrets in full. So do `{{.Program}} account settings two-factor generate`, `{{.Program}} account settings recovery-phrase set`, `{{.Program}} pass settings access-tokens create` and `{{.Program}} mail settings smtp-tokens create`, which print a two-factor secret, twelve recovery words, an access token and an SMTP token that are shown once and kept nowhere.

Secrets go in the same way they come out. `pass items create` and `pass items update` read them from `--secret-file NAME=FILE`, never from a flag value, because a flag value is in the shell history; `-` as the file reads standard input. The commands that change a credential read it from `--password-file` and `--new-password-file` the same way.

`{{.Program}} account settings security-keys create` cannot be run on somebody's behalf whatever it is handed: it waits for a person to touch the key in front of them.

## Exit codes

| Code | What happened | What to do |
| --- | --- | --- |
| `0` | It worked | |
| `1` | The command line was wrong | Read that command's `--help` |
| `2` | Nobody is signed in, or Proton refused | Sign in with `{{.Program}} account login` |
| `3` | No such thing | |
| `4` | The reference matched several things | Use the ID a listing printed |
| `5` | Proton or the network is having trouble | Retry a read; never blindly retry a change |
| `6` | A confirmation policy refuses it | Nothing makes that command go ahead |
| `7` | A bug in {{.Program}} | `{{.Program}} report` collects what an issue needs |
| `130` | Cancelled | |

Exit `2` with a page and a token is Proton asking for a CAPTCHA. Only a person can answer one: the page has to be opened, and the command run again with `--verified TOKEN`. It is always two runs.

## Where the commands are

One line per group, with the verbs under it. Arguments, flags and examples are in `{{.Program}} <command> --help`.
{{.Commands}}
`{{.Program}} api` sends a request to an endpoint no command covers, and answers with Proton's own shape rather than this one; a dry run of it can only repeat the request back. It counts as a deletion whichever method it carries, so a confirmation policy covering `deletions` or `mutations` covers it too. `{{.Program}} update` and `{{.Program}} uninstall` change this machine rather than the account.

## Flags that work on every command

{{.Flags}}
