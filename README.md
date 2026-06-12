# Chatbang Pro

**Enhanced fork of [chatbang](https://github.com/ahmedhosssam/chatbang)** — a stronger, more reliable terminal client for ChatGPT (no API key).

| | |
|---|---|
| **This repo (maintained)** | [github.com/KaraBala10/chatbang-pro](https://github.com/KaraBala10/chatbang-pro) |
| **Original project** | [github.com/ahmedhosssam/chatbang](https://github.com/ahmedhosssam/chatbang) |

> Chatbang Pro builds on Ahmed Hossam’s original idea and pushes it much further: stabler browser automation, DOM-based replies (no clipboard), long-answer handling, better Chromium detection, full Unicode prompts, and clearer recovery when the session drops.

Use **ChatGPT from your terminal** — free to run, with **no API key** and **no API quotas**. Chatbang drives the real [ChatGPT web app](https://chatgpt.com) in Chromium, so you get the full product (models, browsing, attachments, and whatever OpenAI ships on the site) instead of a trimmed-down API integration. If you want a capable **CLI-style ChatGPT client** without maintaining keys or billing, this is built for that.

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

Download and install from the **[Releases](https://github.com/KaraBala10/chatbang-pro/releases)** page — each version includes step-by-step instructions.

## Requirements

- **Google Chrome** must be installed. On Debian/Ubuntu amd64:

```bash
curl -fL https://dl.google.com/linux/direct/google-chrome-stable_current_amd64.deb \
  -o /tmp/google-chrome-stable_current_amd64.deb

sudo apt install /tmp/google-chrome-stable_current_amd64.deb
```

## Configuration

Run setup once to create `$HOME/.config/chatbang/`:

```bash
chatbang-pro --config
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
chatbang-pro --headless      # force headless
chatbang-pro --no-headless   # show the browser while chatting
```

## Usage

Start an interactive chat session:

```bash
chatbang-pro
```

Type a prompt at `>`, wait for `[Thinking...]`, then the reply is printed in the terminal. Empty lines are ignored; type `exit` or `quit` to leave cleanly.

```bash
chatbang-pro --help    # show help
chatbang-pro --config  # log in / refresh browser profile
chatbang-pro --gpt https://chatgpt.com/g/g-81BdggBV3-website-mobile-app-builder-ui-ux-web-design
chatbang-pro -g g-81BdggBV3-website-mobile-app-builder-ui-ux-web-design
```

Use `--gpt`, `--custom-gpt`, or `-g` with a full custom GPT link, a `/g/g-...` path, or just the `g-...` id.

### Tips for long replies

- Very long answers (big lists, long text) can take several minutes; the tool waits up to 15 minutes per reply.
- After a large reply (>6000 characters), the next prompt starts a **fresh chat** automatically so Chrome stays stable.
- Follow-up questions that refer to the previous answer should be in the **same** prompt, or ask again in one message, because a fresh chat does not see earlier turns.

## Project layout

```
cmd/chatbang-pro/     CLI entry point (main)
internal/
  app/                wiring: config load, session start, prompt loop
  cli/                flag parsing
  config/             browser detection and config file
  chaturl/            ChatGPT / custom GPT URL helpers
  help/               --help output
  prompt/             interactive terminal input
  session/            chromedp browser automation and reply polling
```

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
