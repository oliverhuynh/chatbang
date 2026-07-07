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
