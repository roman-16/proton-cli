# Security Policy

> **Disclaimer:** This is an unofficial, community-built tool and is not endorsed by or affiliated with Proton AG. Use at your own risk.

How credentials are stored, what is encrypted with what, and how to reduce your risk as a user are documented in [Security and encryption](docs/about/security.md). This file is the policy for reporting a problem with any of it.

## Reporting a vulnerability

**Please do not report security issues via public GitHub issues, pull requests, or discussions.**

Instead, use one of these private channels:

1. **Preferred** - Open a [private security advisory](https://github.com/roman-16/proton-cli/security/advisories/new) on GitHub. This is encrypted, scoped to the maintainer, and lets us collaborate on a fix in a private fork.
2. **Alternative** - Email <roman@lerchster.dev> with the details. Use `[proton security]` in the subject line.

Please include:

- A clear description of the vulnerability and its impact.
- Steps to reproduce, ideally with a minimal example.
- The affected version(s) of `proton` (output of `proton --version`).
- Your operating system and Go toolchain version, if relevant.
- Whether you've disclosed this to anyone else, and any disclosure timeline you have in mind.

You can expect:

- An initial acknowledgement within **7 days**.
- A triage assessment (severity, scope, planned fix) within **14 days**.
- A patched release for confirmed critical issues within **30 days** where feasible.

If you don't get a response within 7 days, please follow up - your message may have been missed.

## Supported versions

Only the **latest released version** of `proton` receives security fixes. There is no long-term-support branch.

Always upgrade to the latest release before reporting an issue you suspect is fixed in newer code.

## Scope

### In scope

- Vulnerabilities in `proton`'s own code, including:
  - Authentication flow (SRP login, two-factor handling).
  - Local credential and key storage.
  - PGP key handling and message decryption logic.
  - Command-line argument parsing and shell injection risks.
  - File I/O (path traversal, symlink races, insecure temp files).
- Misuse of upstream cryptographic libraries ([`gopenpgp`](https://github.com/ProtonMail/gopenpgp), [`go-srp`](https://github.com/ProtonMail/go-srp)) inside `proton`.
- Build / supply-chain issues in the release pipeline that could lead to a malicious binary being distributed under the `proton` name.

### Out of scope

- Vulnerabilities in **Proton's services or infrastructure**. Report those to Proton's [Bug Bounty Programme](https://proton.me/security/bug-bounty) directly (or email <security@proton.me>).
- Vulnerabilities in **upstream Go dependencies**. Report those to the respective projects. We will track upstream advisories and update dependencies in a timely manner.
- Issues that only manifest in **modified, forked, or unofficially redistributed builds** of `proton`.
- Account-safety issues caused by **violating Proton's Terms of Service** (e.g., using `proton` against accounts where automated access is prohibited). Such use is at your own risk.
- Theoretical issues without a demonstrated impact.

## Disclosure policy

We follow [coordinated disclosure](https://en.wikipedia.org/wiki/Coordinated_vulnerability_disclosure):

1. The reporter and maintainer agree on a disclosure timeline (default: 90 days from initial report, or sooner if a fix is shipped).
2. A patched release is published.
3. A security advisory is published on GitHub with credit to the reporter (unless they prefer to remain anonymous).
4. If a CVE is warranted, one is requested.
