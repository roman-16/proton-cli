# proton account security-log

Sign-ins and credential changes Proton recorded.

Nothing is kept until recording is on, and it can keep the IP address of each event as well. Sign-ins and changes made with proton are not recorded.

Turning recording off deletes the events with it.

Every command under `proton account security-log`, with the arguments and flags it takes. For these commands in use, see [the account guide](README.md).

Holds `delete`, `disable`, `enable`, `get` and `list`.

## `delete`

Remove every event the log holds.

Your password is asked for again.

What is recorded from now on is unchanged, and the log fills up again from the next sign-in.

```
proton account security-log delete
```

```bash
proton account security-log delete
proton account security-log delete --yes --password-file /run/secrets/proton
```

| Flag | Description |
| --- | --- |
| `--password-file string` | Read the account password from a file, or - for stdin |
| `--totp string` | Two-factor code |

## `disable`

Stop recording sign-ins and credential changes.

Your password is asked for again.

Turning recording off deletes the events it holds, and this refuses while there are any. `security-log delete` removes them.

--detailed stops the IP addresses being recorded and leaves everything else on, the events included.

```
proton account security-log disable
```

```bash
proton account security-log disable --detailed
proton account security-log disable
```

| Flag | Description |
| --- | --- |
| `--detailed` | Stop recording the IP address of each event |
| `--password-file string` | Read the account password from a file, or - for stdin |
| `--totp string` | Two-factor code |

## `enable`

Record sign-ins and credential changes.

Your password is asked for again.

--detailed records the IP address of each event as well, and turns recording on if it was off.

```
proton account security-log enable
```

```bash
proton account security-log enable
proton account security-log enable --detailed
proton account security-log enable --detailed --password-file /run/secrets/proton
```

| Flag | Description |
| --- | --- |
| `--detailed` | Also record the IP address of each event |
| `--password-file string` | Read the account password from a file, or - for stdin |
| `--totp string` | Two-factor code |

## `get`

Show what is being recorded, and how much of it there is.

```
proton account security-log get
```

```bash
proton account security-log get
```

## `list`

List what Proton recorded, newest first.

An IP column appears once `enable --detailed` is on. Device, location, provider and protection are filled in for an account Proton Sentinel watches.

--output json carries every field of each event, and --limit 0 answers with the whole log rather than a page.

```
proton account security-log list
```

```bash
proton account security-log list
proton account security-log list --limit 0 --output json
```

| Flag | Description |
| --- | --- |
| `--limit int` | How many events per page; 0 for all of them (default `25`) |
| `--page int` | Which page of results, counting from zero |

---

Every command also takes the [flags that work everywhere](../about/commands.md#flags-that-work-on-every-command).
