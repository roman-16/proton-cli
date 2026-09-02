# proton api

Send a raw authenticated request to the Proton API.

The response is passed through as the API returned it, so this is where to reach anything the commands do not cover.

Every command under `proton api`, with the arguments and flags it takes. For these commands in use, see [the api guide](README.md).

```
proton api METHOD ENDPOINT
```

```bash
proton api GET /core/v4/users
proton api GET /mail/v4/messages --query 'PageSize=5'
proton api POST /mail/v4/labels --body '{"Name":"Work","Color":"#8080FF","Type":1}'
```

| Flag | Description |
| --- | --- |
| `--body string` | JSON request body |
| `--query stringArray` | Query parameter as key=value (repeatable) |

---

Every command also takes the [flags that work everywhere](../about/commands.md#flags-that-work-on-every-command).
