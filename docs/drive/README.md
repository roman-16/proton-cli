# Drive

Upload, download, share and organize Proton Drive as ordinary paths. Files are encrypted before they leave your machine and decrypted after they arrive, block by block, with your keys.

This page is what people actually do. For every command and flag, see the reference: [items](items.md), [trash](trash.md), [photos](photos.md), [shared](shared.md), [sharing](sharing.md), [invitations](invitations.md), [settings](settings.md).

## Look around

```bash
proton drive items list                       # the root
proton drive items list /Documents
proton drive items list /Build --pattern "*.tmp" --recursive
proton drive items get /Documents/report.pdf  # type, size, checksum, sharing state
```

## Upload and download

```bash
proton drive items upload ./report.pdf /Documents
proton drive items upload --recursive ./project /Backup
pg_dump mydb | proton drive items upload - /Backups/db.sql

proton drive items download /Documents/report.pdf --dest-dir ./downloads/
proton drive items download /Documents/report.pdf --dest - | less
```

Uploads show progress on stderr.

**A name already taken is refused**, so nothing is overwritten by accident. `--if-exists` answers the question instead:

```bash
proton drive items upload --if-exists replace ./report.pdf /Documents  # a new revision of it
proton drive items upload --if-exists rename ./report.pdf /Documents   # keeps both, as "report (1).pdf"
proton drive items upload --if-exists skip ./report.pdf /Documents     # leaves what is there alone
```

With `--recursive` the answer is about the folder the tree lands in:

| `--if-exists` | With `--recursive` |
| --- | --- |
| `replace` | Merges the tree file by file |
| `rename` | Puts the whole tree beside the old one, as `project (1)` |
| `skip` | Writes none of it |

A file standing where a folder must go, or a folder where a file must go, is refused before anything is written.

## Move, rename, copy, organize

```bash
proton drive items update /Documents/old.txt --name new.txt
proton drive items move /Documents/report.pdf --into /Archive
proton drive items copy /Documents/report.pdf --into /Archive
proton drive items create /Documents/2026/Q1/receipts   # makes 2026 and Q1 on the way
```

A path names every folder along it, so the ones above the last are made too: `Created 3 folders down to /Documents/2026/Q1/receipts`.

The folder you asked for is the exception. A name Drive already holds is refused, as is a path running through a file.

## Trash and delete

```bash
proton drive items trash /Documents/old.pdf     # reversible
proton drive items delete /Documents/old.pdf    # permanent
proton drive trash list
proton drive trash list --sort trashed --desc --page 1
proton drive trash restore 7Kd91mQx
proton drive trash empty                        # permanent, and everything below
```

The trash is one list however many volumes it is spread over. Photos are kept on a volume of their own, and both `trash list` and `trash empty` cover them.

`trash list` pages and sorts like any other listing, and says how many items there are in total. `trash empty` deletes exactly that many.

`trash`, `delete`, `move` and `copy` all take filters instead of paths: `--pattern`, `--larger-than`, `--smaller-than`, `--older-than`, `--newer-than`, `--scope`, `--recursive` and `--all`. Try them with `list` or `--dry-run` first:

```bash
proton drive items list /Build --pattern "*.tmp" --recursive          # see what matches
proton drive items trash --scope /Build --pattern "*.tmp" --recursive
proton drive items delete --larger-than 100MB --scope /Downloads --recursive --dry-run
```

A filter that matches a folder and the files inside it selects the folder alone, because a folder is trashed, deleted, moved and copied whole. See [Filters and bulk changes](../using/filters.md).

A trashed item has no place in the tree, so it has no path. Address it by the ID its listing showed.

## Earlier versions

Uploading over a file with `--if-exists replace` keeps what was there as a revision.

```bash
proton drive items revisions list /Documents/report.pdf
proton drive items revisions download /Documents/report.pdf 8f3a1c22 --dest ./earlier.pdf
proton drive items revisions restore /Documents/report.pdf 8f3a1c22
```

`download` reads an old version out and leaves the file alone. `restore` puts one back in place.

Proton restores in the background, so the file changes a moment after the command returns. The version it replaces stays in the history, so a restore can itself be undone.

Neither command touches the version the file is at now. Restoring it would do nothing, and deleting it would be deleting the file.

## Share it

### A public link

```bash
proton drive items share link /Documents/report.pdf --expires 7d \
  --link-password-file /run/secrets/report-link
proton drive items share get /Documents/report.pdf     # who has access, plus the link
proton drive items share unlink /Documents/report.pdf
```

The password that opens a public link is a secret, so it comes from a file or from standard input rather than from a flag value. Proton allows at most 50 characters.

- `--expires never` makes an expiring link permanent.
- `--clear-link-password` removes the password, because a value read from a file has no way of saying "none".

### With named people

```bash
proton drive items share add /Documents/project bob@proton.me --edit --message "Draft for review"
proton drive items share update /Reports jane@proton.me --edit=false
proton drive items share remove /Documents/report.pdf bob@proton.me
```

`share update` applies whether they have accepted yet or not. Nothing is re-encrypted: the key they hold still opens the share, and only what they may do with it changes.

`share resend` sends an unanswered invitation again rather than cancelling and inviting afresh.

### What is shared, and by whom

```bash
proton drive shared list       # what other people have shared with you
proton drive sharing list      # what you have left open
proton drive invitations list
proton drive invitations accept INVITATION_ID
```

An item somebody shared with you does not live in your tree, so it has **no path**. Address it by the ID the listing shows.

An item whose name cannot be decrypted is still listed, so you can still act on it.

## Photos

```bash
proton drive photos list --tag favorites
proton drive photos upload ./IMG_0001.jpg
proton drive photos download 3Ns8pT2v --dest-dir ./photos/
proton drive photos favorite 3Ns8pT2v
```

Tags are `favorites`, `screenshots`, `videos`, `live-photos`, `motion-photos`, `selfies`, `portraits`, `bursts`, `panoramas` and `raw`.

Photos have no path either, so address them by ID.

```bash
proton drive photos albums create --name Holiday
proton drive photos albums add ALBUM_ID PHOTO_ID...
proton drive photos list --album ALBUM_ID
proton drive photos albums delete Holiday --delete-photos
```

## Settings

```bash
proton drive settings set version-history 30d
```

`version-history` is how long previous versions are kept: `off`, `7d`, `30d`, `180d`, `1y` or `10y`. Keeping more than the default is a paid feature.
