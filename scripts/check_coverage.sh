#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

OVERALL_MIN=75
SERVICE_MIN=85
TRANSLATE_MIN=85

cleanup() {
  rm -f service.cover
}
trap cleanup EXIT

echo "[coverage] running full test coverage profile"
go test ./... -coverprofile=cover.out >/tmp/stratum.cover.log

overall_pct="$(go tool cover -func=cover.out | awk '/^total:/ {gsub("%","",$3); print $3}')"
translate_pct="$(go tool cover -func=cover.out | awk '/TranslateRequest/ {gsub("%","",$3); print $3; exit}')"

if [[ -z "${translate_pct}" ]]; then
  echo "[coverage] ERROR: failed to read TranslateRequest coverage"
  exit 1
fi

echo "[coverage] running service package coverage profile"
go test ./internal/service -coverprofile=service.cover >/tmp/stratum.service.cover.log
service_pct="$(go tool cover -func=service.cover | awk '/^total:/ {gsub("%","",$3); print $3}')"

printf '[coverage] overall=%.1f%% (min=%s%%)\n' "$overall_pct" "$OVERALL_MIN"
printf '[coverage] service=%.1f%% (min=%s%%)\n' "$service_pct" "$SERVICE_MIN"
printf '[coverage] TranslateRequest=%.1f%% (min=%s%%)\n' "$translate_pct" "$TRANSLATE_MIN"

check_ge() {
  local actual="$1"
  local min="$2"
  awk -v a="$actual" -v m="$min" 'BEGIN {exit (a+0 >= m+0) ? 0 : 1}'
}

check_ge "$overall_pct" "$OVERALL_MIN" || {
  echo "[coverage] FAIL: overall coverage below threshold"
  exit 1
}
check_ge "$service_pct" "$SERVICE_MIN" || {
  echo "[coverage] FAIL: service coverage below threshold"
  exit 1
}
check_ge "$translate_pct" "$TRANSLATE_MIN" || {
  echo "[coverage] FAIL: TranslateRequest coverage below threshold"
  exit 1
}

echo "[coverage] PASS"
