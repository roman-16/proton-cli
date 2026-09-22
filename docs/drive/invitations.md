# proton drive invitations

Shares other people have offered you.

Every command under `proton drive invitations`, with the arguments and flags it takes. For these commands in use, see [the drive guide](README.md).

Holds `accept`, `decline` and `list`.

## `accept`

Accept invitations.

```
proton drive invitations accept REF...
```

```bash
proton drive invitations accept 5bH2mQxK
```

## `decline`

Decline invitations.

```
proton drive invitations decline REF...
```

```bash
proton drive invitations decline 5bH2mQxK
```

## `list`

List invitations waiting for an answer.

NAME is what is being offered and TYPE what kind of thing it is: a file, a folder, or a photo album. A name that cannot be decrypted is left empty, and the invitation can still be accepted or declined by the ID beside it.

```
proton drive invitations list
```

```bash
proton drive invitations list
```

| Flag | Description |
| --- | --- |
| `--desc` | Reverse the order |
| `--limit int` | How many invitations per page; 0 for all of them |
| `--page int` | Which page of results, counting from zero |
| `--sort string` | Order by: created, name, sender (default `created`) |

---

Every command also takes the [flags that work everywhere](../about/commands.md#flags-that-work-on-every-command).
