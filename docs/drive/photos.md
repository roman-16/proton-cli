# proton drive photos

Your photo library.

Every command under `proton drive photos`, with the arguments and flags it takes. For these commands in use, see [the drive guide](README.md).

Holds `albums`, `delete`, `download`, `favorite`, `links`, `list`, `share`, `trash`, `unfavorite` and `upload`.

## `albums`

Photo albums.

Holds `add`, `create`, `delete`, `list`, `remove`, `share` and `update`.

### `albums add`

Put photos into an album.

```
proton drive photos albums add REF PHOTO_REF...
```

```bash
proton drive photos albums add Holidays 5bH2mQxK
```

### `albums create`

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

### `albums delete`

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

### `albums list`

List albums.

```
proton drive photos albums list
```

```bash
proton drive photos albums list
```

| Flag | Description |
| --- | --- |
| `--desc` | Reverse the order |
| `--limit int` | How many albums per page; 0 for all of them |
| `--page int` | Which page of results, counting from zero |
| `--sort string` | Order by: name, photos (default `name`) |

### `albums remove`

Take photos out of an album.

```
proton drive photos albums remove REF PHOTO_REF...
```

```bash
proton drive photos albums remove Holidays 5bH2mQxK
```

### `albums share`

The people you share an album with.

Holds `add`, `confirm`, `get`, `remove`, `resend` and `update`.

### `albums share add`

Invite someone to an album.

EMAIL may be an address outside Proton. Proton emails them an invitation to create an account, and nothing reaches them until they have one and you run `share confirm`.

```
proton drive photos albums share add REF EMAIL
```

```bash
proton drive photos albums share add Holidays jane@proton.me
proton drive photos albums share add Holidays jane@proton.me --access editor --message 'Photos from the trip'
```

| Flag | Description |
| --- | --- |
| `--access string` | What they may do with it: viewer, editor (default `viewer`) |
| `--message string` | Note to include in the invitation email |

### `albums share confirm`

Let somebody in once they join Proton.

Use it for an address that had no Proton account when you invited it. It is refused until the account exists, and `share get` says who is ready. Afterwards they hold an ordinary invitation, which they still have to accept.

```
proton drive photos albums share confirm REF EMAIL
```

```bash
proton drive photos albums share confirm Holidays sam@example.com
```

### `albums share get`

Show how an album is shared.

```
proton drive photos albums share get REF
```

```bash
proton drive photos albums share get Holidays
```

### `albums share remove`

Revoke someone's access, or cancel their invitation.

```
proton drive photos albums share remove REF EMAIL
```

```bash
proton drive photos albums share remove Holidays jane@proton.me
```

### `albums share resend`

Send an unanswered invitation again.

```
proton drive photos albums share resend REF EMAIL
```

```bash
proton drive photos albums share resend Holidays jane@proton.me
```

### `albums share update`

Change what somebody may do with an album.

Name them by address. It works whether they have accepted the share or still have it pending.

```
proton drive photos albums share update REF EMAIL
```

```bash
proton drive photos albums share update Holidays jane@proton.me --access viewer
```

| Flag | Description |
| --- | --- |
| `--access string` | What they may do with it: viewer, editor (default `viewer`) |

### `albums update`

Rename an album, or change its cover.

A cover has to be a photo the album holds. Anything you do not mention is left alone.

```
proton drive photos albums update REF
```

```bash
proton drive photos albums update Holidays --cover 5bH2mQxK
```

| Flag | Description |
| --- | --- |
| `--cover string` | Which of the album's photos represents it |
| `--name string` | New name for the album |

## `delete`

Delete photos permanently.

```
proton drive photos delete REF...
```

```bash
proton drive photos delete 5bH2mQxK
```

## `download`

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

## `favorite`

Mark photos as favourites.

```
proton drive photos favorite REF...
```

```bash
proton drive photos favorite 5bH2mQxK
```

## `links`

Links that open a photo for anyone.

Holds `create`, `get` and `revoke`.

### `links create`

Make a link that opens a photo for anyone.

An item carries one link, so running it again changes that link rather than making a second one, and a URL you have already shared keeps working.

