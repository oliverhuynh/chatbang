# keep-browser Feature

## What it does
`--keep-browser` launches Chrome once and keeps it running in the background.
Subsequent invocations reuse the same browser instance, avoiding cold-start latency.

## How it works

### First run (no existing browser)
1. `findBrowserWSURL(39227, 1s)`  → times out (no browser)
2. `chromedp.NewExecAllocator` with same flags as normal run, plus:
   - `--remote-debugging-port=39227` — fixed port for discovery
   - `chromedp.ModifyCmdFunc` — clears `Pdeathsig=0` so Chrome survives parent exit
3. `chromedp.NewRemoteAllocator` is NOT used here — the exec allocator manages the first launch

### Subsequent runs (existing browser)
1. `findBrowserWSURL(39227, 1s)`  → succeeds, returns WS URL
2. `chromedp.NewRemoteAllocator` connects to the running browser
3. No new Chrome process is spawned

### Close behavior
In `keepBrowser` mode, `Close()` skips **both** `ctxCancel()` and `allocCancel()`:
- `ctxCancel()` would close the CDP tab (triggers Chrome to exit in some configs)
- `allocCancel()` would kill the Chrome process

### Why ModifyCmdFunc?
Chromedp's `allocate_linux.go` sets `Pdeathsig = syscall.SIGKILL` on the Chrome
process. This is an OS-level signal: when the parent (chatbang-pro) exits,
Chrome receives SIGKILL. `ModifyCmdFunc` replaces `allocateCmdOptions`, so
Pdeathsig defaults to 0 (no signal).

### --kill-browser
Connects to the browser on port 39227 via `chromedp.NewRemoteAllocator`,
sends `browser.Close()` CDP command for graceful shutdown.

## Key files
- `internal/session/session.go` — `New()`, `Close()`, `findBrowserWSURL()`, `KillBackgroundBrowser()`
- `internal/cli/cli.go` — `KeepBrowser`, `KillBrowser` options
- `internal/app/app.go` — wires flags, conditional startup message

### Background tab activation (2nd+ run fix)

On 2nd+ `--keep-browser` run, `openTab()` creates a new tab via `chromedp.NewRemoteAllocator`. 
Chrome opens this tab in the **background** (not focused). Background tabs in Chrome
defer or batch React state updates, so `#composer-submit-button` remains disabled
even after the textarea value is set and an InputEvent is dispatched.

**Fix**: `submitPrompt()` in `internal/session/session.go` now calls
`target.ActivateTarget` via CDP **before** `chromedp.Click(#composer-submit-button)`.
Activating the tab causes Chrome to process pending React state updates immediately,
enabling the submit button so the CDP click lands.

```go
chromedp.ActionFunc(func(ctx context.Context) error {
    return target.ActivateTarget(chromedp.FromContext(ctx).Target.TargetID).Do(ctx)
}),
chromedp.Click(`#composer-submit-button`, chromedp.ByID),
```

Without this, text is pasted into the textarea but never submitted (user must
manually activate the tab and click Submit).

## Key files
- `internal/session/session.go` — `New()`, `Close()`, `findBrowserWSURL()`, `KillBackgroundBrowser()`, `submitPrompt()`
- `internal/cli/cli.go` — `KeepBrowser`, `KillBrowser` options
- `internal/app/app.go` — wires flags, conditional startup message
