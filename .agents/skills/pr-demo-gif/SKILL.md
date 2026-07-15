---
name: pr-demo-gif
description: Use when a pull request contains a user-visible Gram dashboard change that reviewers should see as a screenshot or short interaction demo.
---

# Demos for frontend PRs

Capture only the changed dashboard behavior and post it as a PR comment. Use one or two PNGs for a static visual change; use a 10–20 second GIF for an interaction.

**REQUIRED SUB-SKILL:** Use `gram-playwright-cli` for browser commands.

## Prerequisites

1. Run `mise run zero:summary`. Read the URL from the **Gram dashboard** row; do not assume a port. Start stopped dashboard or server processes with `madprocs`.
2. Use the `ecommerce-api` project. Verify the seed using the database URL from the **Database** row:

   ```bash
   psql "<database-url>" -c "SELECT p.slug, om.gram_account_type FROM projects p JOIN organization_metadata om ON om.id = p.organization_id WHERE p.slug = 'ecommerce-api' AND NOT p.deleted;"
   ```

   Expect one row with `gram_account_type = 'enterprise'`. Otherwise run `mise run seed` before continuing.

3. Invoke Playwright only through `mise run playwright`. The task uses the repo configuration and installs Chromium on demand.
4. Use `./tools/ffmpeg` for GIF conversion.

The login flow is credential-less: open the dashboard, click **Login** if redirected, and wait for `/speakeasy`. If the first load is blank after a fresh browser install, navigate to the URL again.

## 1. Rehearse

Use a named session so every command reaches the same browser:

```bash
mise run playwright -s=pr-demo open "<dashboard-url>"
mise run playwright -s=pr-demo snapshot
```

Navigate to the feature and rehearse the exact interaction using snapshot refs. Keep the browser open. Video recording starts only when requested, so rehearsal does not create footage.

Return to the intended starting state before capture. Hide an irrelevant fixed development dock with `eval` only if it obscures the changed behavior; page navigation removes DOM-only adjustments.

## 2. Capture

Create ignored artifact directories:

```bash
mkdir -p .playwright-cli/pr-demos /tmp/pr-demo
```

### Static change

Prepare the exact frame, then capture a viewport or element screenshot:

```bash
mise run playwright -s=pr-demo screenshot --hires --filename=.playwright-cli/pr-demos/demo.png
mise run playwright -s=pr-demo screenshot <element-ref> --hires --filename=.playwright-cli/pr-demos/demo-detail.png
```

The shared config keeps the CSS viewport at 1440×900 and uses a 2× device scale factor, so `--hires` produces a crisp 2880×1800 viewport image. Use `--full-page` only when the changed layout cannot fit in the viewport.

### Interaction change

Start recording only after the page is ready:

```bash
mise run playwright -s=pr-demo video-start .playwright-cli/pr-demos/demo.webm --size=1440x900
mise run playwright -s=pr-demo video-show-actions --duration=700 --position=top-right --cursor=pointer
```

Perform the rehearsed clicks, fills, and navigation with normal CLI commands. The action overlay supplies the pointer, target highlight, and action label; do not inject a fake cursor. Let each important state remain visible long enough to read. When a longer hold is needed:

```bash
mise run playwright -s=pr-demo run-code "async page => await page.waitForTimeout(1000)"
```

For a meaningful transition, optionally add a short chapter card:

```bash
mise run playwright -s=pr-demo video-chapter "<title>" --description="<what changes>" --duration=1200
```

Stop recording to flush the WebM, then close the session:

```bash
mise run playwright -s=pr-demo video-stop
mise run playwright -s=pr-demo close
```

If a take goes wrong, stop it, restore the starting state in a new session, and record again. Do not include setup, login, exploration, or unrelated page tours.

## 3. Convert and inspect

Convert interaction footage in the scratchpad with a two-pass palette:

```bash
./tools/ffmpeg -ss <trim-seconds> -i .playwright-cli/pr-demos/demo.webm \
  -vf "fps=10,scale=1200:-1:flags=lanczos,split[s0][s1];[s0]palettegen[p];[s1][p]paletteuse" \
  /tmp/pr-demo/demo.gif
```

Inspect the final PNG or GIF before publishing. Confirm it shows the changed behavior, contains no sensitive data, and the GIF is under roughly 10 MB. Increase the scale toward 1440 for small text, or crop to the relevant region before `fps=` instead of shrinking the whole frame.

## 4. Host in a secret gist

GitHub does not expose PR attachment upload through its API. Create a secret gist from a text placeholder, then push the binary through the gist repository:

```bash
cd /tmp/pr-demo
echo "demo" > placeholder.md
gh gist create placeholder.md --desc "PR demo"
git clone https://gist.github.com/<gist-id>.git gist
cp demo.gif gist/
cd gist
git add demo.gif
git commit -m "add demo gif"
git -c credential.helper='!gh auth git-credential' push
```

For screenshots, copy the PNG from `.playwright-cli/pr-demos/` instead. Secret is the default; `gh gist create` has no `--secret` flag. Never pass a binary directly to `gh gist create`, because it silently fails. The raw URL is:

```text
https://gist.githubusercontent.com/<user>/<gist-id>/raw/<file>
```

## 5. Post the PR comment

```bash
gh pr comment <pr> --body "$(cat <<'EOF'
### Demo

![demo](https://gist.githubusercontent.com/<user>/<gist-id>/raw/demo.gif)

What it shows:
1. <starting state>
2. <interaction>
3. <changed behavior>
EOF
)"
```

Keep the numbered list short and aligned with the visible steps.
