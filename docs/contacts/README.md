# Contacts

Your Proton address book from the terminal: typed addresses and phones, the full vCard field set, groups, duplicate merging, and import and export. Cards are encrypted and signed with your user key.

This page is what people actually do. For every command and flag, see the reference: [contacts](contacts.md), [groups](groups.md), [keys](keys.md).

`REF` is a contact ID, a name, or an email address.

## The address book

```bash
proton contacts list --keyword jane --sort email
proton contacts get jane
proton contacts create --name "Jane Roe" --email jane@example.com --phone "+43 1 234567"
proton contacts update jane --email jane@newdomain.com
proton contacts delete jane
```

`--email` and `--phone` are repeatable. On `update` they **replace** the existing values rather than adding to them, so pass every value you want the contact to keep.

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

`--email`, `--phone`, `--address` and `--website` are repeatable and may say what kind they are, the way Proton's own editor offers one on each. A bare value states no kind, which vCard distinguishes from `other`.

| Field | Kinds |
| --- | --- |
| `--email` · `--address` · `--website` | `home`, `work`, `other` |
| `--phone` | `home`, `work`, `other`, `cell`, `main`, `fax`, `pager` |

A word before the colon that is not one of these is part of the value, so `--website https://example.com` keeps its scheme.

## Groups

Proton groups **addresses**, not people, so a colleague's work address can be in the team group while their personal one is not.

```bash
proton contacts groups create --name Team --color "#8080FF"
proton contacts groups get Team                               # and who is in it
proton contacts groups add Team jane                          # all of Jane's addresses
proton contacts groups add Team jane --email jane@acme.com    # only that one
proton contacts groups remove Team jane
```

With `--email`, exactly one contact may be named, because which address belongs to whom is a question only one contact can answer.

A *listing* of groups cannot say who is in one, because Proton keeps membership on the address rather than on the group. `get` is what asks the addresses.

## Merge duplicates

```bash
proton contacts merge --dry-run    # what it would fold, and into what
proton contacts merge
```

Contacts are duplicates when they share an **email address**, compared without regard to case. Sharing only a name is not enough, since people are routinely called the same thing.

The oldest contact of each set is kept, so groups and pinned keys that refer to it keep working. Fields from the others are added, and nothing is overwritten.

## Import and export

```bash
proton contacts export --dest-dir ./address-book     # one .vcf per contact
proton contacts export --dest - > contacts.vcf       # one file, all of them
proton contacts import contacts.vcf
```

**A property this tool has no flag for still travels.** The stored card goes out and comes back whole, so an anniversary, a photo or a second postal address survives a round trip even though no flag sets one.

**An import is addressed by UID.** A card carries the UID of the contact it is, so reading a file back changes that contact rather than making a second one. Export, edit, import, and the address book says what the file says.

A card with no name and no address is skipped and reported. The rest still land.

Nothing is merged on import, so reading a file that has no UIDs twice creates duplicates. Run `contacts merge` afterwards to fold them together.

## Pinned keys

Pinning a public key to a contact means mail to that address is encrypted to the key *you* trust, not just whatever the server hands back.

```bash
proton contacts keys pin jane --key jane-pubkey.asc
proton contacts keys pin jane --email jane@example.com --key -    # armored key on stdin
proton contacts keys pin jane --key jane.asc --no-encrypt         # pin for verification only
proton contacts keys unpin jane
```

`--email` picks which of the contact's addresses the key applies to when there are several.

`--scheme` is `pgp-mime` by default, or `pgp-inline`.
