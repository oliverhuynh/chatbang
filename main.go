package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/url"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/chromedp/chromedp"

	markdown "github.com/MichaelMure/go-term-markdown"
	"golang.org/x/term"
)

var version = "dev"

const (
	navTimeout             = 60 * time.Second
	responseTimeout        = 15 * time.Minute
	pollIntervalActive     = 1 * time.Second  // while the model is still streaming
	pollIntervalDone       = 350 * time.Millisecond // after streaming stops
	stablePollsDefault     = 2 // consecutive unchanged readings before fetch
	stablePollsLarge       = 4 // for very long replies still being finalized in DOM
	confirmDelayDefault    = 400 * time.Millisecond
	confirmDelayLarge      = 1500 * time.Millisecond
	textChunkSize          = 20000
	partialMinGap          = 15 * time.Second
	largeResponseThreshold = 6000 // refresh chat before next prompt
	plainTextMinLen        = 800

	chatURL     = "https://chatgpt.com"
	tempChatURL = "https://chatgpt.com/?temporary-chat=true"

	promptPrefix      = "> "
	promptPlaceholder = "Ask anything… (type exit to quit)"
)

// Shared DOM snippets for assistant messages.
const jsAssistantNodes = `
		let nodes = document.querySelectorAll('[data-message-author-role="assistant"]');
		if (!nodes.length) nodes = document.querySelectorAll('article[data-turn="assistant"]');`

const jsIsStreaming = `
		function isStillStreaming(node) {
			if (!node) return true;
			if (node.getAttribute('data-is-streaming') === 'true') return true;
			if (node.querySelector('[data-is-streaming="true"]')) return true;
			if (node.querySelector('.result-streaming')) return true;
			return false;
		}`

var browserSearchDirs = []string{"/bin/", "/usr/bin/"}

// Common executable names for Chromium-based browsers.
var browsers = []string{
	"chromium",
	"chromium-browser",
	"google-chrome",
	"google-chrome-stable",
	"microsoft-edge",
	"microsoft-edge-stable",
	"brave-browser",
	"vivaldi",
	"opera",
	"msedge",
	"ungoogled-chromium",
}

// suppressChromedpNoise hides harmless CDP events from newer Chrome versions
// that chromedp does not handle yet (e.g. EventTopLayerElementsUpdated).
func suppressChromedpNoise(format string, a ...interface{}) {
	msg := fmt.Sprintf(format, a...)
	if strings.Contains(msg, "unhandled node event") ||
		strings.Contains(msg, "unhandled page event") {
		return
	}
	log.Printf(format, a...)
}

func browserAllocatorOptions(browserPath, profileDir string, headless bool) []chromedp.ExecAllocatorOption {
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.ExecPath(browserPath),
		chromedp.Flag("disable-blink-features", "AutomationControlled"),
		chromedp.Flag("exclude-switches", "enable-automation"),
		chromedp.Flag("disable-extensions", false),
		chromedp.UserAgent("Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"),
		chromedp.Flag("disable-default-apps", false),
		chromedp.Flag("disable-dev-shm-usage", true),
		chromedp.Flag("disable-gpu", false),
		chromedp.Flag("no-first-run", true),
		chromedp.Flag("no-default-browser-check", true),
		chromedp.Flag("disable-background-timer-throttling", true),
		chromedp.Flag("disable-backgrounding-occluded-windows", true),
		chromedp.Flag("disable-renderer-backgrounding", true),
		chromedp.UserDataDir(profileDir),
		chromedp.Flag("profile-directory", "Default"),
	)
	opts = append(opts, chromedp.Flag("headless", headless))
	return opts
}

func detectBrowser() (string, error) {
	for _, dir := range browserSearchDirs {
		for _, name := range browsers {
			path := dir + name
			if _, err := os.Stat(path); err == nil {
				return path, nil
			}
		}
	}
	return "", fmt.Errorf("no Chromium-based browser found in /bin or /usr/bin")
}

func parseBoolConfig(value string) (bool, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true, true
	case "0", "false", "no", "off":
		return false, true
	default:
		return false, false
	}
}

