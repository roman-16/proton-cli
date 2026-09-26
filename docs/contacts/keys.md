# proton contacts keys

Public keys pinned to a contact.

Pinning a key means mail to that address is encrypted to the key you trust, rather than to whatever the server hands back. Whether it is encrypted at all is one of the address's settings: see `contacts emails`.

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

A key is pinned to one address. Name that address as REF when the contact holds more than one; naming the contact is enough when they hold one.

Pinning turns encryption to the address on. To keep a key for verifying signatures only, pin it and then run `contacts emails update REF --encrypt off`.

```
proton contacts keys pin REF
```

```bash
proton contacts keys pin jane --key jane-pubkey.asc
proton contacts keys pin jane@example.com --key -
```

| Flag | Description |
| --- | --- |
| `--key string` | Armoured public key file (- for stdin) |

## `unpin`

Remove the keys pinned to a contact.

A key is pinned to one address. Name that address as REF when the contact holds more than one; naming the contact is enough when they hold one.

```
proton contacts keys unpin REF
```

```bash
proton contacts keys unpin jane
proton contacts keys unpin jane@example.com
```

---

Every command also takes the [flags that work everywhere](../about/commands.md#flags-that-work-on-every-command).
