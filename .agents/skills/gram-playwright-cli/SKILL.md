---
name: gram-playwright-cli
description: Use when automating the Gram dashboard in a browser, capturing screenshots, inspecting pages.
---

# Browser Automation with Playwright CLI in Gram

## Quick start

```bash
# open new browser
mise run playwright open
# navigate to a page
mise run playwright goto https://playwright.dev
# interact with the page using refs from the snapshot
mise run playwright click e15
mise run playwright type "page.click"
mise run playwright press Enter
# take a screenshot (rarely used, as snapshot is more common)
mise run playwright screenshot
# close the browser
mise run playwright close
```

## Commands

### Core

```bash
mise run playwright open
# open and navigate right away
mise run playwright open https://example.com/
mise run playwright goto https://playwright.dev
mise run playwright type "search query"
mise run playwright click e3
mise run playwright dblclick e7
# --submit presses Enter after filling the element
mise run playwright fill e5 "user@example.com"  --submit
mise run playwright drag e2 e8
# drop files or data onto an element (from outside the page)
mise run playwright drop e4 --path=./image.png
mise run playwright drop e4 --data="text/plain=hello world"
mise run playwright hover e4
mise run playwright select e9 "option-value"
mise run playwright upload ./document.pdf
mise run playwright check e12
mise run playwright uncheck e12
mise run playwright snapshot
# search the snapshot for text or a regexp, returns matching nodes with surrounding context
mise run playwright find "Sign in"
mise run playwright find --regex "Sign (in|up)"
# wrap the regexp in slashes to add flags, e.g. /i for case-insensitive
mise run playwright find --regex "/sign (in|up)/i"
mise run playwright eval "document.title"
mise run playwright eval "el => el.textContent" e5
# get element id, class, or any attribute not visible in the snapshot
mise run playwright eval "el => el.id" e5
mise run playwright eval "el => el.getAttribute('data-testid')" e5
mise run playwright dialog-accept
mise run playwright dialog-accept "confirmation text"
mise run playwright dialog-dismiss
mise run playwright resize 1920 1080
mise run playwright close
```

### Navigation

```bash
mise run playwright go-back
mise run playwright go-forward
mise run playwright reload
```

### Keyboard

```bash
mise run playwright press Enter
mise run playwright press ArrowDown
mise run playwright keydown Shift
mise run playwright keyup Shift
```

### Mouse

```bash
mise run playwright mousemove 150 300
mise run playwright mousedown
mise run playwright mousedown right
mise run playwright mouseup
mise run playwright mouseup right
mise run playwright mousewheel 0 100
```

### Save as

```bash
mise run playwright screenshot
mise run playwright screenshot e5
mise run playwright screenshot --filename=page.png
mise run playwright screenshot --hires
mise run playwright pdf --filename=page.pdf
```

### Tabs

```bash
mise run playwright tab-list
mise run playwright tab-new
mise run playwright tab-new https://example.com/page
mise run playwright tab-close
mise run playwright tab-close 2
mise run playwright tab-select 0
```

### Storage

```bash
mise run playwright state-save
mise run playwright state-save auth.json
mise run playwright state-load auth.json

# Cookies
mise run playwright cookie-list
mise run playwright cookie-list --domain=example.com
mise run playwright cookie-get session_id
mise run playwright cookie-set session_id abc123
mise run playwright cookie-set session_id abc123 --domain=example.com --httpOnly --secure
mise run playwright cookie-delete session_id
mise run playwright cookie-clear

# LocalStorage
mise run playwright localstorage-list
mise run playwright localstorage-get theme
mise run playwright localstorage-set theme dark
mise run playwright localstorage-delete theme
mise run playwright localstorage-clear

# SessionStorage
mise run playwright sessionstorage-list
mise run playwright sessionstorage-get step
mise run playwright sessionstorage-set step 3
mise run playwright sessionstorage-delete step
mise run playwright sessionstorage-clear
```

### Network

```bash
mise run playwright route "**/*.jpg" --status=404
mise run playwright route "https://api.example.com/**" --body='{"mock": true}'
mise run playwright route-list
mise run playwright unroute "**/*.jpg"
mise run playwright unroute
```