The password is read from a file, never from a flag value, and may be at most 50 characters. --clear-link-password takes it off again, and --expires never makes an expiring link permanent.

An album has no public link. Share one with the people you want to see it, with `photos albums share add`.

```
proton drive photos links create REF
```

```bash
proton drive photos links create 5bH2mQxK
proton drive photos links create 5bH2mQxK --expires 7d
proton drive photos links create 5bH2mQxK --link-password-file /run/secrets/photo-link
```

| Flag | Description |
| --- | --- |
| `--clear-link-password` | Remove the public link's password |
| `--expires string` | Stop working after DURATION (e.g. 7d, 2w, 6mo), or never |
| `--link-password-file string` | Read the public link's password from a file, or - for stdin |

### `links get`

Show the link on a photo, URL and all.

Use it to recover a URL you mislaid, rather than revoking the link and making a new one. The URL appears here and in no listing.

```
proton drive photos links get REF
```

```bash
proton drive photos links get 5bH2mQxK
```

### `links revoke`

Stop the link on a photo working.

The item is untouched; only the link stops working. This cannot take back what somebody already read.

```
proton drive photos links revoke REF
```

```bash
proton drive photos links revoke 5bH2mQxK
```

## `list`

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
| `--album string` | Show only what is in this album, by name or ID |
| `--desc` | Reverse the order |
| `--limit int` | How many photos per page; 0 for all of them |
| `--page int` | Which page of results, counting from zero |
| `--sort string` | Order by: captured (default `captured`) |
| `--tag string` | Show only photos with this tag: favorites, screenshots, videos, live-photos, motion-photos, selfies, portraits, bursts, panoramas, raw |

## `share`

The people you share a photo with.

Holds `add`, `confirm`, `get`, `remove`, `resend` and `update`.

### `share add`

Invite someone to a photo.

EMAIL may be an address outside Proton. Proton emails them an invitation to create an account, and nothing reaches them until they have one and you run `share confirm`.

```
proton drive photos share add REF EMAIL
```

```bash
proton drive photos share add 5bH2mQxK jane@proton.me
proton drive photos share add 5bH2mQxK jane@proton.me --access editor
```

| Flag | Description |
| --- | --- |
| `--access string` | What they may do with it: viewer, editor (default `viewer`) |
| `--message string` | Note to include in the invitation email |

### `share confirm`

Let somebody in once they join Proton.

Use it for an address that had no Proton account when you invited it. It is refused until the account exists, and `share get` says who is ready. Afterwards they hold an ordinary invitation, which they still have to accept.

```
proton drive photos share confirm REF EMAIL
```

```bash
proton drive photos share confirm 5bH2mQxK sam@example.com
```

### `share get`

Show how a photo is shared.

```
proton drive photos share get REF
```

```bash
proton drive photos share get 5bH2mQxK
```

### `share remove`

Revoke someone's access, or cancel their invitation.

```
proton drive photos share remove REF EMAIL
```

```bash
proton drive photos share remove 5bH2mQxK jane@proton.me
```

### `share resend`

Send an unanswered invitation again.

```
proton drive photos share resend REF EMAIL
```

```bash
proton drive photos share resend 5bH2mQxK jane@proton.me
```

### `share update`

Change what somebody may do with a photo.

Name them by address. It works whether they have accepted the share or still have it pending.

```
proton drive photos share update REF EMAIL
```

```bash
proton drive photos share update 5bH2mQxK jane@proton.me --access viewer
```

| Flag | Description |
| --- | --- |
| `--access string` | What they may do with it: viewer, editor (default `viewer`) |

## `trash`

Move photos to the trash.

```
proton drive photos trash REF...
```

```bash
proton drive photos trash 5bH2mQxK
```

## `unfavorite`

Remove photos from favourites.

```
proton drive photos unfavorite REF...
```

```bash
proton drive photos unfavorite 5bH2mQxK
```

## `upload`

Upload a photo to the library.

```
proton drive photos upload SRC
```

```bash
proton drive photos upload ./IMG_2291.jpg
```

---

Every command also takes the [flags that work everywhere](../about/commands.md#flags-that-work-on-every-command).