func parseConfig(r io.Reader) (browser string, headless bool, headlessSet bool) {
	headless = true // default for chat sessions

	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		switch key {
		case "browser":
			browser = value
		case "headless":
			if parsed, ok := parseBoolConfig(value); ok {
				headless = parsed
				headlessSet = true
			}
		}
	}
	return browser, headless, headlessSet
}

type cliOptions struct {
	wantConfig    bool
	wantHelp      bool
	headless      bool
	temporaryChat bool
	customGPT     string
}

func flagValue(args []string, i int) (string, int, bool) {
	if i+1 >= len(args) {
		return "", i, false
	}
	return args[i+1], i + 1, true
}

func setCustomGPT(opts *cliOptions, value string) {
	opts.customGPT = strings.TrimSpace(value)
}

// parseCLI applies flags and modes. --config takes precedence over --help.
func parseCLI(args []string, headless bool) cliOptions {
	opts := cliOptions{headless: headless}
	for i := 1; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "--config":
			opts.wantConfig = true
		case "--help", "-h":
			opts.wantHelp = true
		case "--headless":
			opts.headless = true
		case "--no-headless":
			opts.headless = false
		case "--temporary-chat", "--temp":
			opts.temporaryChat = true
		case "--gpt", "--custom-gpt", "-g":
			value, next, ok := flagValue(args, i)
			if !ok {
				continue
			}
			setCustomGPT(&opts, value)
			i = next
		default:
			if value, ok := strings.CutPrefix(arg, "--gpt="); ok {
				setCustomGPT(&opts, value)
			} else if value, ok := strings.CutPrefix(arg, "--custom-gpt="); ok {
				setCustomGPT(&opts, value)
			}
		}
	}
	return opts
}

func normalizeCustomGPTURL(input string) (string, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", fmt.Errorf("custom GPT URL is empty")
	}

	switch {
	case strings.HasPrefix(input, "/g/"):
		input = "https://chatgpt.com" + input
	case strings.HasPrefix(input, "g-"):
		input = "https://chatgpt.com/g/" + input
	}

	u, err := url.Parse(input)
	if err != nil {
		return "", fmt.Errorf("invalid custom GPT URL: %w", err)
	}
	if u.Scheme == "" {
		u, err = url.Parse("https://" + input)
		if err != nil {
			return "", fmt.Errorf("invalid custom GPT URL: %w", err)
		}
	}

	host := strings.ToLower(u.Hostname())
	if host != "chatgpt.com" && host != "www.chatgpt.com" {
		return "", fmt.Errorf("custom GPT URL must be on chatgpt.com")
	}

	path := strings.TrimSuffix(u.EscapedPath(), "/")
	if !strings.HasPrefix(path, "/g/g-") {
		return "", fmt.Errorf("custom GPT URL must look like https://chatgpt.com/g/g-...")
	}

	return "https://chatgpt.com" + path, nil
}

func customGPTPathPrefix(chatURL string) string {
	if !strings.Contains(chatURL, "/g/g-") {
		return ""
	}
	u, err := url.Parse(chatURL)
	if err != nil {
		return ""
	}
	return strings.TrimSuffix(u.Path, "/")
}

func customGPTNewChatURL(chatURL string) string {
	gptPrefix := customGPTPathPrefix(chatURL)
	if gptPrefix == "" {
		return chatURL
	}
	return "https://chatgpt.com" + gptPrefix + "/c/new"
}

func isOnCustomGPTPathJS(gptPrefix string) (string, error) {
	prefixJSON, err := json.Marshal(gptPrefix)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(`(() => {
		const expected = %s;
		if (!expected) return true;
		const p = location.pathname.replace(/\/$/, '');
		if (!p || p === '/') return false;
		return p === expected || p.startsWith(expected + '/');
	})()`, prefixJSON), nil
}

func isOnCustomGPTPath(ctx context.Context, gptPrefix string) (bool, error) {
	if gptPrefix == "" {
		return true, nil
	}
	js, err := isOnCustomGPTPathJS(gptPrefix)
	if err != nil {
		return false, err
	}
	var ok bool
	if err := chromedp.Run(ctx, chromedp.Evaluate(js, &ok)); err != nil {
		return false, err
	}
	return ok, nil
}

