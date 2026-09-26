# Contacts

Your Proton address book from the terminal: typed addresses and phones, the full vCard field set, photos, groups, how mail to each address is sent, duplicate merging, and import and export. Cards are encrypted and signed with your user key.

This page is what people actually do. For every command and flag, see the reference: [contacts](contacts.md), [emails](emails.md), [groups](groups.md), [keys](keys.md).

`REF` is a contact ID, a name, or an email address.

## The address book

```bash
proton contacts list --keyword jane --sort email
proton contacts get jane
proton contacts create --name "Jane Roe" --email jane@example.com --phone "+43 1 234567"
proton contacts update jane --email jane@newdomain.com
proton contacts delete jane
```

A repeatable flag such as `--email` or `--phone` **replaces** the existing values on `update` rather than adding to them, so pass every value you want the contact to keep. To remove a detail altogether, pass its `--clear-` flag:

```bash
proton contacts update jane --clear-note --clear-job-title
```

## Fields

Every field takes the same flag on `create` and `update`:

```bash
proton contacts create --name "Jane Roe" \
  --first-name Jane --last-name Roe --nickname Janey \
  --email work:jane@acme.com --email home:jane@example.com \
  --phone cell:+43123456 --address home:"1 Example St, Vienna" \
  --organization Acme --job-title Engineer --birthday 1990-01-31 \
  --language de-AT --timezone Europe/Vienna --note "Likes tea"
```

`--nickname`, `--email`, `--phone`, `--address`, `--website`, `--organization`, `--job-title`, `--role`, `--language`, `--timezone` and `--note` are repeatable. `--email`, `--phone`, `--address` and `--website` may say what kind they are. A bare value states no kind, which is not the same as `other`.

| Field | Kinds |
| --- | --- |
| `--email` · `--address` · `--website` | `home`, `work`, `other` |
| `--phone` | `home`, `work`, `other`, `cell`, `main`, `fax`, `pager` |

A word before the colon that is not one of these is part of the value, so `--website https://example.com` keeps its scheme.

## Photo

```bash
proton contacts update jane --photo ~/Pictures/jane.jpg
proton contacts update jane --photo https://example.com/jane.png
proton contacts update jane --clear-photo
```

An image file is shrunk until its shorter side is at most 180 pixels and stored as JPEG, without its metadata. JPEG, PNG, GIF and WebP are read, and `--photo -` reads the image from standard input. A web address is stored as it is given.

`contacts get` shows the photo's format and size, or its web address. With `--output json`, `photo` holds the web address, or the image itself as a `data:` URI.

## How mail to an address is sent

Each address has its own settings, which `proton` and Proton's own apps follow when they send to it:

```bash
proton contacts emails list jane
proton contacts emails update jane@acme.com --email-format plain-text
proton contacts emails update jane@acme.com --sign on --scheme pgp-inline
proton contacts emails update jane@example.com --encrypt off
```

| Flag | Values | Default |
| --- | --- | --- |
| `--email-format` | `automatic` sends mail as it was written, `plain-text` always as plain text | `automatic` |
| `--encrypt` | `on`, `off` | `on` when there is a key to encrypt to |
| `--sign` | `on`, `off`, `default` | `default`, which follows `mail settings` `sign` |
| `--scheme` | `pgp-mime`, `pgp-inline`, `default` | `default`, which follows `mail settings` `pgp-scheme` |

The settings depend on each other, and `emails update` refuses a combination that cannot be sent:

- **A Proton address** takes `--email-format` alone. Mail to it is always encrypted and signed.
- **Encrypted mail is always signed.** `--encrypt` needs a key: a trusted one, or one the address's provider publishes.
- **Signed mail's format follows the scheme:** plain text under `pgp-inline`, as it was written under `pgp-mime`.

With `--eo-password-file`, a message goes to an address that is not encrypted to as a password-protected link, whatever its settings say.

## Groups

Proton groups **addresses**, not people, so a colleague's work address can be in the team group while their personal one is not.

```bash
proton contacts groups create --name Team --color "#8080FF"
proton contacts groups get Team                       # and who is in it
proton contacts groups add Team jane                  # all of Jane's addresses
proton contacts groups add Team jane@acme.com         # only that one
proton contacts groups remove Team jane
```

Naming a contact means every address they hold; naming one of their addresses means that one. Both forms can appear in the same command.

A *listing* of groups does not show members. `get` does.

## Merge duplicates

```bash
proton contacts merge --dry-run    # what it would fold, and into what
proton contacts merge
```

Contacts are duplicates when they share an **email address**, compared without regard to case. Sharing only a name is not enough, since people are routinely called the same thing.

The oldest contact of each set is kept, so groups and trusted keys that refer to it keep working. Fields from the others are added, and nothing is overwritten.

## Import and export

```bash
proton contacts export --dest-dir ./address-book     # one .vcf per contact
proton contacts export --dest - > contacts.vcf       # one file, all of them
proton contacts import contacts.vcf
```

**A property this tool has no flag for still travels.** The stored card goes out and comes back whole, so a logo, a related person or a custom property survives a round trip even though no flag sets one.

**An import is addressed by UID.** A card carries the UID of the contact it is, so reading a file back changes that contact rather than making a second one. Export, edit, import, and the address book says what the file says.

A card with no name and no address is skipped and reported. The rest still land.

Nothing is merged on import, so reading a file that has no UIDs twice creates duplicates. Run `contacts merge` afterwards to fold them together.

## Trusted keys

Trusting a public key for a contact means mail to that address is encrypted to the key *you* chose, not just whatever the server hands back.

```bash
proton contacts keys trust jane --key jane-pubkey.asc
proton contacts keys trust jane@example.com --key -         # armored key on stdin
proton contacts keys untrust jane@example.com
```

A key is trusted for one address. Name that address when the contact holds several; naming the contact is enough when they hold one.

Trusting turns encryption to the address on. To keep a key for verifying signatures only, turn it off again with `proton contacts emails update jane@example.com --encrypt off`. Untrusting removes the keys and leaves the address's other settings as they are.
