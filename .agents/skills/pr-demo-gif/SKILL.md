---
name: pr-demo-gif
description: Use the Playwright MCP browser to capture a demo (screenshots or a GIF recording) of a user-visible frontend change and post it as a PR comment
metadata:
  relevant_files:
    - "client/dashboard/**"
    - "elements/**"
---

# Demos for frontend PRs

When a PR contains a user-visible frontend change (dropdowns, dialogs, toolbars, filters, any interaction behavior in `client/dashboard` or `elements`), capture a short demo of the change working and post it as a PR comment — proactively, without being asked. It shows reviewers the behavior instantly in a way a text description of click sequences cannot.

Demo **only the changed interaction**. Do not tour the whole page.

Pick the format:

- **Static change** (styling, layout, a new panel or empty state) → one or two screenshots via `browser_take_screenshot`.
- **Interaction change** (something opens, filters, animates, reorders) → a 10–20 second GIF made from the session video.

## Prerequisites

- Discover the dashboard URL with `mise run zero:summary` — read the address from the **Gram dashboard** row (don't assume a port). The same table shows whether each service is RUNNING; if the dashboard or server is STOPPED, start the stack with `madprocs` first. Dev-idp auto-login is enabled, and the local TLS cert is browser-trusted (mkcert CA in the NSS store, set up by `mise run zero:tls` — rerun that if you see cert errors).
- Use the **`ecommerce-api`** project for all flows (`default` is empty). Before recording, verify the database is seeded by probing it directly (the connection string is in the **Database** row of `zero:summary`):

  ```bash
  psql "<database-url>" -c "SELECT p.slug, om.gram_account_type FROM projects p JOIN organization_metadata om ON om.id = p.organization_id WHERE p.slug = 'ecommerce-api' AND NOT p.deleted;"
  ```

  Expect one row with `gram_account_type = 'enterprise'` (the seed sets that last, so it doubles as a completeness check). No row, or a different account type, means the stack isn't (fully) seeded — run `mise run seed`, then proceed. Paths like `/speakeasy/ecommerce-api/insights` redirect to `/speakeasy/projects/ecommerce-api/insights`.

- Drive the browser exclusively through the **Playwright MCP tools**.
- `ffmpeg` is available at `./tools/ffmpeg` as a managed mise tool. Nothing needs installing.

## How recording works

Every Playwright MCP session records video (configured in `.playwright-mcp.config.json` at the repo root). The `.webm` is written to `.playwright-mcp/videos/` when the session ends via `browser_close` — video capture cannot be started or stopped mid-session. That shapes the whole workflow: **rehearse in one session, throw that video away, then do one clean choreographed take in a fresh session.**

Login flow (needed at most once — the browser profile persists across sessions, so auth carries over into the clean take): `browser_navigate` to the dashboard URL → redirected to `/login` → click the **Login** button → dev-idp auto-authenticates → lands on `/speakeasy`. If the very first load in a fresh browser profile comes up blank or broken, just navigate again — Chromium's cert verifier can re-initialize mid-flight on first run; subsequent loads are stable.

### 1. Rehearse

Log in, navigate to the feature, and explore with `browser_snapshot` until you know the exact click sequence and the elements involved. Then `browser_close`, and clear out the rehearsal footage so the clean take is unambiguous:

```bash
rm -f .playwright-mcp/videos/*.webm
```

### 2. Clean take

Open a fresh session (already logged in) and:

1. Navigate directly to the target page. The video records at 1920×1080, matching the default viewport 1:1, so don't `browser_resize` — a mismatched viewport gets letterboxed or scaled in the recording.
2. Prepare the frame (below).
3. Drive the interaction at a readable pace — pause with `browser_wait_for` (`time` param) between steps so each state change registers on screen. Keep the whole take ~10–20s.
4. `browser_close` to flush the video. The lone `.webm` in `.playwright-mcp/videos/` is your take.

If the take goes wrong, just close, delete the videos, and take it again — takes are cheap.

**Preparing the frame** (via `browser_evaluate`):

- **Hide the fixed docks** — the "Ask anything" and "Developer Toolkit" `position:fixed` divs clutter the frame. Find them by `textContent` and set `display: none`.
- **Inject a fake cursor** — recordings show no pointer, so overlay one:

```js
() => {
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
};
```

Both of these are DOM state — they vanish on navigation, so re-apply them if the demo crosses a page load (better: choreograph the demo within a single page).

**Before each click**, glide the fake cursor onto the target so the viewer's eye follows the action: call `browser_evaluate` with the target element's ref and

```js
(el) => {
  const r = el.getBoundingClientRect();
  const c = document.getElementById("__cursor");
  c.style.transform = `translate(${r.x + r.width / 2 - 10}px, ${r.y + r.height / 2 - 10}px)`;
  c.animate(
    [
      { boxShadow: "0 0 0 0 rgba(255,80,80,.6)" },
      { boxShadow: "0 0 0 14px rgba(255,80,80,0)" },
    ],
    { duration: 350, delay: 450 },
  );
};
```

then wait ~0.5s (`browser_wait_for`) for the glide to finish, then `browser_click` the element.

Gotchas:

- **Modal Radix layers set `pointer-events: none` on `<body>`**, so `browser_click` fails its actionability check. Fall back to `browser_run_code_unsafe` with a coordinate click: `const box = await page.locator(...).boundingBox(); await page.mouse.click(box.x + box.width / 2, box.y + box.height / 2);`
- **Targets below the fold**: scroll them into view (`el.scrollIntoView({block: "center"})` via `browser_evaluate`) _before_ moving the cursor or measuring positions.
- To debug a click that isn't landing, inject document-level capture listeners for `pointerdown/click` that `console.log` the actual target, and read them back with `browser_console_messages` — this reveals e.g. clicks resolving to `<html>` under a modal layer.

### 3. Convert to GIF

Work in the session scratchpad (never the repo tree). Trim the dead setup time at the start (`-ss`) and convert with a two-pass palette:

```bash
./tools/ffmpeg -ss <trim-seconds> -i .playwright-mcp/videos/<file>.webm \
  -vf "fps=10,scale=1200:-1:flags=lanczos,split[s0][s1];[s0]palettegen[p];[s1][p]paletteuse" \
  demo.gif
```

The 1200px width is a legibility/size compromise for the 1920px-wide recording — UI text shrinks to ~62% in the GIF. If the demo hinges on small text, either bump the scale toward 1440 (bigger GIF) or crop to the relevant region instead of scaling: `crop=w:h:x:y` before the `fps=` filter. Sanity-check the GIF is under ~10 MB so it loads quickly in the PR.

For the screenshots path there is no conversion — `browser_take_screenshot` writes PNGs into `.playwright-mcp/` directly.

### 4. Host in a secret gist

GitHub's PR comment-attachment upload has **no API**, so host the GIF (or PNGs — images render inline from any URL) in a secret gist's git repo:

```bash
echo "demo" > placeholder.md
gh gist create placeholder.md --desc "PR demo"     # secret is the default; there is NO --secret flag
git clone https://gist.github.com/<gist-id>.git gist && cd gist
cp ../demo.gif .
git add demo.gif && git commit -m "add demo gif"
git -c credential.helper='!gh auth git-credential' push   # without this, push hangs on a credential prompt
```

Gotchas:

- `gh gist create` **silently fails on binary files** — always create the gist from a placeholder text file, then push binaries via git.
- The raw URL format is `https://gist.githubusercontent.com/<user>/<gist-id>/raw/<file>.gif`.
- The gist raw URL is link-visible (like the PR itself) — fine for normal dashboard demos, but don't record anything sensitive.

### 5. Post the PR comment

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
