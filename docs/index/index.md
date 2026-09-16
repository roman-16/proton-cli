# proton index

An encrypted, searchable copy of your mail, files and events on this machine.

Every command under `proton index`, with the arguments and flags it takes. For these commands in use, see [the index guide](README.md).

Holds `create`, `delete`, `list`, `update` and `watch`.

## `create`

Index an app so its contents can be searched.

Name the apps to index, or none for every app that can be. A first build of a large mailbox takes hours; stopping it and running it again carries on where it left off, newest first.

What it writes is encrypted to your account's keys, under ~/.config/proton-cli/index.

```
proton index create [REF...]
```

```bash
proton index create
proton index create mail
proton index create drive calendar
proton index create drive --dry-run
```

## `delete`

Remove an index from this machine.

Name the apps to remove, or none for all of them. Nothing in your account changes, and searches go back to what Proton can answer.

Removing the last one removes the index key too; building again starts from nothing.

```
proton index delete [REF...]
```

```bash
proton index delete
proton index delete --yes mail
```

## `list`

List what is indexed on this machine.

Reads the files and nothing else, so it works signed out. INDEXED counts what a search would look through; a build that has not finished says how much of the app it has reached.

```
proton index list
```

```bash
proton index list
proton index list --output json
```

## `update`

Bring every index up to date.

Applies what has happened since the last run, and carries on a build that was interrupted. It creates nothing: an app with no index is left alone.

A search catches up by itself, so this is for having it done already: run it from cron, or leave `index watch` attached.

```
proton index update
```

```bash
proton index update
proton index update --quiet
```

## `watch`

Keep every index current until you stop it.

Applies changes as they land, one line per batch, so a search answers without catching up first.

It indexes nothing that is not indexed already; `index create` does that.

```
proton index watch
```

```bash
proton index watch
proton index watch --output json
```

---

Every command also takes the [flags that work everywhere](../about/commands.md#flags-that-work-on-every-command).
