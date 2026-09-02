# proton drive

Files and folders in Drive.

Every command under `proton drive`, with the arguments and flags it takes. For these commands in use, see [the guide](../apps/drive.md).

Holds `invitations`, `items`, `photos`, `settings`, `shared`, `sharing` and `trash`.

## `invitations`

Shares other people have offered you.

Holds `accept`, `decline` and `list`.

### `invitations accept`

Accept invitations.

```
proton drive invitations accept REF...
```

```bash
proton drive invitations accept 5bH2mQxK
```

### `invitations decline`

Decline invitations.

```
proton drive invitations decline REF...
```

```bash
proton drive invitations decline 5bH2mQxK
```

### `invitations list`

List invitations waiting for an answer.

```
proton drive invitations list
```

```bash
proton drive invitations list
```

## `items`

Files and folders.

Holds `copy`, `create`, `delete`, `download`, `get`, `list`, `move`, `revisions`, `share`, `trash`, `update` and `upload`.

### `items copy`

Copy files into another folder.

```
proton drive items copy [PATH...]
```

```bash
proton drive items copy /Documents/report.pdf --into /Archive
proton drive items copy --pattern '*.pdf' --scope /Documents --into /Backup
```

| Flag | Description |
| --- | --- |
| `--all` | Act on everything in scope, rather than a subset |
| `--into string` | Destination folder |
| `--larger-than string` | Match files above SIZE (e.g. 100MB, 2GB) |
| `--newer-than string` | Match files newer than DURATION |
| `--older-than string` | Match files older than DURATION (e.g. 30d, 2w, 1h) |
| `--pattern string` | Match names against a shell glob, e.g. *.tmp |
| `--recursive` | Descend into subfolders when filtering |
| `--scope string` | Look only inside this folder (default: the whole drive) |
| `--smaller-than string` | Match files below SIZE |

### `items create`

Create a folder, and any missing folder above it.

```
proton drive items create PATH
```

```bash
proton drive items create /Documents/2026
```

### `items delete`

Delete files or folders permanently.

```
proton drive items delete [PATH...]
```

```bash
proton drive items delete /Documents/report.pdf
proton drive items delete --pattern '*.tmp' --scope /Build --recursive --yes
```

| Flag | Description |
| --- | --- |
| `--all` | Act on everything in scope, rather than a subset |
| `--larger-than string` | Match files above SIZE (e.g. 100MB, 2GB) |
| `--newer-than string` | Match files newer than DURATION |
| `--older-than string` | Match files older than DURATION (e.g. 30d, 2w, 1h) |
| `--pattern string` | Match names against a shell glob, e.g. *.tmp |
| `--recursive` | Descend into subfolders when filtering |
| `--scope string` | Look only inside this folder (default: the whole drive) |
| `--smaller-than string` | Match files below SIZE |

### `items download`

Download a file.

```
proton drive items download PATH
```

```bash
proton drive items download /Documents/report.pdf --dest-dir .
proton drive items download /Documents/report.pdf --dest - > report.pdf
```

| Flag | Description |
| --- | --- |
| `--dest string` | Write to this path, or - for stdout |
| `--dest-dir string` | Write into this directory, keeping each item's own name |
| `--force` | Overwrite a file that already exists |

### `items get`

Show a file or folder's details.

```
proton drive items get PATH
```

```bash
proton drive items get /Documents/report.pdf
```

### `items list`

List what is in a folder.

The filters are the same ones move, copy, trash and delete take, so a selection can be worked out here and then handed to the verb that acts on it. PATH is what those commands call --scope.

```
proton drive items list [PATH]
```

```bash
proton drive items list
proton drive items list /Documents
```

| Flag | Description |
| --- | --- |
| `--desc` | Reverse the order |
| `--larger-than string` | Match files above SIZE (e.g. 100MB, 2GB) |
| `--newer-than string` | Match files newer than DURATION |
| `--older-than string` | Match files older than DURATION (e.g. 30d, 2w, 1h) |
| `--page int` | Which page of results, counting from zero |
| `--page-size int` | How many items per page (default `50`) |
| `--pattern string` | Match names against a shell glob, e.g. *.tmp |
| `--recursive` | Descend into subfolders when filtering |
| `--smaller-than string` | Match files below SIZE |
| `--sort string` | Order by: name, size, modified (default `name`) |

