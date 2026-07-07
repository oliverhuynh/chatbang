#!/usr/bin/env bash
set -euo pipefail

. .env
MODEL="${MODEL:-cheapest-models}"
# chatbang/gpt-4o"
HOST9ROUTER="http://9router:20128/v1"
URL="${HOST9ROUTER}/chat/completions"

curl -sS "$URL" \
  -H "Authorization: Bearer ${OPENAI_API_KEY}" \
  -H 'Content-Type: application/json' \
  -d "{
    \"model\": \"${MODEL}\",
    \"messages\": [
      {\"role\": \"user\", \"content\": \"Say hello in one short sentence.\"}
    ]
  }"
