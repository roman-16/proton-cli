# Raw API

`proton api` sends an authenticated request to any Proton endpoint, using the session you already have. It is the escape hatch for anything the commands do not cover.

For every flag, see the [reference](api.md).

```bash
proton api GET /drive/volumes
proton api GET /mail/v4/messages --query Page=0 --query PageSize=10
proton api POST /calendar/v1 --body '{"Name":"Work","Color":"#7272a7","Display":1,"AddressID":"..."}'
proton api DELETE /mail/v4/labels/LABEL_ID
proton api GET /calendar/v1 --output json | jq -r '.Calendars[].ID'
```

Responses come back as the API returned them, in Proton's `PascalCase`.

**Encrypted fields stay encrypted.** This command does no key handling. For decrypted content, use the commands for that app.

Stdout carries the response and nothing else. A body that is not JSON - a proxy's error page standing in for the API's answer - goes to stderr instead, and the command exits `5`. So a `jq` downstream is never handed something it cannot parse.

## Find an endpoint

The [API reference](https://proton-cli.lerchster.dev/api-reference/) documents Proton's endpoints: paths, methods, request and response schemas, and query parameters.

It is generated from Proton's own web client source, and a weekly job keeps it in step with upstream.

The spec behind it is served as [`openapi.yaml`](https://proton-cli.lerchster.dev/openapi.yaml), for a code generator or an HTTP client to read.
