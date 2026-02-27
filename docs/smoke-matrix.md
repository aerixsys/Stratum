# Smoke Testing Guide

Use `scripts/smoke_bedrock.sh` to quickly verify a running Stratum endpoint.

## Required Smoke Scenarios

| Scenario | Checks |
| --- | --- |
| Health and readiness | `/health` and `/ready` return OK |
| Model list | `GET /v1/models` returns models |
| Chat sync | A response with content is returned |
| Chat stream | SSE ends with `data: [DONE]` |
| Tool calling | Tool-call request path succeeds |
| Prompt caching | `5m` cache hint is accepted |
| Bad model | Returns `invalid_request_error` |

Optional:
- multimodal image input (pass `--image-url`)

## Run

```bash
./scripts/smoke_bedrock.sh \
  --base-url http://127.0.0.1:8000 \
  --api-key '<API_KEY>' \
  --chat-model 'amazon.nova-micro-v1:0' \
  --report-path smoke-report.txt
```

The script exits non-zero if any required check fails and writes a pass/fail report to `smoke-report.txt`.

## Full Model Sweep

Probe every model currently returned by `/v1/models` for availability diagnostics.

```bash
./scripts/test_all_models.sh \
  --base-url http://127.0.0.1:8000 \
  --api-key '<API_KEY>' \
  --output-dir reports
```

Generated outputs:
- `model-test-report-<timestamp>.json`
- `model-test-report-<timestamp>.csv`
