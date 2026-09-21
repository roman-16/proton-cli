# Account

Sign in once and your password is never asked for again on that machine.

This page covers signing in and out, unattended and two-password sign-in, running several Proton accounts side by side, the sessions Proton holds across your devices, unlocking data after a password reset, and your account settings.

For every command and flag, see the reference: [account](account.md), [keys](keys.md), [security-log](security-log.md), [sessions](sessions.md), [profiles](profiles.md), [settings](settings.md).

## Check who you are signed in as

```console
$ proton account get
Email:       you@proton.me
Name:        Roman
Storage:     ━━━━━───────────────   26%  119.6 GB of 465.7 GB
Max Upload:  4.7 GB
Profile:     default
Session:     valid
Unlocked:    yes
ID:          Kd91mQxT7v
```

`Session: valid` and `Unlocked: yes` together mean this machine can act as the account right now.

## Sign in

```bash
proton account login
```

It asks for your email, password and two-factor code, attaches the account to a profile, and saves the session. Every later command acts as whichever profile it names.

Signing in also **unlocks your keys**, so your password is needed once per machine and not again. Your password never leaves your machine. See [Security](../about/security.md).

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
Email: alice@proton.me
Password:
Touch your security key.
✓ Signed in as alice@proton.me (profile "default").
```

With an authenticator app enabled as well, the code prompt is the choice: type a code, or press Enter to use the key.

```console
$ proton account login
Email: alice@proton.me
Password:
This account also has a security key. Press Enter to use it instead of a code.
Two-factor code:
Touch your security key.
✓ Signed in as alice@proton.me (profile "default").
```

The key has to be one you plug in. On Windows the sign-in goes through Windows Hello, so a key built into the machine counts.

A passkey living in a phone does not work. Sign in with a code instead.

Registering a key is [Register a security key](#register-a-security-key).

**On Linux, a key needs udev rules** to be readable by anyone but root. Every distribution ships them with `libfido2` or its own FIDO package. See [Troubleshooting](../help/troubleshooting.md#a-security-key-that-nothing-finds).

### Two-password mode

If your account is in [two-password mode](https://proton.me/support/switch-two-password-mode), `login` asks for the second password once it has signed in:

```console
$ proton account login
Email: alice@proton.me
Password:
Second password:
✓ Signed in as alice@proton.me (profile "default").
```

Afterwards nothing is asked again. `proton account settings get` reports which mode the account is in.

### A Pass extra password

Pass can be protected with [a password of its own](https://proton.me/support/pass-extra-password). `login` does not ask for it; the first `proton pass` command does, once per session.

An unattended run has nobody to ask, so it hands the password over here instead:

```bash
proton account login --user alice@proton.me \
    --password-file /run/secrets/proton \
    --extra-password-file /run/secrets/proton-pass
```

On an account with no extra password, the flag is reported as unnecessary and the sign-in carries on. See [Pass](../pass/README.md#an-extra-password) for what it changes there.

### Sign in without a terminal

A password is read from a file, never from a flag value. A path of `-` reads standard input.

```bash
# from a file
proton account login --user alice@proton.me --password-file /run/secrets/proton

# from a pipe
printf '%s' "$PW" | proton account login --user alice@proton.me --password-file -
```

The second and extra passwords are read the same way, through flags of their own.

Standard input has one reader, so an account with more than one secret takes at most one of them from a pipe:

```bash
proton account login --user alice@proton.me \
    --password-file /run/secrets/proton \
    --second-password-file /run/secrets/proton-second