### DevTools

```bash
mise run playwright console
mise run playwright console warning
mise run playwright requests
mise run playwright request 5
mise run playwright run-code "async page => await page.context().grantPermissions(['geolocation'])"
mise run playwright run-code --filename=script.js
mise run playwright tracing-start
mise run playwright tracing-stop
mise run playwright video-start video.webm
mise run playwright video-chapter "Chapter Title" --description="Details" --duration=2000
mise run playwright video-stop

# annotate each subsequent action (click, type, ...) with a callout naming the action and highlighting the target
# --cursor accepts pointer (the default) or none
mise run playwright video-show-actions --duration=600 --position=top-right --cursor=pointer
mise run playwright video-hide-actions

# launch the dashboard for UI review / design feedback — user annotates the page, you receive the annotated screenshot, snapshot, and notes
mise run playwright show --annotate

# generate a Playwright locator for an element from its ref or selector
mise run playwright generate-locator e5 --raw

# show a persistent highlight overlay for an element, optionally with a custom style
mise run playwright highlight e5
mise run playwright highlight e5 --style="outline: 3px dashed red"
# hide a single element highlight, or all page highlights when no target is given
mise run playwright highlight e5 --hide
mise run playwright highlight --hide
```

## Raw output

The global `--raw` option strips page status, generated code, and snapshot sections from the output, returning only the result value. Use it to pipe command output into other tools. Commands that don't produce output return nothing.

```bash
mise run playwright --raw eval "JSON.stringify(performance.timing)" | jq '.loadEventEnd - .navigationStart'
mise run playwright --raw eval "JSON.stringify([...document.querySelectorAll('a')].map(a => a.href))" > links.json
mise run playwright --raw snapshot > before.yml
mise run playwright click e5
mise run playwright --raw snapshot > after.yml
diff before.yml after.yml
TOKEN=$(mise run playwright --raw cookie-get session_id)
mise run playwright --raw localstorage-get theme
```

For structured output wrapping every reply as JSON, pass --json

```bash
mise run playwright list --json
```

## Open parameters

```bash
# Use specific browser when creating session
mise run playwright open --browser=chrome
mise run playwright open --browser=firefox
mise run playwright open --browser=webkit
mise run playwright open --browser=msedge

# Emulate a generic mobile device (Pixel 10 for Chromium, iPhone 17 for WebKit).
# Prefer this when a mobile layout is acceptable: mobile pages are usually
# lighter, so snapshots are smaller and cheaper.
mise run playwright open --mobile
mise run playwright open --device="iPhone 15"

# Use persistent profile (by default profile is in-memory)
mise run playwright open --persistent
# Use persistent profile with custom directory
mise run playwright open --profile=/path/to/profile

# Connect to browser via Playwright Extension
mise run playwright attach --extension=chrome

# Connect to a running Chrome or Edge by channel name
mise run playwright attach --cdp=chrome
mise run playwright attach --cdp=msedge

# Connect to a running browser via CDP endpoint
mise run playwright attach --cdp=http://localhost:9222

# Start with config file
mise run playwright open --config=my-config.json

# Close the browser
mise run playwright close
# Detach from an attached browser (leaves the external browser running)
mise run playwright -s=msedge detach
# Delete user data for the default session
mise run playwright delete-data
```

## URLs with `&` on Windows

On Windows, `cmd.exe` and PowerShell treat `&` as a command separator, so URLs with multiple query parameters get truncated before `mise run playwright` runs. Escape `&` with `^&` in `cmd.exe`, or use `--%` in PowerShell:

```batch
mise run playwright goto "https://example.com/?a=1^&b=2"
```

```powershell
mise run playwright --% goto "https://example.com/?a=1&b=2"
```

## Snapshots

After each command, mise run playwright provides a snapshot of the current browser state.

```bash
> mise run playwright goto https://example.com
### Page
- Page URL: https://example.com/
- Page Title: Example Domain
### Snapshot
[Snapshot](.playwright-cli/page-2026-02-14T19-22-42-679Z.yml)
```

