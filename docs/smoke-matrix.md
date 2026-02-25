# Smoke Matrix

Use `scripts/smoke_bedrock.sh` for repeatable compatibility checks.

## Required scenarios

- Chat sync
- Chat streaming SSE + `[DONE]`
- Tool-call request path
- Prompt caching (`5m` and optional `1h` when model supports)
- Error mapping (`unsupported model` -> `invalid_request_error`)

## Optional scenarios (run when model/profile inputs are provided)

- Reasoning path
- Cross-region profile invocation
- Application profile invocation
- Multimodal image input
- Guardrail-enabled requests

## Command

```bash
./scripts/smoke_bedrock.sh \
  --base-url http://127.0.0.1:8000 \
  --api-key '<API_KEY>' \
  --chat-model 'amazon.nova-micro-v1:0' \
  --report-path smoke-report.txt
```

The script exits non-zero on failure and writes a report with pass/fail/skip summary.

## Full Model Sweep

To probe every model returned by `/v1/models` and capture per-model errors:

```bash
./scripts/test_all_models.sh \
  --base-url http://127.0.0.1:8000 \
  --api-key '<API_KEY>' \
  --output-dir .
```

This generates:

- `model-test-report-<timestamp>.json`
- `model-test-report-<timestamp>.csv`

Each row includes model id, endpoint used, HTTP status, parsed `error.type`, error message, and latency.
