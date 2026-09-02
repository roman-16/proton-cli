# proton account

Your Proton account, its settings and your session.

Every command under `proton account`, with the arguments and flags it takes. For these commands in use, see [the account guide](README.md).

Holds `get`, `login`, `logout`, `profiles`, `sessions` and `settings`.

## `get`

Show the account, its storage and this machine's session.

```
proton account get
```

```bash
proton account get
proton account get --output json
```

## `login`

Sign in and save the session for this profile.

Signing in also unlocks your keys, so your password is needed once per machine and not again. Anything a flag has not set is asked for, as long as this is a terminal.

Security key: you are asked to touch it. If the account also has an authenticator app, pressing Enter at the code prompt reaches for the key instead. A key needs a person present, so unattended jobs want --totp.

Two-password mode: the second password is asked for after signing in. That is the password your keys are locked with. A one-password account is never asked for it.

Pass extra password: not asked for here. The first `pass` command asks, and the session can then reach Pass for as long as it lives. Pass it now with --extra-password-file when there is nobody to ask.

Human verification: Proton may ask you to prove you are human. The page is printed and can be solved on any device, so a machine with no display signs in like any other. A run that cannot ask prints the page and the token to repeat the command with.

Signing in again as the same account changes nothing, so an unattended job can run it first to recover from an expired session.

```
proton account login
```

```bash
proton account login
proton account login --profile work
proton account login --user me@proton.me --password-file /run/secrets/proton
proton account login --user me@proton.me --password-stdin --totp 123456
proton account login --user me@proton.me --password-file /run/secrets/proton --second-password-file /run/secrets/proton-second
proton account login --user me@proton.me --password-file /run/secrets/proton --extra-password-file /run/secrets/proton-pass
```

| Flag | Description |
| --- | --- |
| `--extra-password-file string` | Read the Pass extra password from a file |
| `--extra-password-stdin` | Read the Pass extra password from stdin |
| `--password-file string` | Read the account password from a file |
| `--password-stdin` | Read the account password from stdin |
| `--second-password-file string` | Read the second password (two-password mode) from a file |
| `--second-password-stdin` | Read the second password (two-password mode) from stdin |
| `--totp string` | Two-factor code |
| `--user string` | Proton account email to sign in as |

## `logout`

Discard the saved session for this profile.

The key password on disk is sealed with a key held by Proton, so deleting the session file is enough to make it unreadable.

--revoke also invalidates the session at Proton, the same as signing out in a Proton app. Use it if the file may have been copied.

```
proton account logout
```

```bash
proton account logout
proton account logout --revoke
proton account logout --all
```

| Flag | Description |
| --- | --- |
| `--all` | Act on everything in scope, rather than a subset |
| `--revoke` | Also invalidate the session at Proton |

---

Every command also takes the [flags that work everywhere](../about/commands.md#flags-that-work-on-every-command).
