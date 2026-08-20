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

ChatBang can turn these attachment sources into a temporary local file for browser upload:

- `image_url.url` / `input_image.image_url` with a public `http://` or `https://` URL
- image data URLs such as `data:image/png;base64,...`
- `file.file_data` / `input_file.file_data` as base64 or a data URL
- `file.file_url` / `input_file.file_url` with a public `http://` or `https://` URL

Opaque OpenAI `file_id` values cannot be resolved by ChatBang because it does not have the caller's OpenAI Files API credentials. Send `file_data`, `file_url`, or an image URL/data URL instead.

Arbitrary server-local paths (`/path/to/file`, `file://...`) are intentionally not accepted by the HTTP API. Because server mode can listen beyond localhost and currently has no auth layer, accepting paths from request JSON would allow a network caller to make ChatBang read local files. For a local file, encode it as `file_data`; `scripts/test-chat-file.sh` does this automatically.

Remote attachment downloads reject loopback/private/link-local and selected reserved network ranges before connecting. Redirects use the same protected dialer. Temporary materialized files are deleted after the request finishes.

### Browser upload strategy

Attachment automation intentionally bypasses the native file picker:

1. Prefer ChatGPT's hidden general-purpose `input#upload-files`.
2. Set local temporary paths with Chrome DevTools Protocol file-input upload (`DOM.setFileInputFiles` via `chromedp.SetUploadFiles`) and verify that the input received every selected file.
3. Wait for ChatGPT to render attachment UI such as the file chip/remove affordance. File selection alone is not treated as success.
4. If the attachment UI is not observed after a short grace period, dispatch one `input`/`change` pair as a React compatibility fallback and verify again.
5. If the general file input has not been mounted, open the composer `+` menu only to make React render attachment controls, then retry the hidden input. Do not automate the operating-system file picker.
6. Use `#upload-photos` only as a fallback when every attachment is an image.
7. Insert the prompt after the attachment is visibly accepted. ChatGPT may keep the send button hidden/disabled while the text editor is empty, so waiting for send readiness before inserting the prompt can deadlock.
8. Wait until the attachment is still present **and** the send button is visible/enabled. Attachment disappearance is a hard failure rather than falling through to a prompt-only message.
9. Submit the attachment-bearing text turn with Enter; attachment-only turns use the verified enabled send button.

Attachment UI acceptance has a short timeout, while final send readiness has a longer 90-second timeout because ChatGPT performs file upload/processing in the background. The file upload and prompt submit are retried together after a dead-browser recovery so a reconnect cannot silently drop an attachment.

`stream=true` is supported as compatibility streaming. ChatBang still waits for the browser-backed request to finish, then emits the completed answer as OpenAI-style SSE `chat.completion.chunk` events followed by `data: [DONE]`. This is buffered/synthetic streaming, not token-by-token streaming from ChatGPT.

Unsupported or intentionally omitted:

- tool calls / function calls
- resolving OpenAI `file_id` values
- local server-path attachments through the HTTP API
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