### `items move`

Move files or folders into another folder.

```
proton drive items move [PATH...]
```

```bash
proton drive items move /Documents/report.pdf --into /Archive
proton drive items move --pattern '*.log' --scope /Build --recursive --into /Archive
```

| Flag | Description |
| --- | --- |
| `--all` | Act on everything in scope, rather than a subset |
| `--into string` | Destination folder |
| `--larger-than string` | Match files above SIZE (e.g. 100MB, 2GB) |
| `--newer-than string` | Match files newer than DURATION |
| `--older-than string` | Match files older than DURATION (e.g. 30d, 2w, 1h) |
| `--pattern string` | Match names against a shell glob, e.g. *.tmp |
| `--recursive` | Descend into subfolders when filtering |
| `--scope string` | Look only inside this folder (default: the whole drive) |
| `--smaller-than string` | Match files below SIZE |

### `items revisions`

Earlier versions of a file.

Uploading over a file with `--if-exists replace` keeps what was there as a revision. Any of them can be read back without disturbing the file, put back in place, or dropped from the history.

Holds `delete`, `download`, `list` and `restore`.

### `items revisions delete`

Delete an earlier version permanently.

```
proton drive items revisions delete PATH REVISION_REF
```

```bash
proton drive items revisions delete /Documents/report.pdf 5bH2mQxK
```

### `items revisions download`

Download an earlier version of a file.

The file keeps whatever it holds now: this reads an old version out, where `revisions restore` puts one back in place.

```
proton drive items revisions download PATH REVISION_REF
```

```bash
proton drive items revisions download /Documents/report.pdf 5bH2mQxK --dest-dir .
```

| Flag | Description |
| --- | --- |
| `--dest string` | Write to this path, or - for stdout |
| `--dest-dir string` | Write into this directory, keeping each item's own name |
| `--force` | Overwrite a file that already exists |

### `items revisions list`

List a file's earlier versions.

```
proton drive items revisions list PATH
```

```bash
proton drive items revisions list /Documents/report.pdf
```

### `items revisions restore`

Restore a file to an earlier version.

```
proton drive items revisions restore PATH REVISION_REF
```

```bash
proton drive items revisions restore /Documents/report.pdf 5bH2mQxK
```

### `items share`

Public links and the people you share with.

Holds `add`, `get`, `link`, `remove`, `resend`, `unlink` and `update`.

### `items share add`

Invite someone to a file or folder.

```
proton drive items share add PATH EMAIL
```

```bash
proton drive items share add /Documents jane@example.com
proton drive items share add /Documents jane@example.com --edit --message 'Have a look'
```

| Flag | Description |
| --- | --- |
| `--edit` | Allow editing rather than only viewing |
| `--message string` | Note to include in the invitation email |

### `items share get`

Show how a file or folder is shared.

```
proton drive items share get PATH
```

```bash
proton drive items share get /Documents/report.pdf
```

### `items share link`

Create or update the public link for a file or folder.

Running it again with different options changes the existing link rather than making a second one, so the URL you have shared keeps working.

The password is read from a file or from stdin, never from a flag value, and may be at most 50 characters. --clear-link-password takes it off again, and --expires never makes an expiring link permanent.

```
proton drive items share link PATH
```

```bash
proton drive items share link /Documents/report.pdf
proton drive items share link /Documents/report.pdf --expires 7d --link-password-file /run/secrets/report-link
proton drive items share link /Documents/report.pdf --clear-link-password --expires never
proton drive items share link /Documents --edit
```

| Flag | Description |
| --- | --- |
| `--clear-link-password` | Remove the public link's password |
| `--edit` | Allow editing rather than only viewing |
| `--expires string` | Stop working after DURATION (e.g. 7d, 2w, 6mo), or never |
| `--link-password-file string` | Read the public link's password from a file |
| `--link-password-stdin` | Read the public link's password from stdin |

### `items share remove`

Revoke someone's access, or cancel their invitation.

```
proton drive items share remove PATH EMAIL
```

