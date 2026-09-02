# What proton-cli can't do

proton-cli aims for parity with what the Proton web clients let you do. This page is where it falls short of that, and why. A refusal that comes from Proton rather than from proton-cli belongs to [Troubleshooting](troubleshooting.md) instead.

## Known gaps

**An update can't be verified offline.** `proton update` downloads the binary and the `checksums.txt` it is checked against from the same release, and neither is signed. Anything able to serve you one can serve you the other. Closing this needs a public key built into the binary, and there isn't one yet - until then, an install from a package manager is verified by the package manager.

**A recurring event needs a zone that can be named.** proton reads one from `TZ`, `/etc/localtime` or `/etc/timezone`, then falls back to your Proton calendar settings. Where none of those answers, a new event is stored as a plain UTC instant - and a recurring one then drifts by an hour when the clocks change. Pass `--zone Europe/Vienna` to be sure.

**A Drive filter walks the tree.** `--pattern` and the size and age filters are matched on your machine, folder by folder, because Drive's index is built by the web client rather than by the server. Over a large tree that is a request per folder, so narrow it with `--scope`.

## Not built yet

Each has an equivalent in a web client, and each is unbuilt for a stated reason rather than an oversight.

| What's missing | Why |
| --- | --- |
| Drive computers and shared bookmarks | Neither exists until you use a desktop client or open somebody's link, so neither can be tested |
| Mail forwarding to another Proton address | Needs an OpenPGP forwarding primitive the Go libraries don't implement |
| Mail forwarding to a non-Proton address | Proton emails the address a link its owner must follow, so a command can start the flow but never finish it |
| Turning the Pass [extra password](apps/pass.md#an-extra-password) on or off | It is a credential, and proton changes none of them - the account password, two-factor and the recovery phrase are absent for the same reason. Setting one from a file cannot ask you to type it twice the way Proton's own clients do, and a typo would leave every item in Pass unreachable; removing one signs every device out of every app. Answering an extra password, so the commands work, is built |

## Out of scope

proton-cli mirrors the Proton web clients for Mail, Drive, Calendar, Pass and Contacts. Other Proton products - VPN, Wallet, Docs, Meet, Lumo, Authenticator - are not covered, and neither are endpoints that exist in the API but have no equivalent action in a web client.

For anything the commands don't reach, [`proton api`](apps/api.md) sends raw authenticated requests to any endpoint.
