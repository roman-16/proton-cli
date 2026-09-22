# proton account sessions

Sessions Proton holds for this account.

Every command under `proton account sessions`, with the arguments and flags it takes. For these commands in use, see [the account guide](README.md).

Holds `create`, `list` and `revoke`.

## `create`

Sign another device in from this one.

CODE is what the other device is showing: run `proton account login --qr` there, or read the code off a Proton app's sign-in screen. That device is signed in as this account, with no password typed on it, and stays signed in after this one signs out.

A code works once. Only ever pass one you are looking at yourself.

```
proton account sessions create CODE
```

```bash
proton account sessions create 0:8FJ3K2QP:qm4Xb2v9tR1sLp0yZcHwKdNfEuAgJi7MoBxVn3Tl5Qs=:Other
```

## `list`

List every signed-in session.

```
proton account sessions list
```

```bash
proton account sessions list
```

| Flag | Description |
| --- | --- |
| `--desc` | Reverse the order |
| `--limit int` | How many sessions per page; 0 for all of them |
| `--page int` | Which page of results, counting from zero |
| `--sort string` | Order by: created, client (default `created`) |

## `revoke`

Invalidate sessions at Proton.

A revoked session can no longer decrypt the key password sealed into its saved file, so revoking makes a leaked session file worthless.

Your password is asked for again. Pass it with --password-file when there is nobody to ask.

```
proton account sessions revoke [REF...]
```

```bash
proton account sessions revoke 5bH2mQxK
proton account sessions revoke --others
proton account sessions revoke 5bH2mQxK --password-file /run/secrets/proton
```

| Flag | Description |
| --- | --- |
| `--others` | Revoke every session except this one |
| `--password-file string` | Read the account password from a file, or - for stdin |
| `--totp string` | Two-factor code |

---

Every command also takes the [flags that work everywhere](../about/commands.md#flags-that-work-on-every-command).
