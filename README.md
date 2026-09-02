<div align="center">

<img src="assets/logo.svg" width="96" height="96" alt="">

# proton-cli

**Your CLI for Proton Mail, Drive, Calendar, Pass and Contacts.**

_One binary, end-to-end encrypted. A community project, not affiliated with Proton AG._

[![Release](https://img.shields.io/github/v/release/roman-16/proton-cli?sort=semver&style=flat-square&color=6D4AFF)](https://github.com/roman-16/proton-cli/releases/latest) [![Downloads](https://img.shields.io/github/downloads/roman-16/proton-cli/total?style=flat-square&color=6D4AFF)](https://github.com/roman-16/proton-cli/releases) [![Platforms](https://img.shields.io/badge/platforms-Linux%20%7C%20macOS%20%7C%20Windows-6D4AFF?style=flat-square)](docs/install.md) [![License](https://img.shields.io/github/license/roman-16/proton-cli?style=flat-square&color=6D4AFF)](LICENSE)

**[proton-cli.lerchster.dev](https://proton-cli.lerchster.dev)** - the documentation, searchable.

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="assets/demo-dark.svg">
  <img src="assets/demo-light.svg" alt="Listing unread mail, uploading a file to Drive, previewing which files a cleanup would remove, and listing Pass items">
</picture>

</div>
<br />

Read your mail, move files in and out of Drive, manage calendars, passwords and contacts, all without opening a browser. proton logs in the way the Proton apps do and does the encryption on your machine, so your keys stay yours.

- **Real end-to-end encryption.** SRP login and the full PGP key hierarchy, handled locally with Proton's own [go-srp](https://github.com/ProtonMail/go-srp) and [gopenpgp](https://github.com/ProtonMail/gopenpgp). No bridge, no proxy, no browser in the middle.
- **Five apps, one binary.** Mail, Drive, Calendar, Pass and Contacts, in a single static executable on Linux, macOS and Windows.
- **Built for pipes and cron.** JSON and YAML with one envelope shape for every list, streaming stdin and stdout, exit codes that mean something, and `--dry-run` on everything that changes state.

## Install

```bash
curl -fsSL https://raw.githubusercontent.com/roman-16/proton-cli/main/scripts/install.sh | sh
```

```powershell
irm https://raw.githubusercontent.com/roman-16/proton-cli/main/scripts/install.ps1 | iex
```

Also on Homebrew, winget, APT, AUR, Nix, npm, and as plain signed binaries. See [Install](docs/install.md).

The command is `proton`, and `proton-cli` works too.

## Sign in

```console
$ proton account login
Email:            you@proton.me
Password:
Two-factor code:  123456

✓ Signed in as you@proton.me.
```

That is the whole setup. Signing in saves the session **and** unlocks your keys, so your password is needed once on this machine and not again.

## Try it

```bash
proton mail messages list --unread
proton mail messages get "Invoice #2291"
proton drive items upload ./report.pdf /Documents
proton calendar events list
proton pass items totp github.com
```

Every command reads the same way, `proton <app> <collection> <verb>`, and anywhere one wants an ID, a subject, name, path or address works too.

→ [First commands](docs/first-commands.md) · [How a command is built](docs/commands.md)

## Documentation

Everything is at **[proton-cli.lerchster.dev](https://proton-cli.lerchster.dev)** and in [`docs/`](docs/README.md) beside the code.

| If you want to | Read |
| --- | --- |
| Get going | [Install](docs/install.md) · [First commands](docs/first-commands.md) |
| Do something in one app | [Mail](docs/mail/README.md) · [Drive](docs/drive/README.md) · [Calendar](docs/calendar/README.md) · [Pass](docs/pass/README.md) · [Contacts](docs/contacts/README.md) |
| Script it | [Scripting](docs/using/scripting.md) · [Output and exit codes](docs/using/output.md) |
| Look up a command | [All commands](docs/about/commands.md) |
| Know whether this is the right tool | [FAQ](docs/about/faq.md) - including how it compares to Proton's own CLIs and to Bridge |
| Understand the encryption | [Security and encryption](docs/about/security.md) |
| Fix a failing command | [Troubleshooting](docs/help/troubleshooting.md) |

[`CHANGELOG.md`](CHANGELOG.md) records what each version changed, and `proton changelog` prints it. Already installed? `proton update`.

## Contributing

Bug reports, ideas, and pull requests are all welcome. [`CONTRIBUTING.md`](CONTRIBUTING.md) covers the setup, and [`SECURITY.md`](SECURITY.md) has the private channel for security issues.

## Disclaimer

proton-cli is an independent, community-built project. It is not endorsed by, affiliated with, or supported by Proton AG. Proton is a trademark of Proton AG. Use it at your own risk, and mind Proton's terms of service.

## License

[MIT](LICENSE)
