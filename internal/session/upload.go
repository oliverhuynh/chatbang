package session

import (
	"context"
	"encoding/json"
	"fmt"
	"mime"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"
)

const attachmentReadyTimeout = 45 * time.Second

var generalUploadSelectors = []string{
	`input#upload-files[type="file"]`,
	`input[type="file"]:not([accept*="image"])`,
}

var imageUploadSelectors = []string{
	`input#upload-photos[type="file"]`,
}

// AskFreshWithFiles starts a fresh chat, attaches local files, submits the prompt,
// and returns the raw assistant text. Files must exist on the same machine as Chrome.
func (s *Session) AskFreshWithFiles(prompt string, files []string) (string, error) {
	if len(files) == 0 {
		return s.AskFresh(prompt)
	}

	resolvedFiles, err := validateUploadFiles(files)
	if err != nil {
		return "", err
	}

	fmt.Fprintf(os.Stderr, "[session] ask start fresh=true files=%d promptNL=%d\n", len(resolvedFiles), strings.Count(prompt, "\n"))
	s.mu.Lock()
	defer s.mu.Unlock()
	fmt.Fprintln(os.Stderr, "[session] ask lock acquired")
	fmt.Fprintln(os.Stderr, "[Thinking...]")

	if err := s.prepareForAsk(true); err != nil {
		fmt.Fprintf(os.Stderr, "[session] prepare failed: %v\n", err)
		return "", err
	}
	if err := s.submitPromptAndFilesWithRetry(prompt, resolvedFiles); err != nil {
		fmt.Fprintf(os.Stderr, "[session] submit failed: %v\n", err)
		return "", err
	}

	result, peakLen, err := waitForResponseText(s.ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[session] wait failed peakLen=%d: %v\n", peakLen, err)
		return "", err
	}
	s.lastPeak = peakLen
	if err := s.cleanupConversationCookies(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not clean conv_key cookies: %v\n", err)
	}
	fmt.Fprintf(os.Stderr, "[session] ask done peakLen=%d resultLen=%d files=%d\n", peakLen, len(result), len(resolvedFiles))
	return result, nil
}

func validateUploadFiles(files []string) ([]string, error) {
	resolved := make([]string, 0, len(files))
	for _, file := range files {
		if strings.TrimSpace(file) == "" {
			return nil, fmt.Errorf("attachment path is empty")
		}
		abs, err := filepath.Abs(file)
		if err != nil {
			return nil, fmt.Errorf("resolve attachment %q: %w", file, err)
		}
		info, err := os.Stat(abs)
		if err != nil {
			return nil, fmt.Errorf("attachment %q: %w", abs, err)
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("attachment %q is not a regular file", abs)
		}
		resolved = append(resolved, abs)
	}
	return resolved, nil
}

func (s *Session) submitPromptAndFilesWithRetry(prompt string, files []string) error {
	err := uploadAndSubmit(s.ctx, prompt, files)
	if err == nil {
		return nil
	}
	if !isSessionDead(err) {
		return fmt.Errorf("submit prompt with attachments: %w", err)
	}

	if err := s.recover(); err != nil {
		return err
	}
	if err := s.prepareForAskOnce(true); err != nil {
		return err
	}
	if err := uploadAndSubmit(s.ctx, prompt, files); err != nil {
		return fmt.Errorf("submit prompt with attachments after reconnect: %w", err)
	}
	return nil
}

func uploadAndSubmit(ctx context.Context, prompt string, files []string) error {
	selector, err := uploadFilesToComposer(ctx, files)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "[session] attached %d file(s) via %s\n", len(files), selector)

	if err := waitForAttachmentReady(ctx, selector, files); err != nil {
		return err
	}

	if strings.TrimSpace(prompt) != "" {
		return submitPrompt(ctx, prompt)
	}
	return submitAttachmentOnly(ctx)
}

