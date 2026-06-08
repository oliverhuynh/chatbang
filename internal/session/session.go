package session

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
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
}

// New opens a browser session and waits until the chat page is ready.
func New(browser, profileDir string, headless bool, chatTarget string) (*Session, error) {
	allocCtx, allocCancel := chromedp.NewExecAllocator(
		context.Background(),
		config.AllocatorOptions(browser, profileDir, headless)...,
	)
	s := &Session{allocCtx: allocCtx, allocCancel: allocCancel, chatURL: chatTarget}
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
	fmt.Println()
	fmt.Println(string(result))
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

func fatalChatErr(err error) {
	if err == nil {
		return
	}
	if errors.Is(err, context.Canceled) || strings.Contains(err.Error(), "browser disconnected") {
		log.Fatal("browser session ended unexpectedly (Chrome disconnected); restart chatbang-pro and try again")
	}
	log.Fatal(err)
}
