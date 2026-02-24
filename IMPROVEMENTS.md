# Stratum V2 Release Tracker

## Locked Constraints (Non-Negotiable)

1. No Bedrock native OpenAI-compatible runtime mode (`/openai/v1`, Mantle/Responses API path) in Stratum runtime architecture.
2. No Bedrock API-key authentication model for Stratum runtime.
3. External client auth remains Stratum Bearer `API_KEY`.
4. Upstream Bedrock auth remains AWS credentials/role.
5. Deployment target is self-hosted VPS.
6. Deployment artifacts are Compose-only for VPS in this release (no CloudFormation/Terraform/Lambda/ECS/Fargate deployment path).

## Completed in this cycle

1. Clean handler/service split:
   - Handlers remain transport-only.
   - Service layer handles chat/embeddings/models orchestration.
2. Bedrock integration boundaries:
   - Chat/embedding/model interfaces for dependency injection and tests.
3. Protocol expansion:
   - `extra_body` supports prompt caching, guardrails, metadata, additional fields/paths, performance config, service tier.
4. Model fallback:
   - `DEFAULT_MODEL` and `DEFAULT_EMBEDDING_MODEL` fallback support.
5. Streaming hardening and contract coverage:
   - JSON-safe SSE payload marshaling.
   - Deterministic stream ordering tests for role/text/tool/reasoning/finish/usage/`[DONE]`.
   - Startup and mid-stream error-path JSON safety tests.
6. Multimodal safety:
   - URL scheme checks.
   - DNS/IP private/local blocking by default.
   - response size/content-type/timeout controls.
7. Request guardrails:
   - Constant-time API-key compare.
   - Request body size limiter middleware.
   - In-memory rate limiter by key/IP.
   - Request ID middleware.
8. API surface:
   - `GET /v1/models/{id}` and `/api/v1/models/{id}`.
   - `/ready` endpoint.
   - Optional `/metrics`.
9. Integration testing:
   - Added protected-route integration tests for auth, CORS preflight, body-limit, rate-limit interplay, request-id propagation, and error envelope consistency.
10. CI quality gates:
    - Added PR workflow for unit tests, race detector, and coverage gate script.
    - Added nightly/manual smoke workflow with report artifact.
11. Smoke harness:
    - Added repeatable smoke script and matrix documentation.
12. VPS ops docs:
    - Added Compose-only deployment guide.
    - Added secret rotation runbook for existing env model.

## Quality Gate Status

1. Verification commands:
   - `go test ./...`
   - `go test ./... -race`
   - `go test ./... -coverprofile=cover.out`
   - `go tool cover -func=cover.out`
   - `./scripts/check_coverage.sh`
2. Coverage thresholds enforced in CI:
   - Overall >= 75%
   - Service package >= 85%
   - Translator core (`TranslateRequest`) >= 85%

## Remaining for strict “one-big-release” hardening

- None.
