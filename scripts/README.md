# Scripts

## OpenAPI Generator

Auto-generates `openapi.yaml` from [ProtonMail/WebClients](https://github.com/ProtonMail/WebClients) TypeScript source files using [ts-morph](https://github.com/dsherret/ts-morph) for full AST parsing with type resolution.

### Usage

```bash
cd scripts
npm install
npm run generate-openapi
```

This outputs `openapi.yaml` in the project root. First run clones the WebClients repo to `/tmp/proton-webclient-openapi` (~30 seconds). Subsequent runs pull updates (~1 second).

### What It Extracts

Per endpoint:

| Source | OpenAPI |
|---|---|
| Function name | `operationId`, `summary` |
| `url` property | `paths` (constants resolved from source) |
| `method` property | HTTP method |
| `data` parameter type | `requestBody` schema (types resolved through imports) |
| `params` object | `parameters` (query) |
| Template literal `${vars}` | `parameters` (path) |
| `input: 'form'\|'binary'` | Request `content-type` (`multipart/form-data`, `application/octet-stream`) |
| `output: 'stream'\|'arrayBuffer'\|'text'` | Response `content-type` |
| JSDoc comments | `description` |
| `@deprecated` tag | `deprecated: true` |
| `/** Public **/` comments, `credentials: 'omit'` | `security: []` |
| `timeout` property | `x-timeout` |
| `keepalive` property | `x-keepalive` |
| `silence` array | `x-expected-errors` |
| All exported enums | Comment block in components section |

Global:

| Source | OpenAPI |
|---|---|
| All `export enum` declarations | Enum reference comments with all values |
| All `export const = 'string'` | Used to resolve URL template constants |
| TypeScript interfaces | Resolved for request body property types, optionality, and comments |

### How It Works

1. **Clone** — shallow clones (or pulls) `ProtonMail/WebClients` into `/tmp/`
2. **Project setup** — creates a ts-morph `Project` with `tsconfig.base.json` for path resolution
3. **Registry** — scans all source files for string/number constants and enum declarations
4. **Parse** — walks all exported declarations in `api/**/*.ts`, extracts endpoint metadata from the AST
5. **Type resolution** — follows TypeScript imports to resolve `data: SomeType` to actual property lists (including `extends`, `Partial<>`, `Omit<>`, etc.)
6. **Emit** — generates OpenAPI 3.1 YAML to stdout

### File Structure

```
openapi-generator/
├── index.ts              — entry point
├── clone.ts              — git clone/pull
├── parse.ts              — ts-morph project setup, file discovery
├── registry.ts           — constant and enum collection
├── extract-endpoint.ts   — endpoint extraction from AST nodes
├── extract-params.ts     — body/query param type resolution
├── emit-yaml.ts          — OpenAPI YAML output
└── types.ts              — shared TypeScript interfaces
```
