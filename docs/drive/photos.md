# proton drive photos

Your photo library.

Every command under `proton drive photos`, with the arguments and flags it takes. For these commands in use, see [the drive guide](README.md).

Holds `albums`, `delete`, `download`, `favorite`, `list`, `trash`, `unfavorite` and `upload`.

## `albums`

Photo albums.

Holds `add`, `create`, `delete`, `list`, `remove` and `update`.

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

### `albums remove`

Take photos out of an album.

```
proton drive photos albums remove REF PHOTO_REF...
```

```bash
proton drive photos albums remove Holidays 5bH2mQxK
```

### `albums update`

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
| `--album string` | Show only what is in this album, by ID |
| `--tag string` | Show only photos with this tag: favorites, screenshots, videos, live-photos, motion-photos, selfies, portraits, bursts, panoramas, raw |

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