```bash
proton drive items share remove /Documents jane@example.com
```

### `items share resend`

Send an unanswered invitation again.

```
proton drive items share resend PATH EMAIL
```

```bash
proton drive items share resend /Reports jane@proton.me
```

### `items share unlink`

Remove the public links for a file or folder.

```
proton drive items share unlink PATH
```

```bash
proton drive items share unlink /Documents/report.pdf
```

### `items share update`

Change what somebody may do with a file or folder.

It applies to whoever holds the address, whether they have accepted yet or not: Proton keeps members and invitations apart, but the question is the same one either way.

```
proton drive items share update PATH EMAIL
```

```bash
proton drive items share update /Reports jane@proton.me --edit
proton drive items share update /Reports jane@proton.me --edit=false
```

| Flag | Description |
| --- | --- |
| `--edit` | Allow editing rather than only viewing |

### `items trash`

Move files or folders to the trash.

```
proton drive items trash [PATH...]
```

```bash
proton drive items trash /Documents/report.pdf
proton drive items trash --pattern '*.tmp' --scope /Build --recursive
proton drive items trash --older-than 1y --scope /Downloads --dry-run
```

| Flag | Description |
| --- | --- |
| `--all` | Act on everything in scope, rather than a subset |
| `--larger-than string` | Match files above SIZE (e.g. 100MB, 2GB) |
| `--newer-than string` | Match files newer than DURATION |
| `--older-than string` | Match files older than DURATION (e.g. 30d, 2w, 1h) |
| `--pattern string` | Match names against a shell glob, e.g. *.tmp |
| `--recursive` | Descend into subfolders when filtering |
| `--scope string` | Look only inside this folder (default: the whole drive) |
| `--smaller-than string` | Match files below SIZE |

### `items update`

Rename a file or folder.

A name is a field like any other, so changing it is `update --name` rather than a verb of its own. To put something somewhere else, use `move`.

```
proton drive items update PATH
```

```bash
proton drive items update /Documents/report.pdf --name summary.pdf
```

| Flag | Description |
| --- | --- |
| `--name string` | New name, without a path |

### `items upload`

Upload a file or directory.

A name already taken is refused, so nothing is overwritten by accident. --if-exists answers instead:

  replace  a new revision, keeping the file's history
  rename   keep both, numbering the one being uploaded
  skip     leave what is there alone

With --recursive that answer is about the folder the tree lands in.

SRC of - reads standard input, and then DEST has to name the file.

```
proton drive items upload SRC [DEST]
```

```bash
proton drive items upload ./report.pdf /Documents
proton drive items upload --recursive ./project /Backup
proton drive items upload --if-exists replace ./report.pdf /Documents
pg_dump mydb | gzip | proton drive items upload - /Backups/db.sql.gz
```

| Flag | Description |
| --- | --- |
| `--if-exists string` | What to do when the folder already has that name: rename, replace, skip |
| `--recursive` | Upload a directory and everything under it |

## `photos`

Your photo library.

Holds `albums`, `delete`, `download`, `favorite`, `list`, `trash`, `unfavorite` and `upload`.

### `photos albums`

Photo albums.

Holds `add`, `create`, `delete`, `list`, `remove` and `update`.

### `photos albums add`

Put photos into an album.

```
proton drive photos albums add REF PHOTO_REF...
```

```bash
proton drive photos albums add Holidays 5bH2mQxK
```

### `photos albums create`

Create an album.

```
proton drive photos albums create
```

```bash
proton drive photos albums create --name Holidays
```

| Flag | Description |
| --- | --- |
| `--name string` | Name for the new album |

### `photos albums delete`

Delete albums.

```
proton drive photos albums delete REF...
```

```bash
proton drive photos albums delete Holidays
proton drive photos albums delete Holidays --delete-photos
```

| Flag | Description |
| --- | --- |
| `--delete-photos` | Also move the album's photos to the trash |

### `photos albums list`

List albums.

```
proton drive photos albums list
```

```bash
proton drive photos albums list
```

### `photos albums remove`

Take photos out of an album.

```
proton drive photos albums remove REF PHOTO_REF...
```

```bash
proton drive photos albums remove Holidays 5bH2mQxK
```

