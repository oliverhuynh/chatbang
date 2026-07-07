package session

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/cdproto/browser"

	"github.com/KaraBala10/chatbang-pro/internal/chaturl"
	"github.com/KaraBala10/chatbang-pro/internal/config"
)

// Session drives a Chromium tab for one ChatGPT conversation target.
type Session struct {
	allocCtx    context.Context
	allocCancel context.CancelFunc
	ctx         context.Context
	ctxCancel   context.CancelFunc
	chatURL     string
	lastPeak    int
	keepBrowser bool
	temporary   bool
	sessionFile string
}

const keepBrowserDebugPort = 39227

// New opens a browser session and waits until the chat page is ready.
func New(browser, profileDir string, headless bool, chatTarget string, keepBrowser bool, sessionFile string, temporary bool) (*Session, error) {
	s := &Session{
		chatURL:     chatTarget,
		temporary:   temporary,
		keepBrowser: keepBrowser,
		sessionFile: sessionFile,
	}

	if keepBrowser {
		if wsURL, err := findBrowserWSURL(keepBrowserDebugPort, time.Second); err == nil {
			allocCtx, allocCancel := chromedp.NewRemoteAllocator(context.Background(), wsURL)
			s.allocCtx = allocCtx
			s.allocCancel = allocCancel
			fmt.Fprintln(os.Stderr, "Reusing background browser…")
			if err := s.openTab(); err != nil {
				allocCancel()
				return nil, err
			}
			return s, nil
		}
	}

	opts := config.AllocatorOptions(browser, profileDir, headless)
	if keepBrowser {
		opts = append(opts, chromedp.Flag("remote-debugging-port", strconv.Itoa(keepBrowserDebugPort)))
		opts = append(opts, chromedp.ModifyCmdFunc(func(cmd *exec.Cmd) {
			if cmd.SysProcAttr != nil {
				cmd.SysProcAttr.Pdeathsig = 0
			}
		}))
	}
	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), opts...)

	s.allocCtx = allocCtx
	s.allocCancel = allocCancel
	if err := s.openTab(); err != nil {
		allocCancel()
		return nil, err
	}
	return s, nil
}

// Close shuts down the browser session.
func (s *Session) Close() {
	fmt.Fprintf(os.Stderr, "[debug] Close: keepBrowser=%v allocCancel!=nil=%v\n", s.keepBrowser, s.allocCancel != nil)
	if s.ctxCancel != nil && !s.keepBrowser {
		s.ctxCancel()
	}
	if s.allocCancel != nil && !s.keepBrowser {
		fmt.Fprintln(os.Stderr, "[debug] Close: calling allocCancel")
		s.allocCancel()
	} else {
		fmt.Fprintln(os.Stderr, "[debug] Close: skipping ctxCancel + allocCancel (keepBrowser mode)")
	}
}

// RunTurn sends one prompt and prints the assistant reply to stdout.
func (s *Session) RunTurn(prompt string) {
	fmt.Fprintln(os.Stderr, "[Thinking...]")

	if err := s.prepareForPrompt(); err != nil {
		fatalChatErr(err)
	}
	if err := ensureCustomGPTPage(s.ctx, s.chatURL, chaturl.CustomGPTPathPrefix(s.chatURL)); err != nil {
		fatalChatErr(err)
	}

	if err := s.submitPromptWithRetry(prompt); err != nil {
		fatalChatErr(err)
	}

	result, peakLen, err := waitForResponse(s.ctx)
	if err != nil {
		fatalChatErr(err)
	}
	s.lastPeak = peakLen

	os.Stdout.Write(result)

	s.maybeSaveSession()
}

// LoginProfile opens a visible non-headless Chrome browser for first-time login.
func LoginProfile(browser, profileDir string) {
	allocatorCtx, allocCancel := chromedp.NewExecAllocator(
		context.Background(),
		config.AllocatorOptions(browser, profileDir, false)...,
	)
	defer allocCancel()

	ctx, ctxCancel := chromedp.NewContext(allocatorCtx, chromedp.WithErrorf(suppressChromedpNoise))
	defer ctxCancel()

	if err := chromedp.Run(ctx, chromedp.Navigate(chaturl.DefaultURL)); err != nil {
		log.Fatalf("Could not open ChatGPT in browser: %v", err)
	}
	if err := waitForChatReady(ctx, chaturl.DefaultURL); err != nil {
		log.Fatalf("ChatGPT did not load: %v", err)
	}

	fmt.Println()
	fmt.Println("A browser window should be open.")
	fmt.Println(" 1. Log in to ChatGPT (if needed)")
	fmt.Println(" 2. Start a chat so the page is ready")
	fmt.Println(" 3. Return here and press Enter to save and close the browser")
	fmt.Print("\nPress Enter when finished: ")

	reader := bufio.NewReader(os.Stdin)
	if _, err := reader.ReadString('\n'); err != nil {
		log.Fatal(err)
	}
	fmt.Println("Configuration saved.")
}

