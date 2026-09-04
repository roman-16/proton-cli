# FAQ

Whether proton-cli is official, how it compares to Proton's own tools, and whether it is safe to give it your password.

If your question is "why did this command fail", [Troubleshooting](../help/troubleshooting.md) is the page you want.

## Is proton-cli made by Proton?

No. It is an independent, community-built project. It is not endorsed by, affiliated with, or supported by Proton AG. Proton is a trademark of Proton AG.

It uses Proton's own open-source cryptography - [go-srp](https://github.com/ProtonMail/go-srp) for login and [gopenpgp](https://github.com/ProtonMail/gopenpgp) for the key hierarchy - and talks to the same API as the Proton web apps. But it is not one of Proton's products.

## How does it compare to Proton's own tools?

Proton ships a CLI for Drive and one for Pass, and Bridge for mail in a desktop client.

| | Mail | Drive | Calendar | Pass | Contacts |
| --- | --- | --- | --- | --- | --- |
| **proton-cli** | ✓ | ✓ | ✓ | ✓ | ✓ |
| Proton Drive CLI | | ✓ | | | |
| Proton Pass CLI | | | | ✓ | |
| Proton Mail Bridge | IMAP/SMTP only | | | | |

Use Proton's own tool when it covers what you need and you want first-party support.

Use this one when you want Mail, Calendar or Contacts from a terminal, or one binary with one grammar and one output shape across all five apps.

They coexist fine. Nothing here changes anything another client relies on.

## How is it different from Proton Mail Bridge?

Bridge is a background service that speaks IMAP and SMTP to a desktop mail client on the same machine. proton-cli is a command you run.

| | Proton Mail Bridge | proton-cli |
| --- | --- | --- |
| What it is | A local IMAP/SMTP server | A single binary you invoke |
| Runs | Continuously, in the background | When you run it, then exits |
| You use it with | Thunderbird, Outlook, Apple Mail | Your shell, a script, cron |
| Covers | Mail | Mail, Drive, Calendar, Pass, Contacts |
| Needs a paid plan | Yes | No |
| Headless server | Possible, awkward | Yes |

If you want mail in a graphical client, use Bridge. If you want mail in a pipeline, use this.

## Is it safe to give it my Proton password?

Your password is never sent to Proton and never leaves your machine.

Login uses [SRP](https://en.wikipedia.org/wiki/Secure_Remote_Password_protocol), a challenge-response exchange in which the server proves it knows your verifier while you prove you know your password. Neither side ever transmits it. The key password derived from it stays local and unlocks your PGP keys in memory.

proton writes one file per profile, mode `0600`. It holds your session tokens and your key password, encrypted with a random key that lives on Proton's servers rather than yours. So revoking the session from any Proton app makes a leaked copy of that file undecryptable.

The honest caveat: **proton-cli is unaudited**, and it is a third-party program you are trusting with a credential. Read [Security and encryption](security.md) before you decide, and judge the source rather than this paragraph.

## Can Proton ban my account for using it?

Proton's Terms of Service govern how you may use your account, and automated access is your responsibility to keep within them.

Ordinary interactive use looks like any other client to Proton. Hammering the API in a tight loop looks like abuse, because it is.

Nobody here can speak for Proton. Sending thousands of messages, polling every second, or scripting anything a person would not plausibly do by hand is where accounts get flagged, and no tool protects you from that.

## Do I need a paid Proton plan?

No. Everything works on a free account, within the limits Proton puts on free accounts. The sending quota is the one you will meet first.

Bridge, by contrast, is paid-only.

## Does it work on a headless server?

Yes, and it signs in for itself.

Proton sometimes asks for a CAPTCHA at login. proton prints the verification page and waits while you solve it on whatever device is to hand, then carries on. There is nothing to install. [What that looks like](../help/troubleshooting.md#proton-asks-for-a-captcha).

## Can I use two Proton accounts at once?

Yes. Each account gets a named profile with its own session file, and they never mix:

```bash
proton account login --profile work
proton --profile work mail messages list
export PROTON_PROFILE=work          # make it the default for this shell
```

See [More than one account](../account/README.md#more-than-one-account).

## Does it sync folders like rclone or Dropbox?

No, and it is not trying to. proton-cli runs an operation and exits: upload this, download that, list what is there. There is no watcher, no daemon and no conflict resolution.

For a scheduled one-way copy, `proton drive items upload --recursive` in a cron job or systemd timer is the shape that fits. [Scripting](../using/scripting.md) has working examples.

## What about VPN, Wallet, Docs, Meet, Lumo and Authenticator?

Not covered, and not planned. proton-cli mirrors what the Proton **web clients** let you do for Mail, Drive, Calendar, Pass and Contacts.

Proton VPN has [its own CLI](https://protonvpn.com/support/linux-vpn-tool). See [What it can't do](../help/limits.md).

## Can it import my Gmail, or add a Zoom link to an event?

No, and neither can any terminal program. Both hand you off to another company's consent screen - Google's, Zoom's - and only a browser can complete one. Proton's own importer works the same way.

Set those up in the web client. Whatever they produce is readable here like anything else.

## Does it support security keys?

Yes. `proton account login` asks you to touch the key you registered with Proton, and signs in with it:

```console
$ proton account login
Email:             you@proton.me
Password:
Touch your security key.
✓ Signed in as you@proton.me (profile "default").
```

On Linux and macOS the key has to be one you plug into USB. On Windows the sign-in goes through Windows Hello, so a key built into the machine works too. A key held in a phone does not: that needs the Bluetooth handoff a browser does.

Registering a key is a browser's job as well, since WebAuthn enrolment has no non-browser form. Add it in Proton's security settings once, and `login` uses it from then on.

If the account also has an authenticator app, a code is asked for and pressing Enter reaches for the key instead. A touch needs you there, so unattended jobs still want `--totp` - or better, a session you signed in once and left in place.

It will also happily *read* you a TOTP code for any other service: `proton pass items totp github.com`.

## Is my mail actually decrypted locally?

Yes. Content arrives encrypted, is decrypted on your machine with your own keys, and outgoing content is encrypted and signed before it leaves. No bridge, no proxy, no server in the middle.

`proton mail messages get` prints a `Signature:` line reporting the verdict of the signature check against the sender's key.

**Anything you export is plaintext**, for the same reason. An `.eml` file on disk is readable by anything that can read a file.

## Which platforms does it run on?

Linux, macOS and Windows, on amd64 and arm64, as one static binary.

Install it with Homebrew, winget, APT, AUR, Nix, npm, a shell one-liner, or by downloading a checksummed binary. See [Install](../install.md).

## Is the source open, and is it audited?

The source is [on GitHub](https://github.com/roman-16/proton-cli) under the MIT licence. It has **not** been audited.

If you find a vulnerability, [report it privately](https://github.com/roman-16/proton-cli/security/advisories/new) rather than in a public issue.

## Something is not covered. Can I still reach it?

Yes. `proton api` sends an authenticated request to any Proton endpoint using the session you already have:

```bash
proton api GET /drive/volumes
proton api POST /calendar/v1 --body '{"Name":"Work","Color":"#7272a7","Display":1,"AddressID":"..."}'
```

Encrypted fields come back encrypted; that command does no key handling. See [Raw API](../api/README.md).
