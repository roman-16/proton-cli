# Raw API

`proton api` sends an authenticated request to any Proton endpoint, using the session you already have. It is the escape hatch for anything the commands don't cover.

Every flag is in the [`proton api` reference](../commands/api.md).

```bash
proton api GET /drive/volumes
proton api GET /mail/v4/messages --query Page=0 --query PageSize=10
proton api POST /calendar/v1 --body '{"Name":"Work","Color":"#7272a7","Display":1,"AddressID":"..."}'
proton api DELETE /mail/v4/labels/LABEL_ID
proton api GET /calendar/v1 --output json | jq -r '.Calendars[].ID'
```

Responses come back as the API returned them, in Proton's `PascalCase`, and **encrypted fields stay encrypted** - this command does no key handling. For decrypted content, use the commands for that app.

`GET`, `HEAD` and `OPTIONS` run as reads. Every other method is treated as an unknown-impact mutation: it asks for confirmation, refuses unattended use without `--yes`, and supports `--dry-run` without sending a request. This is deliberately conservative because an arbitrary endpoint may communicate externally, alter security settings, or make an irreversible change.

With `--output json`, a successful response must contain exactly one JSON document. Malformed, concatenated, or trailing non-JSON response bytes are rejected before anything reaches stdout.

## Finding an endpoint

[`openapi.yaml`](https://github.com/roman-16/proton-cli/blob/main/openapi.yaml) documents roughly 740 endpoints - paths, methods, request and response schemas, and query parameters. It is generated from Proton's own web client source, and a weekly job keeps it in step with upstream.
