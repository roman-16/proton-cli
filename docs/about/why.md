# Why it works this way

Why a few things behave the way they do. Nothing here is needed to use proton. It is for when a choice looks arbitrary and you want to know whether it was.

## Why colour is asked for by name

proton asks your terminal for a colour by name - the same eight names ANSI has had since 1976 - and your terminal decides what each one looks like.

Proton's purple comes out as whatever purple your theme uses, and the output stays legible on a light background without proton ever having to guess you are on one.

The swatch beside a label, folder, calendar or group is the exception, and it is not proton picking a colour. That hex is the value *you* gave the label. Redrawing it from your theme would misreport a field rather than respect a preference.

How faithfully it lands depends on whether your terminal takes 24-bit colour. Set `COLORTERM=truecolor` if it does and does not say so.

## Why a stream has no footer

A table measures its columns across every row it holds. A stream has no "every row", so there is no header rule and no footer to print: the widths would have to be guessed before the data existed.

A machine-format stream has no envelope for the same reason. An envelope has to be closed, and a watch ends when you stop it rather than when the data runs out. Each line is its own object instead, which is what `jq` reads without `--slurp`.

## Why it asks before some removals and not others

There are two ways a removal surprises you: the wrong verb, and the wrong filter. proton stops for exactly those two and nothing else.

Only a permanent removal says *This cannot be undone*, because only a permanent removal cannot be. Trashing is recoverable, and `restore` puts things back, so it asks the shorter question.

A removal you named yourself, with a verb that can be undone, is not a surprise and does not interrupt you.

