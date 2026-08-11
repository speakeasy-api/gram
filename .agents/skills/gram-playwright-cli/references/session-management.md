# Browser Session Management

Run multiple isolated browser sessions concurrently with state persistence.

## Named Browser Sessions

Use `-s` flag to isolate browser contexts:

```bash
# Browser 1: Authentication flow
mise run playwright -s=auth open https://app.example.com/login

# Browser 2: Public browsing (separate cookies, storage)
mise run playwright -s=public open https://example.com

# Commands are isolated by browser session
mise run playwright -s=auth fill e1 "user@example.com"
mise run playwright -s=public snapshot
```

## Browser Session Isolation Properties

Each browser session has independent:

- Cookies
- LocalStorage / SessionStorage
- IndexedDB
- Cache
- Browsing history
- Open tabs

## Browser Session Commands

```bash
# List all browser sessions
mise run playwright list

# Stop a browser session (close the browser)
mise run playwright close                # stop the default browser
mise run playwright -s=mysession close   # stop a named browser

# Stop all browser sessions
mise run playwright close-all

# Forcefully kill all daemon processes (for stale/zombie processes)
mise run playwright kill-all

# Delete browser session user data (profile directory)
mise run playwright delete-data                # delete default browser data
mise run playwright -s=mysession delete-data   # delete named browser data
```

## Environment Variable

Set a default browser session name via environment variable:

```bash
export PLAYWRIGHT_CLI_SESSION="mysession"
mise run playwright open example.com  # Uses "mysession" automatically
```

## Common Patterns

### Concurrent Scraping

```bash
#!/bin/bash
# Scrape multiple sites concurrently

# Start all browsers
mise run playwright -s=site1 open https://site1.com &
mise run playwright -s=site2 open https://site2.com &
mise run playwright -s=site3 open https://site3.com &
wait

# Take snapshots from each
mise run playwright -s=site1 snapshot
mise run playwright -s=site2 snapshot
mise run playwright -s=site3 snapshot

# Cleanup
mise run playwright close-all
```

### A/B Testing Sessions

```bash
# Test different user experiences
mise run playwright -s=variant-a open "https://app.com?variant=a"
mise run playwright -s=variant-b open "https://app.com?variant=b"

# Compare
mise run playwright -s=variant-a screenshot
mise run playwright -s=variant-b screenshot
```

### Persistent Profile

By default, browser profile is kept in memory only. Use `--persistent` flag on `open` to persist the browser profile to disk:

```bash
# Use persistent profile (auto-generated location)
mise run playwright open https://example.com --persistent

# Use persistent profile with custom directory
mise run playwright open https://example.com --profile=/path/to/profile
```

## Attaching to a Running Browser

Use `attach` to connect to a browser that is already running, instead of launching a new one.

### Attach by channel name

Connect to a running Chrome or Edge instance by its channel name. The browser must have remote debugging enabled — navigate to `chrome://inspect/#remote-debugging` in the target browser and check "Allow remote debugging for this browser instance".

```bash
# Attach to Chrome
mise run playwright attach --cdp=chrome

# Attach to Chrome Canary
mise run playwright attach --cdp=chrome-canary

# Attach to Microsoft Edge
mise run playwright attach --cdp=msedge

# Attach to Edge Dev
mise run playwright attach --cdp=msedge-dev
```

Supported channels: `chrome`, `chrome-beta`, `chrome-dev`, `chrome-canary`, `msedge`, `msedge-beta`, `msedge-dev`, `msedge-canary`.

When `--session` is not provided, the session is named after the channel (e.g. `--cdp=msedge` creates a session called `msedge`), so parallel attaches to Chrome and Edge don't collide on `default`. Pass `--session=<name>` to override.

### Attach via CDP endpoint

Connect to a browser that exposes a Chrome DevTools Protocol endpoint:

```bash
mise run playwright attach --cdp=http://localhost:9222
```

### Attach via browser extension

Connect to a browser with the Playwright extension installed:

```bash
mise run playwright attach --extension
```

### Detach

Tear down an attached session without affecting the external browser:

```bash
# Detach the default attached session
mise run playwright detach

# Detach a specific attached session
mise run playwright -s=msedge detach
```

`detach` only works on sessions created via `attach`. For sessions created via `open`, use `close`.

## Default Browser Session

When `-s` is omitted, commands use the default browser session:

```bash
# These use the same default browser session
mise run playwright open https://example.com
mise run playwright snapshot
mise run playwright close  # Stops default browser
```

## Browser Session Configuration

Configure a browser session with specific settings when opening:

```bash
# Open with config file
mise run playwright open https://example.com --config=.playwright/my-cli.json

# Open with specific browser
mise run playwright open https://example.com --browser=firefox

# Open in headed mode
mise run playwright open https://example.com --headed

# Open with persistent profile
mise run playwright open https://example.com --persistent
```

## Best Practices

### 1. Name Browser Sessions Semantically

```bash
# GOOD: Clear purpose
mise run playwright -s=github-auth open https://github.com
mise run playwright -s=docs-scrape open https://docs.example.com

# AVOID: Generic names
mise run playwright -s=s1 open https://github.com
```

### 2. Always Clean Up

```bash
# Stop browsers when done
mise run playwright -s=auth close
mise run playwright -s=scrape close

# Or stop all at once
mise run playwright close-all

# If browsers become unresponsive or zombie processes remain
mise run playwright kill-all
```

### 3. Delete Stale Browser Data

```bash
# Remove old browser data to free disk space
mise run playwright -s=oldsession delete-data
```