func uploadFilesToComposer(ctx context.Context, files []string) (string, error) {
	allImages := true
	for _, file := range files {
		if !looksLikeImage(file) {
			allImages = false
			break
		}
	}

	if selector, found, err := tryUploadSelectors(ctx, generalUploadSelectors, files); found || err != nil {
		return selector, err
	}

	// The hidden input is normally present even while the + menu is closed. If a
	// ChatGPT UI revision lazily mounts it, opening the menu is a safe way to ask
	// React to render attachment controls without automating the native file dialog.
	_ = openAttachmentMenu(ctx)
	if selector, found, err := tryUploadSelectors(ctx, generalUploadSelectors, files); found || err != nil {
		return selector, err
	}

	// #upload-photos is intentionally an image-only fallback. General documents
	// must never be sent through it because the browser accept filter can reject
	// them without a useful error.
	if allImages {
		if selector, found, err := tryUploadSelectors(ctx, imageUploadSelectors, files); found || err != nil {
			return selector, err
		}
	}

	return "", fmt.Errorf("ChatGPT attachment file input was not found (#upload-files)")
}

func tryUploadSelectors(ctx context.Context, selectors, files []string) (string, bool, error) {
	for _, selector := range selectors {
		exists, err := uploadSelectorExists(ctx, selector)
		if err != nil {
			return "", false, err
		}
		if !exists {
			continue
		}
		if err := chromedp.Run(ctx, chromedp.SetUploadFiles(selector, files, chromedp.ByQuery)); err != nil {
			return selector, true, fmt.Errorf("set upload files on %s: %w", selector, err)
		}
		count, err := uploadInputFileCount(ctx, selector)
		if err != nil {
			return selector, true, err
		}
		if count != len(files) {
			return selector, true, fmt.Errorf("ChatGPT file input accepted %d of %d selected file(s)", count, len(files))
		}
		return selector, true, nil
	}
	return "", false, nil
}

func uploadSelectorExists(ctx context.Context, selector string) (bool, error) {
	selectorJSON, err := json.Marshal(selector)
	if err != nil {
		return false, err
	}
	var exists bool
	if err := chromedp.Run(ctx, chromedp.Evaluate(fmt.Sprintf(`!!document.querySelector(%s)`, selectorJSON), &exists)); err != nil {
		return false, err
	}
	return exists, nil
}

func uploadInputFileCount(ctx context.Context, selector string) (int, error) {
	selectorJSON, err := json.Marshal(selector)
	if err != nil {
		return 0, err
	}
	var count int
	js := fmt.Sprintf(`(() => {
		const input = document.querySelector(%s);
		return input && input.files ? input.files.length : -1;
	})()`, selectorJSON)
	if err := chromedp.Run(ctx, chromedp.Evaluate(js, &count)); err != nil {
		return 0, err
	}
	if count < 0 {
		return 0, fmt.Errorf("ChatGPT attachment file input disappeared after selection")
	}
	return count, nil
}

func openAttachmentMenu(ctx context.Context) error {
	const js = `(() => {
		const button = document.querySelector('[data-testid="composer-plus-btn"]') ||
			[...document.querySelectorAll('button')].find(btn =>
				(btn.getAttribute('aria-label') || '').toLowerCase().includes('add files'));
		if (!button) return false;
		button.click();
		return true;
	})()`
	var clicked bool
	if err := chromedp.Run(ctx, chromedp.Evaluate(js, &clicked)); err != nil {
		return err
	}
	if clicked {
		time.Sleep(250 * time.Millisecond)
	}
	return nil
}

func waitForAttachmentReady(ctx context.Context, selector string, files []string) error {
	// DOM.setFileInputFiles reliably populates input.files. ChatGPT still needs to
	// observe the selection and finish its own background upload. Give React a
	// short grace period first; only dispatch input/change if no attachment UI
	// evidence appears, avoiding duplicate upload handlers on healthy builds.
	fallbackAt := time.Now().Add(1500 * time.Millisecond)
	deadline := time.Now().Add(attachmentReadyTimeout)
	fallbackDispatched := false
	var state composerUploadState

	for time.Now().Before(deadline) {
		var err error
		state, err = attachmentComposerState(ctx)
		if err != nil {
			return err
		}
		if state.Ready {
			return nil
		}
		if uploadErrorText(state.StatusText) != "" {
			return fmt.Errorf("ChatGPT attachment upload failed: %s", uploadErrorText(state.StatusText))
		}

		if !fallbackDispatched && time.Now().After(fallbackAt) && !composerMentionsFiles(state.ComposerText, files) {
			if err := dispatchUploadEvents(ctx, selector); err != nil {
				return err
			}
			fallbackDispatched = true
			fmt.Fprintln(os.Stderr, "[session] attachment UI not observed; dispatched file input events fallback")
		}
		time.Sleep(250 * time.Millisecond)
	}

	if strings.TrimSpace(state.StatusText) != "" {
		return fmt.Errorf("timed out waiting for ChatGPT attachment upload: %s", strings.TrimSpace(state.StatusText))
	}
	return fmt.Errorf("timed out waiting for ChatGPT attachment upload to enable the send button")
}

