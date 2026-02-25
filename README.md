# Stratum Gateway

Stratum is a Go gateway that accepts OpenAI-style chat requests and invokes Amazon Bedrock with AWS credentials.

## Runtime Model

- Client auth: `Authorization: Bearer <API_KEY>`
- Upstream auth: AWS credentials/role
- Self-hosted VPS target
- No Bedrock native `/openai/v1` runtime mode
- No Bedrock API-key runtime auth mode

## What It Supports

- Chat Completions (`sync` + streaming `SSE`)
- Models API (`list` + `get by id`)
- Tool calling
- Multimodal chat input (`image_url`), including `data:` and hardened remote fetch
- Cross-region and app inference profiles
- Reasoning/interleaved thinking pass-through
- Prompt caching controls

## What It Does Not Expose

- Embeddings API
- Image generation/output API

## Endpoints

Public:
- `GET /health`
- `GET /ready`
- `GET /metrics` (only when `ENABLE_METRICS=true`)

Protected (`/v1/*` and alias `/api/v1/*`):
- `GET /v1/models`
- `GET /v1/models/{id}`
- `POST /v1/chat/completions`

## Quick Start (Binary)

```bash
cp .env.example .env
go build -o stratum ./cmd/server
./stratum
```

## Model Policy

Policy file: `config/model-policy.yaml`

- Excludes are applied at API exposure and request validation time
- Blocked models are hidden from `GET /v1/models`
- Blocked `GET /v1/models/{id}` returns `404`
- Blocked chat model request returns `400 invalid_request_error`
- Restart service after policy edits

## Usage Notes (Reasoning Models)

- Bedrock usage reports `prompt_tokens`, `completion_tokens`, and `total_tokens` (plus cache fields).
- Bedrock does not provide a native standalone `reasoning_tokens` field in usage.
- For reasoning-capable responses, Stratum may include:
  - `usage.completion_tokens_details.reasoning_tokens`
  - `usage.completion_tokens_details.reasoning_tokens_estimated=true`
  - `usage.completion_tokens_details.reasoning_tokens_method="char_ratio_v1"`
- Treat this reasoning split as analytics-oriented estimate, not authoritative upstream billing data.

## Test Commands

```bash
go test ./...
go test ./... -race
go test ./... -coverprofile=cover.out
go tool cover -func=cover.out
```

Model sweep report:

```bash
./scripts/test_all_models.sh --base-url http://127.0.0.1:8000 --output-dir reports
```

## Operations Docs

- `docs/vps-deploy.md`
- `docs/secret-rotation.md`
- `docs/smoke-matrix.md`
