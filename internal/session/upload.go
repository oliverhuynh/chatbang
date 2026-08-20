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

	cdpinput "github.com/chromedp/cdproto/input"
	"github.com/chromedp/chromedp"
)

const (
	attachmentObservedTimeout = 15 * time.Second
	attachmentSendTimeout     = 90 * time.Second
)

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
		if info.Size() == 0 {
			return nil, fmt.Errorf("attachment %q is empty (0 bytes)", abs)
		}
		if err := makeStagedAttachmentReadable(abs); err != nil {
			return nil, err
		}
		fmt.Fprintf(os.Stderr, "[session] attachment ready name=%q bytes=%d\n", filepath.Base(abs), info.Size())
		resolved = append(resolved, abs)
	}
	return resolved, nil
}

func makeStagedAttachmentReadable(path string) error {
	dir := filepath.Dir(path)
	if !strings.HasPrefix(filepath.Base(dir), "chatbang-upload-") {
		return nil
	}

	// Server-materialized attachments live in a private MkdirTemp directory and
	// are written mode 0600. Chrome may run under a different OS user, so CDP can
	// select the path while the browser itself cannot read the bytes; in that case
	// input.files exposes the filename but reports size=0. Allow traversal of only
	// the ephemeral staging directory and read access to the staged file.
	if err := os.Chmod(dir, 0o711); err != nil {
		return fmt.Errorf("make staged attachment directory browser-readable: %w", err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		return fmt.Errorf("make staged attachment browser-readable: %w", err)
	}
	return nil
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
	// Finish editing the composer before starting file upload. ChatGPT renders the
	// attachment card before background upload/processing is complete; mutating the
	// editor during that window can reset the in-flight attachment state.
	if strings.TrimSpace(prompt) != "" {
		if err := insertAttachmentPrompt(ctx, prompt); err != nil {
			return err
		}
	}

	selector, err := uploadFilesToComposer(ctx, files)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "[session] selected %d attachment file(s) via %s\n", len(files), selector)

	// SetUploadFiles can itself trigger ChatGPT's upload lifecycle. Observe first
	// and only synthesize input/change as a fallback when no attachment UI appears.
	// This avoids starting the same upload twice on Chrome builds that already emit
	// the relevant file-input lifecycle from DOM.setFileInputFiles.
	if err := waitForAttachmentObserved(ctx, selector, files); err != nil {
		return err
	}

	// Do not mutate the prompt after upload starts. ChatGPT keeps send disabled while
	// the attachment is uploading/processing, so this waits for the real pipeline.
	if err := waitForAttachmentSendReady(ctx, files); err != nil {
		return err
	}
	return submitAttachmentTurn(ctx, prompt)
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
		if err := verifyBrowserUploadFileSizes(ctx, selector, files); err != nil {
			return selector, true, err
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

func verifyBrowserUploadFileSizes(ctx context.Context, selector string, files []string) error {
	selectorJSON, err := json.Marshal(selector)
	if err != nil {
		return err
	}
	var observed []int64
	js := fmt.Sprintf(`(() => {
		const input = document.querySelector(%s);
		if (!input || !input.files) return null;
		return Array.from(input.files, file => file.size);
	})()`, selectorJSON)
	if err := chromedp.Run(ctx, chromedp.Evaluate(js, &observed)); err != nil {
		return err
	}
	if len(observed) != len(files) {
		return fmt.Errorf("Chrome exposes size metadata for %d of %d selected attachment(s)", len(observed), len(files))
	}
	for i, file := range files {
		info, err := os.Stat(file)
		if err != nil {
			return fmt.Errorf("stat staged attachment %q after browser selection: %w", file, err)
		}
		if observed[i] != info.Size() {
			return fmt.Errorf(
				"Chrome sees attachment %q as %d bytes, but ChatBang staged %d bytes; Chrome cannot read the staged path (check shared filesystem/permissions between ChatBang and Chrome)",
				filepath.Base(file), observed[i], info.Size(),
			)
		}
		fmt.Fprintf(os.Stderr, "[session] browser attachment size verified name=%q bytes=%d\n", filepath.Base(file), observed[i])
	}
	return nil
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

func waitForAttachmentObserved(ctx context.Context, selector string, files []string) error {
	fallbackAt := time.Now().Add(1500 * time.Millisecond)
	deadline := time.Now().Add(attachmentObservedTimeout)
	fallbackDispatched := false
	var state composerUploadState

	for time.Now().Before(deadline) {
		var err error
		state, err = attachmentComposerState(ctx, files)
		if err != nil {
			return err
		}
		if uploadErrorText(state.StatusText) != "" {
			return fmt.Errorf("ChatGPT attachment upload failed: %s", uploadErrorText(state.StatusText))
		}
		if state.AttachmentsObserved {
			fmt.Fprintf(os.Stderr, "[session] ChatGPT attachment UI observed count=%d\n", state.AttachmentCount)
			return nil
		}
		if !fallbackDispatched && time.Now().After(fallbackAt) {
			if err := dispatchUploadEvents(ctx, selector); err != nil {
				return err
			}
			fallbackDispatched = true
			fmt.Fprintln(os.Stderr, "[session] attachment UI not observed; dispatched file input events fallback")
		}
		time.Sleep(200 * time.Millisecond)
	}

	if strings.TrimSpace(state.StatusText) != "" {
		return fmt.Errorf("timed out waiting for ChatGPT to accept attachment: %s", strings.TrimSpace(state.StatusText))
	}
	return fmt.Errorf("timed out waiting for ChatGPT attachment UI after selecting %d file(s)", len(files))
}

func insertAttachmentPrompt(ctx context.Context, prompt string) error {
	fmt.Fprintf(os.Stderr, "[session] insert attachment prompt nl=%d: %s\n", strings.Count(prompt, "\n"), quotedPreview(prompt, 800))
	if err := chromedp.Run(ctx,
		chromedp.WaitVisible(`#prompt-textarea`, chromedp.ByID),
		chromedp.Click(`#prompt-textarea`, chromedp.ByID),
		chromedp.ActionFunc(func(ctx context.Context) error {
			return cdpinput.InsertText(prompt).Do(ctx)
		}),
		chromedp.Sleep(400*time.Millisecond),
	); err != nil {
		return fmt.Errorf("insert prompt text: %w", err)
	}

	observed, err := attachmentPromptText(ctx)
	if err != nil {
		return err
	}
	if strings.TrimSpace(observed) == "" {
		// Rare fallback for builds where CDP Input.insertText does not update the
		// controlled editor. This mirrors normal browser input events and is only
		// used after verifying the primary insert produced no visible text.
		promptJSON, _ := json.Marshal(prompt)
		js := fmt.Sprintf(`(() => {
			const el = document.querySelector('#prompt-textarea');
			if (!el) return false;
			el.focus();
			if (el instanceof HTMLTextAreaElement || el instanceof HTMLInputElement) {
				el.value = %s;
				el.dispatchEvent(new InputEvent('input', { bubbles: true, data: %s, inputType: 'insertFromPaste' }));
				el.dispatchEvent(new Event('change', { bubbles: true }));
				return true;
			}
			el.textContent = %s;
			el.dispatchEvent(new InputEvent('input', { bubbles: true, data: %s, inputType: 'insertFromPaste' }));
			return true;
		})()`, promptJSON, promptJSON, promptJSON, promptJSON)
		var applied bool
		if err := chromedp.Run(ctx, chromedp.Evaluate(js, &applied), chromedp.Sleep(300*time.Millisecond)); err != nil {
			return fmt.Errorf("fallback prompt insertion: %w", err)
		}
		if !applied {
			return fmt.Errorf("prompt textarea disappeared during fallback insertion")
		}
		observed, err = attachmentPromptText(ctx)
		if err != nil {
			return err
		}
	}

	observedRunes := len([]rune(observed))
	promptRunes := len([]rune(prompt))
	if strings.TrimSpace(observed) == "" {
		return fmt.Errorf("prompt insertion completed but ChatGPT editor stayed empty")
	}
	if promptRunes >= 50000 && observedRunes < promptRunes-2000 {
		return fmt.Errorf("prompt appears truncated in ChatGPT composer: expected about %d runes, observed %d", promptRunes, observedRunes)
	}
	return nil
}

func attachmentPromptText(ctx context.Context) (string, error) {
	const js = `(() => {
		const el = document.querySelector('#prompt-textarea');
		if (!el) return '';
		if (el instanceof HTMLTextAreaElement || el instanceof HTMLInputElement) return el.value || '';
		return el.innerText || el.textContent || '';
	})()`
	var value string
	if err := chromedp.Run(ctx, chromedp.Evaluate(js, &value)); err != nil {
		return "", err
	}
	return value, nil
}

func waitForAttachmentSendReady(ctx context.Context, files []string) error {
	deadline := time.Now().Add(attachmentSendTimeout)
	var lostAt time.Time
	var state composerUploadState

	for time.Now().Before(deadline) {
		var err error
		state, err = attachmentComposerState(ctx, files)
		if err != nil {
			return err
		}
		if uploadErrorText(state.StatusText) != "" {
			return fmt.Errorf("ChatGPT attachment upload failed: %s", uploadErrorText(state.StatusText))
		}
		if state.AttachmentsObserved {
			lostAt = time.Time{}
			if state.SendReady {
				fmt.Fprintln(os.Stderr, "[session] attachments retained and send button enabled")
				return nil
			}
		} else {
			if lostAt.IsZero() {
				lostAt = time.Now()
			} else if time.Since(lostAt) > 3*time.Second {
				return fmt.Errorf("ChatGPT attachment disappeared before the message became sendable")
			}
		}
		time.Sleep(250 * time.Millisecond)
	}

	if strings.TrimSpace(state.StatusText) != "" {
		return fmt.Errorf("timed out waiting for attachment send readiness: %s", strings.TrimSpace(state.StatusText))
	}
	return fmt.Errorf("timed out after %s waiting for ChatGPT to enable send with %d attachment(s)", attachmentSendTimeout, len(files))
}

type composerUploadState struct {
	SendPresent         bool   `json:"sendPresent"`
	SendReady           bool   `json:"sendReady"`
	AttachmentsObserved bool   `json:"attachmentsObserved"`
	AttachmentCount     int    `json:"attachmentCount"`
	StatusText          string `json:"statusText"`
}

func attachmentComposerState(ctx context.Context, files []string) (composerUploadState, error) {
	expected := make([]string, 0, len(files))
	for _, file := range files {
		expected = append(expected, filepath.Base(file))
	}
	expectedJSON, err := json.Marshal(expected)
	if err != nil {
		return composerUploadState{}, err
	}

	js := fmt.Sprintf(`(() => {
		const expectedNames = %s.map(value => String(value || '').toLowerCase().replace(/\s+/g, ' ').trim());
		const send = document.querySelector('#composer-submit-button') || document.querySelector('button[data-testid="send-button"]');
		const prompt = document.querySelector('#prompt-textarea');
		const composer = prompt?.closest?.('form') || send?.closest?.('form') || prompt?.parentElement?.parentElement || document.body;
		const normalize = value => String(value || '').toLowerCase().replace(/\s+/g, ' ').trim();
		const stemOf = name => name.replace(/\.[a-z0-9]{1,10}$/i, '');
		const extensionOf = name => (name.match(/(\.[a-z0-9]{1,10})$/i) || [,''])[1];
		const matchesName = (label, name) => {
			label = normalize(label);
			name = normalize(name);
			if (!label || !name) return false;
			if (label.includes(name)) return true;
			const stem = stemOf(name);
			const ext = extensionOf(name);
			if (stem.length >= 4 && label.includes(stem) && (!ext || label.includes(ext))) return true;
			const prefix = stem.slice(0, Math.min(8, stem.length));
			const suffix = ext || name.slice(-4);
			return prefix.length >= 4 && label.includes(prefix) && suffix && label.includes(suffix);
		};
		const selectors = [
			'[role="group"][aria-label]',
			'[data-testid*="chip"]',
			'[data-testid*="attachment"]',
			'[data-testid*="upload"]',
			'[data-testid*="file"]',
			'[aria-label*="Remove file" i]',
			'[aria-label*="Remove attachment" i]'
		];
		const nodes = [];
		const seen = new Set();
		for (const node of Array.from(composer.querySelectorAll(selectors.join(',')))) {
			if (!(node instanceof HTMLElement)) continue;
			if (node.closest('textarea,[contenteditable="true"]')) continue;
			if (seen.has(node)) continue;
			seen.add(node);
			nodes.push(node);
		}
		const labelFor = node => {
			const values = [];
			for (const el of [node, node.parentElement, node.parentElement?.parentElement]) {
				if (!el) continue;
				values.push(el.getAttribute?.('aria-label') || '');
				values.push(el.getAttribute?.('title') || '');
				values.push(el.getAttribute?.('data-testid') || '');
				values.push(el.innerText || el.textContent || '');
			}
			return normalize(values.join(' '));
		};
		const labels = nodes.map(labelFor);
		const namesReady = expectedNames.every(name => labels.some(label => matchesName(label, name)));
		const removeNodes = Array.from(composer.querySelectorAll('[aria-label*="Remove file" i], [aria-label*="Remove attachment" i]'));
		const removeCount = new Set(removeNodes).size;
		const countReady = expectedNames.length > 0 && removeCount >= expectedNames.length;
		const attachmentCount = Math.max(removeCount, namesReady ? expectedNames.length : 0);
		const statusNodes = [
			...document.querySelectorAll('[role="alert"]'),
			...document.querySelectorAll('[role="dialog"]'),
			...document.querySelectorAll('[data-sonner-toast]')
		];
		const statusText = statusNodes.map(node => (node.innerText || node.textContent || '').trim())
			.filter(Boolean).join(' | ').slice(0, 500);
		const isVisible = node => {
			if (!(node instanceof HTMLElement)) return false;
			const rect = node.getBoundingClientRect();
			const style = getComputedStyle(node);
			return rect.width > 0 && rect.height > 0 && style.display !== 'none' && style.visibility !== 'hidden';
		};
		const sendReady = !!send && isVisible(send) && !send.disabled &&
			send.getAttribute('aria-disabled') !== 'true' && send.getAttribute('data-disabled') !== 'true' &&
			getComputedStyle(send).pointerEvents !== 'none';
		return {
			sendPresent: !!send,
			sendReady,
			attachmentsObserved: namesReady || countReady,
			attachmentCount,
			statusText
		};
	})()`, expectedJSON)

	var state composerUploadState
	if err := chromedp.Run(ctx, chromedp.Evaluate(js, &state)); err != nil {
		return composerUploadState{}, err
	}
	return state, nil
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

func submitAttachmentTurn(ctx context.Context, prompt string) error {
	const selector = `#composer-submit-button, button[data-testid="send-button"]`
	if err := chromedp.Run(ctx, chromedp.Click(selector, chromedp.ByQuery)); err != nil {
		return fmt.Errorf("submit attachment turn via send button: %w", err)
	}
	fmt.Fprintln(os.Stderr, "[session] attachment turn submitted via send button")
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