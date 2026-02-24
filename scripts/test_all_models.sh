#!/usr/bin/env bash
set -euo pipefail

BASE_URL="http://127.0.0.1:8000"
API_KEY="${API_KEY:-}"
ENV_FILE=".env"
MODE="auto" # auto | chat | embeddings
LIMIT=0
TIMEOUT_SEC=90
DELAY_SEC="0.10"
MAX_TOKENS=64
PROMPT_TEXT="health ping"
OUTPUT_DIR="."
STRICT=0

usage() {
  cat <<USAGE
Usage: $0 [options]

Options:
  --base-url URL         Gateway base URL (default: http://127.0.0.1:8000)
  --api-key KEY          API key (defaults to API_KEY env or .env file)
  --env-file PATH        Env file to load API key from (default: .env)
  --mode MODE            auto | chat | embeddings (default: auto)
  --limit N              Test only first N models (default: 0 = all)
  --timeout-sec N        Request timeout seconds (default: 90)
  --delay-sec SEC        Delay between requests in seconds (default: 0.10)
  --max-tokens N         max_tokens for chat probe (default: 64)
  --prompt TEXT          Chat prompt text (default: "health ping")
  --output-dir DIR       Report output directory (default: current dir)
  --strict               Exit non-zero when any model returns non-200
  -h, --help             Show help

Outputs:
  model-test-report-<timestamp>.json
  model-test-report-<timestamp>.csv
USAGE
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --base-url) BASE_URL="$2"; shift 2 ;;
    --api-key) API_KEY="$2"; shift 2 ;;
    --env-file) ENV_FILE="$2"; shift 2 ;;
    --mode) MODE="$2"; shift 2 ;;
    --limit) LIMIT="$2"; shift 2 ;;
    --timeout-sec) TIMEOUT_SEC="$2"; shift 2 ;;
    --delay-sec) DELAY_SEC="$2"; shift 2 ;;
    --max-tokens) MAX_TOKENS="$2"; shift 2 ;;
    --prompt) PROMPT_TEXT="$2"; shift 2 ;;
    --output-dir) OUTPUT_DIR="$2"; shift 2 ;;
    --strict) STRICT=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "Unknown option: $1"; usage; exit 1 ;;
  esac
done

if ! command -v jq >/dev/null 2>&1; then
  echo "ERROR: jq is required. Install it first (sudo apt install -y jq)." >&2
  exit 1
fi

if [[ "$MODE" != "auto" && "$MODE" != "chat" && "$MODE" != "embeddings" ]]; then
  echo "ERROR: --mode must be one of: auto, chat, embeddings" >&2
  exit 1
fi

if [[ -z "$API_KEY" && -f "$ENV_FILE" ]]; then
  API_KEY="$(awk -F= '/^API_KEY=/{print $2}' "$ENV_FILE" | tail -n1 | tr -d '\r"')"
  if [[ -z "$API_KEY" ]]; then
    API_KEY="$(awk -F= '/^OPENAI_API_KEY=/{print $2}' "$ENV_FILE" | tail -n1 | tr -d '\r"')"
  fi
fi

if [[ -z "$API_KEY" ]]; then
  echo "ERROR: API key not found. Pass --api-key or set API_KEY in $ENV_FILE" >&2
  exit 1
fi

mkdir -p "$OUTPUT_DIR"

timestamp="$(date -u +"%Y%m%dT%H%M%SZ")"
report_json="${OUTPUT_DIR}/model-test-report-${timestamp}.json"
report_csv="${OUTPUT_DIR}/model-test-report-${timestamp}.csv"
entries_jsonl="$(mktemp)"
trap 'rm -f "$entries_jsonl"' EXIT

echo "[model-test] fetching model inventory from ${BASE_URL}/v1/models"
models_resp="$(curl -sS -H "Authorization: Bearer ${API_KEY}" "${BASE_URL}/v1/models" -w $'\n%{http_code}')"
models_status="${models_resp##*$'\n'}"
models_body="${models_resp%$'\n'*}"
if [[ "$models_status" != "200" ]]; then
  echo "ERROR: failed to fetch models (status=$models_status body=$models_body)" >&2
  exit 1
fi

mapfile -t models < <(printf '%s' "$models_body" | jq -r '.data[].id')
if [[ "${#models[@]}" -eq 0 ]]; then
  echo "ERROR: no models returned by /v1/models" >&2
  exit 1
