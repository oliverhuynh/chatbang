package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/chromedp/chromedp"

	markdown "github.com/MichaelMure/go-term-markdown"
)

const (
	navTimeout             = 60 * time.Second
	responseTimeout        = 15 * time.Minute
	pollInterval           = 2 * time.Second
	stablePolls            = 6
	doneIdlePolls          = 5
	confirmDelay           = 4 * time.Second
	textChunkSize          = 20000
	partialMinGap          = 15 * time.Second
	largeResponseThreshold = 6000 // refresh chat before next prompt
	plainTextMinLen        = 800
)

// Shared DOM snippet: latest assistant message node(s).
const jsAssistantNodes = `
		let nodes = document.querySelectorAll('[data-message-author-role="assistant"]');
		if (!nodes.length) nodes = document.querySelectorAll('article[data-turn="assistant"]');`

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

// parseCLI applies headless flags and reports special modes. --config takes precedence over --help.
func parseCLI(args []string, headless bool) (wantConfig, wantHelp bool, headlessOut bool) {
	headlessOut = headless
	for _, arg := range args[1:] {
		switch arg {
		case "--config":
			wantConfig = true
		case "--help", "-h":
			wantHelp = true
		case "--headless":
			headlessOut = true
		case "--no-headless":
			headlessOut = false
		}
	}
	return wantConfig, wantHelp, headlessOut
}

func printHelp() {
	const helpMarkdown = "`Chatbang` is a simple tool to access ChatGPT from the terminal, without needing for an API key.  \n" +
		"## Configuration  \n `Chatbang` requires a Chromium-based browser (e.g. Chrome, Edge, Brave) to work, so you need to have one. And then make sure that it points to the right path to your chosen browser in the default config path for `Chatbang`: `$HOME/.config/chatbang/chatbang`.  \n\nIt's default is: ``` browser=/usr/bin/google-chrome ```  \nChange it to your favorite Chromium-based browser.  \n\n" +
		"You also need to log in to ChatGPT in `Chatbang`'s Chromium session, so you need to do: ```bash chatbang --config ``` That opens ChatGPT in a dedicated browser profile; log in, then press Enter in the terminal.  \n\n" +
		"Chat runs headless by default. Set `headless=false` in `$HOME/.config/chatbang/chatbang`, or use `--no-headless` to show the browser. Use `--headless` to force headless mode.  \n\n"
	fmt.Println(string(markdown.Render(helpMarkdown, 80, 2)))
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

	wantConfig, wantHelp, headless := parseCLI(os.Args, headless)
	if wantConfig {
		loginProfile(defaultBrowser, profileDir)
		return
	}
	if wantHelp {
		printHelp()
		return
	}

	sess, err := newChatSession(defaultBrowser, profileDir, headless)
	if err != nil {
		log.Fatal(err)
	}
	defer sess.close()

	fmt.Print("> ")
	promptScanner := bufio.NewScanner(os.Stdin)
	for promptScanner.Scan() {
		prompt := strings.TrimSpace(promptScanner.Text())
		if prompt == "" {
			fmt.Print("> ")
			continue
		}
		runOneTurn(sess, prompt)
		fmt.Print("> ")
	}
}

type chatSession struct {
	allocCtx    context.Context
	allocCancel context.CancelFunc
	ctx         context.Context
	ctxCancel   context.CancelFunc
	lastPeak    int
}

func newChatSession(browser, profileDir string, headless bool) (*chatSession, error) {
	allocCtx, allocCancel := chromedp.NewExecAllocator(
		context.Background(),
		browserAllocatorOptions(browser, profileDir, headless)...,
	)
	s := &chatSession{allocCtx: allocCtx, allocCancel: allocCancel}
	if err := s.openTab(); err != nil {
		allocCancel()
		return nil, err
	}
	return s, nil
}

func waitForChatReady(ctx context.Context) error {
	// Must use the tab context (not a short-lived child) or Chrome dies when the child is canceled.
	const readyJS = `(() => {
		if (document.title.includes('Just a moment')) return false;
		return !!document.querySelector('#prompt-textarea');
	})()`
	deadline := time.Now().Add(navTimeout)
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			return ctx.Err()
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
		time.Sleep(pollInterval)
	}
	return fmt.Errorf("chatgpt.com did not become ready within %s", navTimeout)
}