func ensureCustomGPTPage(ctx context.Context, chatURL, gptPrefix string) error {
	if gptPrefix == "" {
		return nil
	}
	ok, err := isOnCustomGPTPath(ctx, gptPrefix)
	if err != nil {
		return err
	}
	if ok {
		return nil
	}

	fmt.Fprintln(os.Stderr, "ChatGPT left the custom GPT — reopening…")
	for _, target := range []string{customGPTNewChatURL(chatURL), chatURL} {
		if err := chromedp.Run(ctx, chromedp.Navigate(target)); err != nil {
			return err
		}
		if err := chromedp.Run(ctx, chromedp.Sleep(2*time.Second)); err != nil {
			return err
		}
		if err := tryActivateCustomGPT(ctx, gptPrefix); err != nil {
			return err
		}
		ok, err := isOnCustomGPTPath(ctx, gptPrefix)
		if err != nil {
			return err
		}
		if ok {
			return nil
		}
	}
	return nil
}

func resolveChatURL(temporaryChat bool, customGPT string) (string, error) {
	if customGPT != "" {
		return normalizeCustomGPTURL(customGPT)
	}
	if temporaryChat {
		return tempChatURL, nil
	}
	return chatURL, nil
}

func printHelp(configPath string) {
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
| `+"`--temporary-chat`"+`, `+"`--temp`"+` | Use [temporary chat](%s) (not saved to history) |
| `+"`--gpt`"+`, `+"`--custom-gpt`"+`, `+"`-g`"+` | Chat with a [custom GPT](https://chatgpt.com/gpts) (full URL, `+"`/g/g-...`"+` path, or `+"`g-...`"+` id) |

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
chatbang-pro -g g-81BdggBV3-website-mobile-app-builder-ui-ux-web-design
chatbang-pro --config                 # log in / refresh browser profile
`+"```"+`

## Tips

- Very long replies can take several minutes (up to 15 minutes per answer).
- After a large reply (>6000 characters), the next prompt starts a **fresh chat** automatically.
- For follow-ups that need prior context, include that context in the same prompt.
`, tempChatURL, configPath)
	fmt.Println(string(markdown.Render(helpMarkdown, 100, 2)))
}

func main() {
	usr, err := user.Current()
	if err != nil {
		fmt.Println("Error fetching user info:", err)
		return
	}

	configDir := filepath.Join(usr.HomeDir, ".config", "chatbang")
	configPath := filepath.Join(configDir, "chatbang")
	profileDir := filepath.Join(configDir, "profile_data")

	cli := parseCLI(os.Args, true)
	if cli.wantHelp {
		printHelp(configPath)
		return
	}

	if err = os.MkdirAll(configDir, 0o755); err != nil {
		fmt.Println("Error creating config directory:", err)
		return
	}

	configFile, err := os.OpenFile(configPath, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		fmt.Println("Error opening config file:", err)
		return
	}
	defer configFile.Close()

	if _, err = configFile.Seek(0, io.SeekStart); err != nil {
		fmt.Println("Error reading config file:", err)
		return
	}
	defaultBrowser, headless, _ := parseConfig(configFile)

	if defaultBrowser == "" {
		detectedBrowser, err := detectBrowser()
		if err != nil {
			fmt.Println("No Chromium-based browser found in /bin, /usr/bin, or config.")
			fmt.Println("Please install a Chromium-based browser or edit the config at", configPath)
			return
		}

		defaultBrowser = detectedBrowser
		defaultConfig := "browser=" + defaultBrowser + "\nheadless=" + strconv.FormatBool(headless) + "\n"

		if _, err = configFile.Seek(0, io.SeekStart); err != nil {
			fmt.Println("Error writing default config:", err)
			return
		}
		if _, err = io.WriteString(configFile, defaultConfig); err != nil {
			fmt.Println("Error writing default config:", err)
			return
		}
	}

	cli = parseCLI(os.Args, headless)
	if cli.wantConfig {
		loginProfile(defaultBrowser, profileDir)
		return
	}

	headless = cli.headless
	chatTarget, err := resolveChatURL(cli.temporaryChat, cli.customGPT)
	if err != nil {
		log.Fatal(err)
	}
	if cli.customGPT != "" {
		if cli.temporaryChat {
			fmt.Fprintln(os.Stderr, "Note: --temp is ignored when --gpt is set.")
		}
		fmt.Fprintf(os.Stderr, "Custom GPT: %s\n", chatTarget)
	} else if cli.temporaryChat {
		fmt.Fprintln(os.Stderr, "Temporary chat mode — conversations are not saved to history.")
	}

	fmt.Fprintf(os.Stderr, "chatbang-pro %s\n", version)
	fmt.Fprintln(os.Stderr, "Starting browser and opening ChatGPT…")
	sess, err := newChatSession(defaultBrowser, profileDir, headless, chatTarget)
	if err != nil {
		log.Fatal(err)
	}
	defer sess.close()

	fmt.Fprintln(os.Stderr, "Ready — start chatting below.")
	promptLoop(sess)
}

func isExitCommand(prompt string) bool {
	switch strings.ToLower(strings.TrimSpace(prompt)) {
	case "exit", "quit", "q", ":q", "/exit", "/quit":
		return true
	default:
		return false
	}
}

func readRuneFromStdin() (rune, error) {
	buf := make([]byte, 0, utf8.UTFMax)
	for {
		var b [1]byte
		n, err := os.Stdin.Read(b[:])
		if n == 0 {
			if err != nil {
				return 0, err
			}
			return 0, io.EOF
		}
		buf = append(buf, b[0])
		r, size := utf8.DecodeRune(buf)
		if r == utf8.RuneError && size == 1 && len(buf) < utf8.UTFMax {
			continue
		}
		return r, nil
	}
}

func readPromptLine() (string, error) {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		scanner := bufio.NewScanner(os.Stdin)
		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				return "", err
			}
			return "", io.EOF
		}
		return scanner.Text(), nil
	}

	oldState, err := term.MakeRaw(fd)
	if err != nil {
		scanner := bufio.NewScanner(os.Stdin)
		fmt.Print(promptPrefix)
		if !scanner.Scan() {
			if scanErr := scanner.Err(); scanErr != nil {
				return "", scanErr
			}
			return "", io.EOF
		}
		return scanner.Text(), nil
	}
	defer term.Restore(fd, oldState)

	fmt.Print(promptPrefix)
	fmt.Print("\x1b[2m" + promptPlaceholder + "\x1b[0m")

	var line []rune
	placeholderVisible := true

	redraw := func() {
		fmt.Print("\r" + promptPrefix + "\x1b[K")
		fmt.Print(string(line))
	}

	for {
		r, err := readRuneFromStdin()
		if err != nil {
			fmt.Println()
			return "", err
		}

		switch r {
		case '\r', '\n':
			fmt.Print("\r\n")
			return string(line), nil
		case 4: // Ctrl+D
			fmt.Print("\r\n")
			return "", io.EOF
		case 3: // Ctrl+C — suggest exit instead of abrupt kill
			fmt.Print("\r\x1b[K")
			fmt.Println("Type exit or quit to leave.")
			line = line[:0]
			placeholderVisible = true
			fmt.Print(promptPrefix)
			fmt.Print("\x1b[2m" + promptPlaceholder + "\x1b[0m")
			continue
		case 127, 8: // backspace / delete
			if len(line) > 0 {
				line = line[:len(line)-1]
				if len(line) == 0 {
					placeholderVisible = true
					fmt.Print("\r" + promptPrefix + "\x1b[K\x1b[2m" + promptPlaceholder + "\x1b[0m")
				} else {
					redraw()
				}
			}
		case unicode.ReplacementChar:
			continue
		default:
			if !unicode.IsPrint(r) {
				continue
			}
			if placeholderVisible {
				placeholderVisible = false
				fmt.Print("\r" + promptPrefix + "\x1b[K")
			}
			line = append(line, r)
			fmt.Print(string(r))
		}
	}
}

func promptLoop(sess *chatSession) {
	for {
		line, err := readPromptLine()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return
			}
			fmt.Fprintln(os.Stderr, "prompt:", err)
			return
		}

		prompt := strings.TrimSpace(line)
		if prompt == "" {
			continue
		}
		if isExitCommand(prompt) {
			return
		}
		runOneTurn(sess, prompt)
	}
}

type chatSession struct {
	allocCtx    context.Context
	allocCancel context.CancelFunc
	ctx         context.Context
	ctxCancel   context.CancelFunc
	chatURL     string
	lastPeak    int
}

func newChatSession(browser, profileDir string, headless bool, chatTarget string) (*chatSession, error) {
	allocCtx, allocCancel := chromedp.NewExecAllocator(
		context.Background(),
		browserAllocatorOptions(browser, profileDir, headless)...,
	)
	s := &chatSession{allocCtx: allocCtx, allocCancel: allocCancel, chatURL: chatTarget}
	if err := s.openTab(); err != nil {
		allocCancel()
		return nil, err
	}
	return s, nil
}

func customGPTActivateJS(gptPrefix string) (string, error) {
	prefixJSON, err := json.Marshal(gptPrefix)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(`(() => {
		const expected = %s;
		const p = location.pathname.replace(/\/$/, '');
		if (!p || p === '/') return false;
		if (p !== expected && !p.startsWith(expected + '/')) return false;
		if (document.querySelector('#prompt-textarea') && p.startsWith(expected + '/')) return true;
		if (p !== expected) return false;

		const buttons = document.querySelectorAll('button, a[role="button"]');
		for (const el of buttons) {
			const t = (el.textContent || '').trim().toLowerCase();
			if (t === 'start chat' || t.includes('start chat')) {
				el.click();
				return false;
			}
		}
		for (const el of document.querySelectorAll('a[href]')) {
			const href = el.getAttribute('href') || '';
			if (href.includes(expected) && href.includes('/c/')) {
				el.click();
				return false;
			}
		}
		return false;
	})()`, prefixJSON), nil
}

func tryActivateCustomGPT(ctx context.Context, gptPrefix string) error {
	clickJS, err := customGPTActivateJS(gptPrefix)
	if err != nil {
		return err
	}
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		var done bool
		if err := chromedp.Run(ctx, chromedp.Evaluate(clickJS, &done)); err != nil {
			return err
		}
		if done {
			return nil
		}
		time.Sleep(pollIntervalActive)
	}
	return nil
}

func waitForChatReady(ctx context.Context, chatURL string) error {
	// Must use the tab context (not a short-lived child) or Chrome dies when the child is canceled.
	gptPrefix := customGPTPathPrefix(chatURL)
	if gptPrefix != "" {
		if err := tryActivateCustomGPT(ctx, gptPrefix); err != nil {
			return err
		}
	}

	pathCheckJS, err := isOnCustomGPTPathJS(gptPrefix)
	if err != nil {
		return err
	}
	readyJS := fmt.Sprintf(`(() => {
		if (document.title.includes('Just a moment')) return false;
		if (!document.querySelector('#prompt-textarea')) return false;
		return %s;
	})()`, pathCheckJS)

	deadline := time.Now().Add(navTimeout)
	nextNotice := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if gptPrefix != "" {
			if err := ensureCustomGPTPage(ctx, chatURL, gptPrefix); err != nil {
				return err
			}
		}
		var ready bool
		err := chromedp.Run(ctx,
			chromedp.Evaluate(readyJS, &ready),
		)
		if err != nil {
			return err
		}
		if ready {
			return chromedp.Run(ctx, chromedp.Sleep(1*time.Second))
		}
		if gptPrefix != "" {
			if err := tryActivateCustomGPT(ctx, gptPrefix); err != nil {
				return err
			}
		}
		if time.Now().After(nextNotice) {
			fmt.Fprintln(os.Stderr, "Still waiting for chat to start…")
			nextNotice = time.Now().Add(10 * time.Second)
		}
		time.Sleep(pollIntervalActive)
	}
	return fmt.Errorf("chatgpt.com did not become ready within %s", navTimeout)
}

func (s *chatSession) openTab() error {
	if s.ctxCancel != nil {
		s.ctxCancel()
	}
	s.ctx, s.ctxCancel = chromedp.NewContext(s.allocCtx, chromedp.WithErrorf(suppressChromedpNoise))
	if gptPrefix := customGPTPathPrefix(s.chatURL); gptPrefix != "" {
		if err := s.openCustomGPT(gptPrefix); err != nil {
			return err
		}
	} else {
		fmt.Fprintf(os.Stderr, "Opening %s…\n", s.chatURL)
		if err := chromedp.Run(s.ctx, chromedp.Navigate(s.chatURL)); err != nil {
			return err
		}
	}
	fmt.Fprintln(os.Stderr, "Waiting for chat to start…")
	return waitForChatReady(s.ctx, s.chatURL)
}

func (s *chatSession) openCustomGPT(gptPrefix string) error {
	targets := []string{customGPTNewChatURL(s.chatURL), s.chatURL}
	for _, target := range targets {
		fmt.Fprintf(os.Stderr, "Opening %s…\n", target)
		if err := chromedp.Run(s.ctx, chromedp.Navigate(target)); err != nil {
			return err
		}
		if err := chromedp.Run(s.ctx, chromedp.Sleep(2*time.Second)); err != nil {
			return err
		}
		if err := tryActivateCustomGPT(s.ctx, gptPrefix); err != nil {
			return err
		}
		ok, err := isOnCustomGPTPath(s.ctx, gptPrefix)
		if err != nil {
			return err
		}
		if ok {
			return nil
		}
	}
	return ensureCustomGPTPage(s.ctx, s.chatURL, gptPrefix)
}

func (s *chatSession) close() {
	if s.ctxCancel != nil {
		s.ctxCancel()
	}
	if s.allocCancel != nil {
		s.allocCancel()
	}
}

func isSessionDead(err error) bool {
	return err != nil && (errors.Is(err, context.Canceled) || strings.Contains(err.Error(), "target closed"))
}

func (s *chatSession) recover() error {
	fmt.Fprintln(os.Stderr, "Reconnecting browser...")
	if err := s.openTab(); err != nil {
		return fmt.Errorf("could not reconnect browser: %w", err)
	}
	return nil
}

func (s *chatSession) prepareForPrompt() error {
	if s.lastPeak <= largeResponseThreshold {
		return nil
	}
	fmt.Fprintln(os.Stderr, "Starting a fresh chat (last reply was large)...")
	s.lastPeak = 0
	if err := chromedp.Run(s.ctx, chromedp.Navigate(s.chatURL)); err != nil {
		return err
	}
	return waitForChatReady(s.ctx, s.chatURL)
}

func runOneTurn(s *chatSession, prompt string) {
	fmt.Println("[Thinking...]")

	if err := s.prepareForPrompt(); err != nil {
		fatalChatErr(err)
	}
	if err := ensureCustomGPTPage(s.ctx, s.chatURL, customGPTPathPrefix(s.chatURL)); err != nil {
		fatalChatErr(err)
	}

	if err := submitPromptWithRetry(s, prompt); err != nil {
		fatalChatErr(err)
	}

	result, peak, err := waitForResponse(s.ctx)
	s.lastPeak = peak
	if err != nil {
		fatalChatErr(err)
	}
	fmt.Println()
	fmt.Println(string(result))
}

func submitPromptWithRetry(s *chatSession, prompt string) error {
	err := submitPrompt(s.ctx, prompt)
	if err == nil {
		return nil
	}
	if !isSessionDead(err) {
		return fmt.Errorf("submit prompt: %w", err)
	}
	if err := s.recover(); err != nil {
		return err
	}
	if err := submitPrompt(s.ctx, prompt); err != nil {
		return fmt.Errorf("submit prompt after reconnect: %w", err)
	}
	return nil
}

func submitPrompt(ctx context.Context, prompt string) error {
	promptJSON, err := json.Marshal(prompt)
	if err != nil {
		return err
	}
	setPromptJS := fmt.Sprintf(`(() => {
		const el = document.querySelector('#prompt-textarea');
		if (!el) throw new Error('prompt textarea not found');
		el.focus();
		if (el.tagName === 'TEXTAREA' || el.tagName === 'INPUT') {
			el.value = %s;
		} else {
			el.textContent = %s;
		}
		el.dispatchEvent(new InputEvent('input', { bubbles: true }));
	})()`, promptJSON, promptJSON)

	return chromedp.Run(ctx,
		chromedp.WaitVisible(`#prompt-textarea`, chromedp.ByID),
		chromedp.Click(`#prompt-textarea`, chromedp.ByID),
		chromedp.Evaluate(setPromptJS, nil),
		chromedp.Sleep(500*time.Millisecond),
		chromedp.Click(`#composer-submit-button`, chromedp.ByID),
	)
}

