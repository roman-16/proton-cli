# All commands

Every command in one table, generated from the command tree. Search this page for a word, then follow the link for the arguments, flags and examples.

```
proton <app> <collection> <verb> [TARGET...] [--flags]
```

Where a command shows `REF`, pass a full ID, the eight-character short ID a list printed, or something you already know: a subject, a name, a path, an email address. See [Naming what to act on](../using/naming.md).

| Command | What it does |
| --- | --- |
| [`proton account get`](../account/account.md#get) | Show the account, its storage and this machine's session |
| [`proton account login`](../account/account.md#login) | Sign in and save the session for this profile |
| [`proton account logout`](../account/account.md#logout) | Discard the saved session for this profile |
| [`proton account profiles delete`](../account/profiles.md#delete) | Remove saved sessions by profile name |
| [`proton account profiles list`](../account/profiles.md#list) | List the profiles with a saved session |
| [`proton account sessions list`](../account/sessions.md#list) | List every signed-in session |
| [`proton account sessions revoke`](../account/sessions.md#revoke) | Invalidate sessions at Proton |
| [`proton account settings get`](../account/settings.md#get) | Show the account settings now in effect |
| [`proton account settings list`](../account/settings.md#list) | List the account settings that can be changed |
| [`proton account settings set`](../account/settings.md#set) | Change one account setting |
| [`proton api`](../api/api.md) | Send a raw authenticated request to the Proton API |
| [`proton calendar events create`](../calendar/events.md#create) | Create an event |
| [`proton calendar events delete`](../calendar/events.md#delete) | Delete events |
| [`proton calendar events export`](../calendar/events.md#export) | Write events out as an .ics file |
| [`proton calendar events get`](../calendar/events.md#get) | Show one event, decrypted |
| [`proton calendar events import`](../calendar/events.md#import) | Read events in from an .ics file |
| [`proton calendar events list`](../calendar/events.md#list) | List events in a date range |
| [`proton calendar events respond`](../calendar/events.md#respond) | Answer an invitation, telling the organizer |
| [`proton calendar events update`](../calendar/events.md#update) | Change an event's title, time, location, description or recurrence |
| [`proton calendar invitations accept`](../calendar/invitations.md#accept) | Take a calendar somebody offered you |
| [`proton calendar invitations decline`](../calendar/invitations.md#decline) | Turn down a calendar somebody offered you |
| [`proton calendar invitations list`](../calendar/invitations.md#list) | List calendars other people have offered you |
| [`proton calendar reminders list`](../calendar/reminders.md#list) | List the reminders due in a date range |
| [`proton calendar reminders watch`](../calendar/reminders.md#watch) | Print each reminder as it comes due |
| [`proton calendar settings calendars create`](../calendar/settings.md#calendars-create) | Create a calendar, or subscribe to one published elsewhere |
| [`proton calendar settings calendars delete`](../calendar/settings.md#calendars-delete) | Delete calendars, and every event in them |
| [`proton calendar settings calendars get`](../calendar/settings.md#calendars-get) | Show one calendar, with the defaults it gives new events |
| [`proton calendar settings calendars list`](../calendar/settings.md#calendars-list) | List your calendars |
| [`proton calendar settings calendars share add`](../calendar/settings.md#calendars-share-add) | Give somebody a calendar |
| [`proton calendar settings calendars share list`](../calendar/settings.md#calendars-share-list) | List who has a calendar |
| [`proton calendar settings calendars share remove`](../calendar/settings.md#calendars-share-remove) | Take somebody's access to a calendar away |
| [`proton calendar settings calendars update`](../calendar/settings.md#calendars-update) | Rename or recolor a calendar, or change what it gives new events |
| [`proton calendar settings get`](../calendar/settings.md#get) | Show the calendar settings now in effect |
| [`proton calendar settings list`](../calendar/settings.md#list) | List the calendar settings that can be changed |
| [`proton calendar settings set`](../calendar/settings.md#set) | Change one calendar setting |
| [`proton contacts create`](../contacts/contacts.md#create) | Create a contact |
| [`proton contacts delete`](../contacts/contacts.md#delete) | Delete contacts |
| [`proton contacts export`](../contacts/contacts.md#export) | Write contacts out as vCards |
| [`proton contacts get`](../contacts/contacts.md#get) | Show one contact in full |
| [`proton contacts import`](../contacts/contacts.md#import) | Read contacts in from a .vcf file |
| [`proton contacts list`](../contacts/contacts.md#list) | List contacts |
| [`proton contacts merge`](../contacts/contacts.md#merge) | Fold duplicate contacts into one |
| [`proton contacts update`](../contacts/contacts.md#update) | Change a contact's details |
| [`proton contacts groups add`](../contacts/groups.md#add) | Add contacts to a group |
| [`proton contacts groups create`](../contacts/groups.md#create) | Create a contact group |
| [`proton contacts groups delete`](../contacts/groups.md#delete) | Delete contact groups |
| [`proton contacts groups get`](../contacts/groups.md#get) | Show one group and the addresses in it |
| [`proton contacts groups list`](../contacts/groups.md#list) | List contact groups |
| [`proton contacts groups remove`](../contacts/groups.md#remove) | Remove contacts from a group |
| [`proton contacts groups update`](../contacts/groups.md#update) | Rename or recolor a contact group |
| [`proton contacts keys list`](../contacts/keys.md#list) | List the keys pinned to a contact |
| [`proton contacts keys pin`](../contacts/keys.md#pin) | Pin a public key so mail to a contact is encrypted to it |
| [`proton contacts keys unpin`](../contacts/keys.md#unpin) | Remove the keys pinned to a contact |
| [`proton drive invitations accept`](../drive/invitations.md#accept) | Accept invitations |
| [`proton drive invitations decline`](../drive/invitations.md#decline) | Decline invitations |
| [`proton drive invitations list`](../drive/invitations.md#list) | List invitations waiting for an answer |
| [`proton drive items copy`](../drive/items.md#copy) | Copy files into another folder |
| [`proton drive items create`](../drive/items.md#create) | Create a folder, and any missing folder above it |
| [`proton drive items delete`](../drive/items.md#delete) | Delete files or folders permanently |
| [`proton drive items download`](../drive/items.md#download) | Download a file |
| [`proton drive items get`](../drive/items.md#get) | Show a file or folder's details |
| [`proton drive items list`](../drive/items.md#list) | List what is in a folder |
| [`proton drive items move`](../drive/items.md#move) | Move files or folders into another folder |
| [`proton drive items revisions delete`](../drive/items.md#revisions-delete) | Delete an earlier version permanently |
| [`proton drive items revisions download`](../drive/items.md#revisions-download) | Download an earlier version of a file |
| [`proton drive items revisions list`](../drive/items.md#revisions-list) | List a file's earlier versions |
| [`proton drive items revisions restore`](../drive/items.md#revisions-restore) | Restore a file to an earlier version |
| [`proton drive items share add`](../drive/items.md#share-add) | Invite someone to a file or folder |
| [`proton drive items share get`](../drive/items.md#share-get) | Show how a file or folder is shared |
| [`proton drive items share link`](../drive/items.md#share-link) | Create or update the public link for a file or folder |
| [`proton drive items share remove`](../drive/items.md#share-remove) | Revoke someone's access, or cancel their invitation |
| [`proton drive items share resend`](../drive/items.md#share-resend) | Send an unanswered invitation again |
| [`proton drive items share unlink`](../drive/items.md#share-unlink) | Remove the public links for a file or folder |
| [`proton drive items share update`](../drive/items.md#share-update) | Change what somebody may do with a file or folder |
| [`proton drive items trash`](../drive/items.md#trash) | Move files or folders to the trash |
| [`proton drive items update`](../drive/items.md#update) | Rename a file or folder |
| [`proton drive items upload`](../drive/items.md#upload) | Upload a file or directory |
| [`proton drive photos albums add`](../drive/photos.md#albums-add) | Put photos into an album |
| [`proton drive photos albums create`](../drive/photos.md#albums-create) | Create an album |
| [`proton drive photos albums delete`](../drive/photos.md#albums-delete) | Delete albums |
| [`proton drive photos albums list`](../drive/photos.md#albums-list) | List albums |
| [`proton drive photos albums remove`](../drive/photos.md#albums-remove) | Take photos out of an album |
| [`proton drive photos albums update`](../drive/photos.md#albums-update) | Change an album's cover |
| [`proton drive photos delete`](../drive/photos.md#delete) | Delete photos permanently |
| [`proton drive photos download`](../drive/photos.md#download) | Download a photo |
| [`proton drive photos favorite`](../drive/photos.md#favorite) | Mark photos as favourites |
| [`proton drive photos list`](../drive/photos.md#list) | List photos |
| [`proton drive photos trash`](../drive/photos.md#trash) | Move photos to the trash |
| [`proton drive photos unfavorite`](../drive/photos.md#unfavorite) | Remove photos from favourites |
| [`proton drive photos upload`](../drive/photos.md#upload) | Upload a photo to the library |
| [`proton drive settings get`](../drive/settings.md#get) | Show the drive settings now in effect |
| [`proton drive settings list`](../drive/settings.md#list) | List the drive settings that can be changed |
| [`proton drive settings set`](../drive/settings.md#set) | Change one drive setting |
| [`proton drive shared list`](../drive/shared.md#list) | List what other people have shared with you |
| [`proton drive sharing list`](../drive/sharing.md#list) | List what you have shared |
| [`proton drive trash empty`](../drive/trash.md#empty) | Delete everything in the trash, permanently |
| [`proton drive trash list`](../drive/trash.md#list) | List what is in the trash |
| [`proton drive trash restore`](../drive/trash.md#restore) | Put items back where they came from |
| [`proton mail conversations attachments download`](../mail/conversations.md#attachments-download) | Download and decrypt attachments from a thread |
| [`proton mail conversations attachments list`](../mail/conversations.md#attachments-list) | List every attachment in a thread |
| [`proton mail conversations delete`](../mail/conversations.md#delete) | Delete threads permanently |
| [`proton mail conversations export`](../mail/conversations.md#export) | Write a whole thread out as .eml files or one mbox |
| [`proton mail conversations forward`](../mail/conversations.md#forward) | Forward the newest message in a thread |
| [`proton mail conversations get`](../mail/conversations.md#get) | Show a whole thread, decrypted |
| [`proton mail conversations label`](../mail/conversations.md#label) | Attach a label to threads |
| [`proton mail conversations list`](../mail/conversations.md#list) | List threads in a folder |
| [`proton mail conversations mark read`](../mail/conversations.md#mark-read) | Mark threads as read |
| [`proton mail conversations mark unread`](../mail/conversations.md#mark-unread) | Mark threads as unread |
| [`proton mail conversations move`](../mail/conversations.md#move) | Move threads to a folder |
| [`proton mail conversations reply`](../mail/conversations.md#reply) | Reply to the newest message in a thread |
| [`proton mail conversations snooze`](../mail/conversations.md#snooze) | Take threads out of the inbox until later |
| [`proton mail conversations star`](../mail/conversations.md#star) | Star threads |
| [`proton mail conversations trash`](../mail/conversations.md#trash) | Move threads to the trash |
| [`proton mail conversations unlabel`](../mail/conversations.md#unlabel) | Detach a label from threads |
| [`proton mail conversations unsnooze`](../mail/conversations.md#unsnooze) | Bring snoozed threads back to the inbox now |
| [`proton mail conversations unstar`](../mail/conversations.md#unstar) | Remove the star from threads |
| [`proton mail drafts create`](../mail/drafts.md#create) | Save a draft without sending it |
| [`proton mail drafts delete`](../mail/drafts.md#delete) | Delete drafts |
| [`proton mail drafts list`](../mail/drafts.md#list) | List drafts |
| [`proton mail drafts send`](../mail/drafts.md#send) | Send a draft as it stands |
| [`proton mail drafts update`](../mail/drafts.md#update) | Change a draft's recipients, subject, body or attachments |
| [`proton mail messages attachments download`](../mail/messages.md#attachments-download) | Download and decrypt attachments |
| [`proton mail messages attachments list`](../mail/messages.md#attachments-list) | List a message's attachments |
| [`proton mail messages delete`](../mail/messages.md#delete) | Delete messages permanently |
| [`proton mail messages empty`](../mail/messages.md#empty) | Delete everything in a folder, permanently |
| [`proton mail messages expire`](../mail/messages.md#expire) | Make messages delete themselves after a while |
| [`proton mail messages export`](../mail/messages.md#export) | Write messages out as .eml or mbox files |
| [`proton mail messages forward`](../mail/messages.md#forward) | Forward a message |
| [`proton mail messages get`](../mail/messages.md#get) | Show one message, decrypted |
| [`proton mail messages label`](../mail/messages.md#label) | Attach a label to messages |
| [`proton mail messages list`](../mail/messages.md#list) | List messages in a folder |
| [`proton mail messages mark read`](../mail/messages.md#mark-read) | Mark messages as read |
| [`proton mail messages mark unread`](../mail/messages.md#mark-unread) | Mark messages as unread |
| [`proton mail messages move`](../mail/messages.md#move) | Move messages to a folder |
| [`proton mail messages reply`](../mail/messages.md#reply) | Reply to a message |
| [`proton mail messages send`](../mail/messages.md#send) | Compose and send a message |
| [`proton mail messages star`](../mail/messages.md#star) | Star messages |
| [`proton mail messages trash`](../mail/messages.md#trash) | Move messages to the trash |
| [`proton mail messages unlabel`](../mail/messages.md#unlabel) | Detach a label from messages |
| [`proton mail messages unschedule`](../mail/messages.md#unschedule) | Cancel a scheduled send, returning the message to drafts |
| [`proton mail messages unstar`](../mail/messages.md#unstar) | Remove the star from messages |
| [`proton mail messages unsubscribe`](../mail/messages.md#unsubscribe) | Ask a mailing list to stop |
| [`proton mail messages watch`](../mail/messages.md#watch) | Print each message as it arrives |
| [`proton mail settings addresses get`](../mail/settings.md#addresses-get) | Show one address, including its signature |
| [`proton mail settings addresses list`](../mail/settings.md#addresses-list) | List the addresses on the account |
| [`proton mail settings addresses update`](../mail/settings.md#addresses-update) | Set an address's display name or signature |
| [`proton mail settings autoreply disable`](../mail/settings.md#autoreply-disable) | Turn the auto-reply off, keeping its schedule |
| [`proton mail settings autoreply enable`](../mail/settings.md#autoreply-enable) | Turn the auto-reply on, keeping its schedule |
| [`proton mail settings autoreply get`](../mail/settings.md#autoreply-get) | Show the auto-reply and its schedule |
| [`proton mail settings autoreply set`](../mail/settings.md#autoreply-set) | Configure the auto-reply and turn it on |
| [`proton mail settings filters apply`](../mail/settings.md#filters-apply) | Run filters over mail that is already in the mailbox |
| [`proton mail settings filters create`](../mail/settings.md#filters-create) | Create a filter |
| [`proton mail settings filters delete`](../mail/settings.md#filters-delete) | Delete filters |
| [`proton mail settings filters disable`](../mail/settings.md#filters-disable) | Disable filters |
| [`proton mail settings filters enable`](../mail/settings.md#filters-enable) | Enable filters |
| [`proton mail settings filters get`](../mail/settings.md#filters-get) | Show what a filter matches and does |
| [`proton mail settings filters list`](../mail/settings.md#filters-list) | List your filters |
| [`proton mail settings filters reorder`](../mail/settings.md#filters-reorder) | Set the order filters run in |
| [`proton mail settings filters update`](../mail/settings.md#filters-update) | Change what a filter is called, matches, or does |
| [`proton mail settings folders create`](../mail/settings.md#folders-create) | Create a folder |
| [`proton mail settings folders delete`](../mail/settings.md#folders-delete) | Delete folders |
| [`proton mail settings folders list`](../mail/settings.md#folders-list) | List your folders |
| [`proton mail settings folders update`](../mail/settings.md#folders-update) | Rename or recolor a folder |
| [`proton mail settings get`](../mail/settings.md#get) | Show the mail settings now in effect |
| [`proton mail settings labels create`](../mail/settings.md#labels-create) | Create a label |
| [`proton mail settings labels delete`](../mail/settings.md#labels-delete) | Delete labels |
| [`proton mail settings labels list`](../mail/settings.md#labels-list) | List your labels |
| [`proton mail settings labels update`](../mail/settings.md#labels-update) | Rename or recolor a label |
| [`proton mail settings list`](../mail/settings.md#list) | List the mail settings that can be changed |
| [`proton mail settings senders allow`](../mail/settings.md#senders-allow) | Always let someone reach the inbox |
| [`proton mail settings senders block`](../mail/settings.md#senders-block) | Send someone's mail straight to blocked |
| [`proton mail settings senders forget`](../mail/settings.md#senders-forget) | Drop a standing decision, letting the spam filter decide again |
| [`proton mail settings senders list`](../mail/settings.md#senders-list) | List every standing decision about a sender |
| [`proton mail settings senders spam`](../mail/settings.md#senders-spam) | Send someone's mail straight to spam |
| [`proton mail settings set`](../mail/settings.md#set) | Change one mail setting |
| [`proton pass aliases contacts allow`](../pass/aliases.md#contacts-allow) | Let a contact's mail reach you again |
| [`proton pass aliases contacts block`](../pass/aliases.md#contacts-block) | Stop a contact's mail reaching you |
| [`proton pass aliases contacts create`](../pass/aliases.md#contacts-create) | Make an address that writes to somebody as the alias |
| [`proton pass aliases contacts delete`](../pass/aliases.md#contacts-delete) | Remove an address an alias can write to |
| [`proton pass aliases contacts list`](../pass/aliases.md#contacts-list) | List the addresses an alias can write to |
| [`proton pass aliases create`](../pass/aliases.md#create) | Create an alias |
| [`proton pass aliases disable`](../pass/aliases.md#disable) | Stop receiving mail sent to an alias |
| [`proton pass aliases enable`](../pass/aliases.md#enable) | Start receiving mail sent to an alias |
| [`proton pass aliases list`](../pass/aliases.md#list) | List your aliases |
| [`proton pass aliases options`](../pass/aliases.md#options) | List the suffixes and mailboxes an alias can use |
| [`proton pass breaches get`](../pass/breaches.md#get) | Show the breaches one address has appeared in |
| [`proton pass breaches list`](../pass/breaches.md#list) | List the addresses Proton watches, and how many breaches each is in |
| [`proton pass invitations accept`](../pass/invitations.md#accept) | Take what somebody offered you |
| [`proton pass invitations decline`](../pass/invitations.md#decline) | Turn down what somebody offered you |
| [`proton pass invitations list`](../pass/invitations.md#list) | List what other people have offered you |
| [`proton pass items create`](../pass/items.md#create) | Create an item |
| [`proton pass items delete`](../pass/items.md#delete) | Delete items permanently |
| [`proton pass items get`](../pass/items.md#get) | Show one item, decrypted |
| [`proton pass items list`](../pass/items.md#list) | List items across your vaults |
| [`proton pass items move`](../pass/items.md#move) | Put an item in another vault |
| [`proton pass items pin`](../pass/items.md#pin) | Keep items at the top of the list |
| [`proton pass items revisions get`](../pass/items.md#revisions-get) | Show one earlier version, decrypted |
| [`proton pass items revisions list`](../pass/items.md#revisions-list) | Show what an item used to be |
| [`proton pass items share add`](../pass/items.md#share-add) | Offer one item to somebody |
| [`proton pass items share get`](../pass/items.md#share-get) | Show how an item is shared |
| [`proton pass items share remove`](../pass/items.md#share-remove) | Take somebody's access to an item away |
| [`proton pass items share update`](../pass/items.md#share-update) | Change what somebody may do with an item |
| [`proton pass items totp`](../pass/items.md#totp) | Print the current two-factor code for an item |
| [`proton pass items trash`](../pass/items.md#trash) | Move items to the trash |
| [`proton pass items unpin`](../pass/items.md#unpin) | Stop keeping items at the top |
| [`proton pass items update`](../pass/items.md#update) | Change an item's fields |
| [`proton pass links create`](../pass/links.md#create) | Make a link that shows one item |
| [`proton pass links get`](../pass/links.md#get) | Show one link, URL and all |
| [`proton pass links list`](../pass/links.md#list) | List the links you have made |
| [`proton pass links revoke`](../pass/links.md#revoke) | Stop a link working |
| [`proton pass export`](../pass/pass.md#export) | Write the vaults you own out as a Proton Pass archive |
| [`proton pass generate`](../pass/pass.md#generate) | Make a password |
| [`proton pass import`](../pass/pass.md#import) | Read a Proton Pass archive back in |
| [`proton pass settings domains list`](../pass/settings.md#domains-list) | List the domains an alias can be made on |
| [`proton pass settings mailboxes create`](../pass/settings.md#mailboxes-create) | Add an address for aliases to forward to |
| [`proton pass settings mailboxes delete`](../pass/settings.md#mailboxes-delete) | Remove an address aliases forward to |
| [`proton pass settings mailboxes list`](../pass/settings.md#mailboxes-list) | List the addresses your aliases forward to |
| [`proton pass settings mailboxes resend`](../pass/settings.md#mailboxes-resend) | Send the confirmation code again |
| [`proton pass settings mailboxes update`](../pass/settings.md#mailboxes-update) | Change a mailbox |
| [`proton pass settings mailboxes verify`](../pass/settings.md#mailboxes-verify) | Confirm an address with the code Proton emailed it |
| [`proton pass shared list`](../pass/shared.md#list) | List the items other people have shared with you |
| [`proton pass sharing list`](../pass/sharing.md#list) | List the items you have shared |
| [`proton pass trash empty`](../pass/trash.md#empty) | Delete everything in the trash, permanently |
| [`proton pass trash list`](../pass/trash.md#list) | List what is in the trash |
| [`proton pass trash restore`](../pass/trash.md#restore) | Put items back where they came from |
| [`proton pass vaults create`](../pass/vaults.md#create) | Create a vault |
| [`proton pass vaults delete`](../pass/vaults.md#delete) | Delete vaults, and everything in them |
| [`proton pass vaults get`](../pass/vaults.md#get) | Show one vault in full |
| [`proton pass vaults list`](../pass/vaults.md#list) | List your vaults |
| [`proton pass vaults share add`](../pass/vaults.md#share-add) | Offer a vault to somebody |
| [`proton pass vaults share get`](../pass/vaults.md#share-get) | Show who can open a vault |
| [`proton pass vaults share remove`](../pass/vaults.md#share-remove) | Take somebody's access to a vault away |
| [`proton pass vaults share update`](../pass/vaults.md#share-update) | Change what somebody may do with a vault |
| [`proton pass vaults transfer`](../pass/vaults.md#transfer) | Make somebody else the owner of a vault |
| [`proton pass vaults update`](../pass/vaults.md#update) | Rename a vault, or change how it looks |
| [`proton changelog`](../proton.md#changelog) | Print what each release changed |
| [`proton completion`](../proton.md#completion) | Generate a shell completion script |
| [`proton report`](../proton.md#report) | Collect what a bug report needs |
| [`proton uninstall`](../proton.md#uninstall) | Remove a curl/PowerShell-installed proton |
| [`proton update`](../proton.md#update) | Update proton to the latest release |
| [`proton version`](../proton.md#version) | Print the version and build information |

## Flags that work on every command

These are declared on the root, so any command takes them and they mean the same thing everywhere.

| Flag | Description |
| --- | --- |
| `--config string` | Settings file to read (env: PROTON_CONFIG; default: config.yaml in the config directory) |
| `--confirm string` | Which commands stop for a yes: default, deletions, mutations, reads, all (env: PROTON_CONFIRM) |
| `-n, --dry-run` | Preview mutations without applying them |
| `--full-ids` | Show full IDs in interactive output (default: shortened to 8 chars on TTY) |
| `--log-level string` | Logging verbosity: debug, info, warn, error (env: PROTON_LOG_LEVEL) |
| `--no-color` | Disable colored output (env: NO_COLOR) |
| `--no-input` | Never prompt; a missing credential becomes an error (env: PROTON_NO_INPUT) |
| `--no-log` | Write no diagnostic log for this run (env: PROTON_NO_LOG) |
| `-o, --output string` | Output format: text, json, yaml (default "text") |
| `-p, --profile string` | Profile to act as (env: PROTON_PROFILE; default: default) |
| `-q, --quiet` | Suppress non-essential stderr output |
| `--verified string` | A human verification already solved, as the refusal printed it (env: PROTON_VERIFIED) |
| `-y, --yes` | Answer confirmation prompts with yes |
| `--zone string` | IANA time zone to work in (env: TZ; default: your system zone) |

See [Settings, files and environment](../using/settings.md) for what each one changes, and [Output and exit codes](../using/output.md) for what a command answers with.