```

### Commands that ask for the password again

These commands ask for your password again even when you are signed in:

- `account keys reactivate`
- `account security-log delete` · `account security-log disable` · `account security-log enable`
- `account settings password set`
- `account settings recovery-email set` · `account settings recovery-email enable` · `account settings recovery-email disable`
- `account settings recovery-phone set` · `account settings recovery-phone enable` · `account settings recovery-phone disable`
- `account settings recovery-phrase set` · `account settings recovery-phrase disable`
- `account settings second-password set` · `account settings second-password disable`
- `account settings security-keys create` · `account settings security-keys delete`
- `account settings two-factor enable` · `account settings two-factor disable`
- `calendar settings calendars delete`
- `mail messages update`
- `mail settings addresses create`
- `mail settings addresses delete`
- `mail settings addresses disable`
- `mail settings addresses enable`
- `mail settings domains create`
- `mail settings domains delete`
- `mail settings domains update`
- `mail settings forwarding accept`
- `mail settings forwarding create`
- `mail settings forwarding delete`
- `mail settings autoreply disable`
- `mail settings autoreply enable`
- `mail settings autoreply set`

```bash
printf '%s' "$PW" | proton calendar settings calendars delete Work --password-file -
```

Only one thing per run may read standard input, so `--password-file -` cannot be combined with a `-` argument that wants the same stream:

```console
$ printf '%s' "$PW" | proton --password-file - mail messages send --body - ...
Error: --password-file - and --body - both read standard input, which can only be read once.
Try:   pass it with --password-file FILE instead
```

## After a password reset

A password reset locks everything encrypted before it. Mail, contacts, calendars, vaults and Drive stay sealed until you bring the old keys back:

```bash
proton account keys reactivate
```

It asks for the password from before the reset, then for your current password. In two-password mode, the one from before the reset is the second password.

```console
$ proton account keys reactivate
Previous password:
Current password:
✓ Reactivated 3 keys.
```

A recovery phrase or a recovery file works instead:

```bash
proton account keys reactivate --recovery-phrase
proton account keys reactivate --recovery-file ~/Downloads/proton_recovery.asc
```

Keys the secret does not open stay locked and are named. A key from an earlier reset opens with the secret from that time, so run the command again with it.

What was locked opens from the next command onwards. Drive files need one more step, under [Drive](../drive/README.md#after-a-password-reset).

`proton account get` counts what is still locked:

```console
$ proton account get
Email:        you@proton.me
Name:         Roman
Storage:      ━━━━━───────────────   26%  119.6 GB of 465.7 GB
Max Upload:   4.7 GB
Profile:      default
Session:      valid
Unlocked:     yes
Locked keys:  3
ID:           Kd91mQxT7v
```

Until then, a command that meets locked data names why:

```console
$ proton mail messages get "Invoice #2291"
Error: This message is sealed to a key that a password reset locked.
Try:   proton account keys reactivate
```

An unattended run hands both secrets over:

```bash
proton account keys reactivate \
    --previous-password-file /run/secrets/proton-old \
    --password-file /run/secrets/proton
```

## Sign out

```bash
proton account logout              # forget the session on this machine
proton account logout --revoke     # also invalidate it at Proton
proton account logout --all        # every profile on this machine
```

Revoking also makes a leaked copy of the session file worthless.

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

## Check the security log

Every sign-in and credential change Proton recorded for your account. This is the "Account monitor" section of Proton's account settings.

```console
$ proton account security-log list
TIME              EVENT            APP
────────────────  ───────────────  ─────────────────────
2026-08-13 15:09  Sign in success  web-account@5.0.407.1
2026-07-02 17:00  Sign in success  web-account@5.0.395.0
2026-06-24 11:33  Sign in success  web-account@5.0.391.1
2026-06-24 11:33  Sign in success  web-account@5.0.391.1
2026-06-24 11:33  Sign out         web-mail@5.0.119.4
2026-06-24 11:32  Sign in success  web-account@5.0.391.1
6 events.
```

A failed sign-in is drawn in red and an attempt in yellow. **Sign-ins and changes made with `proton` are not recorded.**

```console
$ proton account security-log get
Status:    on
Detailed:  off
Events:    6
```

```bash
proton account security-log enable
proton account security-log enable --detailed   # also record the IP address of each event
```

With `--detailed` an IP column appears in the listing. Device, location, provider and protection are filled in for an account Proton Sentinel watches.

`enable`, `disable` and `delete` ask for your password again, or take it from `--password-file`.

To keep the log somewhere of your own:

```bash
proton account security-log list --limit 0 --output json > events.json
```

Turning recording off deletes the events with it. While there are any, `disable` refuses:

```console
$ proton account security-log disable
Error: Turning the security log off deletes the 6 events it holds.
Try:   proton account security-log delete, then run this again
```

`proton account security-log delete` removes every event and goes on recording. `disable --detailed` stops the IP addresses being recorded and leaves the events alone.

## Where the session lives

In a file per profile on this machine, listed under [Files on disk](../using/settings.md#files-on-disk); what protects it is under [Security](../about/security.md#what-is-stored-on-disk). A session ends when it is revoked or when Proton expires it, and either means signing in again.

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

Values can be given by name or by number, and mistakes are caught before anything is sent.

`get` shows more than `set` can change. Proton Sentinel and Dark Web Monitoring are readable here and turned on at [account.proton.me](https://account.proton.me), along with billing and account deletion. Your password, your second factor and the ways back into the account have collections of their own, below.

Mail, Calendar and Drive each have settings of their own, under `proton mail settings` and so on. Pass and Contacts have none.

## Change your password

```bash
proton account settings password set
```

Your current password is asked for first, then the new one, twice. It needs at least eight characters and cannot be the one it replaces.

With nobody to ask, both come from files:

```bash
proton account settings password set \
  --password-file /run/secrets/proton --new-password-file /run/secrets/proton-new
