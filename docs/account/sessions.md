# proton account sessions

Sessions Proton holds for this account.

Every command under `proton account sessions`, with the arguments and flags it takes. For these commands in use, see [the account guide](README.md).

Holds `list` and `revoke`.

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

```
proton account sessions revoke [REF...]
```

```bash
proton account sessions revoke 5bH2mQxK
proton account sessions revoke --others
```

| Flag | Description |
| --- | --- |
| `--others` | Revoke every session except this one |

---

Every command also takes the [flags that work everywhere](../about/commands.md#flags-that-work-on-every-command).
