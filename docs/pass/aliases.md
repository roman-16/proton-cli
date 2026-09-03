# proton pass aliases

Hide-my-email addresses that forward to you.

Every command under `proton pass aliases`, with the arguments and flags it takes. For these commands in use, see [the pass guide](README.md).

Holds `contacts`, `create`, `disable`, `enable`, `list` and `options`.

## `contacts`

Addresses an alias can write to.

Holds `allow`, `block`, `create`, `delete` and `list`.

### `contacts allow`

Let a contact's mail reach you again.

```
proton pass aliases contacts allow REF ALIAS_CONTACT_REF...
```

```bash
proton pass aliases contacts allow shopping seller@example.com
```

### `contacts block`

Stop a contact's mail reaching you.

```
proton pass aliases contacts block REF ALIAS_CONTACT_REF...
```

```bash
proton pass aliases contacts block shopping seller@example.com
```

### `contacts create`

Make an address that writes to somebody as the alias.

Proton answers with a second address standing for that one person. Mail you send there reaches them as though the alias had written it, so your real address is never shown.

```
proton pass aliases contacts create REF EMAIL
```

```bash
proton pass aliases contacts create shopping seller@example.com
proton pass aliases contacts create shopping seller@example.com --name "The seller"
```

| Flag | Description |
| --- | --- |
| `--name string` | A name for them |

### `contacts delete`

Remove an address an alias can write to.

```
proton pass aliases contacts delete REF ALIAS_CONTACT_REF...
```

```bash
proton pass aliases contacts delete shopping seller@example.com
```

### `contacts list`

List the addresses an alias can write to.

```
proton pass aliases contacts list REF
```

```bash
proton pass aliases contacts list shopping
```

## `create`

Create an alias.

The address is a prefix you choose plus a suffix Proton offers. Mail sent to it arrives in the mailboxes you name. Run `aliases options` to see the suffixes and mailboxes available.

```
proton pass aliases create
```

```bash
proton pass aliases create --prefix shop --mailbox me@proton.me
proton pass aliases create --prefix news --mailbox me@proton.me --vault Work --name 'Newsletter alias'
```

| Flag | Description |
| --- | --- |
| `--mailbox stringArray` | Where mail to the alias should arrive (repeatable) |
| `--name string` | Name for the alias item |
| `--prefix string` | The part before the @ |
| `--suffix string` | The part from the @ onwards (default: the first Proton offers) |
| `--vault string` | Which vault to keep it in, by name or ID |

## `disable`

Stop receiving mail sent to an alias.

```
proton pass aliases disable REF
```

```bash
proton pass aliases disable shop
```

## `enable`

Start receiving mail sent to an alias.

```
proton pass aliases enable REF
```

```bash
proton pass aliases enable shop
```

## `list`

List your aliases.

```
proton pass aliases list
```

```bash
proton pass aliases list
proton pass aliases list --vault Work
```

| Flag | Description |
| --- | --- |
| `--vault string` | Show only this vault, by name or ID |

## `options`

List the suffixes and mailboxes an alias can use.

A suffix is the domain an alias is made on, and is what --suffix takes.

Proton adds a random word in front of the suffix, and only settles on it when the alias is created.

```
proton pass aliases options
```

```bash
proton pass aliases options
```

---

Every command also takes the [flags that work everywhere](../about/commands.md#flags-that-work-on-every-command).