That is the floor rather than the whole story. A [confirmation policy](../using/confirmations.md#making-more-commands-ask) can ask for more. It cannot ask for less.

## Why the confirmation policy resolves the other way

Every setting proton reads takes the nearest answer: a flag over a variable, a variable over the file. The confirmation policy takes the strictest one instead, and a `deny` cannot be lifted by anything on the command line.

The two kinds of setting answer different questions.

`--output json` says how proton should behave for the convenience of whoever is running it, and the person at the keyboard knows best what they want this time.

`deny: {"*": deletions}` says what proton must not do by accident, and the whole value of that sentence is that the next command cannot argue with it. If a flag could switch off a guard, it would not be a guard.

It follows that no policy can make proton *less* careful than it is with no configuration at all. The built-in rules are the weakest source, so writing a policy can only ratchet.

**This is not a security boundary**, and it is worth being plain about that. Anything running as you can edit the file that declares a deny. It stops a command run carelessly - which is most of them, and increasingly ones nobody typed - and it stops nothing that is trying.

## Why one flag name means one thing

`--to` is always an email recipient. `--into` is always a remote container. `--dest` is always a local path. `--force` only ever means "overwrite a local file". `--all` only ever means "everything in scope".

```bash
proton mail messages send --to alice@proton.me       # a recipient
proton mail messages list --to alice@proton.me       # matching a recipient
proton mail messages move REF --into archive         # a container over there
proton drive items download /report.pdf --dest .     # a path over here
```

`--dest` is spelled that way because `--output` was already the format every command answers in. A flag that meant the format almost everywhere and a filename on fourteen commands would mean those fourteen could not produce JSON at all.

Five flags have a single-letter form, and they are the five typed most: `-p`, `-o`, `-n`, `-q`, `-y`. The whole shorthand namespace belongs to the root, so no subcommand can take a letter and `-p` is the profile everywhere.

## Why a password is never a flag value

`argv` is readable by every user on the machine through `ps`, and it survives in shell history and in unit files. So a password is read from a pipe or a file - `--password-stdin` or `--password-file` - and never from `--password`.

`--password-file` is usually the one to reach for, since systemd's `LoadCredential=`, Kubernetes secrets and Docker secrets all hand you a path already.

A password you choose *for somebody else* follows the same rule and gets a name of its own, because it is not the account's: `--link-password-file` for what opens a Drive public link, `--eo-password-file` for what a recipient outside Proton types.

Each is held to the bounds Proton's own clients hold it to - at most 50 characters for a link, at least 8 for a message - and refused here rather than by the server.

## Why there are three ways to say none

Removing an optional value is one idea with three spellings, and which one is right follows from what the value can carry.

**The value says it.** `--expires never` is the answer to the question `--expires` asks, and it is the word every screen already prints for something that does not expire: `Expires: never`. A separate flag would be a second way to say the same thing, and then the two would have to be kept from being given at once.

**`--no-x` is a state.** `calendar events update --no-remind` removes an event's reminders, and `calendar settings calendars update --no-remind` gives new events in that calendar none. One word covers both because it names the state of having none rather than the act of removing.

**`--clear-x` is an act, on something that already exists.** It is what is left when the flag's own value cannot carry the word. A signature is text, and `""` is not something a shell shows you typing. A link's password is read from a file, and a path cannot say "none". So `--clear-signature` and `--clear-link-password`, each sitting beside the flag it clears, which `TestAClearFlagSitsBesideWhatItClears` keeps true.

A verb whose entire subject is the field answers in its own words instead. `mail messages expire REF --never` is the same idea as `--in 7d`, not a flag clearing something.

The rule holds for what Pass stores, too, which is where it matters most. The secret parts of an item - a password, a TOTP URI, a card's number, CVV and PIN, a private key, a hidden field - arrive through `--secret-file NAME=FILE` or `--secret-stdin NAME`. They name their field rather than having a flag each, because an item has several, a card three at once, and standard input can be read only once.

`--password-file` itself is not available to them: it means the account password everywhere in this CLI, and one flag name means one thing.

A new password needs no file at all. `proton pass items create --generate-password` makes one locally, stores it, and prints it beside the item's ID, so the only copy that ever existed is the one in the vault.

## Why a count is always exact

A confirmation and a dry run are read as a statement of what is about to happen, so the number in them has to be the number of things that happen.

That is why the trash is one list across every volume the account has. Photos are kept on a volume of their own, and a listing that read only the default one would say three while `trash empty` destroyed eight hundred. Both now walk the same set, fully paged, and `empty` counts what it will delete rather than what one page held.

It is why a filter that matches a folder and the files inside it selects the folder alone. Every verb it feeds - `trash`, `delete`, `move`, `copy` - acts on a folder whole, so the files under it were never separate work. Counting them would inflate the number and then ask Proton twice, the second time about something that has already moved.

And it is why a bulk verb acts on the IDs its selection resolved, in batches of fifty, reading the answer Proton gives per item. A path resolved a second time can mean something else by then, and a batch that half-succeeded and reported the number it hoped for would be the count lying at the last moment. What was refused is named instead, and the count says what landed.

## Why an occurrence count is a number or nothing

A recurring rule that says neither how many times it repeats nor when it stops repeats for ever. Such a series has no number of occurrences - not a very large one - which is why the rule is read rather than the occurrences counted.

Three places used to answer with a cap instead. `events get` reported a thousand occurrences for "every weekday forever", a series delete reported two hundred, and the walk underneath both stopped at ten thousand and returned as though the rule had ended. Each cap was true of the walk and false of the series.

So a count is either exact or not given. `occurrence_count` on an event is a number when the rule ends and absent when it does not. A change that reached a series reports `occurrences`, a number or `null`, because there the question is always about a series and `null` can only mean one thing.

In words, the sentence says it outright - "and all 500 occurrences of it", or "and every occurrence of it, a series with no end" - and the table under it is a sample: enough to recognise what is about to change, few enough that the question underneath is still on screen.

## Why the zone is settled once

Every wall-clock reading in this CLI is read against a named zone: `--start` on an event, `--send-at` on a message, `--until` on a snooze, the whole days `--start` and `--end` cut a listing into.

It is the same zone for all of them - the one the person running the command is in - so it is settled once per invocation, in the same place every other setting is.

The zone is a **name**, not an offset, because Proton anchors an event to one. A weekly 09:00 meeting anchored to `Europe/Vienna` stays at 09:00 when the clocks change; the same meeting stored as a UTC instant slides to 08:00. An offset pins the instant and says nothing about the anchor, so requiring one on every input would leave the load-bearing half of the answer to be guessed in silence.

`TZ` is where the zone is read from when no flag or file names one, rather than a variable of this CLI's own. A machine that has already been told which zone it is in should not have to be told again, and a second name for the setting every other tool reads would leave the two free to disagree.

Naming it once also keeps printing and parsing in step. An occurrence reference is a wall-clock reading the CLI prints and a person types back. Printed in one zone and read in another, it is a reference nobody can use.

## Why an ambiguous time is refused rather than resolved

For two hours a year a wall-clock reading names no instant, because the clocks went forward over it. For two hours a year it names two, because they went back.

Go answers both without complaint - it moves a time out of the gap and picks a side of the overlap - so `--start 2026-03-29T02:30` used to store an event at 03:30, and `--start 2026-10-25T02:30` used to pick one of two instants by a rule nobody wrote down.

Neither can be settled from a zone name, and both are decidable from the command line alone, before anything reaches the network. So they are refused, and the message names the offset form that settles it.

That is what an offset is for here: the four hours a year where a clock reading cannot name an instant, rather than every input in the year.

## Why a listing carries no secret

`items get` says in its own help that it prints passwords in full, because that is the command for reading one. That sentence only means something if the commands beside it do not.

So a Pass listing is a different type from a Pass item. `Item` is what a row knows - what the thing is, where it lives, what it is called - and `FullItem` is the decrypted whole.

`items list`, `aliases list`, `trash list`, `shared list` and `sharing list` can only hold the first, in text and in JSON alike, and the compiler is what says so rather than a rule somebody has to remember.

`revisions list` says what changed and when; `revisions get` reads one revision back whole. `links list` says which links exist; `links get` hands over the URL, which is the key that opens the item.

One path still decrypts everything at once, and it is the one whose job that is: `pass export`, which warns as it writes.

## Why the reference is generated

Prose drifts and a command tree does not. Everything in the [command reference](commands.md) - every argument, every flag, every example - is read out of the running program.

So a command that exists is a command that is documented, under its current name and with its current flags. The examples are parsed against the real tree, so one cannot name a command that was renamed.

The guides beside it are written by hand, in files of their own. A page that is half generated cannot be regenerated.

## Why search lags

`list` reads Proton's server-side index, which is eventually consistent. A message you just sent may not appear for a few seconds, and one you just deleted may still show up. The web client behaves the same way.

To confirm a change, run `get` on the ID the command printed rather than searching for the subject again.
