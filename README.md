# Chatbang Pro

**Enhanced fork of [chatbang](https://github.com/ahmedhosssam/chatbang)** — a stronger, more reliable terminal client for ChatGPT (no API key).

| | |
|---|---|
| **This repo (maintained)** | [github.com/KaraBala10/chatbang-pro](https://github.com/KaraBala10/chatbang-pro) |
| **Original project** | [github.com/ahmedhosssam/chatbang](https://github.com/ahmedhosssam/chatbang) |

> Chatbang Pro builds on Ahmed Hossam’s original idea and pushes it much further: stabler browser automation, DOM-based replies (no clipboard), long-answer handling, better Chromium detection, full Unicode prompts, and clearer recovery when the session drops.

Use **ChatGPT from your terminal** — free to run, with **no API key** and **no API quotas**. Chatbang drives the real [ChatGPT web app](https://chatgpt.com) in Chromium, so you get the full product (models, browsing, attachments, and whatever OpenAI ships on the site) instead of a trimmed-down API integration. If you want a capable **CLI-style ChatGPT client** without maintaining keys or billing, this is built for that.

![Chatbang](./assets/chatbang.png)

## Why Chatbang Pro?

Compared to the upstream [chatbang](https://github.com/ahmedhosssam/chatbang) release flow, this fork focuses on **production-style reliability**:

- **DOM extraction** — reads the latest assistant message from the page (no clipboard permission).
- **Smarter browser setup** — detects many Chromium-based binaries; anti-automation flags; dedicated profile under `~/.config/chatbang/`.
- **Long & complex replies** — up to 15 minutes per answer; auto **fresh chat** after very large responses (>6000 chars) to keep Chrome stable.
- **Unicode & RTL** — prompts are filled via JavaScript so RTL and non-Latin text work reliably.
- **Headless by default** — `headless=true/false` in config, or `--headless` / `--no-headless` on the CLI.
- **Resilience** — Cloudflare-ready waits where possible; one reconnect attempt; partial reply text when the browser drops mid-stream.
- **Quieter logs** — filters harmless chromedp/CDP noise on newer Chrome builds.

Credit for the original design and article: [How I Made ChatGPT Run on My Terminal](https://ahmedhosssam.github.io/posts/chatbang/).

## Installation

On Linux amd64 (binary from [releases](https://github.com/KaraBala10/chatbang-pro/releases), or build from source below):

```bash
curl -L https://github.com/KaraBala10/chatbang-pro/releases/download/v1.0.0/chatbang -o chatbang
chmod +x chatbang
sudo mv chatbang /usr/bin/chatbang
chatbang --config
```

### Install from source

```bash
git clone https://github.com/KaraBala10/chatbang-pro.git
cd chatbang-pro
go mod tidy
go build -o chatbang main.go
sudo mv chatbang /usr/bin/chatbang
```

## Requirements

- A Chromium-based browser (Chrome, Edge, Brave, etc.) installed under `/bin` or `/usr/bin` (Snap builds are not supported).
- Go 1.24+ (only if building from source).

## Configuration

Run setup once to create `$HOME/.config/chatbang/`:

```bash
chatbang --config
```

This opens ChatGPT in a **visible** browser window using a dedicated profile at `$HOME/.config/chatbang/profile_data`. Log in if needed, then return to the terminal and **press Enter** to save the session.

Edit `$HOME/.config/chatbang/chatbang` to customize:

```
browser=/usr/bin/google-chrome
headless=true
```

| Option | Description |
|--------|-------------|
| `browser` | Path to your Chromium-based browser executable. |
| `headless` | `true` (default) runs chat in the background; `false` shows the browser window. |

CLI overrides for headless mode:

```bash
chatbang --headless      # force headless
chatbang --no-headless   # show the browser while chatting
```

## Usage

Start an interactive chat session:

```bash
chatbang
```

Type a prompt at `>`, wait for `[Thinking...]`, then the reply is printed in the terminal. Empty lines are ignored; use `Ctrl+C` to quit.

```bash
chatbang --help    # show help
chatbang --config  # log in / refresh browser profile
```

### Tips for long replies

- Very long answers (big lists, long text) can take several minutes; the tool waits up to 15 minutes per reply.
- After a large reply (>6000 characters), the next prompt starts a **fresh chat** automatically so Chrome stays stable.
- Follow-up questions that refer to the previous answer should be in the **same** prompt, or ask again in one message, because a fresh chat does not see earlier turns.

## How it works

Chatbang automates a real Chromium session (via [chromedp](https://github.com/chromedp/chromedp)):

1. Opens [chatgpt.com](https://chatgpt.com) with a dedicated profile and anti-automation flags.
2. Waits until the page is ready (including passing Cloudflare checks when possible).
3. Fills `#prompt-textarea` via JavaScript (supports Unicode and RTL text).
4. Clicks the send button and polls until the assistant finishes streaming.
5. Reads the latest assistant message from the page DOM (no clipboard permission required).
6. Prints short replies as markdown; long or multi-line replies as plain text.

If the browser disconnects, Chatbang tries to reconnect once. Partial text may be shown with a warning when possible.

## Contributing & upstream

- **Bugs and features for this fork:** open issues/PRs on [KaraBala10/chatbang-pro](https://github.com/KaraBala10/chatbang-pro).
- **Original project:** [ahmedhosssam/chatbang](https://github.com/ahmedhosssam/chatbang) — please star and support the author’s work there too.
