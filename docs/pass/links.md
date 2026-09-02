# proton pass links

Links that show an item to somebody without an account.

Every command under `proton pass links`, with the arguments and flags it takes. For these commands in use, see [the pass guide](README.md).

Holds `create`, `get`, `list` and `revoke`.

## `create`

Make a link that shows one item to somebody with no Proton account.

The URL is the secret. The key that opens the item travels in the part after the '#', which a browser never sends to Proton, so anyone holding the whole URL can read the item until the link expires or is revoked.

--expires is required.

```
proton pass links create REF
```

```bash
proton pass links create github.com --expires 7d
proton pass links create github.com --expires 24h --views 1
```

| Flag | Description |
| --- | --- |
| `--expires string` | How long the link lasts (e.g. 7d, 24h) |
| `--views int` | Stop working after this many openings |

## `get`

Show one link, URL and all.

Use this to recover a link you mislaid, rather than revoking it and making a new one.

The URL is the secret, so it appears here and in no listing.

```
proton pass links get REF
```

```bash
proton pass links get 5bH2mQxK
```

## `list`

List the links you have made.

The URLs are not shown: each one carries the key that opens its item. To read a URL, use `links get`, or `items share get` for the links on one item.

```
proton pass links list
```

```bash
proton pass links list
```

## `revoke`

Stop a link working.

The item is untouched; only the link stops working. This cannot take back what somebody already read.

```
proton pass links revoke REF...
```

```bash
proton pass links revoke 5bH2mQxK
```

---

Every command also takes the [flags that work everywhere](../about/commands.md#flags-that-work-on-every-command).
