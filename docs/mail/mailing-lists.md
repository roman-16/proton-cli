# proton mail mailing-lists

The senders that write to you as a list.

Every command under `proton mail mailing-lists`, with the arguments and flags it takes. For these commands in use, see [the mail guide](README.md).

Holds `get`, `list`, `remove`, `unsubscribe` and `update`.

## `get`

Show one mailing list in full.

```
proton mail mailing-lists get REF
```

```bash
proton mail mailing-lists get 'Trailhead Weekly'
```

## `list`

List the senders that write to you as a list.

Shows the ones still writing. --unsubscribed shows the ones you have left.

RULE is what happens to a list's mail as it arrives, set by `update`.

```
proton mail mailing-lists list
```

```bash
proton mail mailing-lists list
proton mail mailing-lists list --sort unread
proton mail mailing-lists list --unsubscribed
```

| Flag | Description |
| --- | --- |
| `--desc` | Reverse the order |
| `--limit int` | How many mailing lists per page; 0 for all of them |
| `--page int` | Which page of results, counting from zero |
| `--sort string` | Order by: received, name, unread, frequency, read (default `received`) |
| `--unsubscribed` | List the ones you have left instead |

## `remove`

Drop a mailing list from the listing.

It unsubscribes from nothing, and the list comes back the next time it writes to you.

```
proton mail mailing-lists remove REF...
```

```bash
proton mail mailing-lists remove 'Trailhead Weekly'
```

## `unsubscribe`

Ask a mailing list to stop writing to you.

A list offers one of three ways, and the answer says which was used. Proton submits a one-click form on your behalf; an unsubscribe address is a message sent from the address the list writes to; a link is a page, which is opened in your browser and printed either way.

A list that offers none of them can only be kept out by blocking the sender.

```
proton mail mailing-lists unsubscribe REF...
```

```bash
proton mail mailing-lists unsubscribe 'Trailhead Weekly'
proton mail mailing-lists unsubscribe news@example.com --dry-run
```

## `update`

Set what happens to a mailing list's mail.

It covers the mail already here and everything that arrives afterwards.

Proton keeps the standing half as a filter of its own, so this is turned off again with `proton mail settings filters disable`, naming the filter `get` shows.

```
proton mail mailing-lists update REF...
```

```bash
proton mail mailing-lists update 'Trailhead Weekly' --into Archive
proton mail mailing-lists update news@example.com --into Archive --mark-read
```

| Flag | Description |
| --- | --- |
| `--into string` | Folder its mail goes to, by name or ID |
| `--mark-read` | Have its mail arrive read |

---

Every command also takes the [flags that work everywhere](../about/commands.md#flags-that-work-on-every-command).
