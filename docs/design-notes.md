# Design notes

Why a few things behave the way they do. Nothing here is needed to use proton - it is here for when a choice looks arbitrary and you want to know whether it was.

## Why colour is asked for by name

proton asks your terminal for a colour by name - the same eight names ANSI has had since 1976 - and your terminal decides what each one looks like. Proton's purple comes out as whatever purple your theme uses, and the output stays legible on a light background without ever having to guess you are on one.

The swatch beside a label, folder, calendar or group is the exception, and it is not proton picking a colour: the hex is the value *you* gave that label. Redrawing it from your theme would misreport a field rather than respect a preference. How faithfully it lands depends on whether your terminal takes 24-bit colour - set `COLORTERM=truecolor` if it does and does not say so.

## Why a stream has no footer

A table measures its columns across every row it holds. A stream has no every row, so there is no header rule and no footer to print - the widths would have to be guessed before the data existed.

For the same reason a machine-format stream has no envelope: an envelope has to be closed, and a watch ends when you stop it rather than when the data runs out. Each line is its own object instead, which is what `jq` reads without `--slurp`.

## Why it asks before some changes

Some changes cross a boundary even when they can be reversed: sending something to another person, exposing or changing a share, changing network connectivity, and changing session or trust state. proton asks at those boundaries, before every permanent removal, and whenever a filter rather than a reference chose the targets.

Only a permanent removal says *This cannot be undone*, because only a permanent removal cannot be. Trashing is recoverable, so it asks the shorter question and `restore` puts things back. A removal you named yourself, with a verb that can be undone, is not a surprise and does not interrupt you.

## Why one flag name means one thing

`--to` is always an email recipient. `--into` is always a destination container. `--force` only ever means "overwrite a local file". `--all` only ever means "everything in scope".

```bash
proton mail messages send --to alice@proton.me       # a recipient
proton mail messages list --to alice@proton.me       # matching a recipient
proton mail messages move REF --into archive         # a destination
```

Five flags have a single-letter form, and they are the five typed most: `-p`, `-o`, `-n`, `-q`, `-y`. The whole shorthand namespace belongs to the root, so no subcommand can take a letter and `-p` is the profile everywhere.

## Why a password is never a flag value

`argv` is readable by every user on the machine through `ps`, and it survives in shell history and in unit files. So a password is read from a pipe or a file - `--password-stdin` or `--password-file` - and never from `--password`.

`--password-file` is usually the one to reach for, since systemd's `LoadCredential=`, Kubernetes secrets and Docker secrets all hand you a path already.

## Why the reference is generated

Prose drifts and a command tree does not. Everything in the [command reference](commands/README.md) - every argument, every flag, every example - is read out of the running program, so a command that exists is a command that is documented, under its current name and with its current flags. The examples are parsed against the real tree, so one cannot name a command that was renamed.

The guides beside it are written by hand, in files of their own. A page that is half generated cannot be regenerated.

## Why search lags

`list` reads Proton's server-side index, which is eventually consistent: a message you just sent may not appear for a few seconds, and one you just deleted may still show up. The web client behaves the same way.

To confirm a change, run `get` on the ID the command printed rather than searching for the subject again.