You can also take a snapshot on demand using `mise run playwright snapshot` command. All the options below can be combined as needed.

```bash
# default - save to a file with timestamp-based name
mise run playwright snapshot

# save to file, use when snapshot is a part of the workflow result
mise run playwright snapshot --filename=after-click.yaml

# snapshot an element instead of the whole page
mise run playwright snapshot "#main"

# limit snapshot depth for efficiency, take a partial snapshot afterwards
mise run playwright snapshot --depth=4
mise run playwright snapshot e34

# include each element's bounding box as [box=x,y,width,height]
mise run playwright snapshot --boxes

# search a large snapshot instead of capturing it all — returns matching nodes
# with 3 lines of context around each match (like grep -C)
mise run playwright find "Add to cart"
mise run playwright find --regex "\\$[0-9]+\\.[0-9]{2}"
```

## Targeting elements

By default, use refs from the snapshot to interact with page elements.

```bash
# get snapshot with refs
mise run playwright snapshot

# interact using a ref
mise run playwright click e15
```

You can also use css selectors or Playwright locators.

```bash
# css selector
mise run playwright click "#main > button.submit"

# role locator
mise run playwright click "getByRole('button', { name: 'Submit' })"

# test id
mise run playwright click "getByTestId('submit-button')"
```

## Browser Sessions

```bash
# create new browser session named "mysession" with persistent profile
mise run playwright -s=mysession open example.com --persistent
# same with manually specified profile directory (use when requested explicitly)
mise run playwright -s=mysession open example.com --profile=/path/to/profile
mise run playwright -s=mysession click e6
mise run playwright -s=mysession close  # stop a named browser
mise run playwright -s=mysession delete-data  # delete user data for persistent session

mise run playwright list
# Close all browsers
mise run playwright close-all
# Forcefully kill all browser processes
mise run playwright kill-all
```

## Gram repository setup

Run browser automation through `mise run playwright`; the task supplies the repository config and installs Chromium on first use. Use existing `pnpm` package scripts for repository checks. Gram does not install `@playwright/test`, so do not bootstrap a Playwright test suite unless the user explicitly asks.

```bash
mise run playwright --help
mise run playwright:install-browser --help
```

Do not use `npm`, `npx`, or `yarn` in this repository.

## Example: Form submission

```bash
mise run playwright open https://example.com/form
mise run playwright snapshot

mise run playwright fill e1 "user@example.com"
mise run playwright fill e2 "password123"
mise run playwright click e3
mise run playwright snapshot
mise run playwright close
```

## Example: Multi-tab workflow

```bash
mise run playwright open https://example.com
mise run playwright tab-new https://example.com/other
mise run playwright tab-list
mise run playwright tab-select 0
mise run playwright snapshot
mise run playwright close
```

## Example: Debugging with DevTools

```bash
mise run playwright open https://example.com
mise run playwright click e4
mise run playwright fill e7 "test"
mise run playwright console
mise run playwright requests
mise run playwright close
```

```bash
mise run playwright open https://example.com
mise run playwright tracing-start
mise run playwright click e4
mise run playwright fill e7 "test"
mise run playwright tracing-stop
mise run playwright close
```

## Example: Interactive session

Ask the user for UI review or design feedback. The user draws boxes on the live page and types comments; you receive the annotated screenshot, the snapshot of the marked region, and the user's notes. Use this whenever the user asks for "UI review", "design feedback", or to "ask the user what they think / want / mean":

```bash
mise run playwright open https://example.com
mise run playwright show --annotate
```

## Specific tasks

- **Request mocking** [references/request-mocking.md](references/request-mocking.md)
- **Running Playwright code** [references/running-code.md](references/running-code.md)
- **Browser session management** [references/session-management.md](references/session-management.md)
- **Storage state (cookies, localStorage)** [references/storage-state.md](references/storage-state.md)
- **Tracing** [references/tracing.md](references/tracing.md)
- **Video recording** [references/video-recording.md](references/video-recording.md)
- **Inspecting element attributes** [references/element-attributes.md](references/element-attributes.md)
