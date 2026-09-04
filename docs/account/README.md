# Account

Sign in once and your password is never asked for again on that machine.

This page covers signing in and out, unattended and two-password sign-in, running several Proton accounts side by side, the sessions Proton holds across your devices, and your account settings.

For every command and flag, see the reference: [account](account.md), [sessions](sessions.md), [profiles](profiles.md), [settings](settings.md).

## Check who you are signed in as

```console
$ proton account get
Email:       you@proton.me
Name:        Roman
Storage:     128.4 GB of 500 GB (26%)
Max Upload:  5.0 GB
Profile:     default
Session:     valid
Unlocked:    yes
ID:          Kd91mQxT…
```

`Session: valid` and `Unlocked: yes` together mean this machine can act as the account right now.

## Sign in

```bash
proton account login
```

It asks for your email, password and two-factor code, attaches the account to a profile, and saves the session. Every later command acts as whichever profile it names.

Signing in also **unlocks your keys**, so your password is needed once per machine and not again. Your password itself never leaves your machine: it derives the keys that decrypt your data locally. See [Security and encryption](../about/security.md).

Signing in again as the same account changes nothing, so an unattended job can run it ahead of its real work and recover on its own from a session that expired or was revoked:

```bash
proton account login --user "$ACCOUNT" --password-file "$CRED"
proton drive items upload backup.zst /backups
```

A second factor is only asked for when the account actually has one enabled.

### Security keys

An account that signs in with a security key is asked to touch it:

```console
$ proton account login
Email:             alice@proton.me
Password:
Touch your security key.
✓ Signed in as alice@proton.me (profile "default").
```

With an authenticator app enabled as well, the code prompt is the choice: type a code, or press Enter to use the key.

```console
$ proton account login
Email:             alice@proton.me
Password:
This account also has a security key. Press Enter to use it instead of a code.
Two-factor code:
Touch your security key.
✓ Signed in as alice@proton.me (profile "default").
```

The key has to be one you plug in. On Windows the sign-in goes through Windows Hello, so a key built into the machine counts.

A passkey living in a phone does not work, because reaching it needs the Bluetooth handoff only a browser performs. Sign in with a code instead.

