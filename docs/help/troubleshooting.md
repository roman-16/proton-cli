# Troubleshooting

What to do when a command fails.

## What the exit code is telling you

Errors say what happened and what to try, so read the message first. The code is what a script reads.

| Code | Means | What to do |
| --- | --- | --- |
| `1` | Something you passed was wrong | Fix the command. Nothing was sent |
| `2` | Authentication failed | Sign in again, or fix the password |
| `3` | Not found | Check the reference, or run the matching `list` |
| `4` | Ambiguous, or a conflict | Narrow the term, or use the ID it printed |
| `5` | Network or server problem | Wait and retry. This is not your command's fault |
| `6` | Refused by your confirmation policy | Nothing about the command was wrong. The policy has to change |
| `130` | Cancelled with Ctrl+C | Nothing to do |

The difference between `2` and `5` matters most to a scheduled job. `2` means fix the credential; `5` means come back later. A rate limit is `5`.

`6` is the one code that retrying never helps. See [Deny](../using/confirmations.md#deny).

## The config file is refused

```console
$ proton mail messages list
Error: /home/you/.config/proton-cli/config.yaml: [4:1] unknown field "loglevel"
```

Nothing runs until the file parses, because it carries your confirmation policy and a policy that quietly fails to load is one that fails open. The position is the line and column in the file. [The key table](../using/settings.md#the-config-file) has the spellings.

Two mistakes account for most of these:

- A key that belongs at the top level, written inside a `per-profile:` section.
- `profile:` written inside a `per-profile:` section. Which profile you are acting as can only be set at the top level.

## Proton asks for a CAPTCHA

Proton's anti-abuse system sometimes asks for human verification at login, usually on a fresh install, from a new network, or after several failed attempts.

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

### A CAPTCHA cannot be solved ahead of time

The challenge belongs to the run that asked for it. Every run gets its own, and solving one does not lower the gate for the next. So the command has to be waiting while you solve it.

The **proof** does outlive the run, which is what makes the two-step below possible.

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

Proton occasionally offers only a code by email or SMS. Its page checks the code and reports the result to nobody, so no command-line client can finish one:

```console
Error: Proton wants to confirm you are human by email or sms, and that cannot be done
       from a terminal.
Try:   sign in to your account in a browser once, then try again
```

## A security key that nothing finds

`login` reports a key it cannot see and a key it cannot open as two different problems, because they have different fixes.

### "No security key is connected to this machine."

Nothing answered on USB. Plug in the key you registered with Proton and run the command again.

A passkey held in a phone cannot answer here at all: reaching it needs a browser's Bluetooth handoff. Use a code instead.

### "A security key is connected but proton cannot open it."

On Linux, `/dev/hidraw*` belongs to root until a udev rule says otherwise, and no sign-in should need to be root.

Every distribution ships the rules with its FIDO packages: `libfido2` on most, with `yubikey-personalization` alongside it for YubiKeys. On NixOS:

```nix
services.udev.packages = [ pkgs.libfido2 ];
```

Unplug the key and plug it in again afterwards. The rules apply when the device appears.

### "This build cannot reach a security key on this machine."

This is a Windows build installed with `go install`. Windows hands out assertions through its own API, which the released binaries are built to reach and a `go install` build is not.

Install from a [release](../install.md), or sign in with `--totp`.

## "Profile is not signed in"

```console
$ proton --profile work mail messages list
Error: Profile "work" is not signed in.
Try:   proton account login --profile work
```

Either nothing was ever signed in under that name, or the session was revoked or expired.

Signing in again as the same account is harmless and idempotent, which is why an unattended job can simply run it first:

```bash
proton account login --user "$ACCOUNT" --password-file "$CRED"
```

## It asks for my password again

Three commands reach endpoints Proton guards behind an elevated session. Proton re-authenticates against the password itself rather than accepting your session:

- `calendar settings calendars delete`
- `mail messages expire`
- `mail settings autoreply set`

They prompt, or take `--password-file` and `--password-stdin`. The key password sealed into your session cannot stand in: it is a one-way derivation.

Proton answers some unrelated refusals with the same code, so one of these may ask for your password and then be refused anyway, for a reason no password would have fixed.

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

**"That is not this account's Pass extra password."** Proton counts wrong answers and ends the session after a few of them, at which point every command exits `2` until you sign in again.

It is not your Proton account password, and it is not the second password of a two-password account.

## A change I just made does not show up

`list` reads Proton's server-side index, which is eventually consistent. A message you just sent may not appear for a few seconds, and one you just deleted may still be listed. The web client behaves the same way.

To confirm a change, run `get` on the ID the command printed rather than searching for the subject again. ([Why](../about/why.md#why-search-lags).)

## Everything started failing at once

Bulk commands page through Proton's API and respect its caps, but an account has limits above that. The sending quota on free accounts is the one most people meet.

A run that suddenly fails with exit `5` after working fine is almost always a rate limit, not a bug. Wait it out. A loop that acts on many things should sleep between iterations.

A 502 from Proton's edge, or a connection that drops, is waited out and retried automatically - for anything that only reads, and for signing in. Nothing that changes something is ever sent twice.

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

Only `account login` and the three elevated commands above ever ask a question, and only when standard input is a terminal. To make a missing credential an error instead:

```bash
proton account login --no-input
PROTON_NO_INPUT=1 proton account login
```

## Colours look wrong

proton asks your terminal for a colour by name, and your terminal decides what it looks like.

The exception is the `■` beside a label, folder, calendar or group. That hex is the value stored on Proton's side, and drawing it faithfully needs 24-bit colour. If your terminal takes it but does not advertise it, set `COLORTERM=truecolor`.

`--no-color` or `NO_COLOR` turns colour off entirely. ([Why it works this way](../about/why.md#why-colour-is-asked-for-by-name).)

## Still stuck

Run the command again with `--log-level debug` and open an [issue](https://github.com/roman-16/proton-cli/issues) with what it printed. Redact the IDs and addresses you would rather not publish; none of them are needed to reproduce a bug.

Please do not report a **security** issue in a public issue. [`SECURITY.md`](https://github.com/roman-16/proton-cli/blob/main/SECURITY.md) has the private channels.
