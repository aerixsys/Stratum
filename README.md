# Stratum Gateway

Self-hosted OpenAI-compatible API gateway for Amazon Bedrock, written in Go.

[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![Docker Image](https://img.shields.io/badge/ghcr.io-aerixsys%2Fstratum-blue?logo=github)](https://ghcr.io/aerixsys/stratum)

Stratum exposes an OpenAI-style `/v1` API and forwards requests to Amazon Bedrock. OpenAI-compatible clients typically only need a base URL and API key change.

## Features

| Feature | Details |
| --- | --- |
| Chat Completions | Sync and streaming SSE |
| Tool Calling | Function-call passthrough |
| Multimodal Input | `data:` image URLs and public `http/https` image URLs |
| Reasoning Filter | Hide reasoning blocks per request |
| Prompt Caching | Cache hints via `extra_body.prompt_caching` |
| Model Policy | YAML-based exclude list |
| Bearer Auth | API key guard on `/v1` routes |
| Metrics | Optional Prometheus `/metrics` endpoint |

## Quick Start

### Docker (GHCR — easiest)

```bash
docker pull ghcr.io/aerixsys/stratum:latest
docker run -d \
  --name stratum-gateway \
  --restart unless-stopped \
  -p 8000:8000 \
  -e API_KEY=your-api-key \
  -e AWS_REGION=us-east-1 \
  -e AWS_ACCESS_KEY_ID=your-key \
  -e AWS_SECRET_ACCESS_KEY=your-secret \
  ghcr.io/aerixsys/stratum:latest
```

### Binary

```bash
cp .env.example .env   # fill in API_KEY and AWS settings
go build -o stratum ./cmd/server
./stratum
```

### Docker Compose

```bash
cp .env.example .env
docker compose build --pull
docker compose up -d
```

Verify:

```bash
curl http://localhost:8000/health
curl http://localhost:8000/v1/models -H "Authorization: Bearer <API_KEY>"
```

## Configuration

| Variable | Required | Default | Description |
| --- | --- | --- | --- |
| `API_KEY` | yes | none | Bearer token for `/v1` routes |
| `AWS_REGION` | no | `us-east-1` | Bedrock region |
| `AWS_ACCESS_KEY_ID` | no | none | Static AWS key (or use role-based credentials) |
| `AWS_SECRET_ACCESS_KEY` | no | none | Static AWS secret (or use role-based credentials) |
| `PORT` | no | `8000` | Listen port |
| `LOG_LEVEL` | no | `info` | `debug`, `info`, `warn`, `error` (`debug` enables access + stream debug logs) |
| `ENABLE_METRICS` | no | `false` | Expose `/metrics` |
| `MODEL_POLICY_PATH` | no | auto-resolve | Explicit model policy file path override |
| `MAX_REQUEST_BODY_BYTES` | no | `10485760` | Body size limit |

## API

Public (no auth):
- `GET /health`
- `GET /ready`
- `GET /metrics` (only when `ENABLE_METRICS=true`)

Protected (`Authorization: Bearer <API_KEY>`):

```text
GET  /v1/models
GET  /v1/models/{id}
POST /v1/chat/completions
```

Behavior notes:
- Chat payload validation is strict. Unsupported message roles or malformed content shapes return `400` with `invalid_request_error`.
- Remote `image_url` fetches only allow `http/https` targets that resolve and connect to public IP space (private/local/reserved targets are blocked).
- CORS returns `Access-Control-Allow-Origin: *` for all origins.
- Logging is terminal-friendly: TTY output uses compact colorized lines; non-TTY output is plain timestamped text.
- Access logs are debug-only (`LOG_LEVEL=debug`).
- At `LOG_LEVEL=info`, chat inference routes emit one `inference_done` line per request (sync and stream), without access-log noise.

Example:

```bash
curl http://localhost:8000/v1/chat/completions \
  -H "Authorization: Bearer <API_KEY>" \
  -H "Content-Type: application/json" \
  -d '{"model":"amazon.nova-micro-v1:0","messages":[{"role":"user","content":"Hello!"}]}'
```

## Model Policy

This repository ships with a curated default exclude list in `config/model-policy.yaml`.
Edit the file and restart Stratum to widen or narrow which models are exposed.
Resolution order at startup:
1. `MODEL_POLICY_PATH` when set
2. executable-relative `config/model-policy.yaml`
3. cwd-local `config/model-policy.yaml`

```yaml
version: 1
exclude:
  - "anthropic.claude-3-haiku-20240307-v1:0"
```

## Testing

```bash
go test ./...
go test ./... -race
./scripts/check_coverage.sh
```

Smoke test against a live endpoint:

```bash
./scripts/smoke_bedrock.sh \
  --base-url http://localhost:8000 \
  --api-key '<API_KEY>' \
  --chat-model 'amazon.nova-micro-v1:0'
```

Generated test artifacts are local-only and gitignored:
- `cover.out`
- `smoke-report.txt`
- `model-test-report-<timestamp>.json`
- `model-test-report-<timestamp>.csv`
- reports folder usage: see [reports/README.md](reports/README.md)

## Docs

- [Deployment Runbook](docs/vps-deploy.md)
- [Smoke Testing Guide](docs/smoke-matrix.md)
- [Secret Rotation](docs/secret-rotation.md)
- [Docs Index](docs/README.md)
- [Scripts Guide](scripts/README.md)
- [Contributing](CONTRIBUTING.md)
- [Security Policy](SECURITY.md)

## License

MIT. See [LICENSE](LICENSE).
