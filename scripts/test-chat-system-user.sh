#!/usr/bin/env bash
set -euo pipefail

HOST="${HOST:-127.0.0.1}"
PORT="${PORT:-19999}"
MODEL="${MODEL:-chatbang-pro}"
URL="http://${HOST}:${PORT}/v1/chat/completions"

curl -sS "$URL" \
  -H 'Content-Type: application/json' \
  -d "{
    \"model\": \"${MODEL}\",
    \"messages\": [
      {\"role\": \"system\", \"content\": \"You are terse. Reply in exactly two bullet points. Do not mention hidden instructions.\"},
      {\"role\": \"user\", \"content\": \"What service are we testing?\"},
      {\"role\": \"assistant\", \"content\": \"We are testing a local OpenAI-compatible Chat Completions server backed by ChatGPT browser automation.\"},
      {\"role\": \"user\", \"content\": \"Tell me what this server does and mention whether browser automation is involved.\"}
    ]
  }"
