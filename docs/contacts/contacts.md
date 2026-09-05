# proton contacts

Contacts, their groups and their pinned keys.

Every command under `proton contacts`, with the arguments and flags it takes. For these commands in use, see [the contacts guide](README.md).

Holds `create`, `delete`, `export`, `get`, `groups`, `import`, `keys`, `list`, `merge` and `update`.

## `create`

Create a contact.

```
proton contacts create
```

```bash
proton contacts create --name 'Jane Roe' --email jane@example.com
proton contacts create --name 'Jane Roe' --email work:jane@acme.com --phone cell:+43123456 --anniversary 2015-06-20
proton contacts create --name 'Jane Roe' --email jane@example.com --phone '+43 660 1234567' --organization Acme
```

| Flag | Description |
| --- | --- |
| `--address stringArray` | Set a postal address, as ADDRESS or KIND:ADDRESS (repeatable) |
| `--anniversary string` | Set the anniversary (e.g. 2015-06-20) |
| `--birthday string` | Set the birthday (e.g. 1990-01-31) |
| `--email stringArray` | Set an email address, as ADDRESS or KIND:ADDRESS (repeatable) |
| `--first-name string` | Set the given name |
| `--gender string` | Set the gender |
| `--job-title string` | Set the job title |
| `--language string` | Set the preferred language (e.g. de-AT) |
| `--last-name string` | Set the family name |
| `--name string` | Set the name shown in listings |
| `--nickname string` | Set the nickname |
| `--note string` | Set the note |
| `--organization string` | Set the organization |
| `--phone stringArray` | Set a phone number, as NUMBER or KIND:NUMBER (repeatable) |
| `--role string` | Set the role played in the organization |
| `--timezone string` | Set the time zone (e.g. Europe/Vienna) |
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
| `--page int` | Which page of results, counting from zero |
| `--page-size int` | How many contacts per page; 0 for all of them (default `50`) |
| `--sort string` | Order by: name, email (default `name`) |

## `merge`

Fold duplicate contacts into one.

Contacts are duplicates when they share an email address, compared without regard to case. Sharing only a name is not enough.

The oldest contact of each set is kept, so groups and pinned keys that refer to it keep working. Fields from the others are added; nothing is overwritten.

```
proton contacts merge
```

```bash
proton contacts merge --dry-run
proton contacts merge
```

## `update`

Change a contact's details.

Only what you pass is replaced. --email and --phone replace the whole list rather than adding to it, so pass every address you want the contact to keep.

```
proton contacts update REF
```

```bash
proton contacts update jane --job-title 'Head of Design'
proton contacts update jane --email jane.roe@work.example --birthday 1990-04-16
```

| Flag | Description |
| --- | --- |
| `--address stringArray` | Replace a postal address, as ADDRESS or KIND:ADDRESS (repeatable) |
| `--anniversary string` | Replace the anniversary (e.g. 2015-06-20) |
| `--birthday string` | Replace the birthday (e.g. 1990-01-31) |
| `--email stringArray` | Replace an email address, as ADDRESS or KIND:ADDRESS (repeatable) |
| `--first-name string` | Replace the given name |
| `--gender string` | Replace the gender |
| `--job-title string` | Replace the job title |
| `--language string` | Replace the preferred language (e.g. de-AT) |
| `--last-name string` | Replace the family name |
| `--name string` | Replace the name shown in listings |
| `--nickname string` | Replace the nickname |
| `--note string` | Replace the note |
| `--organization string` | Replace the organization |
| `--phone stringArray` | Replace a phone number, as NUMBER or KIND:NUMBER (repeatable) |
| `--role string` | Replace the role played in the organization |
| `--timezone string` | Replace the time zone (e.g. Europe/Vienna) |
| `--website stringArray` | Replace a website, as URL or KIND:URL (repeatable) |

---

Every command also takes the [flags that work everywhere](../about/commands.md#flags-that-work-on-every-command).
