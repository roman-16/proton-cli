# proton drive invitations

Shares other people have offered you.

Every command under `proton drive invitations`, with the arguments and flags it takes. For these commands in use, see [the drive guide](README.md).

Holds `abuse`, `accept`, `decline` and `list`.

## `abuse`

Report what an invitation offers to Proton, without accepting it.

Proton receives the key to the whole of what is offered. Copyright and stolen-data reports need --message and --email. Nothing withdraws a report, and the invitation stays until you accept or decline it.

```
proton drive invitations abuse REF
```

```bash
proton drive invitations abuse 5bH2mQxK --category spam --good-faith
proton drive invitations abuse 5bH2mQxK --category stolen-data --message 'These are our customer records' --email jane@proton.me --good-faith
```

| Flag | Description |
| --- | --- |
| `--category string` | What the report is about: spam, copyright, child-abuse, non-consensual-intimate, stolen-data, malware, other |
| `--email string` | Where Proton can reach you about the report; required for copyright and stolen-data |
| `--good-faith` | Confirm, in good faith, that what the report says is correct and complete |
| `--message string` | What Proton should know; required for copyright and stolen-data |

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
