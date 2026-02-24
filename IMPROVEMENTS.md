# Stratum V2 Release Tracker

## Completed in this cycle

1. Clean handler/service split:
   - Handlers are transport-only.
   - Service layer added for chat/embeddings/models.
2. Bedrock integration boundaries:
   - Chat/embedding/model interfaces for dependency injection and tests.
3. Protocol expansion:
   - `extra_body` supports prompt caching, guardrails, metadata, additional fields/paths, performance config, service tier.
4. Model fallback:
   - `DEFAULT_MODEL` and `DEFAULT_EMBEDDING_MODEL` fallback support.
5. Streaming hardening:
   - JSON-safe SSE payload marshaling.
   - `[DONE]` handling and usage chunk gating via `stream_options.include_usage`.
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
9. Tests:
   - Added service, middleware, schema, server, handler, and Bedrock helper tests.

## Quality Gate Status

1. Coverage:
   - Overall: `79.5%` (target >= 75%).
   - Service package: `91.7%`.
   - Translator core (`TranslateRequest`): `88.7%`.
2. Verification commands:
   - `go test ./...`
   - `go test ./... -coverprofile=cover.out`
   - `go tool cover -func=cover.out`
   - `go vet ./...`

## Remaining for strict “one-big-release” hardening

1. Add dedicated stream contract tests for:
   - tool-call chunk ordering
   - reasoning delta ordering
   - malformed upstream error injection paths
2. Add full HTTP integration tests for auth + CORS + rate-limit interplay on protected routes.
3. Add production deployment artifacts:
   - Terraform/CloudFormation baseline.
   - IAM least-privilege policy docs.
   - Secrets Manager/rotation runbook.