```

This session goes on working. Another machine signed in to this account asks for the new password the next time it opens your keys.

## Turn two-password mode on or off

In two-password mode one password signs you in and a second one opens your keys.

```console
$ proton account settings second-password get
Status:  on
```

```bash
proton account settings second-password enable   # start using a second password
proton account settings second-password set      # change the second password
proton account settings second-password disable  # go back to one password
```

`enable` keeps the password you sign in with and asks for the second one, which then opens your keys. `disable` locks your keys with the password you sign in with again, and the second password stops working.

Signing in then takes both. Pass the second one with `--second-password-file`, or answer the prompt that follows your password.

In two-password mode, `account settings password set` changes the password you sign in with and leaves your keys alone.

## Ask for a code at every sign-in

```console
$ proton account settings two-factor get
Authenticator App:  off
Security Keys:      none
```

```bash
proton account settings two-factor enable
```

A secret is printed for your authenticator app, and a code from it confirms the app has it. Proton then prints recovery codes, once: each signs you in a single time if you lose the app.

With nobody to ask, it is two runs - one to make the secret, one to confirm it:

```bash
proton account settings two-factor generate --output json
proton account settings two-factor enable --totp 123456 --password-file /run/secrets/proton
```

`proton account settings two-factor disable` turns it off again, asking for your password and a current code. A recovery code works in place of the code.

A registered security key goes on being asked for after the authenticator app is turned off.

## Register a security key

```console
$ proton account settings security-keys create --name "YubiKey 5C"
Password:
Touch your security key.
Security key PIN:
✓ Created security key "YubiKey 5C" - a key is asked for at every sign-in.
```

The PIN is asked for only by a key that has one. On Windows the prompt comes from Windows itself.

The key has to be one you plug in. A built-in authenticator - Windows Hello, Touch ID, a machine's own secure element - and a passkey held in a phone are registered at [account.proton.me](https://account.proton.me).

An account may have four keys. From the first one, a key is asked for at every sign-in.

```console
$ proton account settings security-keys list
ID        NAME
────────  ──────────
5bH2mQxK  YubiKey 5C
9xL4pQrT  Spare key
2 security keys.
```

Rename one with `update`, which asks for nothing else:

```bash
proton account settings security-keys update "Spare key" --name "Key in the safe"
```

`delete` takes a key off the account and asks for your password:

```console
$ proton account settings security-keys delete "Key in the safe"
Would delete security key "Key in the safe". This cannot be undone. Continue? [y/N] y
Password:
✓ Deleted security key "Key in the safe".
```

Removing the last one stops a key being asked for at sign-in. The credential stays on the key itself, where only its manufacturer's own tool clears it.

## Set a recovery email or phone

```console
$ proton account settings recovery-email get
Address:         jane.roe@example.com
Verified:        yes
Allow Recovery:  on
```

```bash
proton account settings recovery-email set jane.roe@example.com
proton account settings recovery-email verify
```

`verify` mails the address a link; open it to finish. Until then Proton will not reset your password by email.

The phone works the same way, with the code arriving by SMS in two steps:

```bash
proton account settings recovery-phone set '+43 660 1234567'
proton account settings recovery-phone verify              # Proton texts a code
proton account settings recovery-phone verify --code 482913
```

`enable` and `disable` are whether Proton may reset your password through the address or number. Turning one off keeps it for security notices. `set none` removes it altogether.

## Set a recovery phrase

```bash
proton account settings recovery-phrase set
```

Twelve words are printed once and kept nowhere. Write them down: anyone holding them can open the account, and you recover with [`proton account keys reactivate --recovery-phrase`](#after-a-password-reset).

```console
$ proton account settings recovery-phrase get
Status:   on
Changed:  2026-04-16 09:12
```

The status is one of `on`, `outdated`, `not set` and `off`. An outdated phrase was set before your keys were replaced: it still opens the data it was made for, and it will not get you back into the account. Setting a new phrase replaces the old one, and `disable` removes it.
