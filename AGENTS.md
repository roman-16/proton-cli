# Agent Guidelines

## Reference Source

The Proton WebClients TypeScript source is available at `/tmp/proton-webclient-openapi/` (cloned from https://github.com/ProtonMail/WebClients). Use it as the primary reference for:

- API endpoint signatures, request/response shapes (`packages/shared/lib/api/`)
- Encryption flows and key handling (`packages/shared/lib/keys/`, `packages/crypto/`)
- How the web client calls endpoints (parameter names, types, ordering)
- Constants and enums (`packages/shared/lib/constants.ts`, etc.)

If the clone is missing or stale, run:
```bash
cd /tmp && git clone --depth 1 --branch main https://github.com/ProtonMail/WebClients.git proton-webclient-openapi
```