func fatalChatErr(err error) {
	if err == nil {
		return
	}
	if errors.Is(err, context.Canceled) || strings.Contains(err.Error(), "browser disconnected") {
		log.Fatal("browser session ended unexpectedly (Chrome disconnected); restart chatbang-pro and try again")
	}
	log.Fatal(err)
}

type responseStatus struct {
	Generating bool   `json:"generating"`
	Len        int    `json:"len"`
	Tail       string `json:"tail"`
	NodeCount  int    `json:"nodeCount"`
}

func (s responseStatus) signature() string {
	return fmt.Sprintf("%d:%s", s.Len, s.Tail)
}

func completionThresholds(contentLen int) (stableNeeded int, confirmWait time.Duration) {
	switch {
	case contentLen > 20000:
		return stablePollsLarge, confirmDelayLarge
	case contentLen > 8000:
		return stablePollsLarge, confirmDelayLarge
	default:
		return stablePollsDefault, confirmDelayDefault
	}
}

func evaluateResponseStatus(ctx context.Context) (responseStatus, error) {
	statusJS := `(() => {
		` + jsIsStreaming + `
		if (document.querySelector('[data-testid="stop-button"]')) return {generating: true};
		` + jsAssistantNodes + `
		if (!nodes.length) return {generating: true, nodeCount: 0};
		const last = nodes[nodes.length - 1];
		if (isStillStreaming(last)) return {generating: true, nodeCount: nodes.length};
		const tc = last.textContent || "";
		const len = tc.length;
		if (!len) return {generating: true, nodeCount: nodes.length};
		return {generating: false, len: len, tail: tc.substring(len - 400), nodeCount: nodes.length};
	})()`

	var status responseStatus
	err := chromedp.Run(ctx, chromedp.Evaluate(statusJS, &status))
	return status, err
}

