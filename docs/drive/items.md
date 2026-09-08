# proton drive items

Files and folders.

Every command under `proton drive items`, with the arguments and flags it takes. For these commands in use, see [the drive guide](README.md).

Holds `copy`, `create`, `delete`, `download`, `get`, `list`, `move`, `revisions`, `share`, `trash`, `update` and `upload`.

## `copy`

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
| `--computer string` | Work inside this computer's files, by name or ID |
| `--into string` | Destination folder |
| `--larger-than string` | Match files above SIZE (e.g. 100MB, 2GB) |
| `--newer-than string` | Match files newer than DURATION |
| `--older-than string` | Match files older than DURATION (e.g. 30d, 2w, 1h) |
| `--pattern string` | Match names against a shell glob, e.g. *.tmp |
| `--recursive` | Descend into subfolders when filtering |
| `--scope string` | Look only inside this folder (default: the whole drive) |
| `--shared string` | Work inside an item shared with you, by name or ID |
| `--smaller-than string` | Match files below SIZE |

## `create`

Create a folder, and any missing folder above it.

```
proton drive items create PATH
```

```bash
proton drive items create /Documents/2026
```

| Flag | Description |
| --- | --- |
| `--computer string` | Work inside this computer's files, by name or ID |
| `--shared string` | Work inside an item shared with you, by name or ID |

## `delete`

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
| `--computer string` | Work inside this computer's files, by name or ID |
| `--larger-than string` | Match files above SIZE (e.g. 100MB, 2GB) |
| `--newer-than string` | Match files newer than DURATION |
| `--older-than string` | Match files older than DURATION (e.g. 30d, 2w, 1h) |
| `--pattern string` | Match names against a shell glob, e.g. *.tmp |
| `--recursive` | Descend into subfolders when filtering |
| `--scope string` | Look only inside this folder (default: the whole drive) |
| `--shared string` | Work inside an item shared with you, by name or ID |
| `--smaller-than string` | Match files below SIZE |

## `download`

Download a file.

Behind a public link, a file is / when the link points at the file itself, and a path inside the folder when it points at a folder. A link with a password takes it from --link-password-file or --link-password-stdin.

```
proton drive items download PATH
```

```bash
proton drive items download /Documents/report.pdf --dest-dir .
proton drive items download /Documents/report.pdf --dest - > report.pdf
proton drive items download /report.pdf --shared Project --dest-dir .
proton drive items download / --link 'https://drive.proton.me/urls/7X2K9M3N1P#kQ81mDx4T9wL' --dest-dir .
proton drive items download / --link 'https://drive.proton.me/urls/7X2K9M3N1P#kQ81mDx4T9wL' --link-password-file /run/secrets/q3-link --dest-dir .
```

| Flag | Description |
| --- | --- |
| `--computer string` | Work inside this computer's files, by name or ID |
| `--dest string` | Write to this path, or - for stdout |
| `--dest-dir string` | Write into this directory, keeping each item's own name |
| `--force` | Overwrite a file that already exists |
| `--link string` | Work inside a public link somebody sent you, by URL |
| `--link-password-file string` | Read the public link's password from a file |
| `--link-password-stdin` | Read the public link's password from stdin |
| `--shared string` | Work inside an item shared with you, by name or ID |

## `get`

Show a file or folder's details.

Reached through a public link, the details include the link itself and, for a link saved with `shared add`, the password its owner set on it. A link names nobody as the author of what is in it, so Created By is absent and Signature reads anonymous.

```
proton drive items get PATH
```

```bash
proton drive items get /Documents/report.pdf
proton drive items get / --shared Q3-report.pdf
```

| Flag | Description |
| --- | --- |
| `--computer string` | Work inside this computer's files, by name or ID |
| `--link string` | Work inside a public link somebody sent you, by URL |
| `--link-password-file string` | Read the public link's password from a file |
| `--link-password-stdin` | Read the public link's password from stdin |
| `--shared string` | Work inside an item shared with you, by name or ID |

## `list`

List what is in a folder.

Takes the same filters as move, copy, trash and delete, so you can preview a selection here before acting on it. What PATH is here, those commands call --scope.

