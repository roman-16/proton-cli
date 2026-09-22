# proton drive links

Links that open a file or folder for anyone.

Every command under `proton drive links`, with the arguments and flags it takes. For these commands in use, see [the drive guide](README.md).

Holds `create`, `get`, `list` and `revoke`.

## `create`

Make a link that opens a file or folder for anyone.

An item carries one link, so running it again changes that link rather than making a second one, and a URL you have already shared keeps working.

The password is read from a file, never from a flag value, and may be at most 50 characters. --clear-link-password takes it off again, and --expires never makes an expiring link permanent.

```
proton drive links create PATH
```

```bash
proton drive links create /Documents/report.pdf --expires 7d
proton drive links create /Documents/report.pdf --expires 7d --link-password-file /run/secrets/report-link
proton drive links create /Documents/report.pdf --clear-link-password --expires never
proton drive links create /Documents --access editor
```

| Flag | Description |
| --- | --- |
| `--access string` | What they may do with it: viewer, editor (default `viewer`) |
| `--clear-link-password` | Remove the public link's password |
| `--computer string` | Work inside this computer's files, by name or ID |
| `--expires string` | Stop working after DURATION (e.g. 7d, 2w, 6mo), or never |
| `--link-password-file string` | Read the public link's password from a file, or - for stdin |
| `--shared string` | Work inside an item shared with you, by name or ID |

## `get`

Show the link on a file or folder, URL and all.

Use it to recover a URL you mislaid, rather than revoking the link and making a new one. The URL appears here and in no listing.

```
proton drive links get PATH
```

```bash
proton drive links get /Documents/report.pdf
```

| Flag | Description |
| --- | --- |
| `--computer string` | Work inside this computer's files, by name or ID |
| `--shared string` | Work inside an item shared with you, by name or ID |

## `list`

List the links you have made.

The URLs are not shown: each one opens its item for anybody holding it. To read a URL, use `links get`, or `photos links get` for a link on a photo.

```
proton drive links list
```

```bash
proton drive links list
```

| Flag | Description |
| --- | --- |
| `--desc` | Reverse the order |
| `--limit int` | How many links per page; 0 for all of them |
| `--page int` | Which page of results, counting from zero |
| `--sort string` | Order by: name, created, opened (default `name`) |

## `revoke`

Stop the link on a file or folder working.

The item is untouched; only the link stops working. This cannot take back what somebody already read.

```
proton drive links revoke PATH
```

```bash
proton drive links revoke /Documents/report.pdf
```

| Flag | Description |
| --- | --- |
| `--computer string` | Work inside this computer's files, by name or ID |
| `--shared string` | Work inside an item shared with you, by name or ID |

---

Every command also takes the [flags that work everywhere](../about/commands.md#flags-that-work-on-every-command).