### `photos albums update`

Change an album's cover.

```
proton drive photos albums update REF
```

```bash
proton drive photos albums update Holidays --cover 5bH2mQxK
```

| Flag | Description |
| --- | --- |
| `--cover string` | Which of the album's photos represents it |

### `photos delete`

Delete photos permanently.

```
proton drive photos delete REF...
```

```bash
proton drive photos delete 5bH2mQxK
```

### `photos download`

Download a photo.

```
proton drive photos download REF
```

```bash
proton drive photos download 5bH2mQxK --dest-dir .
```

| Flag | Description |
| --- | --- |
| `--dest string` | Write to this path, or - for stdout |
| `--dest-dir string` | Write into this directory, keeping each item's own name |
| `--force` | Overwrite a file that already exists |

### `photos favorite`

Mark photos as favourites.

```
proton drive photos favorite REF...
```

```bash
proton drive photos favorite 5bH2mQxK
```

### `photos list`

List photos.

```
proton drive photos list
```

```bash
proton drive photos list
proton drive photos list --album Holidays
proton drive photos list --tag favorites
```

| Flag | Description |
| --- | --- |
| `--album string` | Show only what is in this album, by ID |
| `--tag string` | Show only photos with this tag: favorites, screenshots, videos, live-photos, motion-photos, selfies, portraits, bursts, panoramas, raw |

### `photos trash`

Move photos to the trash.

```
proton drive photos trash REF...
```

```bash
proton drive photos trash 5bH2mQxK
```

### `photos unfavorite`

Remove photos from favourites.

```
proton drive photos unfavorite REF...
```

```bash
proton drive photos unfavorite 5bH2mQxK
```

### `photos upload`

Upload a photo to the library.

```
proton drive photos upload SRC
```

```bash
proton drive photos upload ./IMG_2291.jpg
```

## `settings`

How Drive behaves.

Holds `get`, `list` and `set`.

### `settings get`

Show the drive settings now in effect.

```
proton drive settings get
```

```bash
proton drive settings get
```

### `settings list`

List the drive settings that can be changed.

```
proton drive settings list
```

```bash
proton drive settings list
```

### `settings set`

Change one drive setting.

```
proton drive settings set KEY VALUE
```

```bash
proton drive settings set revision-retention 30
```

## `shared`

Files and folders other people have shared with you.

Holds `list`.

### `shared list`

List what other people have shared with you.

These do not live in your tree, so they have no path: they are addressed by the ID this listing shows, the way trashed items and photos are.

An item whose name cannot be read is still listed, because knowing it is there is what lets you act on it.

```
proton drive shared list
```

```bash
proton drive shared list
```

## `sharing`

What you have shared with other people.

Holds `list`.

### `sharing list`

List what you have shared, by link or with named people.

`items share get PATH` answers the question for one item; this answers the one you actually have, which is what have I left open.

```
proton drive sharing list
```

```bash
proton drive sharing list
```

## `trash`

Items you have removed but not yet deleted.

Holds `empty`, `list` and `restore`.

### `trash empty`

Delete everything in the trash, permanently.

That is everything `trash list` shows, on every volume the account has: trashed photos go with it.

```
proton drive trash empty
```

```bash
proton drive trash empty
```

### `trash list`

List what is in the trash.

Everything the account has trashed is here, photos included: they are kept on a volume of their own, and `trash empty` deletes both.

A trashed item has no path any more, so it is addressed by the ID shown. An item whose name cannot be read is still listed, because knowing it is there is what lets you act on it.

```
proton drive trash list
```

```bash
proton drive trash list
proton drive trash list --sort trashed --desc
```

| Flag | Description |
| --- | --- |
| `--desc` | Reverse the order |
| `--page int` | Which page of results, counting from zero |
| `--page-size int` | How many items per page (default `50`) |
| `--sort string` | Order by: name, size, trashed (default `name`) |

### `trash restore`

Put items back where they came from.

A trashed item has no path any more, so it is named by the ID that `trash list` shows.

```
proton drive trash restore REF...
```

```bash
proton drive trash restore 5bH2mQxK
```

---

Every command also takes the [flags that work everywhere](README.md#flags-that-work-on-every-command).
