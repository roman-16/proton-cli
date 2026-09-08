# proton drive shared

Files and folders other people have shared with you.

Every command under `proton drive shared`, with the arguments and flags it takes. For these commands in use, see [the drive guide](README.md).

Holds `add`, `leave`, `list` and `remove`.

## `add`

Add a public link to what is shared with you.

URL is the link as it was sent to you, including everything after the #. Once added it appears in `shared list` and opens with `--shared REF`, with nothing to pass again. A link with a password takes it from --link-password-file or --link-password-stdin and keeps it.

```
proton drive shared add URL
```

```bash
proton drive shared add 'https://drive.proton.me/urls/7X2K9M3N1P#kQ81mDx4T9wL'
proton drive shared add 'https://drive.proton.me/urls/7X2K9M3N1P#kQ81mDx4T9wL' --link-password-file /run/secrets/q3-link
```

| Flag | Description |
| --- | --- |
| `--link-password-file string` | Read the public link's password from a file |
| `--link-password-stdin` | Read the public link's password from stdin |

## `leave`

Give up an item somebody shared with you.

Only a new invitation from whoever shared it brings it back, so this asks first. To take a public link you saved out of the listing, use `shared remove`.

```
proton drive shared leave REF...
```

```bash
proton drive shared leave Project
proton drive shared leave Project --yes
```

## `list`

List what other people have shared with you.

Items shared with you directly and public links you saved with `shared add` are listed together. A saved link shows `public link` under SHARED BY.

These are not in your tree and have no path of their own. To open one, pass `--shared REF` to any `items` command: / is then the item itself, and anything below it is a path inside it. A saved link can only be listed, shown and downloaded.

An item whose name cannot be decrypted is still listed, so you can still act on it by ID.

```
proton drive shared list
```

```bash
proton drive shared list
```

## `remove`

Forget a public link you saved.

The link itself keeps working, and `shared add` brings it back. To give up an item somebody shared with you directly, use `shared leave`.

```
proton drive shared remove REF...
```

```bash
proton drive shared remove Q3-report.pdf
```

---

Every command also takes the [flags that work everywhere](../about/commands.md#flags-that-work-on-every-command).
