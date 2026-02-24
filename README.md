# Stratum Gateway

Stratum is a Go API gateway that accepts OpenAI-style requests and invokes Amazon Bedrock models using the AWS SDK.

The runtime architecture intentionally remains:
- Gateway Bearer API key auth for clients.
- AWS credentials/role auth upstream to Bedrock.
- No Bedrock native `/openai/v1` endpoint mode in this release.

## Features

- Chat Completions (sync + streaming SSE).
- Tool calling (OpenAI tool schema -> Bedrock tool config).
- Embeddings (Cohere batch + Titan single-input path).
- Multimodal image input (`data:` URLs and remote URL fetch with SSRF guards).
- Reasoning/interleaved thinking pass-through fields.
- Prompt caching controls including TTL (`5m`, `1h` on supported model families).
- Cross-region and application inference profile model discovery.
- Model APIs (`GET /v1/models`, `GET /v1/models/{id}`).
- Request guardrails (body-size limit + in-memory rate limiter).
- Structured request logs with request ID.
- Readiness and optional metrics endpoint.

## Quick Start

```bash
cp .env.example .env
go build -o stratum ./cmd/server
./stratum
```

## API Endpoints

| Method | Path | Description |
|---|---|---|
| `GET` | `/health` | Liveness check |
| `GET` | `/ready` | Readiness check |
| `GET` | `/metrics` | Optional text metrics (`ENABLE_METRICS=true`) |
| `GET` | `/v1/models` | List available models/profiles |
| `GET` | `/v1/models/{id}` | Get model/profile by ID |
| `POST` | `/v1/chat/completions` | Chat completion (sync or stream) |
| `POST` | `/v1/embeddings` | Embeddings |

Backward-compatible route prefix is also available under `/api/v1/*`.

All `/v1/*` and `/api/v1/*` endpoints require:
`Authorization: Bearer <API_KEY>`

## Extended `extra_body` Controls

`POST /v1/chat/completions` supports Bedrock-specific extensions under `extra_body`, including:
- `prompt_caching`
- `guardrail_config`
- `request_metadata`
- `additional_model_request_fields`
- `additional_model_response_field_paths`
- `performance_config`
- `service_tier`

Example:

```json
{
  "model": "anthropic.claude-sonnet-4-5-20250929-v1:0",
  "messages": [{"role":"user","content":"hello"}],
  "stream": true,
  "stream_options": {"include_usage": true},
  "extra_body": {
    "prompt_caching": {"enabled": true, "ttl": "1h"},
    "guardrail_config": {
      "guardrail_identifier": "gr-123",
      "guardrail_version": "1"
    },
    "request_metadata": {"tenant": "acme"},
    "additional_model_response_field_paths": ["/stop_sequence"],
    "performance_config": {"latency": "optimized"},
    "service_tier": "priority"
  }
}
```

## Test and Coverage

```bash
go test ./...
go test ./... -coverprofile=cover.out
go tool cover -func=cover.out
go tool cover -html=cover.out
```

## Configuration

See `.env.example` for full configuration. Important controls include:
- `DEFAULT_MODEL`, `DEFAULT_EMBEDDING_MODEL`
- `MAX_REQUEST_BODY_BYTES`
- `RATE_LIMIT_RPM`, `RATE_LIMIT_BURST`
- `ALLOW_PRIVATE_IMAGE_FETCH`, `IMAGE_MAX_BYTES`, `IMAGE_FETCH_TIMEOUT_SECONDS`
- `ENABLE_METRICS`
