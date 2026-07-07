# OpenAI-Compatible Server

## What it does
`--server` runs a local HTTP server that exposes:

- `POST /v1/chat/completions`

The server is backed by the existing ChatGPT browser automation flow rather than the OpenAI API.

## Default bind

- Host: `127.0.0.1`
- Port: `19999`
- Full default URL: `http://127.0.0.1:19999/v1/chat/completions`

## CLI flags

- `--server` — enable server mode
- `--listen` — listen host/IP only
- `--port` — listen port only
- `--gpt` / `--custom-gpt` — target a custom GPT instead of default ChatGPT
- `--keep-browser` — optional; reuse one Chrome instance across server restarts/invocations

Examples:

```bash
chatbang-pro --server
chatbang-pro --server --port 20000
chatbang-pro --server --listen 0.0.0.0 --port 20000
chatbang-pro --server --listen [::1]
```

`--listen` accepts host/IP only. Bracketed IPv6 input such as `[::1]` is normalized correctly before binding.

## Request compatibility

Minimal supported request body:

- `model`
- `messages`
- optional `stream=false`

The implementation accepts common text-only message forms:

- string `content`
- array-of-parts `content` with `type=text`

Unsupported or intentionally omitted:

- `stream=true`
- tool calls / function calls
- auth
- strict token accounting
- multi-choice completions

## Response shape

The server returns a minimal OpenAI-style chat completion response:

- `object=chat.completion`
- one assistant choice
- `finish_reason=stop`

`usage` is present but zero-filled.

## Runtime behavior

- Each request uses a fresh temporary chat to avoid cross-request context bleed.
- Requests are serialized through one `session.Session`; concurrent clients do not run in parallel through the browser.
- If the browser/tab is dead before prompt submission, session prep now routes through `recover()` and reopens the tab instead of wedging the server.

## Key files

- `internal/server/server.go` — HTTP handler, request flattening, error responses
- `internal/session/session.go` — per-request browser ask flow and recovery
- `internal/cli/cli.go` — `--server`, `--listen`, `--port`
- `internal/app/app.go` — server startup and bind address wiring

## Manual test helpers

Repo-local curl helpers:

- `scripts/test-chat-basic.sh`
- `scripts/test-chat-system-user.sh`
- `scripts/test-chat-stream-error.sh`