PATH is in your own files. --computer REF lists inside a computer instead, --shared REF inside something somebody shared with you, and --link URL inside a public link somebody sent you. In the last two, / is the item itself.

```
proton drive items list [PATH]
```

```bash
proton drive items list
proton drive items list /Documents
proton drive items list / --computer 'Work laptop'
proton drive items list / --shared Project
proton drive items list / --link 'https://drive.proton.me/urls/7X2K9M3N1P#kQ81mDx4T9wL'
```

| Flag | Description |
| --- | --- |
| `--computer string` | Work inside this computer's files, by name or ID |
| `--desc` | Reverse the order |
| `--larger-than string` | Match files above SIZE (e.g. 100MB, 2GB) |
| `--link string` | Work inside a public link somebody sent you, by URL |
| `--link-password-file string` | Read the public link's password from a file |
| `--link-password-stdin` | Read the public link's password from stdin |
| `--newer-than string` | Match files newer than DURATION |
| `--older-than string` | Match files older than DURATION (e.g. 30d, 2w, 1h) |
| `--page int` | Which page of results, counting from zero |
| `--page-size int` | How many items per page; 0 for all of them (default `50`) |
| `--pattern string` | Match names against a shell glob, e.g. *.tmp |
| `--recursive` | Descend into subfolders when filtering |
| `--shared string` | Work inside an item shared with you, by name or ID |
| `--smaller-than string` | Match files below SIZE |
| `--sort string` | Order by: name, size, modified (default `name`) |

## `move`

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
| `--computer string` | Work inside this computer's files, by name or ID |
| `--into string` | Destination folder |
| `--larger-than string` | Match files above SIZE (e.g. 100MB, 2GB) |
| `--newer-than string` | Match files newer than DURATION |
| `--older-than string` | Match files older than DURATION (e.g. 30d, 2w, 1h) |
| `--pattern string` | Match names against a shell glob, e.g. *.tmp |
| `--recursive` | Descend into subfolders when filtering |
| `--scope string` | Look only inside this folder (default: the whole drive) |
| `--shared string` | Work inside an item shared with you, by name or ID |
| `--smaller-than string` | Match files below SIZE |

## `revisions`

Earlier versions of a file.

Uploading over a file with `--if-exists replace` keeps what was there as a revision. You can read any revision back, restore it, or delete it.

Holds `delete`, `download`, `list` and `restore`.

### `revisions delete`

Delete an earlier version permanently.

```
proton drive items revisions delete PATH REVISION_REF
```

```bash
proton drive items revisions delete /Documents/report.pdf 5bH2mQxK
```

| Flag | Description |
| --- | --- |
| `--computer string` | Work inside this computer's files, by name or ID |
| `--shared string` | Work inside an item shared with you, by name or ID |

### `revisions download`

Download an earlier version of a file.

The file itself is not changed. To put an old version back in place, use `revisions restore`.

```
proton drive items revisions download PATH REVISION_REF
```

```bash
proton drive items revisions download /Documents/report.pdf 5bH2mQxK --dest-dir .
```

| Flag | Description |
| --- | --- |
| `--computer string` | Work inside this computer's files, by name or ID |
| `--dest string` | Write to this path, or - for stdout |
| `--dest-dir string` | Write into this directory, keeping each item's own name |
| `--force` | Overwrite a file that already exists |
| `--shared string` | Work inside an item shared with you, by name or ID |

### `revisions list`

List a file's earlier versions.

```
proton drive items revisions list PATH
```

```bash
proton drive items revisions list /Documents/report.pdf
```

| Flag | Description |
| --- | --- |
| `--computer string` | Work inside this computer's files, by name or ID |
| `--shared string` | Work inside an item shared with you, by name or ID |

### `revisions restore`

Restore a file to an earlier version.

```
proton drive items revisions restore PATH REVISION_REF
```

```bash
proton drive items revisions restore /Documents/report.pdf 5bH2mQxK
```

| Flag | Description |
| --- | --- |
| `--computer string` | Work inside this computer's files, by name or ID |
| `--shared string` | Work inside an item shared with you, by name or ID |

## `share`

Public links and the people you share with.

Holds `add`, `get`, `link`, `remove`, `resend`, `unlink` and `update`.

