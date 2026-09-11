# Drive

Upload, download, share and organize Proton Drive as ordinary paths. Files are encrypted before they leave your machine and decrypted after they arrive, block by block, with your keys.

This page is what people actually do. For every command and flag, see the reference: [items](items.md), [trash](trash.md), [photos](photos.md), [computers](computers.md), [shared](shared.md), [sharing](sharing.md), [invitations](invitations.md), [settings](settings.md), [volumes](volumes.md).

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

**A name already taken is refused.** `--if-exists` answers instead:

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

Every missing folder along the path is made: `Created 3 folders down to /Documents/2026/Q1/receipts`.

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

A filter that matches a folder and the files inside it selects the folder alone. See [Filters and bulk changes](../using/filters.md).

A trashed item has no path. Address it by the ID its listing showed.

## Earlier versions

Uploading over a file with `--if-exists replace` keeps what was there as a revision.

```bash
proton drive items revisions list /Documents/report.pdf
proton drive items revisions download /Documents/report.pdf 8f3a1c22 --dest ./earlier.pdf
proton drive items revisions restore /Documents/report.pdf 8f3a1c22
```

`download` reads an old version out and leaves the file alone. `restore` puts one back in place.

The file changes a moment after the command returns. The version it replaces stays in the history, and a restore can itself be undone.

Neither command touches the version the file is at now. Restoring it would do nothing, and deleting it would be deleting the file.

## Share it

### A public link

```bash
proton drive items share link /Documents/report.pdf --expires 7d \
  --link-password-file /run/secrets/report-link
proton drive items share get /Documents/report.pdf     # who has access, plus the link
proton drive items share unlink /Documents/report.pdf
```

The password comes from a file or from standard input, never from a flag value, and is at most 50 characters.

- `--expires never` makes an expiring link permanent.
- `--clear-link-password` removes the password.

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

An item somebody shared with you has no path of its own. Open it with `--shared`, naming it by the ID or the name the listing showed:

```bash
proton drive items list / --shared Project              # a shared folder
proton drive items download /report.pdf --shared Project --dest-dir ./downloads/
proton drive items download / --shared quarterly.pdf    # a shared file is / itself
```

ROLE in `shared list` is what you may do there: a viewer lists and downloads, an editor uploads and changes things as well.

`shared leave` gives up an item somebody shared with you. It asks first; only a new invitation from them brings it back.

An item whose name cannot be decrypted is still listed and can be acted on by ID.

### A link somebody sent you

A public link is a tree of its own. Name it with `--link`, and everything in it by path from its root:

```bash
proton drive items list / --link 'https://drive.proton.me/urls/7X2K9M3N1P#kQ81mDx4T9wL'
proton drive items download /Q3-report.pdf --link 'https://drive.proton.me/urls/7X2K9M3N1P#kQ81mDx4T9wL' --dest-dir ./downloads/
proton drive items download / --link 'https://drive.proton.me/urls/7X2K9M3N1P#kQ81mDx4T9wL' --dest-dir .
```

A link to a single file is `/` itself. Quote the link: the password is what follows the `#`, which a shell would otherwise drop.

A link with a password of its own takes it from a file or from standard input, as `items share link` does:

```bash
proton drive items download / --link 'https://drive.proton.me/urls/7X2K9M3N1P#kQ81mDx4T9wL' --link-password-file /run/secrets/q3-link --dest-dir .
```

A link that allows editing takes new files and folders. `items get /` shows `Link Access: edit` on one:

```bash
proton drive items upload ./photo.jpg / --link 'https://drive.proton.me/urls/7X2K9M3N1P#kQ81mDx4T9wL'
proton drive items create /2026 --link 'https://drive.proton.me/urls/7X2K9M3N1P#kQ81mDx4T9wL'
proton drive items upload --recursive ./album /2026 --link 'https://drive.proton.me/urls/7X2K9M3N1P#kQ81mDx4T9wL'
```

`--if-exists rename` and `skip` work in a link; `replace` is refused.

What you uploaded is yours to rename or delete for an hour:

```bash
proton drive items update /photo.jpg --name holiday.jpg --link 'https://drive.proton.me/urls/7X2K9M3N1P#kQ81mDx4T9wL'
proton drive items delete /photo.jpg --link 'https://drive.proton.me/urls/7X2K9M3N1P#kQ81mDx4T9wL'
```

Deleting is permanent: a link has no trash, and a folder goes with everything in it. `delete` names what is not yours before it asks. Nothing else in a link can be changed from here.

None of this needs an account. Signed in, what you upload names your address as Created By. Without an account it names nobody and cannot be changed afterwards. Behind a link, `items get` names nobody for anything you did not upload yourself, and reports Signature as `anonymous`.

Save a link to open it later without the URL or the password. Saving needs an account:

```bash
proton drive shared add 'https://drive.proton.me/urls/7X2K9M3N1P#kQ81mDx4T9wL'
proton drive shared list
proton drive items download / --shared Q3-report.pdf --dest-dir .
proton drive shared remove Q3-report.pdf
```

A saved link shows `public link` under SHARED BY. `shared remove` forgets it; the link itself keeps working, and `shared add` brings it back.

## Computers

A computer running the Proton Drive desktop app syncs its files to your account, and they are a tree of their own rather than part of your files.

```bash
proton drive computers list
proton drive items list / --computer "Work laptop"
proton drive items download /Documents/report.pdf --computer 7Kd91mQx --dest-dir .
proton drive computers update "Work laptop" --name "Office PC"
proton drive computers delete 7Kd91mQx
```

Every `items` command takes `--computer`, and paths then start at that computer's root. Only the desktop app can add a computer.

`computers delete` stops the syncing and takes the computer off the list. Set it up again by signing the desktop app in on it once more.

## Photos

```bash
proton drive photos list --tag favorites
proton drive photos upload ./IMG_0001.jpg
proton drive photos download 3Ns8pT2v --dest-dir ./photos/
proton drive photos favorite 3Ns8pT2v
```

Tags are `favorites`, `screenshots`, `videos`, `live-photos`, `motion-photos`, `selfies`, `portraits`, `bursts`, `panoramas` and `raw`.

Photos have no path; address them by ID.

```bash
proton drive photos albums create --name Holiday
proton drive photos albums add ALBUM_ID PHOTO_ID...
proton drive photos list --album ALBUM_ID
proton drive photos albums delete Holiday --delete-photos
```

## After a password reset

A password reset locks the volume your files are on, and Drive starts again empty. The old files are still there, on a volume marked `locked`:

```console
$ proton drive volumes list
ID        TYPE   STATE      USED  RESTORE  CREATED
────────  ─────  ──────  ───────  ───────  ────────────────
7Kd91mQx  files  active   1.2 GB           2026-05-04 09:14
3Ns8pT2v  files  locked  38.6 GB           2023-02-11 18:02
2 volumes.
```

Bring the keys back first, as under [Account](../account/README.md#after-a-password-reset), then put the files back:

```bash
proton account keys reactivate
proton drive volumes restore 3Ns8pT2v
```

They come back as a folder named `Restored files` and the date. Proton moves them in its own time, so they appear as it finishes. Computers and the photo library stay where they are.

To give the old files up instead:

```bash
proton drive volumes delete 3Ns8pT2v
```

Proton removes them within 72 hours, and nothing brings them back afterwards.

## Settings

```bash
proton drive settings set version-history 30d
```

`version-history` is how long previous versions are kept: `off`, `7d`, `30d`, `180d`, `1y` or `10y`. Keeping more than the default is a paid feature.
