# Security

What leaves your machine, what is kept on it, and how to limit what an unaudited tool can cost you.

Signing in runs [Secure Remote Password](https://en.wikipedia.org/wiki/Secure_Remote_Password_protocol) through Proton's own [go-srp](https://github.com/ProtonMail/go-srp), so your password is never sent to Proton, not even hashed. Your keys are unlocked on your machine with [gopenpgp](https://github.com/ProtonMail/gopenpgp), and everything is decrypted and encrypted here.

## What leaves your machine

- API requests to `https://mail.proton.me/api` over HTTPS, authenticated with your session tokens.
- Encrypted payloads you asked to create: an encrypted message, an encrypted file block, an encrypted event.
- The SRP proof during login, which does not reveal your password.
- Opening a public link with `--link` or `--shared` carries your session, so Proton knows which account opened it, and the owner sees one more visit.
- Once a day, on an install no package manager owns, a request to GitHub asking which release is newest. It carries nothing about you or your account, and `PROTON_NO_UPDATE_CHECK` ends it ([Updating](../install.md#updating)).

**What never leaves:** your password, your second password if you have one, your key password, and your private keys.

There is no telemetry, and there is nothing to turn off. proton-cli never reports a command you ran, a feature you used, or the fact that you ran it at all. The diagnostic log is written to your disk and read by nobody unless you run `proton report` and paste it somewhere yourself ([Reporting a bug](../help/troubleshooting.md#reporting-a-bug)). What is known about how the tool is used comes from counters the distribution channels publish, and is [on the site](https://proton-cli.lerchster.dev/stats/) in full.

**Anything you export is plaintext.** `mail messages export` and `contacts export` decrypt on the way out, so the files they write are readable by anything. Put them where you would be comfortable putting the mail itself.

## What is stored on disk

The paths are listed under [Files on disk](../using/settings.md#files-on-disk). Two of them hold something sensitive.

**The session file**, one per profile, mode `0600`, holds the session tokens and your key password. The key password is stored encrypted with a key that Proton holds and hands out only to a live session, so it is never on disk in cleartext, and **revoking the session** - `proton account logout --revoke`, or from any Proton app - makes a leaked copy undecryptable. The file still holds the session's refresh token, so it is not safe to share; revoking neutralises a leak, it does not excuse one.

**The diagnostic log**, mode `0600`, holds what each run did, written so it can be handed to a stranger: addresses, IDs, paths, tokens, subjects, filenames, search terms and flag values never enter it. `--no-log` or `PROTON_NO_LOG` stops it being written ([Settings](../using/settings.md#the-diagnostic-log)).

## A file downloaded from a public link

Every file downloaded anywhere else is checked against a hash list its author signed. A public link is served without that signature, because whoever opens a link is outside the share and cannot hold the key that signed it. Each block is still checked against the hash list the link came with, so a file that arrives damaged is refused - but the list itself is Proton's word rather than the author's.

## Two-factor and security keys

A code is asked for only when the account has one enabled. Security keys work at sign-in on Linux, macOS and Windows; see [Security keys](../account/README.md#security-keys) for what each platform reaches.

## Reducing your risk

proton-cli is **unaudited**. To limit what that costs you:

- Run it as your normal user, never as root.
- Verify release checksums against the [releases page](https://github.com/roman-16/proton-cli/releases) before installing a downloaded binary.
- Keep it up to date with `proton update`, or your package manager.
- Revoke the session on any machine you lose control of, with `proton account sessions revoke` or Proton's own session settings.
- Keep automated use plausible. Proton reacts to volume and rate, and an account flagged for that is an account problem rather than a bug here.

Found a vulnerability? [`SECURITY.md`](https://github.com/roman-16/proton-cli/blob/main/SECURITY.md) has the private reporting channels. Please do not open a public issue.