### `share add`

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
| `--computer string` | Work inside this computer's files, by name or ID |
| `--edit` | Allow editing rather than only viewing |
| `--message string` | Note to include in the invitation email |
| `--shared string` | Work inside an item shared with you, by name or ID |

### `share get`

Show how a file or folder is shared.

```
proton drive items share get PATH
```

```bash
proton drive items share get /Documents/report.pdf
```

| Flag | Description |
| --- | --- |
| `--computer string` | Work inside this computer's files, by name or ID |
| `--shared string` | Work inside an item shared with you, by name or ID |

### `share link`

Create or update the public link for a file or folder.

Running it again changes the existing link rather than making a second one, so a URL you have already shared keeps working.

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
| `--computer string` | Work inside this computer's files, by name or ID |
| `--edit` | Allow editing rather than only viewing |
| `--expires string` | Stop working after DURATION (e.g. 7d, 2w, 6mo), or never |
| `--link-password-file string` | Read the public link's password from a file |
| `--link-password-stdin` | Read the public link's password from stdin |
| `--shared string` | Work inside an item shared with you, by name or ID |

### `share remove`

Revoke someone's access, or cancel their invitation.

```
proton drive items share remove PATH EMAIL
```

```bash
proton drive items share remove /Documents jane@example.com
```

| Flag | Description |
| --- | --- |
| `--computer string` | Work inside this computer's files, by name or ID |
| `--shared string` | Work inside an item shared with you, by name or ID |

### `share resend`

Send an unanswered invitation again.

```
proton drive items share resend PATH EMAIL
```

```bash
proton drive items share resend /Reports jane@proton.me
```

| Flag | Description |
| --- | --- |
| `--computer string` | Work inside this computer's files, by name or ID |
| `--shared string` | Work inside an item shared with you, by name or ID |

### `share unlink`

Remove the public links for a file or folder.

```
proton drive items share unlink PATH
```

```bash
proton drive items share unlink /Documents/report.pdf
```

| Flag | Description |
| --- | --- |
| `--computer string` | Work inside this computer's files, by name or ID |
| `--shared string` | Work inside an item shared with you, by name or ID |

### `share update`

Change what somebody may do with a file or folder.

Name them by address. It works whether they have accepted the share or still have it pending.

```
proton drive items share update PATH EMAIL
```

```bash
proton drive items share update /Reports jane@proton.me --edit
proton drive items share update /Reports jane@proton.me --edit=false
```

| Flag | Description |
| --- | --- |
| `--computer string` | Work inside this computer's files, by name or ID |
| `--edit` | Allow editing rather than only viewing |
| `--shared string` | Work inside an item shared with you, by name or ID |

## `trash`

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
| `--computer string` | Work inside this computer's files, by name or ID |
| `--larger-than string` | Match files above SIZE (e.g. 100MB, 2GB) |
| `--newer-than string` | Match files newer than DURATION |
| `--older-than string` | Match files older than DURATION (e.g. 30d, 2w, 1h) |
| `--pattern string` | Match names against a shell glob, e.g. *.tmp |
| `--recursive` | Descend into subfolders when filtering |
| `--scope string` | Look only inside this folder (default: the whole drive) |
| `--shared string` | Work inside an item shared with you, by name or ID |
| `--smaller-than string` | Match files below SIZE |

## `update`

Rename a file or folder.

Renaming is `update --name`; there is no `rename` verb. To put something somewhere else, use `move`.

PATH names something inside a tree, never the tree itself: to rename a computer, use `computers update`.

```
proton drive items update PATH
```

```bash
proton drive items update /Documents/report.pdf --name summary.pdf
```

| Flag | Description |
| --- | --- |
| `--computer string` | Work inside this computer's files, by name or ID |
| `--name string` | New name, without a path |
| `--shared string` | Work inside an item shared with you, by name or ID |

## `upload`

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
| `--computer string` | Work inside this computer's files, by name or ID |
| `--if-exists string` | What to do when the folder already has that name: rename, replace, skip |
| `--recursive` | Upload a directory and everything under it |
| `--shared string` | Work inside an item shared with you, by name or ID |

---

Every command also takes the [flags that work everywhere](../about/commands.md#flags-that-work-on-every-command).
