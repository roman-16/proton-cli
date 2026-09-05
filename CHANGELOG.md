# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

Adding a version section here is what publishes a release, so this file is the one place a version is decided: see [Releases](CONTRIBUTING.md#releases). Versions that shipped before this file existed are on the [releases page](https://github.com/roman-16/proton-cli/releases).

## [3.4.0] - 2026-09-05

### Added

- `calendar events create` and `update` take `--color`, giving one event a colour of its own by accent name or hex; `events get` reports it. It colours what the reference names: one occurrence, `--onwards` for that one and every later one, or the whole series. A colour can only be changed, never taken off, and Proton's own apps draw a per-event colour only on a paid plan.
- `calendar events export` carries an event's own colour as a `COLOR` property, and `import` reads one back as the nearest of Proton's twenty accents, so a round trip keeps it.
- `contacts export` writes each address's groups as `CATEGORIES`, and `contacts import` files addresses into the groups a card names, creating any you do not have. `--no-groups` leaves them out.
- `--force-color`, or `FORCE_COLOR`, paints even when the output is piped: a pager, a multiplexer, a CI log that renders the escapes itself.

### Changed

- **Breaking.** What you write decides whether a calendar event has a time of day. `events create --start 2026-07-01` needs `--all-day` to agree, and `--all-day` beside a time of day or a duration under a day is refused. `events update --start` with a bare day moves the event and keeps its time of day instead of dropping it to midnight; `--all-day=false` gives one back, with a `--start` saying which time.
- **Breaking.** `--end` on an all-day event is the last day it runs through, not the midnight after it. A script that added a day to compensate now books one day too many.
- **Breaking.** `calendar events create` with neither `--end` nor `--duration` lasts as long as its calendar says a new event lasts, rather than a fixed hour. `calendar settings calendars get` shows that length.
- Sending stops when a recipient's contact cannot be read, instead of encrypting to whatever key Proton hands back. A contact that will not open cannot say whether it pins a key. Affects `mail messages send`, `reply`, `forward` and `mail drafts send`.
- Every change says when the run could not read part of what it acted on - in the confirmation, the dry run, the result, and as `skipped` under `--output json` - the way a listing already does.
- `contacts update`, `merge`, `keys pin` and `unpin` save over a card whose signature does not verify and say so. `update` refused outright and the others said nothing; a detached signature cannot tell tampering from a retired key.
- `calendar settings calendars get` and `contacts groups get` show a colour as its swatch and name, which their listings already did.

### Fixed

- An all-day calendar event could not be given a time of day back: a `--start` naming one was accepted, discarded, and reported as a success.
- Colour in a Windows console. Nothing there sets `TERM`, so every screen was drawn plain.
- `pass invitations accept` took the server's word for who sent an offer. The key an offer carries is now checked against the keys Proton publishes for the address it names as sender, and one nobody signed is refused; `pass invitations list` leaves a vault's name blank rather than reading it from such a key.
- A vault key written here is sealed to your primary user key alone, as Proton's own client seals it. Every key the account ever had could open one created or accepted before, retired ones included.

## [3.3.1] - 2026-09-04

### Fixed

- `contacts create`, `update`, `import` and `merge` were refused with `Invalid PGP packets` on an account holding more than one user key. A contact card is now sealed to your primary user key alone, which is what Proton's own clients send.
- `contacts update` and `contacts merge` added a second copy of the address and of every pinned key each time they ran. `contacts export` and `contacts keys list` showed one copy per past edit; running `contacts update` on an affected contact once more clears it.
- `mail settings autoreply enable` and `disable` take `--password-file`, `--password-stdin` and `--totp`. Proton guards the switch whichever way it goes, so both failed outside a terminal.

## [3.3.0] - 2026-09-04

### Added

- `proton mail settings forwarding` hands mail arriving at one of your addresses to another Proton account, end-to-end encrypted: `list`, `get`, `create`, `delete`, `enable`, `disable`, `resend`. Accepting a forwarding somebody sent you is not built - that happens in a Proton client.

### Changed

- An account or recipient using post-quantum encryption now works instead of exiting `8`. Signing in, sending, and sharing a calendar, file or vault all read Proton's ML-DSA and ML-KEM keys. `proton` still never creates one.
- Sharing with an address outside Proton exits `1` with a sentence naming the address, instead of `4` carrying Proton's own error. Affects `calendar settings calendars share add`, `pass vaults share add` and `drive items share add`.
- `proton report` links the bug form directly and no longer asks in its own words for what the form already asks for.
- `proton report` says where your settings came from rather than the file's path, which holds your home directory.

### Removed

- The `go install` row from the install instructions. It resolved to a version from before the command moved and installed nothing; use a package manager, the install script or a release binary.

### Fixed

- Sharing with a Proton address whose key this build cannot read says so, instead of calling it an address outside Proton.
- `proton report` carries the whole run. A report of a listing that made 750 requests held two lines. A run too large for an issue form is trimmed to its first and last records plus everything above debug between them, and says how many it left out; `--dest` writes all of it.
- `proton report` runs on the defaults when the config file is too broken to parse, and says what was wrong with it. It was the one command that could not run in exactly the situation worth reporting.

## [3.2.0] - 2026-09-03

### Added

- Tab completion offers back what your listings showed: where a command takes a reference, the short ID and the subject, name or address beside it. It reads what this machine remembers rather than asking Proton, so a collection you have not listed yet offers nothing and says which listing would fill it.
- Exit code `8` means the command reached a Proton feature this build does not implement. Nothing about the command was wrong, and neither a retry nor different arguments changes the answer.

### Changed

- A short ID printed by a `get` resolves in the next command. Only what a listing showed was remembered before.
- An account or recipient whose keys use post-quantum encryption says so and exits `8`. Signing in to such an account failed as a wrong password, and sending to one as an unreadable key.

## [3.1.0] - 2026-09-03

### Added

- `proton report` prints what a bug report needs: the build, your settings and a redacted trace of the run that failed. It needs no account and no network. `--all` takes every run still kept, `--dest` writes a file to attach.
- Every run records what it did to `~/.config/proton-cli/logs/`, one file per day, whatever `--log-level` says. Addresses, IDs and filenames are replaced by stand-ins before anything is written, so a log is safe to attach to an issue. The last 16 files are kept; `--no-log`, `PROTON_NO_LOG` or `no-log:` in the config file writes none.
- Exit code `7` means the failure is a bug in proton rather than something you typed. Every failure you can act on keeps the code it had.

### Changed

- A crash prints one line pointing at `proton report` instead of a Go stack trace.
- Refusals that arrived as bare text now say what to do: a pinned key that does not match the recipient's, an inline attachment with no `--html`, a reply to a message with no reply address, an unknown `pass items create --type`, an alias in a Pass import, and a `--remind` that is not a warning time.
- `uninstall --purge` also removes the diagnostic log.

### Fixed

- A listing that cannot decrypt something says how many are missing instead of dropping them in silence, and `--output json` carries a `skipped` count.
- "Failed to unlock any address keys." now says which of four things went wrong, and the log records it per key.
- `--log-level debug` shows what Mail, Drive, Calendar, Contacts and Pass log while they work. It never showed those lines before.

## [3.0.0] - 2026-09-02

### Added

- A config file. `~/.config/proton-cli/config.yaml` holds what you would otherwise retype on every command or export as its own variable - `output`, `profile`, `zone`, `quiet`, `log-level` and the rest - with a `per-profile:` section for narrowing one to an account. It does not exist until you write it, and a key it does not recognise is an error rather than a line quietly ignored. `--config` and `PROTON_CONFIG` read a different one.
- A confirmation policy says what `proton` must stop for and what it must not do at all: `--confirm`, `PROTON_CONFIRM`, or `confirm:` in the config file, scoped as narrowly as one command. A denied command exits `6` and nothing answers it - not `--yes`, not `--dry-run`, not a `--confirm` on the command line. Nothing you can write makes `proton` less careful than it is with no configuration at all.
- Pass protected with an extra password can be reached at all: the first `pass` command asks for it, and Proton then lets the session reach Pass for as long as it lives. `account login --extra-password-file` or `--extra-password-stdin` hands it over for a run with nobody to ask.
- `pass items share add|get|remove|update` shares one item and leaves the vault around it alone: what travels is the item's own key, so they can open that item and nothing else.
- `pass vaults share update` changes what somebody may do with a vault, and `pass vaults transfer` makes a member the owner of it.
- `pass shared list` shows the items other people share with you, `pass sharing list` the ones you share. An item shared with you is in no vault of yours, so `items list` never had it.
- `pass items move REF --into VAULT` puts an item in another vault, keeping its history. It gets a new ID, which the command prints.
- `pass items revisions get REF REVISION` reads an earlier version back in full, and `pass links get REF` prints a link's whole URL.
- `pass generate --words N` makes a passphrase from Proton's own wordlist, and `--generate-password` on `pass items create|update` makes the password on your machine and prints it beside the new ID, so a new login needs no file to read from.
- `--zone`, and `TZ` beside it, set the one zone a run works in: what a written time means, what a listing prints, what an event is anchored to. Every event created or re-timed says which zone that was.
- `drive trash list` shows each item's name and when it was trashed, and sorts and pages like any other listing: `--sort name|size|trashed`, `--desc`, `--page`, `--page-size`.
- `--expires never` removes an expiry, on a public link and on a message.
- The changelog and a browsable [Proton API reference](https://proton-cli.lerchster.dev/api-reference/) are on the site. The reference is generated from the same `openapi.yaml` the repository holds, served at [proton-cli.lerchster.dev/openapi.yaml](https://proton-cli.lerchster.dev/openapi.yaml) for a code generator or an HTTP client to read.

### Changed

- **Breaking.** `--output` means the response format on every command. The commands that write bytes to disk take `--dest` and `--dest-dir` for where they go - every `download`, plus `mail messages export`, `contacts export`, `calendar events export` and `pass export`. A script passing a path to `--output` now fails, naming the three formats it accepts.
- **Breaking.** A secret is never a flag value. `pass items create|update` no longer take `--password`, `--totp-uri`, `--totp-field`, `--hidden`, `--number`, `--cvv`, `--pin` or `--private-key`; use `--secret-file NAME=FILE`, `--secret-stdin NAME` or `--generate-password`. `drive items share link --password` is now `--link-password-file` / `--link-password-stdin`, with `--clear-link-password` to take one off. `--eo-password` on `mail messages send`, `reply`, `forward` and `drafts send` is now `--eo-password-file` / `--eo-password-stdin`.
- **Breaking.** No Pass listing carries a secret, in any format. `items list`, `aliases list`, `trash list` and `shared list` used to print every password in the account under `--output json`; read one with `items get`, `items revisions get` or `links get`.
- **Breaking.** `--future` is now `--onwards`, on `calendar events update` and `calendar events delete`.
- **Breaking.** `pass vaults share list` is now `pass vaults share get`, and answers one record rather than a list.
- **Breaking.** `pass export` holds the vaults you own, as Proton's own export does. A vault somebody shared with you is theirs to back up; the command says on stderr how much it left out.
- **Breaking.** An empty list under `--output json` and `--output yaml` is `[]` rather than a missing key, so `jq '.attendees[]'` iterates nothing instead of failing on `null`.
- **Breaking.** A timed event whose zone cannot be named is refused rather than stored without one, and a clock reading that names two instants or none - the four hours a year the clocks move - needs an explicit offset instead of being settled in silence. `02:30` on a spring morning used to be stored as `03:30`.
- An occurrence count is exact or it is not given. "Every weekday forever" reported a thousand occurrences and deleting a series reported two hundred, both of them the cap the walk stopped at; `occurrence_count` is now absent where there is no number, and a change reaching a whole series shows what it would touch first.
- A filtered `drive items trash|delete|move|copy` sends fifty items to a request rather than one, so a large selection finishes in a fraction of the round trips.

### Fixed

- `drive trash list` shows everything `drive trash empty` deletes. Trashed photos sit on a volume of their own and were missing from the listing while `empty` destroyed them anyway, so the number you confirmed was not the number that went.
- A filtered Drive command counts a folder and the files inside it once rather than twice - a folder is trashed, deleted, moved and copied whole - and names what Proton refused rather than reporting the count it hoped for.
- `proton api` keeps stdout for the response. A proxy's HTML error page standing in for the API's answer arrived on stdout with exit `0`, so a `jq` downstream broke while the pipeline looked like it had worked; it goes to stderr now and exits `5`.
- `mail settings autoreply set` refuses a schedule its `--repeat` mode does not allow before it reports anything, so `--dry-run` no longer predicts a change the real run would be refused for.
- `drive items download` refuses a path that already exists before the transfer rather than after it.

### Security

- A failed Drive block transfer no longer quotes the signed storage URL it was talking to. That URL is a credential for the block it names, and it was landing in error text that gets logged and pasted into issues. A redirect off the host Proton named is no longer followed with the storage token in hand.

## [2.10.0] - 2026-08-31

### Added

- Signing in with a security key. An account with one is asked to touch it, and one that also has an authenticator app reaches for the key when the code prompt is answered with nothing. The same answer satisfies the re-authentication guarding `calendar settings calendars delete`, `mail messages expire` and `mail settings autoreply set`, which an account with only a key could not give before.
- A CAPTCHA can be solved on any device. `proton` prints the page, opens it in your browser where it can, and waits while you solve it - here, on your phone, anywhere. A machine with no display signs in like any other.
- `--verified TOKEN` and `PROTON_VERIFIED` carry a verification solved out of band, for a run that cannot wait for one. A run that may not ask - `--no-input`, or no terminal - now prints the page and the token to repeat the command with, so a scheduled job can hand the link to a person and try again.

### Changed

- A CAPTCHA opens in your own browser rather than a window `proton` draws. Nothing to install on Linux: `libwebkit2gtk`, `libgtk-3` and `glib-networking` are no longer needed, the `.deb` and `.rpm` packages no longer depend on GTK or WebKit, and a `go install` build verifies exactly as a release binary does.

### Removed

- The embedded CAPTCHA webview helper, and `PROTON_HV_HELPER` with it. Your browser does the job and there is no helper left to point at.

## [2.9.0] - 2026-08-31

### Added

- Accounts in Proton's two-password mode can be used at all: `account login` asks for the second password once the sign-in is through, or reads it from `--second-password-file` or `--second-password-stdin`. Such an account used to sign in and then fail on every key it tried to open.
- `account settings get` says whether an account keeps the password that signs it in apart from the one that opens its data, as a `Two-Password Mode` row and as `two_password_mode`.
- Every command, argument and flag has a reference page at [proton-cli.lerchster.dev](https://proton-cli.lerchster.dev), generated from the same command tree `--help` prints from.

### Changed

- A `--help` screen ends with a link to that command's page instead of repeating the global flags, which are listed on `proton --help` alone.
- A short ID starts after the dash an ID may begin with, so nothing a listing shortens can be read as a flag when it is pasted back. About one ID in sixty-four begins with one, and a short ID copied for such a thing before this release needs listing again.
- Keys that do not open name the secret that was refused - `Incorrect second password.` in two-password mode - rather than coming out as a decryption failure.
- An ambiguous reference lists its candidates as short IDs, unless shortening them would print the same token twice.

### Fixed

- Installing an older version from the APT repository works again: it carries the last ten releases rather than only the newest, so `apt install proton-cli=2.8.0` and a rollback find something to install.
- The key password kept with a session is a cache: one that has stopped opening the keys is worked out again instead of leaving a profile that decrypts nothing, which is what a password changed elsewhere used to leave behind. Signing in drops the one a previous session left, so `account get` no longer calls such a profile unlocked.
- A command that needs your password but takes no `--password-file` points at `proton account login`, instead of naming a flag it would reject.

## [2.8.0] - 2026-08-28

### Added

- Messages say which thread they belong to: `conversation_id` is on every message under `--output json` and `--output yaml`, from `mail messages list`, `watch` and `get`. Acting on the whole thread no longer costs a second lookup.

### Changed

- `calendar reminders list` and `calendar reminders watch` report the event a reminder warns about in full - its location, description, attendees and the rest sit on the row beside `fires`, `remind` and `says`. `reminders list` gains a `LOCATION` column, so a script reading that text output by column position will shift.

### Fixed

- Signing in survives a bad moment at Proton's edge: a 502 or a dropped connection is waited out and asked again, instead of coming out as a complaint about JSON and exit `1`. Only what spends nothing is ever asked twice - a two-factor code least of all.
- A sign-in refused for a wrong password or two-factor code exits `2`, and one that outlasts the waiting - a rate limit included - exits `5`, so a scheduled job can tell "fix the password" from "come back later".

## [2.7.0] - 2026-08-27

### Added

- `mail messages watch` prints a line the moment a message arrives and stays attached until you stop it. Without `--folder` it covers the inbox and every folder set to notify, and a thread coming back from snooze counts as arriving.
- `calendar reminders list` says which reminders fall due between two dates, and `calendar reminders watch` prints each one at the second it is due. When a reminder fires is Proton's own answer, so an all-day event reminds at the hour its calendar chose rather than at midnight.
- `--notify` on `mail settings folders create` and `mail settings folders update` says whether mail landing in a folder is worth being told about. It is what `mail messages watch` covers when you name no folder.
- A watch's output is a line per thing rather than a collection: under `--output json` each line is one object instead of an envelope with a count, and under `--output yaml` each thing is its own document. Stopping one with Ctrl+C or SIGTERM exits `0`.

### Changed

- `mail settings folders list` shows a `NOTIFY` column, and a folder carries `notify` in JSON. A script reading that text output by column position will shift.

## [2.6.0] - 2026-08-26

### Added

- Calendar sharing: `calendar settings calendars share add|list|remove` hands a calendar to another Proton account, and `calendar invitations list|accept|decline` is the other side of it.
- `calendar settings calendars create --url` subscribes to a calendar published elsewhere, and `calendar settings calendars get` shows one in full.
- `calendar events export` and `calendar events import PATH` read and write `.ics` files. An import is addressed by each event's UID, so exporting, editing and importing back is a restore rather than a second copy.
- Events take `--end`, `--status`, optional attendees, and reminders that email rather than notify.
- `contacts export` and `contacts import PATH` read and write vCard files, addressed by UID the same way.
- `contacts merge` folds duplicate contacts into one, and `contacts groups get` shows which addresses are in a group.
- Contact values can say what kind they are - `--email work:jane@acme.com` - and eleven more fields are settable, among them the organization, the title and the birthday.
- Vault sharing: `pass vaults share add|list|remove` and `pass invitations list|accept|decline`.
- `pass export` and `pass import PATH` write and read a Proton Pass archive, the same zip the web client produces. Without a passphrase the archive is not encrypted, and says so as it writes.
- `pass links create|list|revoke` shows one item to somebody without a Proton account, for a while or a number of views.
- `pass breaches list|get` reports which of your addresses have turned up in somebody else's data breach.
- `pass aliases contacts create|list|block|allow|delete` gives an alias an address per correspondent, so a reply leaves as the alias instead of the mailbox behind it.
- `pass settings mailboxes list|create|verify|resend|update|delete` manages where aliases forward, and `pass settings domains list` shows what an alias can be made on.
- `pass items revisions list`, `pass items pin|unpin`, `pass items totp`, `pass vaults get`, and `pass generate` for a password.
- Items take custom sections: `--field SECTION/NAME=VALUE`, `--hidden` for a secret one, and `--totp-field` for a second code. An identity takes all 31 of its fields, not 13.
- `pass vaults create` and `update` take `--description`, `--icon` and `--color`.
- `mail settings filters create --if "subject contains invoice" --move-to Archive` describes a filter in conditions and actions and lets Proton write the Sieve; `--sieve` still takes a script of your own. `filters get` shows an existing one in the same words, and `filters update` rewrites a rule in place, keeping its position in the order.
- `mail settings filters apply` runs filters over mail already in the mailbox, and `filters reorder` sets the order they run in.
- `mail settings senders list|block|allow|spam|forget` manages the block and allow lists.
- `mail conversations snooze|unsnooze` takes a thread out of the inbox until a time you choose, and `snoozed` is addressable wherever a folder is - as are the categories Proton sorts into: `social`, `promotions`, `newsletters`, `transactions` and `updates`.
- `mail messages expire` makes a message delete itself later, `mail messages unsubscribe` asks a mailing list to stop, and `mail messages empty --folder` empties a folder outright.
- Five more mail settings: `next-message-on-move`, `pgp-scheme`, `remove-image-metadata`, `right-to-left` and `spam-action`.
- `drive shared list` shows what other people have shared with you and `drive sharing list` what you have shared. `drive items share update|resend` changes or resends an invitation, and `drive photos albums update --cover` sets an album's cover.
- `PROTON_HV_HELPER` names a CAPTCHA helper to run instead of the built-in one, which is what lets a `go install` build verify at all.
- `--passphrase-file` and `--passphrase-stdin` hand over a passphrase without putting it in the command line.

### Changed

- **Breaking.** `mail messages search` and `mail conversations search` are gone. `mail messages list` and `mail conversations list` take the same filters; they were always one request to Proton.
- **Breaking.** `drive folders create` is now `drive items create`, and `drive share ...` is now `drive items share ...`. Update any scripts that call them.
- The CAPTCHA window works on a Nix install. It opened blank saying "TLS support is not available", because nothing supplied the TLS backend GIO loads as a module.

### Fixed

- `contacts create` reports Proton's refusal instead of exiting `0` having written nothing. Proton answers inside a successful response, and only the response was being read.
- `contacts groups add` puts every one of a contact's addresses in the group, as it always said it would, and `--email` acts on the address you named rather than one that happens to belong to another contact.
- `pass aliases options` lists the domain an alias can be made on rather than a whole suffix. Proton mints the word in front of it afresh on every request, so what was listed had already stopped working by the time it could be typed back.
- `drive photos albums create` prints the new album's ID instead of nothing.

## [2.5.0] - 2026-08-17

### Added

- An install no package manager owns says when a release lands: once a day, under the command's own output. `PROTON_NO_UPDATE_CHECK=1` ends it, and a package-managed copy, a pipe and `--quiet` never see it.
- `proton changelog` says what a release changed, including one you have not installed. `--since` and `--until` cover the ground between two.
- Windows on ARM64 has its own build. winget, npm, the PowerShell installer and `proton update` hand an ARM64 machine a native binary; npm had none for it at all.

### Changed

- `update --dry-run` and `uninstall --dry-run` no longer ask you to sign in: they change this machine, not your account.

### Fixed

- Tab completion answers again - every shell's completion script was being refused as a mistyped command.

## [2.4.1] - 2026-08-16

### Changed

- `drive items upload --recursive` sends a deep tree in fewer round trips.

### Fixed

- `drive folders create /a/b/c` makes the folders above `c` instead of failing, and says how many it made.
- `drive folders create` names the file standing in the path instead of failing with `Link has no hash key`.

## [2.4.0] - 2026-08-16

### Changed

- The command is `proton`. `proton-cli` stays on your `PATH` as a second name, so nothing already written breaks and upgrading does not sign you out.
- **Breaking.** `go install github.com/roman-16/proton-cli/cmd/proton@latest` - the path gained `/cmd/proton`. Every other way of installing is unchanged.
- Colours are asked for by name, so output follows your terminal's theme instead of overruling it. Swatches keep their exact colour.

## [2.3.0] - 2026-08-15

### Added

- `drive items revisions download PATH REVISION_REF` reads an earlier version out to disk or into a pipe, leaving the file as it is.
- `drive items revisions delete PATH REVISION_REF` removes an earlier version permanently.
- `pass aliases disable REF` and `pass aliases enable REF` stop and start an alias receiving mail without burning the address.
- `--if-exists replace|rename|skip` on `drive items upload` answers a name Drive already has: a new revision, both under a numbered name, or nothing. Without it an upload still refuses.
- `pass items get` and `pass items update` handle aliases as themselves: the address, whether it is on, where it forwards, `--mailbox` and `--display-name`.
- `pass aliases create` prints the address Proton made, since it adds a word to your prefix.
- Single-letter forms for the global flags: `-p`, `-o`, `-n`, `-q`, `-y`. They cluster, so `-qn` is a quiet dry run.
- A caveat prints as its own `!` line on stderr, rather than as commentary above the green tick.

### Changed

- **Breaking.** An unrecognised reference exits `3` (not found) rather than `1`. Update scripts that read `1` as "no such thing".
- **Breaking.** A bad command line exits `1` (user error) rather than `2` (authentication), even when signed out.
- Colour marks only what carries a verdict or a colour of its own; every verdict is still spelled out in words.
- Label, folder, calendar and group lists show a colour as its swatch and name rather than a hex code.
- The `FLAGS` column counts attachments instead of drawing `📎`, which no monospace font sizes correctly.
- Tables measure width in terminal cells, so CJK subjects and emoji filenames stay aligned.
- An empty result reads `No messages match.` after a filter, rather than `No messages.`
- Transfers show speed and time left, and number themselves within a batch (`[3/27]`).
- Commands send independent requests together and never fetch the same thing twice, so they finish in fewer round trips.
- Commands that decrypt no longer wait out the key unlock before they start.

### Removed

- `--app-version` and `PROTON_APP_VERSION`. Nothing needed them.

### Fixed

- IDs that begin with `-` are no longer read as flags, in full, short and `SHARE/ITEM` references.
- `drive items revisions restore` reports success instead of an error when Proton accepts it.
- A recursive upload meeting a file where a folder must go is refused before anything is written, not part way through.
- A command that needs a feature your plan lacks says so, instead of retrying and losing the reason.
