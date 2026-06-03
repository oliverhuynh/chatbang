# Chatbang

`Chatbang` is a simple tool to access ChatGPT from the terminal, without needing an API key.

![Chatbang](./assets/chatbang.png)

## Installation

On Linux:

```bash
curl -L https://github.com/ahmedhosssam/chatbang/releases/latest/download/chatbang -o chatbang
chmod +x chatbang
sudo mv chatbang /usr/bin/chatbang
```

### Install from source

```bash
git clone git@github.com:ahmedhosssam/chatbang.git
cd chatbang
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
3. Fills `#prompt-textarea` via JavaScript (supports Unicode and Arabic).
4. Clicks the send button and polls until the assistant finishes streaming.
5. Reads the latest assistant message from the page DOM (no clipboard permission required).
6. Prints short replies as markdown; long or multi-line replies as plain text.

If the browser disconnects, Chatbang tries to reconnect once. Partial text may be shown with a warning when possible.

For the original design story, see [How I Made ChatGPT Run on My Terminal](https://ahmedhosssam.github.io/posts/chatbang/). The current version uses DOM extraction instead of the clipboard flow described in that article.