func fetchFullResponse(ctx context.Context) (string, error) {
	lenJS := `(() => {
		` + jsAssistantNodes + `
		if (!nodes.length) return 0;
		return (nodes[nodes.length - 1].textContent || "").length;
	})()`

	var totalLen int
	if err := chromedp.Run(ctx, chromedp.Evaluate(lenJS, &totalLen)); err != nil {
		return "", err
	}
	if totalLen == 0 {
		return "", nil
	}

	var sb strings.Builder
	for offset := 0; offset < totalLen; offset += textChunkSize {
		chunkJS := fmt.Sprintf(`(() => {
			%s
			if (!nodes.length) return "";
			const t = nodes[nodes.length - 1].textContent || "";
			return t.substring(%d, %d);
		})()`, jsAssistantNodes, offset, offset+textChunkSize)

		var part string
		if err := chromedp.Run(ctx, chromedp.Evaluate(chunkJS, &part)); err != nil {
			if sb.Len() > 0 {
				return strings.TrimSpace(sb.String()), nil
			}
			return "", err
		}
		sb.WriteString(part)
		if offset+textChunkSize >= totalLen {
			break
		}
	}
	return strings.TrimSpace(sb.String()), nil
}

func renderResponse(text string) ([]byte, error) {
	if text == "" {
		return nil, fmt.Errorf("empty response from ChatGPT")
	}
	// Long or multi-line text is unreadable when passed through markdown rendering.
	if strings.Count(text, "\n") >= 2 || len(text) >= plainTextMinLen {
		return []byte(text + "\n"), nil
	}
	return markdown.Render(text, 80, 2), nil
}

