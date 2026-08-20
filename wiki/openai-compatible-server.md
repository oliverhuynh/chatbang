# OpenAI-Compatible Server

## What it does
`--server` runs a local HTTP server that exposes:

- `POST /v1/chat/completions`
- `GET /v1/models`
- `GET /models`

The server is backed by the existing ChatGPT browser automation flow rather than the OpenAI API.

## Default bind

- Host: `127.0.0.1`
- Port: `19999`
- Full default URL: `http://127.0.0.1:19999/v1/chat/completions`

Model discovery URLs:

- `http://127.0.0.1:19999/v1/models`
- `http://127.0.0.1:19999/models`

## CLI flags

- `--server` — enable server mode
- `--listen` — listen host/IP only
- `--port` — listen port only
- `--gpt` / `--custom-gpt` — target a custom GPT instead of default ChatGPT
- `--keep-browser` — optional; reuse one Chrome instance across server restarts/invocations
- browser-mode flags such as `--headless` and `--no-headless` still apply in server mode

Examples:

```bash
chatbang-pro --server
chatbang-pro --server --no-headless
chatbang-pro --server --port 20000
chatbang-pro --server --listen 0.0.0.0 --port 20000
chatbang-pro --server --listen [::1]
```

`--listen` accepts host/IP only. Bracketed IPv6 input such as `[::1]` is normalized correctly before binding.

## Request compatibility

Minimal supported request body:

- `model`
- `messages`
- optional `stream`

The implementation accepts:

- string `content`
- text parts: `text`, `input_text`, `output_text`
- Chat Completions image parts: `image_url`
- Chat Completions file parts: `file`
- Responses-style image parts: `input_image`
- Responses-style file parts: `input_file`

This lets requests translated from the Responses API by a proxy such as 9router retain file/image inputs when they reach ChatBang's Chat Completions endpoint.

### Attachment sources

ChatBang can turn these attachment sources into a local file for browser upload:

- `image_url.url` / `input_image.image_url` with a public `http://` or `https://` URL
- image data URLs such as `data:image/png;base64,...`
- `file.file_data` / `input_file.file_data` as base64 or a data URL
- `file.file_url` / `input_file.file_url` with a public `http://` or `https://` URL
- `file://` URLs and absolute local paths as a ChatBang extension when the file already exists on the server machine

Opaque OpenAI `file_id` values cannot be resolved by ChatBang because it does not have the caller's OpenAI Files API credentials. Send `file_data`, `file_url`, or an image URL/data URL instead.

Remote attachment downloads reject loopback/private/link-local destinations before connecting. Temporary materialized files are deleted after the request finishes.

### Browser upload strategy

Attachment automation intentionally bypasses the native file picker:

1. Prefer ChatGPT's hidden general-purpose `input#upload-files`.
2. Set local paths with Chrome DevTools Protocol file-input upload (`DOM.setFileInputFiles` via `chromedp.SetUploadFiles`).
3. If the general input has not been mounted, open the composer `+` menu only to make React render attachment controls, then retry the hidden input. Do not automate the operating-system file picker.
4. Use `#upload-photos` only as a fallback when every attachment is an image.
5. Wait until ChatGPT's send button exists and is enabled before inserting/submitting the prompt. This is the readiness signal that the background attachment upload completed.
6. If CDP populated the input but ChatGPT did not mount a send button, dispatch one `input`/`change` event pair as a React compatibility fallback, then wait again.

The file upload and prompt submit are retried together after a dead-browser recovery so a reconnect cannot silently drop an attachment.

`stream=true` is supported as compatibility streaming. ChatBang still waits for the browser-backed request to finish, then emits the completed answer as OpenAI-style SSE `chat.completion.chunk` events followed by `data: [DONE]`. This is buffered/synthetic streaming, not token-by-token streaming from ChatGPT.

Unsupported or intentionally omitted:

- tool calls / function calls
- resolving OpenAI `file_id` values
- auth
- strict token accounting
- multi-choice completions

Prompt translation:

- The first non-empty `system` message starts the `# System` section.
- Later `system` messages are appended under `# System` without additional headings; clients can use those messages for retrieved context or memory.
- Earlier `user` and `assistant` turns are preserved under `# Conversation`.
- The final non-empty `user` message is placed under `# User`.
- Attachment positions are represented in the flattened text with markers such as `[Attached file: report.pdf]`; the actual file bytes are uploaded separately through the ChatGPT composer.
- This is still a prompt shim over one ChatGPT browser submission, not native OpenAI role execution.

## Response shape

For `stream=false` or when `stream` is omitted, the server returns a minimal OpenAI-style chat completion response:

- `object=chat.completion`
- one assistant choice
- `finish_reason=stop`

For `stream=true`, the server returns `Content-Type: text/event-stream` and emits:

1. an assistant-role chunk
2. one content chunk containing the completed browser response
3. a final chunk with `finish_reason=stop`
4. `data: [DONE]`

All chunks for one request share the same completion ID, model, and creation timestamp.

`usage` is present but zero-filled on non-streaming responses.

For model discovery, the server returns a static OpenAI-style list response with a few common ChatGPT model IDs (`gpt-4o`, `gpt-4o-mini`, `gpt-4.1`, `gpt-4.1-mini`). The list is compatibility-only and is not strict capability gating.

## Runtime behavior

- Each request uses a fresh temporary chat to avoid cross-request context bleed.
- Requests are serialized through one `session.Session`; concurrent clients do not run in parallel through the browser.
- If the browser/tab is dead before prompt submission, session prep routes through `recover()` and reopens the tab instead of wedging the server.
- Server mode uses the same browser launch path as standalone mode, so `--headless` / `--no-headless` affect the server-owned browser process on new launches.
- The ChatGPT composer currently automates a contenteditable `DIV` / editor surface rather than a plain `TEXTAREA`; multiline insertion debugging must be verified in browser-side editor state, not only in server-side flattened prompt logs.

## Key files

- `internal/server/server.go` — HTTP handler and response handling
- `internal/server/attachments.go` — OpenAI content-part parsing, safe materialization, URL download protection
- `internal/session/session.go` — normal per-request browser ask flow and recovery
- `internal/session/upload.go` — direct CDP attachment upload, readiness checks, attachment-aware retry
- `internal/cli/cli.go` — `--server`, `--listen`, `--port`
- `internal/app/app.go` — server startup and bind address wiring

## Manual test helpers

Repo-local curl helpers:

- `scripts/test-chat-basic.sh`
- `scripts/test-chat-system-user.sh`
- `scripts/test-chat-stream.sh`
- `scripts/test-chat-file.sh`
- `scripts/test-models.sh`
