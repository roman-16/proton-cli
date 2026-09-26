# proton contacts

Contacts, their groups, email settings and keys.

Every command under `proton contacts`, with the arguments and flags it takes. For these commands in use, see [the contacts guide](README.md).

Holds `create`, `delete`, `emails`, `export`, `get`, `groups`, `import`, `keys`, `list`, `merge` and `update`.

## `create`

Create a contact.

A photo from a file is shrunk until its shorter side is at most 180 pixels and stored as JPEG; JPEG, PNG, GIF and WebP are read. A web address is stored as it is given.

```
proton contacts create
```

```bash
proton contacts create --name 'Jane Roe' --email jane@example.com
proton contacts create --name 'Jane Roe' --email work:jane@acme.com --phone cell:+43123456 --anniversary 2015-06-20
proton contacts create --name 'Jane Roe' --email jane@example.com --phone '+43 660 1234567' --organization Acme
proton contacts create --name 'Jane Roe' --email jane@example.com --photo https://example.com/jane.png
```

| Flag | Description |
| --- | --- |
| `--address stringArray` | Set a postal address, as ADDRESS or KIND:ADDRESS (repeatable) |
| `--anniversary string` | Set the anniversary (e.g. 2015-06-20) |
| `--birthday string` | Set the birthday (e.g. 1990-01-31) |
| `--email stringArray` | Set an email address, as ADDRESS or KIND:ADDRESS (repeatable) |
| `--first-name string` | Set the given name |
| `--gender string` | Set the gender |
| `--job-title stringArray` | Set a job title (repeatable) |
| `--language stringArray` | Set a preferred language, e.g. de-AT (repeatable) |
| `--last-name string` | Set the family name |
| `--name string` | Set the name shown in listings |
| `--nickname stringArray` | Set a nickname (repeatable) |
| `--note stringArray` | Set a note (repeatable) |
| `--organization stringArray` | Set an organization (repeatable) |
| `--phone stringArray` | Set a phone number, as NUMBER or KIND:NUMBER (repeatable) |
| `--photo string` | Set the photo: an image file, - for stdin, or a web address |
| `--role stringArray` | Set a role played in an organization (repeatable) |
| `--timezone stringArray` | Set a time zone, e.g. Europe/Vienna (repeatable) |
| `--website stringArray` | Set a website, as URL or KIND:URL (repeatable) |

## `delete`

Delete contacts.

```
proton contacts delete REF...
```

```bash
proton contacts delete jane
```

## `export`

Write contacts out as .vcf files, or as one stream with --dest -.

Naming contacts writes those; naming none writes the whole address book, narrowed by --keyword.

The stored card goes out whole, so properties this tool has no flag for are exported too. Each address's groups go out as CATEGORIES beside it, which is what `import` reads them back from.

```
proton contacts export [REF...]
```

```bash
proton contacts export --dest-dir ./address-book
proton contacts export jane --dest jane.vcf
proton contacts export --dest - > contacts.vcf
```

| Flag | Description |
| --- | --- |
| `--dest string` | Write to this path, or - for stdout |
| `--dest-dir string` | Write into this directory, keeping each item's own name |
| `--force` | Overwrite a file that already exists |
| `--keyword string` | Match text in the name or the address |

## `get`

Show one contact in full.

```
proton contacts get REF
```

```bash
proton contacts get jane@example.com
proton contacts get 'Jane Roe'
```

## `import`

Read contacts in from a .vcf file, or from stdin with -.

Each card goes in whole, so a property this tool has no flag for survives the trip. A card with no name and no address is skipped and reported: there would be nothing to file it under.

The groups a card names in CATEGORIES are applied: an address goes into a group of that name, and a group you do not have is created. --no-groups leaves them out, and --dry-run shows what the file would put where.

A card that carries a UID replaces the contact with that UID, which is what makes a file from `export` a backup. A card without one is a new contact, so importing such a file twice creates duplicates; use `merge` afterwards to fold them together.

```
proton contacts import PATH
```

