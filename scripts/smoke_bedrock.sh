#!/usr/bin/env bash
set -euo pipefail

BASE_URL=""
API_KEY=""
CHAT_MODEL=""
MULTIMODAL_IMAGE_URL=""
REPORT_PATH="smoke-report.txt"

usage() {
  cat <<USAGE
Usage: $0 --base-url URL --api-key KEY --chat-model MODEL [options]

Required:
  --base-url URL              Gateway base URL (e.g. http://127.0.0.1:8000)
  --api-key KEY               Stratum API key
  --chat-model MODEL          Chat model ID

Optional:
  --image-url URL             Public image URL for multimodal check
  --report-path PATH          Output report path (default: smoke-report.txt)
USAGE
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --base-url) BASE_URL="$2"; shift 2 ;;
    --api-key) API_KEY="$2"; shift 2 ;;
    --chat-model) CHAT_MODEL="$2"; shift 2 ;;
    --image-url) MULTIMODAL_IMAGE_URL="$2"; shift 2 ;;
    --report-path) REPORT_PATH="$2"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "Unknown arg: $1"; usage; exit 1 ;;
  esac
done

if [[ -z "$BASE_URL" || -z "$API_KEY" || -z "$CHAT_MODEL" ]]; then
  usage
  exit 1
fi

AUTH_HEADER="Authorization: Bearer ${API_KEY}"
PASS_COUNT=0
FAIL_COUNT=0
SKIP_COUNT=0
REPORT_LINES=()
REQ_STATUS=""
REQ_BODY=""

record_pass() {
  PASS_COUNT=$((PASS_COUNT + 1))
  REPORT_LINES+=("PASS | $1 | $2")
}
record_fail() {
  FAIL_COUNT=$((FAIL_COUNT + 1))
  REPORT_LINES+=("FAIL | $1 | $2")
}
record_skip() {
  SKIP_COUNT=$((SKIP_COUNT + 1))
  REPORT_LINES+=("SKIP | $1 | $2")
}

curl_json() {
  local method="$1"
  local path="$2"
  local payload="${3:-}"
  local out status body
  if [[ -n "$payload" ]]; then
    out="$(curl -sS -X "$method" "${BASE_URL}${path}" -H "$AUTH_HEADER" -H "Content-Type: application/json" -d "$payload" -w $'\n%{http_code}')"
  else
    out="$(curl -sS -X "$method" "${BASE_URL}${path}" -H "$AUTH_HEADER" -w $'\n%{http_code}')"
  fi
  status="${out##*$'\n'}"
  body="${out%$'\n'*}"
  printf '%s\n%s' "$status" "$body"
}

run_request() {
  local result
  result="$(curl_json "$@")"
  REQ_STATUS="${result%%$'\n'*}"
  REQ_BODY="${result#*$'\n'}"
}

check_health() {
  run_request GET /health
  if [[ "$REQ_STATUS" == "200" ]]; then
    record_pass "health" "200"
  else
    record_fail "health" "status=${REQ_STATUS} body=${REQ_BODY}"
  fi

  run_request GET /ready
  if [[ "$REQ_STATUS" == "200" ]]; then
    record_pass "ready" "200"
  else
    record_fail "ready" "status=${REQ_STATUS} body=${REQ_BODY}"
  fi
}

check_models() {
  run_request GET /v1/models
  if [[ "$REQ_STATUS" == "200" ]] && [[ "$REQ_BODY" == *'"object":"list"'* ]]; then
    record_pass "models_list" "200 list"
  else
    record_fail "models_list" "status=${REQ_STATUS} body=${REQ_BODY}"
  fi
}

check_chat_sync() {
  local payload
  payload="$(cat <<JSON
{"model":"${CHAT_MODEL}","messages":[{"role":"user","content":"ping"}]}
JSON
)"
  run_request POST /v1/chat/completions "$payload"
  if [[ "$REQ_STATUS" == "200" ]] && [[ "$REQ_BODY" == *'"object":"chat.completion"'* ]]; then
    record_pass "chat_sync" "200 chat.completion"
  else
    record_fail "chat_sync" "status=${REQ_STATUS} body=${REQ_BODY}"
  fi
}

