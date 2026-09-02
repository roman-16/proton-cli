---
gradient: var(--app-drive)
href: /drive/
order: 2
summary: Revisions, sharing with people, what others shared with you, and photo albums.
title: Drive
---

```bash frame="none"
proton drive items list /Documents
proton drive items upload --recursive ./project /Backup
proton drive items download /Documents/report.pdf --dest-dir .
proton drive items share link /Documents/report.pdf --expires 7d
```
