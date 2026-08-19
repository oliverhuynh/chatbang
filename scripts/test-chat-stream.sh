#!/usr/bin/env bash
set -euo pipefail

HOST="${HOST:-127.0.0.1}"
PORT="${PORT:-19999}"
MODEL="${MODEL:-chatbang-pro}"
URL="http://${HOST}:${PORT}/v1/chat/completions"

curl -i -N -sS "$URL" \
  -H 'Content-Type: application/json' \
  -d "{
    \"model\": \"${MODEL}\",
    \"stream\": true,
    \"messages\": [
      {\"role\": \"user\", \"content\": \"Reply with exactly: stream works\"}
    ]
  }"