type composerUploadState struct {
	SendPresent bool   `json:"sendPresent"`
	Ready       bool   `json:"ready"`
	StatusText  string `json:"statusText"`
	ComposerText string `json:"composerText"`
}

func attachmentComposerState(ctx context.Context) (composerUploadState, error) {
	const js = `(() => {
		const send = document.querySelector('#composer-submit-button') || document.querySelector('[data-testid="send-button"]');
		const prompt = document.querySelector('#prompt-textarea');
		const composer = prompt && (prompt.closest('form') || prompt.parentElement?.parentElement || prompt.parentElement);
		const statusNodes = [
			...document.querySelectorAll('[role="alert"]'),
			...document.querySelectorAll('[role="dialog"]'),
			...document.querySelectorAll('[data-sonner-toast]')
		];
		const statusText = statusNodes.map(node => (node.innerText || node.textContent || '').trim())
			.filter(Boolean).join(' | ').slice(0, 500);
		return {
			sendPresent: !!send,
			ready: !!send && !send.disabled && send.getAttribute('aria-disabled') !== 'true',
			statusText,
			composerText: composer ? (composer.innerText || composer.textContent || '').slice(0, 2000) : ''
		};
	})()`
	var state composerUploadState
	if err := chromedp.Run(ctx, chromedp.Evaluate(js, &state)); err != nil {
		return composerUploadState{}, err
	}
	return state, nil
}

func composerMentionsFiles(text string, files []string) bool {
	lower := strings.ToLower(text)
	for _, file := range files {
		name := strings.ToLower(filepath.Base(file))
		if name != "" && strings.Contains(lower, name) {
			return true
		}
	}
	return false
}

func dispatchUploadEvents(ctx context.Context, selector string) error {
	selectorJSON, err := json.Marshal(selector)
	if err != nil {
		return err
	}
	js := fmt.Sprintf(`(() => {
		const input = document.querySelector(%s);
		if (!input) return false;
		input.dispatchEvent(new Event('input', { bubbles: true, composed: true }));
		input.dispatchEvent(new Event('change', { bubbles: true }));
		return true;
	})()`, selectorJSON)
	var dispatched bool
	if err := chromedp.Run(ctx, chromedp.Evaluate(js, &dispatched)); err != nil {
		return err
	}
	if !dispatched {
		return fmt.Errorf("ChatGPT attachment file input disappeared before event fallback")
	}
	return nil
}

func uploadErrorText(text string) string {
	trimmed := strings.TrimSpace(text)
	lower := strings.ToLower(trimmed)
	for _, needle := range []string{
		"upload failed",
		"failed to upload",
		"unable to upload",
		"file is too large",
		"file too large",
		"unsupported file",
		"not supported",
		"upload limit",
		"too many files",
	} {
		if strings.Contains(lower, needle) {
			return trimmed
		}
	}
	return ""
}

func submitAttachmentOnly(ctx context.Context) error {
	c := chromedp.FromContext(ctx)
	if c == nil || c.Target == nil {
		return fmt.Errorf("browser target is unavailable")
	}
	if err := target.ActivateTarget(c.Target.TargetID).Do(ctx); err != nil {
		return err
	}

	const js = `(() => {
		const send = document.querySelector('#composer-submit-button') || document.querySelector('[data-testid="send-button"]');
		if (!send || send.disabled || send.getAttribute('aria-disabled') === 'true') return false;
		send.click();
		return true;
	})()`
	var clicked bool
	if err := chromedp.Run(ctx, chromedp.Evaluate(js, &clicked)); err != nil {
		return err
	}
	if !clicked {
		return fmt.Errorf("ChatGPT send button was not ready for attachment-only message")
	}
	return nil
}

func looksLikeImage(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	if ext == "" {
		return false
	}
	mimeType := mime.TypeByExtension(ext)
	return strings.HasPrefix(strings.ToLower(mimeType), "image/")
}
