# What it can't do

proton-cli aims for parity with what the Proton web clients let you do. This page is where it falls short.

A refusal that comes from Proton rather than from proton-cli belongs to [Troubleshooting](troubleshooting.md) instead.

## Known gaps

**An update cannot be verified offline.** `proton update` checks the download against `checksums.txt` from the same release, and neither is signed. An install from a package manager is verified by the package manager.

**Post-quantum keys are read, never generated.** An account that opted in during Proton's rollout signs in, and mail and shares reach others who opted in. Turning the setting on is done in Proton's own settings, and adding an address to such an account is refused.

**A recurring event needs a zone that can be named.** proton reads one from `TZ`, `/etc/localtime` or `/etc/timezone`, then from your Proton calendar settings. Where none of those answers, a recurring event drifts by an hour when the clocks change. Pass `--zone Europe/Vienna` to be sure.

**A Drive filter over a large tree is slow.** Narrow it with `--scope`.

## Not built yet

Each of these has an equivalent in a web client.

| What is missing | What to do instead |
| --- | --- |
| Setting up mail forwarding | Add the forwarding at [account.proton.me](https://account.proton.me). Accepting one sent to you, pausing it, resuming it and taking it down are built |
| Mail forwarding to a non-Proton address | Proton emails the address a link its owner must follow; finish it there |
| Turning on the `@pm.me` short domain | Switch it on at [account.proton.me](https://account.proton.me). Adding, disabling and deleting other addresses is built |
| Turning the Pass [extra password](../pass/README.md#an-extra-password) on or off | Use the Pass app. *Answering* one is built |

proton changes no credentials: the account password, two-factor, the recovery phrase and the Pass extra password are all set at [account.proton.me](https://account.proton.me) or in the Pass app.

## Out of scope

proton-cli mirrors the Proton web clients for Mail, Drive, Calendar, Pass and Contacts.

Other Proton products are not covered: VPN, Wallet, Docs, Meet, Lumo and Authenticator. Proton VPN has [its own CLI](https://protonvpn.com/support/linux-vpn-tool).

Endpoints that exist in the API but have no equivalent action in a web client are also out of scope.

For anything the commands do not reach, [`proton api`](../api/README.md) sends raw authenticated requests to any endpoint.