fi
if [[ "$LIMIT" =~ ^[0-9]+$ ]] && (( LIMIT > 0 )) && (( LIMIT < ${#models[@]} )); then
  models=("${models[@]:0:LIMIT}")
fi

echo "[model-test] total models to test: ${#models[@]}"

ok_count=0
err_count=0
idx=0

for model_id in "${models[@]}"; do
  idx=$((idx + 1))

  op_mode="$MODE"
  if [[ "$MODE" == "auto" ]]; then
    if [[ "$model_id" == *"embed"* ]]; then
      op_mode="embeddings"
    else
      op_mode="chat"
    fi
  fi

  endpoint="/v1/chat/completions"
  if [[ "$op_mode" == "embeddings" ]]; then
    endpoint="/v1/embeddings"
  fi

  if [[ "$op_mode" == "chat" ]]; then
    payload="$(jq -nc \
      --arg model "$model_id" \
      --arg prompt "$PROMPT_TEXT" \
      --argjson max_tokens "$MAX_TOKENS" \
      '{model:$model,messages:[{role:"user",content:$prompt}],max_tokens:$max_tokens}')"
  else
    payload="$(jq -nc --arg model "$model_id" '{model:$model,input:"health ping"}')"
  fi

  start_ms="$(date +%s%3N)"
  raw_out=""
  curl_err=""
  set +e
  raw_out="$(curl -sS --max-time "$TIMEOUT_SEC" -H "Authorization: Bearer ${API_KEY}" -H "Content-Type: application/json" \
    -X POST "${BASE_URL}${endpoint}" -d "$payload" -w $'\n%{http_code}' 2>&1)"
  rc=$?
  set -e
  end_ms="$(date +%s%3N)"
  latency_ms=$((end_ms - start_ms))

  http_status=""
  body=""
  result=""
  error_type=""
  error_message=""
  finish_reason=""

  if (( rc != 0 )); then
    http_status="CURL_ERROR"
    result="error"
    error_type="transport_error"
    error_message="$raw_out"
  else
    http_status="${raw_out##*$'\n'}"
    body="${raw_out%$'\n'*}"

    if [[ "$http_status" == "200" ]]; then
      result="ok"
      if [[ "$op_mode" == "chat" ]]; then
        finish_reason="$(printf '%s' "$body" | jq -r '.choices[0].finish_reason // ""' 2>/dev/null || true)"
      else
        finish_reason="n/a"
      fi
    else
      result="error"
      error_type="$(printf '%s' "$body" | jq -r '.error.type // ""' 2>/dev/null || true)"
      error_message="$(printf '%s' "$body" | jq -r '.error.message // ""' 2>/dev/null || true)"
      if [[ -z "$error_type" ]]; then
        error_type="http_${http_status}"
      fi
      if [[ -z "$error_message" ]]; then
        error_message="$body"
      fi
    fi
  fi

  error_message="$(printf '%s' "$error_message" | tr '\n' ' ' | tr '\r' ' ' | tr -s ' ' | cut -c1-400)"

  if [[ "$result" == "ok" ]]; then
    ok_count=$((ok_count + 1))
  else
    err_count=$((err_count + 1))
  fi

  jq -nc \
    --arg model_id "$model_id" \
    --arg endpoint "$endpoint" \
    --arg operation_mode "$op_mode" \
    --arg http_status "$http_status" \
    --arg result "$result" \
    --arg error_type "$error_type" \
    --arg error_message "$error_message" \
    --arg finish_reason "$finish_reason" \
    --argjson latency_ms "$latency_ms" \
    '{
      model_id: $model_id,
      endpoint: $endpoint,
      operation_mode: $operation_mode,
      http_status: $http_status,
      result: $result,
      error_type: $error_type,
      error_message: $error_message,
      finish_reason: $finish_reason,
      latency_ms: $latency_ms
    }' >> "$entries_jsonl"

  printf '[%d/%d] %-6s %-45s status=%s\n' "$idx" "${#models[@]}" "$result" "$model_id" "$http_status"

  sleep "$DELAY_SEC"
done

jq -s \
  --arg generated_at "$(date -u +"%Y-%m-%dT%H:%M:%SZ")" \
  --arg base_url "$BASE_URL" \
  --arg mode "$MODE" \
  '{
    generated_at: $generated_at,
    base_url: $base_url,
    mode: $mode,
    total: length,
    success: (map(select(.result == "ok")) | length),
    errors: (map(select(.result != "ok")) | length),
    by_http_status: (reduce .[] as $i ({}; .[$i.http_status] += 1)),
    by_error_type: (reduce (map(select(.error_type != ""))[]) as $i ({}; .[$i.error_type] += 1)),
    by_endpoint: (reduce .[] as $i ({}; .[$i.endpoint] += 1)),
    entries: .
  }' "$entries_jsonl" > "$report_json"

jq -r '
  (["model_id","endpoint","operation_mode","http_status","result","error_type","error_message","finish_reason","latency_ms"] | @csv),
  (.entries[] | [
    .model_id,
    .endpoint,
    .operation_mode,
    .http_status,
    .result,
    .error_type,
    .error_message,
    .finish_reason,
    (.latency_ms|tostring)
  ] | @csv)
' "$report_json" > "$report_csv"

echo
echo "[model-test] completed"
echo "  total:   ${#models[@]}"
echo "  success: ${ok_count}"
echo "  errors:  ${err_count}"
echo "  json:    ${report_json}"
echo "  csv:     ${report_csv}"
echo
echo "[model-test] error type summary"
jq -r '.by_error_type | to_entries[]? | "  \(.key): \(.value)"' "$report_json"

if (( STRICT == 1 )) && (( err_count > 0 )); then
  exit 1
fi