check_chat_stream() {
  local payload out
  payload="$(cat <<JSON
{"model":"${CHAT_MODEL}","stream":true,"messages":[{"role":"user","content":"stream ping"}]}
JSON
)"
  out="$(curl -sS -N -X POST "${BASE_URL}/v1/chat/completions" -H "$AUTH_HEADER" -H "Content-Type: application/json" -d "$payload" || true)"
  if [[ "$out" == *"data: [DONE]"* ]]; then
    record_pass "chat_stream" "contains [DONE]"
  else
    record_fail "chat_stream" "missing [DONE], output=${out}"
  fi
}

check_tool_call() {
  local payload
  payload='{"model":"'"${CHAT_MODEL}"'","messages":[{"role":"user","content":"what is weather in NYC? use tool"}],"tools":[{"type":"function","function":{"name":"get_weather","description":"Get weather","parameters":{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]}}}]}'
  run_request POST /v1/chat/completions "$payload"
  if [[ "$REQ_STATUS" == "200" ]] && [[ "$REQ_BODY" == *'"choices"'* ]]; then
    record_pass "tool_call_path" "200"
  else
    record_fail "tool_call_path" "status=${REQ_STATUS} body=${REQ_BODY}"
  fi
}

check_prompt_caching() {
  local payload
  payload='{"model":"'"${CHAT_MODEL}"'","messages":[{"role":"user","content":"cache me"}],"extra_body":{"prompt_caching":{"enabled":true,"ttl":"5m"}}}'
  run_request POST /v1/chat/completions "$payload"
  if [[ "$REQ_STATUS" == "200" ]]; then
    record_pass "prompt_cache_5m" "200"
  else
    record_fail "prompt_cache_5m" "status=${REQ_STATUS} body=${REQ_BODY}"
  fi

  payload='{"model":"'"${CHAT_MODEL}"'","messages":[{"role":"user","content":"cache me 1h"}],"extra_body":{"prompt_caching":{"enabled":true,"ttl":"1h"}}}'
  run_request POST /v1/chat/completions "$payload"
  if [[ "$REQ_STATUS" == "200" || "$REQ_STATUS" == "400" ]]; then
    # 1h support is model-dependent; both valid success and explicit validation error are informative.
    record_pass "prompt_cache_1h" "status=${REQ_STATUS}"
  else
    record_fail "prompt_cache_1h" "status=${REQ_STATUS} body=${REQ_BODY}"
  fi
}

check_multimodal() {
  local image_url payload
  if [[ -n "$MULTIMODAL_IMAGE_URL" ]]; then
    image_url="$MULTIMODAL_IMAGE_URL"
  else
    image_url='data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAusB9Wv4R4UAAAAASUVORK5CYII='
  fi

  payload='{"model":"'"${CHAT_MODEL}"'","messages":[{"role":"user","content":[{"type":"text","text":"describe image"},{"type":"image_url","image_url":{"url":"'"${image_url}"'"}}]}]}'
  run_request POST /v1/chat/completions "$payload"
  if [[ "$REQ_STATUS" == "200" || "$REQ_STATUS" == "400" ]]; then
    # Some models may not support image input; explicit 400 still confirms gateway path.
    record_pass "multimodal" "status=${REQ_STATUS}"
  else
    record_fail "multimodal" "status=${REQ_STATUS} body=${REQ_BODY}"
  fi
}

check_error_mapping() {
  local payload
  payload='{"model":"missing-model","messages":[{"role":"user","content":"trigger unsupported"}]}'
  run_request POST /v1/chat/completions "$payload"
  if [[ "$REQ_STATUS" == "400" ]] && [[ "$REQ_BODY" == *'"type":"invalid_request_error"'* ]]; then
    record_pass "error_mapping" "invalid_request_error"
  else
    record_fail "error_mapping" "status=${REQ_STATUS} body=${REQ_BODY}"
  fi
}

check_health
check_models
check_chat_sync
check_chat_stream
check_tool_call
check_prompt_caching
check_multimodal
check_error_mapping

{
  echo "Stratum Smoke Report"
  echo "Generated: $(date -u +"%Y-%m-%dT%H:%M:%SZ")"
  echo "Base URL: ${BASE_URL}"
  echo
  printf '%s\n' "${REPORT_LINES[@]}"
  echo
  echo "Summary: pass=${PASS_COUNT} fail=${FAIL_COUNT} skip=${SKIP_COUNT}"
} >"${REPORT_PATH}"

cat "${REPORT_PATH}"

if [[ "$FAIL_COUNT" -gt 0 ]]; then
  exit 1
fi