```bash
proton contacts import contacts.vcf
proton contacts import contacts.vcf --dry-run
proton contacts import google.vcf --no-groups
proton contacts import - < exported.vcf
```

| Flag | Description |
| --- | --- |
| `--no-groups` | Leave the groups the file names out |

## `list`

List contacts.

```
proton contacts list
```

```bash
proton contacts list
proton contacts list --output json
```

| Flag | Description |
| --- | --- |
| `--desc` | Reverse the order |
| `--keyword string` | Match text in the name or the address |
| `--limit int` | How many contacts per page; 0 for all of them (default `50`) |
| `--page int` | Which page of results, counting from zero |
| `--sort string` | Order by: name, email (default `name`) |

## `merge`

Fold duplicate contacts into one.

Contacts are duplicates when they share an email address, compared without regard to case. Sharing only a name is not enough.

The oldest contact of each set is kept, so groups and trusted keys that refer to it keep working. Fields from the others are added; nothing is overwritten.

```
proton contacts merge
```

```bash
proton contacts merge --dry-run
proton contacts merge
```

## `update`

Change a contact's details.

Only what you pass is replaced. A repeatable flag replaces the whole list rather than adding to it, so pass every value you want the contact to keep. --clear-note removes every note, --clear-photo the photo, and so on for each detail.

A photo from a file is shrunk until its shorter side is at most 180 pixels and stored as JPEG; JPEG, PNG, GIF and WebP are read. A web address is stored as it is given.

```
proton contacts update REF
```

```bash
proton contacts update jane --job-title 'Head of Design'
proton contacts update jane --email jane.roe@work.example --birthday 1990-04-16
proton contacts update jane --photo ~/Pictures/jane.jpg
proton contacts update jane --clear-note --clear-photo
```

| Flag | Description |
| --- | --- |
| `--address stringArray` | Replace a postal address, as ADDRESS or KIND:ADDRESS (repeatable) |
| `--anniversary string` | Replace the anniversary (e.g. 2015-06-20) |
| `--birthday string` | Replace the birthday (e.g. 1990-01-31) |
| `--clear-address` | Remove every postal address |
| `--clear-anniversary` | Remove the anniversary |
| `--clear-birthday` | Remove the birthday |
| `--clear-email` | Remove every email address |
| `--clear-first-name` | Remove the given name |
| `--clear-gender` | Remove the gender |
| `--clear-job-title` | Remove every job title |
| `--clear-language` | Remove every preferred language |
| `--clear-last-name` | Remove the family name |
| `--clear-nickname` | Remove every nickname |
| `--clear-note` | Remove every note |
| `--clear-organization` | Remove every organization |
| `--clear-phone` | Remove every phone number |
| `--clear-photo` | Remove the photo |
| `--clear-role` | Remove every role |
| `--clear-timezone` | Remove every time zone |
| `--clear-website` | Remove every website |
| `--email stringArray` | Replace an email address, as ADDRESS or KIND:ADDRESS (repeatable) |
| `--first-name string` | Replace the given name |
| `--gender string` | Replace the gender |
| `--job-title stringArray` | Replace a job title (repeatable) |
| `--language stringArray` | Replace a preferred language, e.g. de-AT (repeatable) |
| `--last-name string` | Replace the family name |
| `--name string` | Replace the name shown in listings |
| `--nickname stringArray` | Replace a nickname (repeatable) |
| `--note stringArray` | Replace a note (repeatable) |
| `--organization stringArray` | Replace an organization (repeatable) |
| `--phone stringArray` | Replace a phone number, as NUMBER or KIND:NUMBER (repeatable) |
| `--photo string` | Replace the photo: an image file, - for stdin, or a web address |
| `--role stringArray` | Replace a role played in an organization (repeatable) |
| `--timezone stringArray` | Replace a time zone, e.g. Europe/Vienna (repeatable) |
| `--website stringArray` | Replace a website, as URL or KIND:URL (repeatable) |

---

Every command also takes the [flags that work everywhere](../about/commands.md#flags-that-work-on-every-command).
