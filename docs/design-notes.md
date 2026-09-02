# Design notes

Why a few things behave the way they do. Nothing here is needed to use proton - it is here for when a choice looks arbitrary and you want to know whether it was.

## Why colour is asked for by name

proton asks your terminal for a colour by name - the same eight names ANSI has had since 1976 - and your terminal decides what each one looks like. Proton's purple comes out as whatever purple your theme uses, and the output stays legible on a light background without ever having to guess you are on one.

The swatch beside a label, folder, calendar or group is the exception, and it is not proton picking a colour: the hex is the value *you* gave that label. Redrawing it from your theme would misreport a field rather than respect a preference. How faithfully it lands depends on whether your terminal takes 24-bit colour - set `COLORTERM=truecolor` if it does and does not say so.

## Why a stream has no footer

A table measures its columns across every row it holds. A stream has no every row, so there is no header rule and no footer to print - the widths would have to be guessed before the data existed.

For the same reason a machine-format stream has no envelope: an envelope has to be closed, and a watch ends when you stop it rather than when the data runs out. Each line is its own object instead, which is what `jq` reads without `--slurp`.

## Why it asks before some removals and not others

There are two ways a removal surprises you: the wrong verb, and the wrong filter. proton stops for exactly those two and nothing else.

Only a permanent removal says *This cannot be undone*, because only a permanent removal cannot be. Trashing is recoverable, so it asks the shorter question and `restore` puts things back. A removal you named yourself, with a verb that can be undone, is not a surprise and does not interrupt you.

That is the floor rather than the whole story: a [confirmation policy](language.md#asking-about-more-than-that) can ask for more. It cannot ask for less.

## Why the confirmation policy resolves the other way

Every setting proton reads takes the nearest answer - a flag over a variable, a variable over the file. The confirmation policy takes the most cautious one instead, and a `deny` cannot be lifted by anything on the command line.

The two kinds of setting are asking different questions. `--output json` says how proton should behave for the convenience of whoever is running it, and the person at the keyboard knows best what they want this time. `deny: {"*": deletions}` says what proton must not do by accident, and the whole value of that sentence is that the next command cannot argue with it. A guard a nearer source can lower is not a guard; it is a suggestion with extra steps.

It follows that no policy can make proton *less* careful than it is with no configuration at all. The built-in rules are simply the weakest source, so writing a policy can only ever ratchet.

What this is not is a security boundary, and it is worth being plain about that. Anything running as you can edit the file that declares a deny. It stops a command run carelessly - which is most of them, and increasingly ones nobody typed - and it stops nothing that is trying.

## Why one flag name means one thing

`--to` is always an email recipient. `--into` is always a remote container. `--dest` is always a local path. `--force` only ever means "overwrite a local file". `--all` only ever means "everything in scope".

```bash
proton mail messages send --to alice@proton.me       # a recipient
proton mail messages list --to alice@proton.me       # matching a recipient
proton mail messages move REF --into archive         # a container over there
proton drive items download /report.pdf --dest .     # a path over here
```

`--dest` is spelled that way because `--output` was already the format every command answers in, and a flag that meant the format almost everywhere and a filename on fourteen commands meant those fourteen could not produce JSON at all.

Five flags have a single-letter form, and they are the five typed most: `-p`, `-o`, `-n`, `-q`, `-y`. The whole shorthand namespace belongs to the root, so no subcommand can take a letter and `-p` is the profile everywhere.

## Why a password is never a flag value

`argv` is readable by every user on the machine through `ps`, and it survives in shell history and in unit files. So a password is read from a pipe or a file - `--password-stdin` or `--password-file` - and never from `--password`.

`--password-file` is usually the one to reach for, since systemd's `LoadCredential=`, Kubernetes secrets and Docker secrets all hand you a path already.

The rule holds for what Pass stores, too, which is where it matters most. The parts of an item that are secret - a password, a TOTP URI, a card's number, CVV and PIN, a private key, a hidden field - arrive through `--secret-file NAME=FILE` or `--secret-stdin NAME`. They name their field rather than having a flag each because an item has several, a card three at once, and standard input can be read only once. `--password-file` itself is not available to them: it means the account password everywhere in this CLI, and one flag name means one thing.

A new password needs no file at all. `proton pass items create --generate-password` makes one here, stores it, and prints it beside the item's ID - so the only copy that ever existed is the one in the vault.

## Why a listing carries no secret

`items get` says in its own help that it prints passwords in full, because that is the command for reading one. That sentence only means something if the commands beside it do not.

So a Pass listing is a different type from a Pass item: `Item` is what a row knows - what the thing is, where it lives, what it is called - and `FullItem` is the decrypted whole. `items list`, `aliases list`, `trash list`, `shared list` and `sharing list` can only hold the first, in text and in JSON alike, and the compiler is what says so rather than a rule somebody has to remember. `revisions list` says what changed and when; `revisions get` reads one revision back whole. `links list` says which links exist; `links get` hands over the URL, which is the key that opens the item.

One path still decrypts everything at once, and it is the one whose job that is: `pass export`, which warns as it writes.

## Why the reference is generated

Prose drifts and a command tree does not. Everything in the [command reference](commands/README.md) - every argument, every flag, every example - is read out of the running program, so a command that exists is a command that is documented, under its current name and with its current flags. The examples are parsed against the real tree, so one cannot name a command that was renamed.

The guides beside it are written by hand, in files of their own. A page that is half generated cannot be regenerated.

## Why search lags

`list` reads Proton's server-side index, which is eventually consistent: a message you just sent may not appear for a few seconds, and one you just deleted may still show up. The web client behaves the same way.

To confirm a change, run `get` on the ID the command printed rather than searching for the subject again.
