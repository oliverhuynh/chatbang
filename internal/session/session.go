package session

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/chromedp/chromedp"

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
	temporary   bool
	sessionFile string
}

// New opens a browser session and waits until the chat page is ready.
func New(browser, profileDir string, headless bool, chatTarget string, sessionFile string, temporary bool) (*Session, error) {
	allocCtx, allocCancel := chromedp.NewExecAllocator(
		context.Background(),
		config.AllocatorOptions(browser, profileDir, headless)...,
	)
	s := &Session{allocCtx: allocCtx, allocCancel: allocCancel, chatURL: chatTarget, sessionFile: sessionFile, temporary: temporary}
	if err := s.openTab(); err != nil {
		allocCancel()
		return nil, err
	}
	return s, nil
}

// Close shuts down the browser session.
func (s *Session) Close() {
	if s.ctxCancel != nil {
		s.ctxCancel()
	}
	if s.allocCancel != nil {
		s.allocCancel()
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

	result, peak, err := waitForResponse(s.ctx)
	s.lastPeak = peak
	if err != nil {
		fatalChatErr(err)
	}
	s.maybeSaveSession()
	fmt.Print(string(result))
}

// LoginProfile opens a visible browser for first-time setup.
func LoginProfile(browser, profileDir string) {
	fmt.Println("Opening browser for ChatGPT setup...")

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

func (s *Session) maybeSaveSession() {
	fmt.Fprintf(os.Stderr, "[debug] maybeSaveSession: temporary=%v sessionFile=%q\n", s.temporary, s.sessionFile)
	if s.sessionFile == "" {
		fmt.Fprintf(os.Stderr, "[debug] maybeSaveSession: skipping (sessionFile=%q)\n", s.sessionFile)
		return
	}

	currentURL, title, ok := currentSessionLocation(s.ctx)
	fmt.Fprintf(os.Stderr, "[debug] maybeSaveSession: currentSessionLocation returned ok=%v url=%q title=%q\n", ok, currentURL, title)
	if !ok {
		return
	}
	fmt.Fprintf(os.Stderr, "[debug] maybeSaveSession: calling SaveSessionSnapshot\n")
	if err := SaveSessionSnapshot(s.sessionFile, currentURL, title); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not save session: %v\n", err)
	}
	fmt.Fprintf(os.Stderr, "[debug] maybeSaveSession: saved successfully\n")
}

func currentSessionLocation(ctx context.Context) (string, string, bool) {
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		currentURL, title, err := sessionLocationSnapshot(ctx)
		fmt.Fprintf(os.Stderr, "[debug] currentSessionLocation: poll url=%q title=%q err=%v\n", currentURL, title, err)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[debug] currentSessionLocation: error, stopping\n")
			return "", "", false
		}
		if u, err := url.Parse(currentURL); err == nil {
			fmt.Fprintf(os.Stderr, "[debug] currentSessionLocation: parsed path=%q, hasSlashC=%v\n", u.Path, strings.Contains(u.Path, "/c/"))
			if strings.Contains(u.Path, "/c/") {
				fmt.Fprintf(os.Stderr, "[debug] currentSessionLocation: found /c/ in path, returning\n")
				return currentURL, title, true
			}
		} else {
			fmt.Fprintf(os.Stderr, "[debug] currentSessionLocation: url.Parse error: %v\n", err)
		}
		time.Sleep(300 * time.Millisecond)
	}
	fmt.Fprintf(os.Stderr, "[debug] currentSessionLocation: timed out after 6s\n")
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

		const url = normalize(location.href || "");
		if (!url || !url.includes("/c/")) {
			return { url: "", title: "" };
		}
		return { url, title: document.title || "" };
	})()`
	if err := chromedp.Run(ctx, chromedp.Evaluate(js, &snapshot)); err != nil {
		return "", "", err
	}
	if snapshot.URL == "" {
		return "", "", fmt.Errorf("no /c/ URL found in location.href")
	}
	return snapshot.URL, snapshot.Title, nil
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
