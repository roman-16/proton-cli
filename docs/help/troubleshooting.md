# Troubleshooting

What to do when a command fails. Every failure prints what happened and what to try; the exit code, and what to do about each, is in [Exit codes](../using/output.md#exit-codes).

## Reporting a bug

Exit `7` is a bug in proton. Nothing you typed caused it, and it says so on the screen:

```console
$ proton mail messages list
Error: None of your addresses' keys could be opened.
proton report  (this looks like a bug in proton, not something you did)
```

```console
$ proton report
```

That prints everything a fix needs: the version, the platform, how it was installed, your settings, and what the run that failed actually did, request by request. Paste it into the [bug form](https://github.com/roman-16/proton-cli/issues/new?template=bug.yml), which asks separately what you ran and what you expected.

**You do not have to reproduce anything.** Every run records what it did to `~/.config/proton-cli/logs/`, one file per day, the last 16 kept, so the failure you just hit is already written down. `proton report` picks the last run that failed - or `--all` for every run still on disk. To write no log at all, see [Settings](../using/settings.md#the-diagnostic-log).

**A long run is shortened to fit the form**: its first and last records, and everything above debug in between. When it says so, `proton report --dest bug.txt` writes the whole run to a file you can attach instead.

**It is safe to post.** Addresses, IDs, filenames, search terms and tokens never enter the log. An address is written as a stand-in like `address:3f9c1e@proton.me`, the same way every time, so a reader can follow which of your addresses failed without learning whose it is. Your configuration file is named by where it is - the default location, `--config`, `PROTON_CONFIG` - and never by its path, which holds your home directory. It goes to stdout rather than straight to a file so that you can read it first.

A crash is the same story with fewer words:

```console
$ proton drive items list
Error: proton crashed. This is a bug.
Try:   proton report
```

The stack trace is not printed - it says nothing to you. It is in the log, and `report` carries it.

## A listing is missing something

```console
$ proton pass items list --vault Work
NAME                  TYPE    VAULT    MODIFIED
aws-root              login   Work     2026-08-30 11:02
github.com            login   Work     2026-08-14 09:41
41 items.
⚠ 1 item could not be decrypted and is not listed.
  This is a bug or damaged data - `proton report` has the details.
```

An item that will not open is left out, and the listing says so. The exit code stays `0`: the command answered, just not completely.

In a machine format the same fact is a field:

```console
$ proton pass items list --vault Work --output json | jq .skipped
1
```

`skipped` is absent when nothing was skipped. A container that cannot be opened says that nothing inside it is listed:

```console
⚠ 1 vault could not be opened, so nothing inside it is listed.
⚠ 1 folder could not be opened, so nothing inside it is listed.
```

The same tally answers a puzzling `get`, where an item that cannot be read is otherwise indistinguishable from one that was never there:

```console
$ proton pass items get stripe-live
Error: No item matching "stripe-live".
Try:   1 item could not be decrypted and is not listed.
       proton report
```

Run `proton report` and open an issue. This is not something you can fix from the command line, and it is not your data being gone - it is proton failing to open something Proton still holds.

## The config file is refused

```console
$ proton mail messages list
Error: /home/you/.config/proton-cli/config.yaml: [4:1] unknown field "loglevel"
```

Nothing runs until the file parses. The position is the line and column in the file. [The key table](../using/settings.md#the-config-file) has the spellings.

Two mistakes account for most of these:

- A key that belongs at the top level, written inside a `per-profile:` section.
- `profile:` written inside a `per-profile:` section. Which profile you are acting as can only be set at the top level.

## Proton asks for a CAPTCHA

Proton sometimes asks for human verification at login: on a fresh install, from a new network, or after several failed attempts.

proton prints the verification page and waits:

```console
$ proton account login
Email:             you@proton.me
Password:
Proton wants to confirm you are human. Solve the CAPTCHA on this page -
you can open it on any device:

  https://verify.proton.me/?methods=captcha&token=eWLVJEM5Op3H5LcsY1cGyUxO

Press Enter once it says you are verified.
✓ Signed in as you@proton.me (profile "default").
```

Open it wherever you like - this machine, your phone, a laptop across the room - solve it, then press Enter and the command carries on.

Where proton can hand the address to a browser it does, but the address is printed either way, so a server with no display verifies exactly like a desktop. There is nothing to install.

### Solving a CAPTCHA in a script

With `--no-input`, `PROTON_NO_INPUT`, or standard input that is not a terminal, proton cannot wait. It exits with both halves you need:

```console
$ PROTON_NO_INPUT=1 proton account login --user you@proton.me --password-file ~/.proton
Error: Proton wants to confirm you are human, and this run cannot wait while you do.
Try:   solve https://verify.proton.me/?methods=captcha&token=eWLVJEM5Op3H5LcsY1cGyUxO
       then run the same command again with --verified eWLVJEM5Op3H5LcsY1cGyUxO
```

Solve the page, then repeat the command with `--verified` or `PROTON_VERIFIED`. This is how a scheduled job gets through one: it surfaces the link to a person and tries again.

### Email or SMS verification

Proton occasionally offers only a code by email or SMS. That cannot be finished from a terminal:

```console
Error: Proton wants to confirm you are human by email or sms, and that cannot be done
       from a terminal.
Try:   sign in to your account in a browser once, then try again
```

## A security key that nothing finds

A key it cannot see and a key it cannot open are two different problems, with different fixes.

### "No security key is connected to this machine."

Nothing answered on USB. Plug in the key you registered with Proton and run the command again.

A passkey held in a phone cannot answer here. Use a code instead.

### "A security key is connected but proton cannot open it."

On Linux, the key is readable only by root until a udev rule says otherwise.

Every distribution ships the rules with its FIDO packages: `libfido2` on most, with `yubikey-personalization` alongside it for YubiKeys. On NixOS:

```nix
services.udev.packages = [ pkgs.libfido2 ];
```

Unplug the key and plug it in again afterwards. The rules apply when the device appears.

### "This build cannot reach a security key on this machine."

This is a Windows build installed with `go install`, which cannot reach a security key.

Install from a [release](../install.md), or sign in with `--totp`.

## "Profile is not signed in"

```console
$ proton --profile work mail messages list
Error: Profile "work" is not signed in.
Try:   proton account login --profile work
```

Either nothing was ever signed in under that name, or the session was revoked or expired.

Signing in again as the same account is harmless, so an unattended job can run it first:

```bash
proton account login --user "$ACCOUNT" --password-file "$CRED"
```

## It asks for my password again

A few commands ask for your password even when you are signed in; they are listed in [Account](../account/README.md#commands-that-ask-for-the-password-again). They prompt, or take `--password-file` and `--password-stdin`.

One of them may ask for your password and then be refused anyway, for a reason no password would have fixed.

## Pass asks for an extra password

```console
$ proton pass items list
Extra password:
```

Your Pass is protected with [an extra password](https://proton.me/support/pass-extra-password), and this session has not answered it yet. Type it once and the session can reach Pass for as long as it lives. Nothing else in proton is affected.

With nobody to ask, the command says so and exits `2`. Hand the password to the sign-in instead:

```bash
proton account login --user "$ACCOUNT" --password-file "$CRED" \
  --extra-password-file /run/secrets/proton-pass
```

**"That is not this account's Pass extra password."** A few wrong answers end the session; every command then exits `2` until you sign in again.

It is not your Proton account password, and it is not the second password of a two-password account.

Turning it on or off is covered by [Pass](../pass/README.md#turn-the-extra-password-on-or-off).

## A change I just made does not show up

A change reaches `list` a few seconds late: a message you just sent may not appear yet, and one you just deleted may still be listed.

To confirm a change, run `get` on the ID the command printed rather than searching for it again.

## Everything started failing at once

An account has rate limits. The sending quota on free accounts is the one most people meet.

A run that suddenly fails with exit `5` after working fine is almost always a rate limit, not a bug. Wait it out. A loop that acts on many things should sleep between iterations.

A dropped connection or a server error is retried automatically for anything that only reads, and for signing in. Nothing that changes something is ever sent twice.

## "Work is a label, not a folder"

```console
$ proton mail messages move 5bH2mQxK --into Work
Error: "Work" is a label, not a folder - moving needs a folder.
Try:   to attach the label instead, use `label --label Work`.
       To see the folders, run `proton mail settings folders list`.
```

A message lives in exactly one folder and carries any number of labels. `move` changes the folder; `label` adds to the labels.

## A short ID does not resolve

Short IDs are remembered per machine, in `~/.config/proton-cli/idcache/<profile>.json`. One copied from another machine's output means nothing here.

Run the matching `list` first, or use the full ID. ([More](../using/naming.md#short-ids).)

## A command hangs instead of failing

It does not, but a prompt on a terminal looks like a hang if you were not expecting one.

Only `account login` and the [commands that ask for your password again](../account/README.md#commands-that-ask-for-the-password-again) ever ask a question, and only when standard input is a terminal. To make a missing credential an error instead:

```bash
proton account login --no-input
PROTON_NO_INPUT=1 proton account login
```

## Colours look wrong

Colours come from your terminal's theme, so they look the way your theme draws them.

The exception is the `■` beside a label, folder, calendar or group, which is the exact colour you gave it and needs 24-bit colour to draw. If your terminal takes it but does not advertise it, set `COLORTERM=truecolor`.

`--no-color` or `NO_COLOR` turns colour off entirely, and `--force-color` or `FORCE_COLOR` keeps it through a pipe - a pager, a multiplexer, a CI log that renders the escapes itself.

## "The link does not exist any more, or has expired"

A public link answers this when its owner removed it, when it passed the date it was set to expire, or when it reached the number of downloads its owner allowed. Ask whoever sent it for a new one.

## Still stuck

```console
$ proton report
```

Paste it into the [bug form](https://github.com/roman-16/proton-cli/issues/new?template=bug.yml). Nothing to reproduce and nothing to redact by hand - see [Reporting a bug](#reporting-a-bug).

To watch a run as it happens instead, `--log-level debug` puts on the screen what the log is recording anyway. It is redacted the same way, so it is safe to paste too.

Please do not report a **security** issue in a public issue. [`SECURITY.md`](https://github.com/roman-16/proton-cli/blob/main/SECURITY.md) has the private channels.
