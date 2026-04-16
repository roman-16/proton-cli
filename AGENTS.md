# Agent Guidelines

## Project Context

This is an open-source CLI tool used by other people. All changes should consider:
- **Backwards compatibility** — consider impact on existing users when changing command syntax or flags, but don't let it block improvements
- **Cross-platform** — must work on Linux, macOS, and Windows (amd64 + arm64)
- **User-facing quality** — README, help text, and error messages should be clear and helpful
- **Distribution** — binaries are published as GitHub Releases via GoReleaser; users install by downloading a binary or via `go install`

## Testing

Tests are **integration tests** that run against the live Proton API. They require `PROTON_USER` and `PROTON_PASSWORD` environment variables.

- **Never run the full test suite** (`just test` / `go test ./...`) — only the user triggers that manually
- **Single tests are allowed** (`just test-one TestName`) when verifying a specific change

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
