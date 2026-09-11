# proton account keys

The keys your data is encrypted with.

Every command under `proton account keys`, with the arguments and flags it takes. For these commands in use, see [the account guide](README.md).

Holds `reactivate`.

## `reactivate`

Bring back the keys a password reset locked.

Give one secret from before the reset - the password, the recovery phrase or a recovery file - and your current password to confirm. With no flag, the previous password is asked for; in two-password mode that is the second one. Keys the secret does not open stay locked and are named. Drive files come back with `proton drive volumes restore`.

```
proton account keys reactivate
```

```bash
proton account keys reactivate
proton account keys reactivate --recovery-phrase
proton account keys reactivate --recovery-file ~/Downloads/proton_recovery.asc
proton account keys reactivate --previous-password-file /run/secrets/proton-old --password-file /run/secrets/proton
```

| Flag | Description |
| --- | --- |
| `--password-file string` | Read the account password from a file |
| `--password-stdin` | Read the account password from stdin |
| `--previous-password-file string` | Read the password from before the reset from a file |
| `--previous-password-stdin` | Read the password from before the reset from stdin |
| `--recovery-file string` | Recover with a recovery file downloaded from Proton |
| `--recovery-phrase` | Recover with the recovery phrase, asked for at the prompt |
| `--recovery-phrase-file string` | Read the recovery phrase from a file |
| `--recovery-phrase-stdin` | Read the recovery phrase from stdin |
| `--totp string` | Two-factor code |

---

Every command also takes the [flags that work everywhere](../about/commands.md#flags-that-work-on-every-command).