func (s *chatSession) openTab() error {
	if s.ctxCancel != nil {
		s.ctxCancel()
	}
	s.ctx, s.ctxCancel = chromedp.NewContext(s.allocCtx, chromedp.WithErrorf(suppressChromedpNoise))
	if err := chromedp.Run(s.ctx, chromedp.Navigate(`https://chatgpt.com`)); err != nil {
		return err
	}
	return waitForChatReady(s.ctx)
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
	if err := chromedp.Run(s.ctx, chromedp.Navigate(`https://chatgpt.com`)); err != nil {
		return err
	}
	return waitForChatReady(s.ctx)
}

func runOneTurn(s *chatSession, prompt string) {
	fmt.Printf("[Thinking...]\n\n")

	if err := s.prepareForPrompt(); err != nil {
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
		log.Fatal("browser session ended unexpectedly (Chrome disconnected); restart chatbang and try again")
	}
	log.Fatal(err)
}

type responseStatus struct {
	Generating bool   `json:"generating"`
	Len        int    `json:"len"`
	Tail       string `json:"tail"`
}

func (s responseStatus) signature() string {
	return fmt.Sprintf("%d:%s", s.Len, s.Tail)
}

func thresholdsForLen(contentLen int) (stableNeeded, idleNeeded int, confirmWait, pollWait time.Duration) {
	switch {
	case contentLen > 20000:
		return 14, 10, 10 * time.Second, 5 * time.Second
	case contentLen > 8000:
		return 10, 7, 8 * time.Second, 4 * time.Second
	case contentLen > 3000:
		return 8, 6, 6 * time.Second, 3 * time.Second
	default:
		return stablePolls, doneIdlePolls, confirmDelay, pollInterval
	}
}

func pollResponseStatus(ctx context.Context, pollWait time.Duration) (responseStatus, error) {
	statusJS := `(() => {
		if (document.querySelector('[data-testid="stop-button"]')) return {generating: true};
		` + jsAssistantNodes + `
		if (!nodes.length) return {generating: true};
		const tc = nodes[nodes.length - 1].textContent || "";
		const len = tc.length;
		if (!len) return {generating: true};
		return {generating: false, len: len, tail: tc.substring(len - 400)};
	})()`

	var status responseStatus
	err := chromedp.Run(ctx,
		chromedp.Sleep(pollWait),
		chromedp.Evaluate(statusJS, &status),
	)
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

func waitForResponse(ctx context.Context) ([]byte, int, error) {
	deadline := time.Now().Add(responseTimeout)
	var lastSig string
	var lastPartial string
	var lastFetch time.Time
	var sawGenerating bool
	var idleAfterGen int
	var stableCount int
	var peakLen int

	returnPartial := func(warn string) ([]byte, int, error) {
		if lastPartial == "" {
			return nil, peakLen, fmt.Errorf("browser disconnected before any response was captured; restart chatbang")
		}
		fmt.Fprintln(os.Stderr, warn)
		out, err := renderResponse(lastPartial)
		return out, max(peakLen, len(lastPartial)), err
	}

	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			return returnPartial("Warning: browser disconnected; showing last captured text.")
		}

		stableNeeded, idleNeeded, confirmWait, pollWait := thresholdsForLen(peakLen)

		status, err := pollResponseStatus(ctx, pollWait)
		if err != nil {
			if ctx.Err() != nil {
				return returnPartial("Warning: browser disconnected; showing last captured text.")
			}
			continue
		}

		if status.Len > peakLen {
			peakLen = status.Len
		}

		if status.Generating {
			sawGenerating = true
			idleAfterGen = 0
			stableCount = 0
			continue
		}

		if status.Len == 0 {
			idleAfterGen = 0
			stableCount = 0
			continue
		}

		if sawGenerating {
			idleAfterGen++
			if idleAfterGen < idleNeeded {
				stableCount = 0
				continue
			}
		}

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
					continue
				}
				out, err := renderResponse(text)
				return out, max(peakLen, len(text)), err
			}
		} else {
			lastSig = sig
			stableCount = 0
			maybeSavePartial(ctx, status.Len, &lastPartial, &lastFetch)
		}
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

	if err := chromedp.Run(ctx, chromedp.Navigate(`https://chatgpt.com`)); err != nil {
		log.Fatalf("Could not open ChatGPT in browser: %v", err)
	}
	if err := waitForChatReady(ctx); err != nil {
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
