---
gradient: var(--app-pass)
href: /apps/pass/
order: 4
summary: Notes, cards, SSH keys, identities, two-factor codes, item history, and backups.
title: Pass
---

```bash frame="none"
proton pass items list --vault Work
proton pass items get github.com
proton pass items create --name GitHub --username roman --url github.com --generate-password
proton pass items totp github.com
```
