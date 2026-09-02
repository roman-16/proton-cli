---
gradient: var(--app-mail)
href: /mail/
order: 1
summary: Threads, attachments, filters, snoozing, block and allow lists, and auto-reply.
title: Mail
---

```bash frame="none"
proton mail messages list --unread
proton mail messages get "Invoice #2291"
proton mail messages send --to alice@proton.me --subject Report --body "See attached."
proton mail messages reply "Invoice #2291" --body "Thanks, paid today."
```