**On Linux, a key needs udev rules** to be readable by anyone but root. Every distribution ships them with `libfido2` or its own FIDO package, and `login` says so if it finds a key it cannot open. See [Troubleshooting](../help/troubleshooting.md#a-security-key-that-nothing-finds).

### Two-password mode

Proton can keep the password that proves who you are apart from the one that opens your data.

If your account is in [two-password mode](https://proton.me/support/switch-two-password-mode), `login` asks for the second password once it has signed in, exactly where Proton's own sign-in asks for it:

```console
$ proton account login
Email:            alice@proton.me
Password:
Second password:
✓ Signed in as alice@proton.me (profile "default").
```

The second password is the one sealed into the session, so afterwards nothing is asked again. `proton account settings get` reports which mode the account is in.

### A Pass extra password

Pass can be protected with [a password of its own](https://proton.me/support/pass-extra-password). `login` does not ask for it, because only Pass needs it: the first `proton pass` command asks, and Proton then lets the session reach Pass for as long as it lives.

An unattended run has nobody to ask, so it hands the password over here instead:

```bash
proton account login --user alice@proton.me \
    --password-file /run/secrets/proton \
    --extra-password-file /run/secrets/proton-pass
```

On an account with no extra password, the flag is reported as unnecessary and the sign-in carries on. See [Pass](../pass/README.md#an-extra-password) for what it changes there.

### Sign in without a terminal

A password is read from a pipe or a file, [never from a flag value](../about/why.md#why-a-password-is-never-a-flag-value).

```bash
# from a pipe
printf '%s' "$PW" | proton account login --user alice@proton.me --password-stdin

# from a file
proton account login --user alice@proton.me --password-file /run/secrets/proton
```

The second and extra passwords are read the same way, through flags of their own.

Standard input has one reader, so an account with more than one secret takes at most one of them from a pipe:

```bash
proton account login --user alice@proton.me \
    --password-file /run/secrets/proton \
    --second-password-file /run/secrets/proton-second
```

### Commands that ask for the password again

These commands reach an endpoint Proton guards behind an elevated session, and ask for your password again at that moment:

- `calendar settings calendars delete`
- `mail messages expire`
- `mail settings autoreply disable`
- `mail settings autoreply enable`
- `mail settings autoreply set`

```bash
printf '%s' "$PW" | proton calendar settings calendars delete Work --password-stdin
```

`--password-stdin` takes standard input for the password and nothing else, so it cannot be combined with a `-` argument that wants the same stream:

```console
$ printf '%s' "$PW" | proton --password-stdin mail messages send --body - ...
Error: --password-stdin and --body - both read standard input, which can only be read once.
Try:   pass it with --password-file instead
```

## Sign out

```bash
proton account logout              # forget the session on this machine
proton account logout --revoke     # also invalidate it at Proton
proton account logout --all        # every profile on this machine
```

Revoking is what makes a leaked copy of the session file worthless: the sealed key password cannot be opened without a live session.

## More than one account

A profile is a named session slot on this machine, so a personal and a work account never mix.

```console
$ proton account login --profile work
✓ Signed in as you@company.com (profile "work").

$ proton --profile work mail messages list
```

To see what is signed in here, answered from disk with no API call:

```console
$ proton account profiles list
PROFILE   EMAIL             UNLOCKED  SAVED             ACTIVE
────────  ────────────────  ────────  ────────────────  ──────
default   you@proton.me     yes       2026-04-15 14:31  ✓
work      you@company.com   yes       2026-04-15 15:02
```

To make a profile the default for a shell:

```bash
export PROTON_PROFILE=work
proton mail messages list
```

A profile nobody signed in acts as nobody, and says so before it reaches the network:

```console
$ proton --profile work mail messages list
Error: Profile "work" is not signed in.
Try:   proton account login --profile work
```

Pointing a profile at a different account works, but has to be said out loud, since the name means that account everywhere else:

```console
$ proton account login --profile work --user someone@else.com
Error: Profile "work" is signed in as alice@company.com.
Try:   proton account logout --profile work
```

## Sessions on your other devices

Every session Proton holds for your account, across all your devices. This is the "Sessions" section of Proton's account settings.

```console
$ proton account sessions list
ID        CLIENT     CREATED           CURRENT
────────  ─────────  ────────────────  ───────
7Kd91mQx  web-mail   2026-04-15 14:31  ✓
3Ns8pT2v  ios-mail   2026-03-02 08:11
9xL4pQrT  web-drive  2026-01-20 19:44
```

```bash
proton account sessions revoke 3Ns8pT2v
proton account sessions revoke --others     # everything but this one
```

**If you lose a device, revoke its session.** That also makes the credentials saved on it useless, even to someone who already copied the file.

## Where the session lives

`account login` writes `~/.config/proton-cli/sessions/<profile>.json` with mode `0600` and reuses it, so later commands do not re-authenticate.

It holds the session tokens and your key password, [encrypted with a key held on Proton's side](../about/security.md#what-is-stored-on-disk).

Two things end a session: revoking it, and Proton expiring it. Either means signing in again.

## Settings

```bash
proton account settings get     # the values now in effect
proton account settings list    # the keys you can change, with their values
proton account settings set locale de_AT
```

| Key | Values |
| --- | --- |
| `locale` | Any text, such as `de_AT` |
| `date-format` | `locale`, `dd/mm/yyyy`, `mm/dd/yyyy`, `yyyy-mm-dd` |
| `time-format` | `locale`, `24h`, `12h` |
| `week-start` | `locale`, `monday` … `sunday` |
| `crash-reports` · `telemetry` | `off`, `on` |

Values can be given by name or by Proton's own number, and mistakes are caught before anything is sent.

`get` shows more than `set` can change. Proton Sentinel, two-factor state, whether the account is in two-password mode, and recovery addresses are readable here but can only be changed at [account.proton.me](https://account.proton.me), along with your password, recovery secrets, billing and account deletion.

Mail, Calendar and Drive each have settings of their own, under `proton mail settings` and so on. Pass and Contacts have none.
