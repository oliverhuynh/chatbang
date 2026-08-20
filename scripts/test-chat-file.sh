#!/usr/bin/env bash
set -euo pipefail

HOST="${HOST:-127.0.0.1}"
PORT="${PORT:-19999}"
MODEL="${MODEL:-gpt-4o}"
URL="http://${HOST}:${PORT}/v1/chat/completions"
FILE="${1:?usage: $0 <file> [prompt]}"
PROMPT="${2:-Summarize the attached file.}"

python3 - "$FILE" "$MODEL" "$PROMPT" <<'PY' | curl -i -N -sS "$URL" \
  -H 'Content-Type: application/json' \
  --data-binary @-
import base64
import json
import os
import sys

path, model, prompt = sys.argv[1:]
with open(path, "rb") as fh:
    data = base64.b64encode(fh.read()).decode("ascii")

json.dump({
    "model": model,
    "stream": True,
    "messages": [{
        "role": "user",
        "content": [
            {"type": "text", "text": prompt},
            {
                "type": "file",
                "file": {
                    "filename": os.path.basename(path),
                    "file_data": data,
                },
            },
        ],
    }],
}, sys.stdout)
PY