func confirmFullResponse(ctx context.Context, confirmWait time.Duration) (string, bool, error) {
	first, err := fetchFullResponse(ctx)
	if err != nil {
		return "", false, err
	}
	if err := chromedp.Run(ctx, chromedp.Sleep(confirmWait)); err != nil {
		return first, true, nil
	}
	second, err := fetchFullResponse(ctx)
	if err != nil {
		return first, true, nil
	}
	if second == first {
		return second, true, nil
	}
	if len(second) > len(first) {
		return second, false, nil
	}
	return second, true, nil
}

func maybeSavePartial(ctx context.Context, statusLen int, lastPartial *string, lastFetch *time.Time) {
	if statusLen <= len(*lastPartial) {
		return
	}
	if time.Since(*lastFetch) < partialMinGap && len(*lastPartial) > 0 {
		return
	}
	if text, err := fetchFullResponse(ctx); err == nil && len(text) > len(*lastPartial) {
		*lastPartial = text
		*lastFetch = time.Now()
	}
}

func responseStarted(baseline responseStatus, status responseStatus) bool {
	if status.Generating {
		return true
	}
	if status.NodeCount > baseline.NodeCount {
		return true
	}
	if status.Len > 0 && status.signature() != baseline.signature() {
		return true
	}
	return false
}

