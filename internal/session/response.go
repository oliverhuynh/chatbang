package session

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	markdown "github.com/MichaelMure/go-term-markdown"
	"github.com/chromedp/chromedp"
)

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
