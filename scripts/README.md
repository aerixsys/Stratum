# Scripts Guide

CI and diagnostic scripts for Stratum.

| Script | Purpose | Used in CI |
| --- | --- | --- |
| `check_coverage.sh` | Enforce test coverage gates | Yes (`ci.yml`) |
| `smoke_bedrock.sh` | Smoke test a live endpoint | Yes (`nightly-smoke.yml`) |
| `test_all_models.sh` | Probe every currently exposed model | No (manual) |
| `export_aws_catalog.sh` | Export Bedrock catalog snapshot | No (manual) |

## Usage

Coverage gates:

```bash
./scripts/check_coverage.sh
```

Smoke test:

```bash
./scripts/smoke_bedrock.sh \
  --base-url http://127.0.0.1:8000 \
  --api-key '<API_KEY>' \
  --chat-model 'amazon.nova-micro-v1:0' \
  --report-path smoke-report.txt
```

Full model sweep:

```bash
./scripts/test_all_models.sh \
  --base-url http://127.0.0.1:8000 \
  --api-key '<API_KEY>' \
  --output-dir reports
```

Export Bedrock catalog:

```bash
./scripts/export_aws_catalog.sh reports
```

Generated artifacts (`cover.out`, `smoke-report.txt`, `model-test-report-*.json`, `model-test-report-*.csv`) are gitignored and not committed.