func (s *Session) openTab() error {
	if s.ctxCancel != nil {
		s.ctxCancel()
	}
	s.ctx, s.ctxCancel = chromedp.NewContext(s.allocCtx, chromedp.WithErrorf(suppressChromedpNoise))

	if chaturl.CustomGPTPathPrefix(s.chatURL) != "" {
		if err := s.openCustomGPT(); err != nil {
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

func (s *Session) openCustomGPT() error {
	fmt.Fprintf(os.Stderr, "Opening %s…\n", s.chatURL)
	return chromedp.Run(s.ctx, chromedp.Navigate(s.chatURL))
}

// findBrowserWSURL polls the browser DevTools endpoint until it responds
// with a valid WebSocket debugger URL or the timeout expires.
func findBrowserWSURL(port int, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 2 * time.Second, Transport: &http.Transport{Proxy: nil}}
	url := fmt.Sprintf("http://127.0.0.1:%d/json/version", port)

	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err == nil {
			var info struct {
				WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
			}
			decErr := json.NewDecoder(resp.Body).Decode(&info)
			resp.Body.Close()
			if decErr == nil && strings.TrimSpace(info.WebSocketDebuggerURL) != "" {
				return info.WebSocketDebuggerURL, nil
			}
		}
		time.Sleep(300 * time.Millisecond)
	}
	return "", fmt.Errorf("no browser on port %d", port)
}

func isSessionDead(err error) bool {
	return err != nil && (errors.Is(err, context.Canceled) || strings.Contains(err.Error(), "target closed"))
}

func (s *Session) recover() error {
	fmt.Fprintln(os.Stderr, "Reconnecting browser...")
	if err := s.openTab(); err != nil {
		return fmt.Errorf("could not reconnect browser: %w", err)
	}
	return nil
}

func (s *Session) prepareForPrompt() error {
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

func (s *Session) submitPromptWithRetry(prompt string) error {
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

// maybeSaveSession writes a session snapshot if the current chat
// URL contains a conversation ID (/c/), indicating a real conversation.
func (s *Session) maybeSaveSession() {
	if s.temporary || s.sessionFile == "" {
		return
	}

	currentURL, title, ok := currentSessionLocation(s.ctx)
	if !ok {
		return
	}
	if err := SaveSessionSnapshot(s.sessionFile, currentURL, title); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not save session: %v\n", err)
	}
}

func currentSessionLocation(ctx context.Context) (string, string, bool) {
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		currentURL, title, err := sessionLocationSnapshot(ctx)
		if err != nil {
			return "", "", false
		}
		if u, err := url.Parse(currentURL); err == nil {
			if u.Query().Get("temporary-chat") == "true" {
				return "", "", false
			}
			if strings.Contains(u.Path, "/c/") {
				return currentURL, title, true
			}
		}
		time.Sleep(300 * time.Millisecond)
	}
	return "", "", false
}

func sessionLocationSnapshot(ctx context.Context) (string, string, error) {
	var snapshot struct {
		URL   string `json:"url"`
		Title string `json:"title"`
	}

	js := `(() => {
		const normalize = value => {
			if (!value) return "";
			if (/^https?:\/\//.test(value)) return value;
			if (value.startsWith('/')) return location.origin + value;
			return "";
		};
		const convoRe = /\/(?:g\/g-[^"'\\\s<>]+\/)?c\/[a-zA-Z0-9-]+(?:[?#][^"'\\\s<>]*)?/g;
		const convoIdRe = /"(?:conversation[_-]?id|conversationId|current_conversation_id|chat_id|chatId)"\s*:\s*"([0-9a-fA-F-]{16,})"/g;
		const candidates = [];
		const push = value => {
			const normalized = normalize(value);
			if (normalized && normalized.includes('/c/')) candidates.push(normalized);
		};
		push(location.href || "");
		Array.from(document.querySelectorAll('a[href]')).forEach(a => push(a.getAttribute('href') || ""));
		const stateText = JSON.stringify(history.state || {});
		const htmlText = document.documentElement ? document.documentElement.innerHTML : "";
		const scriptText = Array.from(document.scripts || []).map(s => s.textContent || '').join('\n');
		const storageText = [...Object.values(localStorage || {}), ...Object.values(sessionStorage || {})].join('\n');
		for (const source of [stateText, htmlText, scriptText, storageText]) {
			const matches = source.match(convoRe) || [];
			for (const match of matches) push(match);
			for (const capture of source.matchAll(convoIdRe)) {
				const value = capture[1];
				push('/c/' + value);
			}
		}
		return { url: candidates.at(-1) || location.href, title: document.title };
	})()`

	if err := chromedp.Run(ctx, chromedp.Evaluate(js, &snapshot)); err != nil {
		return "", "", err
	}
	return snapshot.URL, snapshot.Title, nil
}

// KillBackgroundBrowser tells the background browser (started with --keep-browser)
// to shut down gracefully via CDP.
func KillBackgroundBrowser() error {
	wsURL, err := findBrowserWSURL(keepBrowserDebugPort, time.Second)
	if err != nil {
		return fmt.Errorf("no background browser found on port %d", keepBrowserDebugPort)
	}

	ctx, cancel := chromedp.NewRemoteAllocator(context.Background(), wsURL)
	defer cancel()

	tabCtx, tabCancel := chromedp.NewContext(ctx)
	defer tabCancel()

	return chromedp.Run(tabCtx, chromedp.ActionFunc(func(ctx context.Context) error {
		return browser.Close().Do(ctx)
	}))
}
