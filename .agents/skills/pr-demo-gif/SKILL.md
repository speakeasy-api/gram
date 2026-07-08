---
name: pr-demo-gif
description: Record a headless-browser demo GIF of a user-visible frontend change and post it as a PR comment
metadata:
  relevant_files:
    - "client/dashboard/**"
    - "elements/**"
---

# Demo GIFs for frontend PRs

When a PR contains a small user-visible frontend change (dropdowns, dialogs, toolbars, filters, any interaction behavior in `client/dashboard` or `elements`), record a short demo GIF of the change working and post it as a PR comment — proactively, without being asked. A 10–20 second GIF shows reviewers the behavior instantly in a way a text description of click sequences cannot.

Demo **only the changed interaction**. Do not tour the whole page.

## Prerequisites

- The local dev stack is running (via `madprocs`): the dashboard serves at `https://localhost:5173` with dev-idp auto-login.
- Use the **`ecommerce-api`** project for all flows (it is seeded and working; `default` is not). Paths like `/speakeasy/ecommerce-api/insights` redirect to `/speakeasy/projects/ecommerce-api/insights`.
- Work entirely inside the session scratchpad directory — never in the repo tree.

One-time setup in the scratchpad:

```bash
npm i playwright-core            # uses system Chrome, no browser download
npx playwright-core install ffmpeg   # needed once for recordVideo
```

## Recording pipeline

### 1. Launch and log in (throwaway context)

```js
import { chromium } from "playwright-core";

const browser = await chromium.launch({
  channel: "chrome",
  headless: true,
  args: [
    "--ignore-certificate-errors",
    "--disable-component-update",
    "--no-first-run",
  ],
});
const loginCtx = await browser.newContext({ ignoreHTTPSErrors: true });
```

**Gotcha — first load in a fresh profile fails wholesale.** Chrome's cert verifier re-initializes mid-flight (`net::ERR_CERT_VERIFIER_CHANGED` / `ERR_FAILED` on nearly all Vite module requests), leaving a blank page. Always warm up: `goto` → wait ~6s → check the page rendered → retry. Subsequent loads are stable.

Login flow: `goto("https://localhost:5173/")` → redirected to `/login` → click the **Login** button → dev-idp auto-authenticates → lands on `/speakeasy`.

**Gotcha — wait on `document.body.textContent`, NOT `innerText`.** Key UI text (including the Login button) is missing from `innerText`.

Then save auth and close the throwaway context so the recorded video starts clean, already logged in:

```js
const state = await loginCtx.storageState();
await loginCtx.close();
```

### 2. Recorded context

```js
const ctx = await browser.newContext({
  ignoreHTTPSErrors: true,
  storageState: state,
  recordVideo: { dir: "video/", size: { width: 1280, height: 800 } },
  viewport: { width: 1280, height: 800 },
});
```

Before driving the flow, clean the frame:

- **Hide the fixed docks** — the "Ask anything" and "Developer Toolkit" `position:fixed` divs. Find them by `textContent` and set `display: none`.
- **Inject a fake cursor** — headless recordings show no pointer, so overlay a fixed-position div and move it with CSS transitions before each click, with a pulse animation on click. Sketch:

```js
await page.evaluate(() => {
  const c = document.createElement("div");
  c.id = "__cursor";
  Object.assign(c.style, {
    position: "fixed",
    left: "0",
    top: "0",
    width: "20px",
    height: "20px",
    borderRadius: "50%",
    background: "rgba(255,80,80,.85)",
    border: "2px solid white",
    zIndex: "999999",
    pointerEvents: "none",
    transition: "transform .45s ease",
    transform: "translate(-100px,-100px)",
  });
  document.body.appendChild(c);
});
// before each click: move cursor, wait for transition, pulse, then click
async function clickAt(page, x, y) {
  await page.evaluate(
    ([x, y]) => {
      const c = document.getElementById("__cursor");
      c.style.transform = `translate(${x - 10}px, ${y - 10}px)`;
    },
    [x, y],
  );
  await page.waitForTimeout(550);
  await page.evaluate(() => {
    const c = document.getElementById("__cursor");
    c.animate(
      [
        { boxShadow: "0 0 0 0 rgba(255,80,80,.6)" },
        { boxShadow: "0 0 0 14px rgba(255,80,80,0)" },
      ],
      { duration: 350 },
    );
  });
  await page.mouse.click(x, y);
  await page.waitForTimeout(400);
}
```

### 3. Drive the interaction

- **Modal Radix layers set `pointer-events: none` on `<body>`**, so Playwright locator clicks fail actionability checks. Use `page.mouse.click(x, y)` with coordinates from `locator.boundingBox()` instead (which `clickAt` above already does).
- **Targets below the fold**: call `locator.scrollIntoViewIfNeeded()` _before_ reading `boundingBox()`.
- To debug why a click isn't landing, inject document-level capture+bubble listeners for `pointerdown/pointerup/mousedown/mouseup/click` that `console.log`, and relay via `page.on("console")` — this reveals e.g. clicks resolving to `<html>` under a modal layer.
- Pace the demo with short `waitForTimeout` pauses so state changes are readable; keep total runtime ~10–20s.

Close the context to flush the video: `await ctx.close()` — the `.webm` lands in `video/`.

### 4. Convert to GIF

Trim the dead time at the start (`-ss`) and convert with a two-pass palette:

```bash
mise exec ffmpeg@latest -- ffmpeg -ss <trim-seconds> -i video/<file>.webm \
  -vf "fps=10,scale=1000:-1:flags=lanczos,split[s0][s1];[s0]palettegen[p];[s1][p]paletteuse" \
  demo.gif
```

### 5. Host the GIF (secret gist)

GitHub's PR comment-attachment upload has **no API**, so host the GIF in a secret gist's git repo:

```bash
echo "demo" > placeholder.md
gh gist create placeholder.md --desc "PR demo gif"     # secret is the default; there is NO --secret flag
git clone https://gist.github.com/<gist-id>.git gist && cd gist
cp ../demo.gif .
git add demo.gif && git commit -m "add demo gif"
git -c credential.helper='!gh auth git-credential' push   # without this, push hangs on a credential prompt
```

Gotchas:

- `gh gist create` **silently fails on binary files** — always create the gist from a placeholder text file, then push the GIF via git.
- The raw URL format is `https://gist.githubusercontent.com/<user>/<gist-id>/raw/<file>.gif`.
- The gist raw URL is link-visible (like the PR itself) — fine for normal dashboard demos, but don't record anything sensitive.

### 6. Post the PR comment

```bash
gh pr comment <pr> --body "$(cat <<'EOF'
### Demo

![demo](https://gist.githubusercontent.com/<user>/<gist-id>/raw/demo.gif)

What it shows:
1. <step>
2. <step>
3. <changed behavior>
EOF
)"
```

Keep the "what it shows" list short and numbered, matching the recorded steps.
