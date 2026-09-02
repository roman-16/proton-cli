# proton pass settings

Pass settings.

Every command under `proton pass settings`, with the arguments and flags it takes. For these commands in use, see [the pass guide](README.md).

Holds `domains` and `mailboxes`.

## `domains`

The domains an alias can be made on.

Holds `list`.

### `domains list`

List the domains an alias can be made on.

These are the values `proton pass aliases create --suffix` accepts: the part of an alias after the @.

```
proton pass settings domains list
```

```bash
proton pass settings domains list
```

## `mailboxes`

The addresses your aliases forward to.

Holds `create`, `delete`, `list`, `resend`, `update` and `verify`.

### `mailboxes create`

Add an address for aliases to forward to.

Proton emails the address a code. The mailbox receives nothing until you pass that code to `mailboxes verify`.

```
proton pass settings mailboxes create EMAIL
```

```bash
proton pass settings mailboxes create me@example.com
```

### `mailboxes delete`

Remove an address aliases forward to.

--transfer-to names the mailbox that its aliases move to. It is required: without a new mailbox, those aliases would stop receiving mail.

```
proton pass settings mailboxes delete REF
```

```bash
proton pass settings mailboxes delete me@example.com --transfer-to other@example.com
```

| Flag | Description |
| --- | --- |
| `--transfer-to string` | Move the aliases arriving here to this mailbox |

### `mailboxes list`

List the addresses your aliases forward to.

An alias is a route, not a mailbox: mail sent to it arrives in one of these. To point an alias at one, run `proton pass items update REF --mailbox`.

```
proton pass settings mailboxes list
```

```bash
proton pass settings mailboxes list
```

### `mailboxes resend`

Send the confirmation code again.

```
proton pass settings mailboxes resend REF
```

```bash
proton pass settings mailboxes resend me@example.com
```

### `mailboxes update`

Change a mailbox.

```
proton pass settings mailboxes update REF
```

```bash
proton pass settings mailboxes update me@example.com --default
```

| Flag | Description |
| --- | --- |
| `--default` | Make new aliases arrive here |

### `mailboxes verify`

Confirm an address with the code Proton emailed it.

```
proton pass settings mailboxes verify REF
```

```bash
proton pass settings mailboxes verify me@example.com --code 123456
```

| Flag | Description |
| --- | --- |
| `--code string` | The code Proton emailed the address |

---

Every command also takes the [flags that work everywhere](../about/commands.md#flags-that-work-on-every-command).
