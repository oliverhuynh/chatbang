#!/usr/bin/env bash
set -euo pipefail

HOST="${HOST:-127.0.0.1}"
PORT="${PORT:-19999}"
URL="http://${HOST}:${PORT}/v1/models"

response="$(curl -sS "$URL")"

case "$response" in
  *'"object":"list"'*'"id":"gpt-4o"'*'"id":"gpt-4o-mini"'*)
    printf '%s\n' "$response"
    ;;
  *)
    printf 'unexpected /v1/models response:\n%s\n' "$response" >&2
    exit 1
    ;;
esac
