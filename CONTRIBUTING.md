# Contributing to Stratum

Thanks for contributing.

## Getting Started

Prerequisites:
- Go `1.25.7` or newer
- AWS credentials with Bedrock access
- Docker and Docker Compose (optional, for container testing)

Setup:

```bash
cp .env.example .env   # fill in API_KEY and AWS settings
go build -o stratum ./cmd/server
./stratum
```

## Before Opening a PR

```bash
go test ./...
go test ./... -race
./scripts/check_coverage.sh
```

## PR Checklist

- Changes stay within the `/v1` API contract
- New or changed behavior has tests
- Docs are updated when user-visible behavior changes
- No secrets or build artifacts are committed
- CI passes (tests, race, coverage)

## Ground Rules

- Keep runtime behavior minimal and explicit
- Avoid extra configuration knobs unless required
- Prefer OpenAI-compatible request and response shapes where applicable
- Keep `README.md`, `docs/`, and `scripts/` aligned with the implementation

## Reporting Issues

- Bugs and feature requests: use GitHub Issues on this repository
- Security vulnerabilities: follow [SECURITY.md](SECURITY.md) and do not use a public issue

By contributing, you agree your changes will be licensed under [MIT](LICENSE).