func waitForResponse(ctx context.Context) ([]byte, int, error) {
	deadline := time.Now().Add(responseTimeout)
	baseline, _ := evaluateResponseStatus(ctx)

	var lastSig string
	var lastPartial string
	var lastFetch time.Time
	var started bool
	var stableCount int
	var peakLen int

	returnPartial := func(warn string) ([]byte, int, error) {
		if lastPartial == "" {
			return nil, peakLen, fmt.Errorf("browser disconnected before any response was captured; restart chatbang-pro")
		}
		fmt.Fprintln(os.Stderr, warn)
		out, err := renderResponse(lastPartial)
		return out, max(peakLen, len(lastPartial)), err
	}

	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			return returnPartial("Warning: browser disconnected; showing last captured text.")
		}

		status, err := evaluateResponseStatus(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return returnPartial("Warning: browser disconnected; showing last captured text.")
			}
			time.Sleep(pollIntervalDone)
			continue
		}

		if status.Len > peakLen {
			peakLen = status.Len
		}

		pollSleep := pollIntervalDone
		if !started {
			if responseStarted(baseline, status) {
				started = true
			} else {
				time.Sleep(pollIntervalDone)
				continue
			}
		}

		if status.Generating {
			stableCount = 0
			pollSleep = pollIntervalActive
		} else if status.Len == 0 {
			stableCount = 0
		} else if started {
			stableNeeded, confirmWait := completionThresholds(peakLen)
			sig := status.signature()
			if sig == lastSig {
				stableCount++
				if stableCount >= stableNeeded {
					text, done, err := confirmFullResponse(ctx, confirmWait)
					if err != nil {
						return returnPartial("Warning: could not fetch full reply; showing last captured text.")
					}
					if !done {
						lastSig = fmt.Sprintf("%d:%s", len(text), text[max(0, len(text)-400):])
						stableCount = 0
						if len(text) > len(lastPartial) {
							lastPartial = text
						}
						time.Sleep(pollIntervalDone)
						continue
					}
					out, err := renderResponse(text)
					return out, max(peakLen, len(text)), err
				}
			} else {
				lastSig = sig
				stableCount = 1
				maybeSavePartial(ctx, status.Len, &lastPartial, &lastFetch)
			}
		}

		time.Sleep(pollSleep)
	}

	if lastPartial != "" {
		return returnPartial("Warning: timed out waiting for reply to finish; showing partial response.")
	}

	return nil, peakLen, fmt.Errorf("timed out after %s waiting for ChatGPT (very long replies may need several minutes)", responseTimeout)
}

