# Security and encryption

Your password never reaches Proton, your keys never leave your machine, and every byte is decrypted after it arrives and encrypted before it leaves.

proton talks to the same API as the Proton web apps, with the same authentication and the same encryption. Nothing proxies your data, and no server other than Proton's sees it.

## How signing in works

1. **Session** - an unauthenticated session is created via `POST /auth/v4/sessions`.
2. **SRP** - login runs [Secure Remote Password](https://en.wikipedia.org/wiki/Secure_Remote_Password_protocol) through Proton's own [go-srp](https://github.com/ProtonMail/go-srp), so your password is never sent to the server, not even hashed. Two-factor codes are handled in the same exchange.
3. **Key password** - your password, stretched with the salt Proton holds for your primary key, unlocks your PGP keys. It stays on your machine.
4. **Session file** - tokens plus the key password are written to `~/.config/proton-cli/sessions/<profile>.json` with mode `0600`, so later commands do not re-authenticate. [What is in that file](#what-is-stored-on-disk).
5. **Refresh** - expired access tokens are refreshed automatically.

An account in [two-password mode](https://proton.me/support/switch-two-password-mode) stretches its **second** password at step 3. That mode's whole point is that the secret proving who you are is not the one that opens your data. Proton's account settings say which mode you are in.

Proton may require human verification during step 2. Its page is solved in a browser, on whatever device is to hand, and the challenge token then stands as the proof on the retry. Nothing is rendered here, so no machine is too bare to sign in. See [Troubleshooting](../help/troubleshooting.md#proton-asks-for-a-captcha).

## The key hierarchy

Proton's encryption is a tree, and proton walks the same one as the web client using [gopenpgp](https://github.com/ProtonMail/gopenpgp):

```
key password
  └── user key
        ├── address keys        (mail, signing)
        ├── calendar keys       (events)
        ├── drive node keys     (files and folders)
        ├── contact encryption  (contact cards)
        └── pass vault keys     (vault and item keys)
```

Unlocking happens lazily. A command that only lists metadata never touches your keys. A command that decrypts asks for the keys at the same time as the content they will open, so the keys cost no round trip of their own.

Below the user key, only the branch a command actually reads is unwrapped: a calendar's keys, a vault's, a file's.

## What is encrypted with what

| Content | Encrypted with | Signed with |
| --- | --- | --- |
| Mail bodies and attachments | Session key per message | Address key |
| Calendar events | Calendar key | Address key |
| Drive file contents | Node key, per block | Address key |
| Drive file and folder names | Parent node key | Address key |
| Contact cards | User key | User key |
| Pass items | AES-256-GCM item key | Symmetric, no signature |
| Pass vaults | AES-256-GCM vault key | Symmetric, no signature |

Reading works the other way around: content arrives encrypted, is decrypted locally, and signatures are verified against the sender's key. `mail messages get` reports the verdict on a `Signature:` line.

## What leaves your machine

- API requests to `https://mail.proton.me/api` over HTTPS, authenticated with your session tokens.
- Encrypted payloads you asked to create: an encrypted message, an encrypted file block, an encrypted event.
- The SRP proof during login, which does not reveal your password.
- Once a day, on an install no package manager owns, a request to GitHub asking which release is newest. It carries nothing about you or your account, and `PROTON_NO_UPDATE_CHECK` ends it ([what that is](../install.md#updating)).

**What never leaves:** your password, your second password if you have one, your key password, and your private keys.

There is no telemetry, and there is nothing to turn off. proton-cli never reports a command you ran, a feature you used, or the fact that you ran it at all. The [diagnostic log](#the-diagnostic-log) is written to your disk and read by nobody unless you run `proton report` and paste it somewhere yourself.

Everything known about how the tool is used comes from counters the distribution channels publish themselves, and is [on the site](https://proton-cli.lerchster.dev/stats/) in full.

**Anything you export is plaintext.** `mail messages export` and `contacts export` decrypt on the way out, so the files they write are readable by anything. Put them where you would be comfortable putting the mail itself.

## What is stored on disk

One file per profile, at `~/.config/proton-cli/sessions/<profile>.json`, mode `0600`. It contains:

- The session **auth tokens** (UID, access, refresh).
- The **salted key password** that unlocks your PGP keys, stored **encrypted** with a random 256-bit AES-GCM *client key*.

That client key is **not** stored on your machine. Proton's servers hold it, and proton fetches it over an authenticated request (`/auth/v4/sessions/local/key`) when a command needs to unlock your keys.

Two consequences:

- The key password is never written to disk in cleartext, so it cannot be lifted from a backup, a synced home directory, a disk image, or with `grep`.
- **Revoking the session** - from any Proton app, or with `proton account logout --revoke` - makes the on-disk blob undecryptable. Without a live session the client key cannot be fetched, so a leaked copy can no longer be turned back into your key password.

The caveat that matters: the file still contains the session **refresh token**, so it is not safe to share. Encryption at rest limits the damage of a leaked copy, and revoking neutralises it. Neither is a substitute for protecting the file.

macOS keeps it under `~/Library/Application Support/proton-cli/`, and Windows under `%APPDATA%\proton-cli\`.

## The diagnostic log

Every run writes what it did to `~/.config/proton-cli/logs/`, one file per day, mode `0600`. The last 16 files are kept. `proton report` reads them back so a bug can be reported without you reproducing it, and `--no-log` or `PROTON_NO_LOG` stops it being written ([what it is for](../help/troubleshooting.md#reporting-a-bug)).

It is written to be handed to a stranger, so **nothing that identifies you goes into it**. That is a property of how it is written rather than a promise about what anybody remembered to leave out: every value in a log record is written under a name, and each name has a declared policy saying what may appear for it. A name with no policy cannot be logged at all - the build fails on it.

| What | How it is recorded |
| --- | --- |
| An email address | `address:3f9c1e@proton.me`. The local part becomes a stand-in; the domain survives only if it is one of Proton's own, and anything else becomes `@elsewhere` |
| An ID - message, file, vault, key, share | `message:88ab02`, a stand-in under its own kind |
| An API path | `/drive/shares/{id}/links/{id}`. The endpoint stays, the arguments go, the query string goes entirely |
| A local file path | `<path>`. The filename does not survive |
| A URL | The scheme, host and path shape. The query - where a verification token lives - goes |
| An error message | Rewritten: addresses, IDs, paths and tokens inside it are replaced before it is written |
| The command | Its path, and the **names** of the flags that were set. Never a flag's value, never a positional argument |
| Counts, statuses, durations, endpoints | As they stand. This is what the log is read for |

**Never recorded, under any name:** a password, a second password, a Pass extra password, a session token, key material, a message subject or body, a filename, a contact's details, a search term.

The stand-ins are derived with HMAC-SHA256 under a 32-byte random salt at `~/.config/proton-cli/logs/salt`, mode `0600`, which never leaves the machine and is never in a report. So one address reads the same way in every line of every run here - which is what lets somebody debugging follow it - and means nothing anywhere else: the same address on another machine produces a different stand-in, and no stand-in can be turned back into an address.

`--log-level debug` puts the same records on your screen, redacted the same way, so pasting terminal output is as safe as pasting a report.

## Elevated operations

Proton guards its most destructive endpoints behind an elevated session scope. A request that needs one and has not got it comes back refused, and the client is expected to prove a human is present: re-run SRP against a scope-granting endpoint, retry the request, then drop the scope again.

proton handles that in the transport layer, the way the web clients do, rather than in each command. So no command has to know which operations are guarded. It runs, the server asks, your password is requested once, the request is retried, and the elevation is dropped immediately afterwards.

Both halves of SRP are verified, on the initial sign-in and on every elevation: the server has to prove it knows your verifier just as you prove you know your password.

## Two-factor authentication

A TOTP code is asked for only when the account actually has one enabled, so a code is never wasted on a guess.

Security keys work at sign-in on Linux, macOS and Windows. See [Does it support security keys?](faq.md#does-it-support-security-keys) for what each platform reaches.

## Reducing your risk

proton-cli is **unaudited**. To limit what that costs you:

- Run it as your normal user, never as root.
- Verify release checksums against the [releases page](https://github.com/roman-16/proton-cli/releases) before installing a downloaded binary.
- Keep it up to date with `proton update`, or your package manager.
- Revoke the session on any machine you lose control of, with `proton account sessions revoke` or Proton's own session settings.
- Keep automated use plausible. Proton's abuse detection reacts to volume and rate, and an account flagged for that is an account-safety problem rather than a bug here.

Found a vulnerability? [`SECURITY.md`](https://github.com/roman-16/proton-cli/blob/main/SECURITY.md) has the private reporting channels. Please do not open a public issue.

## Where the API definitions come from

The endpoint shapes are generated from Proton's own open-source [web client](https://github.com/ProtonMail/WebClients) into an [API reference](https://proton-cli.lerchster.dev/api-reference/). A weekly workflow regenerates it, so the CLI tracks upstream changes rather than guessing.
