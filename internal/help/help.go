package help

import (
	"fmt"

	markdown "github.com/MichaelMure/go-term-markdown"

	"github.com/KaraBala10/chatbang-pro/internal/chaturl"
)

// Print renders CLI help to stdout.
func Print(configPath string) {
	helpMarkdown := fmt.Sprintf(`# Chatbang Pro

**ChatGPT in your terminal** — no API key, no API quotas. Chatbang drives the real ChatGPT web app in Chromium.

## Usage

`+"```"+`bash
chatbang-pro [flags]
`+"```"+`

Type a prompt at `+"`> `"+`, wait for `+"`[Thinking...]`"+`, then read the reply. Type **exit** or **quit** to leave cleanly.

## Flags

| Flag | Description |
|------|-------------|
| `+"`-h`"+`, `+"`--help`"+` | Show this help |
| `+"`--config`"+` | Open ChatGPT in a visible browser to log in or refresh your session |
| `+"`--headless`"+` | Force headless mode (browser runs in the background) |
| `+"`--no-headless`"+` | Show the browser window while chatting |
| `+"`--temporary-chat`"+`, `+"`--temp`"+` | Use [temporary chat](%s) (works with `+"`--gpt`"+` too) |
| `+"`--keep-browser`"+` | Keep the browser running between invocations, reusing it next run |
| `+"`--kill-browser`"+` | Kill the background browser (started with `+"`--keep-browser`"+`) |
| `+"`--gpt`"+`, `+"`--custom-gpt`"+`, `+"`-g`"+` | Chat with a [custom GPT](https://chatgpt.com/gpts) (full URL, `+"`/g/g-...`"+` path, or `+"`g-...`"+` id) |
| `+"`--sessions`"+` | List saved ChatGPT conversation sessions |
| `+"`--resume`"+` | Resume a saved session by id or `+"`last`"+` |
| `+"`--message`"+`, `+"`-m`"+` | Send one prompt, print the reply, and exit (non-interactive) |

## First-time setup

1. Install a Chromium-based browser (Chrome, Edge, Brave, etc.) under `+"`/bin`"+` or `+"`/usr/bin`"+`.
2. Run `+"`chatbang-pro --config`"+` — log in to ChatGPT, then press **Enter** in the terminal.
3. Start chatting: `+"`chatbang-pro`"+`

## Configuration file

Path: `+"`%s`"+`

`+"```"+`
browser=/usr/bin/google-chrome
headless=true
`+"```"+`

| Key | Description |
|-----|-------------|
| `+"`browser`"+` | Path to your Chromium-based browser executable |
| `+"`headless`"+` | `+"`true`"+` (default) hides the browser; `+"`false`"+` shows it |

CLI flags override `+"`headless`"+` for that run only.

## Examples

`+"```"+`bash
chatbang-pro                          # interactive chat (headless)
chatbang-pro --no-headless            # show the browser while chatting
chatbang-pro --temporary-chat         # private temporary chat session
chatbang-pro --temp --no-headless     # visible temporary chat
chatbang-pro --gpt https://chatgpt.com/g/g-81BdggBV3-website-mobile-app-builder-ui-ux-web-design
chatbang-pro --gpt https://chatgpt.com/g/g-xxx --temp
chatbang-pro -g g-81BdggBV3-website-mobile-app-builder-ui-ux-web-design
chatbang-pro --gpt https://chatgpt.com/g/g-xxx --message "كيفك"
chatbang-pro -m "What is 2+2?"
chatbang-pro --keep-browser           # keep Chrome alive and reuse it next run
chatbang-pro --kill-browser            # kill the background browser
chatbang-pro --sessions               # list saved sessions
chatbang-pro --resume last            # resume most recent saved session
chatbang-pro --config                 # log in / refresh browser profile
`+"```"+`

## Tips

- Very long replies can take several minutes (up to 15 minutes per answer).
- After a large reply (>6000 characters), the next prompt starts a **fresh chat** automatically.
- For follow-ups that need prior context, include that context in the same prompt.
`, chaturl.TempURL, configPath)
	fmt.Println(string(markdown.Render(helpMarkdown, 100, 2)))
}