func loginProfile(defaultBrowser string, profileDir string) {
	fmt.Println("Opening browser for ChatGPT setup...")

	allocatorCtx, allocCancel := chromedp.NewExecAllocator(
		context.Background(),
		browserAllocatorOptions(defaultBrowser, profileDir, false)...,
	)
	defer allocCancel()

	ctx, ctxCancel := chromedp.NewContext(allocatorCtx, chromedp.WithErrorf(suppressChromedpNoise))
	defer ctxCancel()

	if err := chromedp.Run(ctx, chromedp.Navigate(chatURL)); err != nil {
		log.Fatalf("Could not open ChatGPT in browser: %v", err)
	}
	if err := waitForChatReady(ctx, chatURL); err != nil {
		log.Fatalf("ChatGPT did not load: %v", err)
	}

	fmt.Println()
	fmt.Println("A browser window should be open.")
	fmt.Println("  1. Log in to ChatGPT (if needed)")
	fmt.Println("  2. Start a chat so the page is ready")
	fmt.Println("  3. Return here and press Enter to save and close the browser")
	fmt.Print("\nPress Enter when finished: ")

	reader := bufio.NewReader(os.Stdin)
	if _, err := reader.ReadString('\n'); err != nil {
		log.Fatal(err)
	}
	fmt.Println("Configuration saved.")
}
