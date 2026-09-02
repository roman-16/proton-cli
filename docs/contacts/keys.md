# proton contacts keys

Public keys pinned to a contact.

Pinning a key means mail to that address is encrypted to the key you trust, rather than to whatever the server hands back.

Every command under `proton contacts keys`, with the arguments and flags it takes. For these commands in use, see [the contacts guide](README.md).

Holds `list`, `pin` and `unpin`.

## `list`

List the keys pinned to a contact.

```
proton contacts keys list REF
```

```bash
proton contacts keys list jane
```

## `pin`

Pin a public key so mail to a contact is encrypted to it.

```
proton contacts keys pin REF
```

```bash
proton contacts keys pin jane --key jane-pubkey.asc
proton contacts keys pin jane --email jane@example.com --key - --no-encrypt
```

| Flag | Description |
| --- | --- |
| `--email string` | Which of the contact's addresses the key applies to |
| `--key string` | Armoured public key file (- for stdin) |
| `--no-encrypt` | Store the key for verification only, leaving encryption off |
| `--scheme string` | PGP scheme for recipients outside Proton: pgp-mime, pgp-inline |

## `unpin`

Remove the keys pinned to a contact.

```
proton contacts keys unpin REF
```

```bash
proton contacts keys unpin jane
proton contacts keys unpin jane --email jane@example.com
```

| Flag | Description |
| --- | --- |
| `--email string` | Which of the contact's addresses to unpin |

---

Every command also takes the [flags that work everywhere](../about/commands.md#flags-that-work-on-every-command).
