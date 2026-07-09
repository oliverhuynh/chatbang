#!/usr/bin/env bash
set -euo pipefail

HOST="${HOST:-127.0.0.1}"
PORT="${PORT:-19999}"
MODEL="${MODEL:-chatbang-pro}"
URL="http://${HOST}:${PORT}/v1/chat/completions"

CONTENT=$(cat ./basic.json)
curl -sS "$URL" \
  -H 'Content-Type: application/json' \
  -d "{
    \"model\": \"${MODEL}\",
    \"messages\": [
      $CONTENT
    ]
  }"
