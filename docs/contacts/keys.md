# proton contacts keys

Public keys you trust for a contact.

Trusting a key means mail to that address is encrypted to that key, rather than to whatever the server hands back. Whether it is encrypted at all is one of the address's settings: see `contacts emails`.

Every command under `proton contacts keys`, with the arguments and flags it takes. For these commands in use, see [the contacts guide](README.md).

Holds `list`, `trust` and `untrust`.

## `list`

List the keys you trust for a contact.

```
proton contacts keys list REF
```

```bash
proton contacts keys list jane
```

## `trust`

Trust a public key so mail to a contact is encrypted to it.

A key is trusted for one address. Name that address as REF when the contact holds more than one; naming the contact is enough when they hold one.

Trusting turns encryption to the address on. To keep a key for verifying signatures only, trust it and then run `contacts emails update REF --encrypt off`.

```
proton contacts keys trust REF
```

```bash
proton contacts keys trust jane --key jane-pubkey.asc
proton contacts keys trust jane@example.com --key -
```

| Flag | Description |
| --- | --- |
| `--key string` | Armoured public key file (- for stdin) |

## `untrust`

Stop trusting a contact's keys.

Keys are trusted for one address. Name that address as REF when the contact holds more than one; naming the contact is enough when they hold one.

```
proton contacts keys untrust REF
```

```bash
proton contacts keys untrust jane
proton contacts keys untrust jane@example.com
```

---

Every command also takes the [flags that work everywhere](../about/commands.md#flags-that-work-on-every-command).
