---
name: pr-demo-gif
description: >-
  Use when a pull request proposes user-visible dashboard changes and needs a
  demo posted to it, such as a screenshot, a screen recording, a demo GIF, or a
  PR comment showing the change. Covers capturing the dashboard with Playwright
  and uploading media with `gh pr comment --attach`. Triggers: "add a demo to
  the PR", "record a GIF", "screenshot this change", "show this in the PR",
  "unknown flag: --attach", HTTP 404 from `gh pr comment --attach`.
---

# Demos for frontend PRs

Capture only the changed dashboard behavior and post it as a PR comment. Use one or two PNGs for a static visual change; use a 10-20 second WebM recording for an interaction.

**REQUIRED SUB-SKILL:** Use `gram-playwright-cli` for browser commands.

**Uploads cannot be undone.** `gh` attachments have no delete endpoint, and deleting the comment does not unpublish the asset: it stays live at an unauthenticated URL. The inspect step in section 3 is the last reversible moment in this workflow.

| Change type                                 | Capture                      | Publish as                  |
| ------------------------------------------- | ---------------------------- | --------------------------- |
| Static visual                               | `screenshot --hires`         | PNG, embedded with alt text |
| Interaction                                 | `video-start` / `video-stop` | WebM, rendered as a player  |
| Interaction needing inline email visibility | WebM, then ffmpeg            | GIF, embedded with alt text |

## Prerequisites

- Discover the dashboard URL with `mise run zero:summary` — read the address from the **Gram dashboard** row (don't assume a port). The same table shows whether each service is RUNNING. A paused worktree shows **Database** and **ClickHouse** as STOPPED: run `mise run wake` (containers, then daemons) and expect two to four minutes. Do not use `mise start` here, which launches the foreground process-manager TUI and starts only the daemons, leaving the server wedged against a stopped Postgres with no diagnostic. Dev-idp auto-login is enabled, and the local TLS cert is browser-trusted (mkcert CA in the NSS store, set up by `mise run zero:tls` — rerun that if you see cert errors).
- Use the **`default`** project for all flows — `mise run seed` (the demo seed retargeted at your dev org) provisions exactly one project. Before recording, verify the database is seeded by probing it directly (the connection string is in the **Database** row of `zero:summary`; drop its `&search_path=public` parameter, which `psql` rejects with `invalid URI query parameter`):

  ```bash
  psql "postgres://gram:gram@127.0.0.1:<port>/gram?sslmode=disable" -c "SELECT p.slug, om.gram_account_type FROM projects p JOIN organization_metadata om ON om.id = p.organization_id WHERE p.slug = 'default' AND NOT p.deleted;"
  ```

  Expect at least one row, every row reading `gram_account_type = 'enterprise'`. Two rows is normal, because your dev org and the demo org each own a `default` project. Run `mise run seed` only on zero rows or a non-enterprise tier.

- Invoke Playwright only through `mise run playwright`. The task uses the repo configuration and installs Chromium on demand.
- Use `./tools/ffmpeg` for any conversion or frame extraction.

The login flow is credential-less: open the dashboard, click **Login** if redirected, and wait for `/speakeasy`. If the first load is blank after a fresh browser install, navigate to the URL again.

## 0. Namespace everything by PR number

Concurrent agent sessions share a worktree and share the Playwright browser. Key the artifact directory **and** the Playwright session name on the PR number, so two sessions cannot overwrite each other's files or drive each other's browser:

```bash
PR=<pr-number>
DIR=.playwright-cli/pr-demos/$PR
mkdir -p "$DIR"
```

Use `-s=pr-demo-$PR` as the session flag in every Playwright command below. If your tooling runs each command in a fresh shell, these variables do not survive between calls: re-export them in every command, or write the literal paths. `.playwright-cli/` is gitignored at any depth. Relative paths resolve against your current directory, so stay at the repo root for the whole workflow: a path that resolves differently at capture time and at upload time silently breaks the body rewrite in section 4.

## 1. Rehearse

```bash
mise run playwright -s=pr-demo-$PR open "<dashboard-url>"
mise run playwright -s=pr-demo-$PR snapshot
```

Navigate to the feature and rehearse the exact interaction using snapshot refs. Keep the browser open. Video recording starts only when requested, so rehearsal does not create footage.

Return to the intended starting state before capture. Hide the fixed development dock with `eval` before every capture, whether or not it obscures the change: it is dev-only chrome and reads as a product bug to a reviewer. Page navigation removes DOM-only adjustments, so re-apply it after navigating.

## 2. Capture

### Static change

Prepare the exact frame, then capture a viewport or element screenshot:

```bash
mise run playwright -s=pr-demo-$PR screenshot --hires --filename="$DIR/demo.png"
mise run playwright -s=pr-demo-$PR screenshot <element-ref> --hires --filename="$DIR/demo-detail.png"
```

The shared config keeps the CSS viewport at 1440x900 and uses a 2x device scale factor, so `--hires` produces a crisp 2880x1800 viewport image. Use `--full-page` only when the changed layout cannot fit in the viewport.

### Interaction change

Start recording only after the page is ready:

```bash
mise run playwright -s=pr-demo-$PR video-start "$DIR/demo.webm" --size=1440x900
mise run playwright -s=pr-demo-$PR video-show-actions --duration=700 --position=top-right --cursor=pointer
```

Perform the rehearsed clicks, fills, and navigation with normal CLI commands. The action overlay supplies the pointer, target highlight, and action label; do not inject a fake cursor. Let each important state remain visible long enough to read. When a longer hold is needed:

```bash
mise run playwright -s=pr-demo-$PR run-code "async page => await page.waitForTimeout(1000)"
```

For a meaningful transition, optionally add a short chapter card:

```bash
mise run playwright -s=pr-demo-$PR video-chapter "<title>" --description="<what changes>" --duration=1200
```

Stop recording to flush the WebM, then close the session:

```bash
mise run playwright -s=pr-demo-$PR video-stop
mise run playwright -s=pr-demo-$PR close
```

Every `mise run playwright` subcommand costs one to two seconds of process startup, and all of it lands in the recording as dead air. Budget for it: a take with six commands and six seconds of deliberate holds runs near thirty seconds. Trim the lead-in and tail before uploading:

```bash
./tools/ffmpeg -y -ss <start> -to <end> -i "$DIR/demo.webm" \
  -c:v libvpx -b:v 1M -crf 30 -an "$DIR/demo-trimmed.webm"
```

The trimmed file is the one you publish, so it is the one section 3 applies to. GitHub serves `.webm` back as `video/webm` and renders it as a player, so no further conversion is needed. If a take goes wrong, stop it, restore the starting state in a new session, and record again. Do not include setup, login, exploration, or unrelated page tours.

### GIF fallback

Convert only when the reviewer needs the demo inline where a player will not render, notably GitHub notification emails. Two-pass palette, output beside the WebM:

```bash
./tools/ffmpeg -ss <trim-seconds> -i "$DIR/demo.webm" \
  -vf "fps=10,scale=1200:-1:flags=lanczos,split[s0][s1];[s0]palettegen[p];[s1][p]paletteuse" \
  "$DIR/demo.gif"
```

A GIF is hard-refused over 10 MB. Increase the scale toward 1440 for small text, or crop to the relevant region before `fps=` instead of shrinking the whole frame.

## 3. Inspect before uploading

You cannot watch a WebM. Extract frames first, then look at them:

```bash
./tools/ffmpeg -i "$DIR/demo-trimmed.webm" -vf "fps=1/3,scale=1200:-1" "$DIR/frame-%02d.png"
```

Aim for eight to ten frames; raise the interval on a longer clip, because reading twenty near-identical frames is the slowest step in this workflow. Read every extracted frame, or the PNG or GIF for the other paths, and confirm all of the following before running any `--attach` command:

1. It shows the changed behavior, and nothing before or after it.
2. No real identity: the user menu, avatar, and member columns show no real name or email. Dev-idp signs you in as `dev@example.com` in `Local Dev Org`, so this is normally satisfied already. If your stack points at a real identity provider and the sidebar shows your actual name, collapse the sidebar and recapture rather than editing the DOM.
3. No **customer** organization: check the org switcher, breadcrumbs, and URL bar. Your own dev org and Speakeasy's own name are fine; a customer's is not. `mise run seed` retargets the demo seed at **your own org**, so the rows are synthetic but the chrome around them is not.
4. No secrets: no API keys, tokens, or environment variable values in view.
5. Within limits: images and GIFs under 10 MB, video under 100 MB.

A recording crosses far more of these surfaces than a deliberately framed screenshot. Recapture rather than publishing a frame you are unsure about.

## 4. Post the PR comment

Two environment gotchas, both of which produce misleading errors:

- Prefix with `env -u GH_TOKEN -u GITHUB_TOKEN`. An environment token wins over your keyring credential, and Actions' `GITHUB_TOKEN` is not allowed to upload attachments.
- Invoke gh through `mise exec` so you get the pinned 2.100.0. A shell without mise active falls through to a system gh, and an older one fails with `unknown flag: --attach`.

Write the body to a file so the same shell variable supplies the path in both the markdown and the flag. `gh` rewrites a body reference in place only when the two paths match **byte for byte**; any mismatch silently appends the image below your text instead, and still exits 0.

```bash
cat > "$DIR/comment.md" <<EOF
### Demo

![<alt text>]($DEMO)

What it shows:
1. <starting state>
2. <interaction>
3. <changed behavior>
EOF

env -u GH_TOKEN -u GITHUB_TOKEN mise exec -- gh pr comment "$PR" \
  --body-file "$DIR/comment.md" --attach "$DEMO#<alt text>"
```

Set `DEMO="$DIR/demo.png"` or `DEMO="$DIR/demo.gif"` first. The heredoc is intentionally unquoted so `$DEMO` expands; escape any literal `` ` `` or `$` in your text. A path containing `#` cannot be attached, because `#` delimits the alt text.

**Video differs.** A video renders as a bare URL on its own line and cannot carry alt text, so passing `#<alt text>` is a hard error. Attach it with no alt text and let it append below your body:

```bash
env -u GH_TOKEN -u GITHUB_TOKEN mise exec -- gh pr comment "$PR" \
  --body-file "$DIR/comment.md" --attach "$DIR/demo-trimmed.webm"
```

Write that body with the numbered list above the player, and no image reference.

Keep the numbered list short and aligned with the visible steps.

## Common mistakes

- **`HTTP 404` from `--attach`** means you lack write access, not that the PR is missing. Attachments need ADMIN, MAINTAIN, or WRITE; READ and TRIAGE both 404. Fine-grained PATs are per-repo, so one minted elsewhere fails on a repo you can otherwise push to. From a fork, fall back to a secret gist: write any text file into `$DIR`, run `gh gist create` on it (binaries passed directly are silently dropped), clone the returned gist repo, copy `$DEMO` in, commit, push with `git -c credential.helper='!gh auth git-credential' push`, then reference the raw URL.
- **Uploads stop at the first failure** and the body is written only if at least one file uploaded, so a partial failure posts a comment containing unresolved local paths. Attach one file at a time unless you need them in a single comment.
- Attaching the demo to the PR body with `gh pr edit` instead of a comment. Use a comment, so the demo sits in the timeline next to the change it describes.

### Verified refusals

`gh` validates every attachment locally before it resolves the repository, so these all fail without uploading or posting anything:

| Command                         | Error                                                                                                |
| ------------------------------- | ---------------------------------------------------------------------------------------------------- |
| `--attach demo.webm#alt`        | `cannot set alt text on video`                                                                       |
| `--attach missing.png`          | `missing.png: no such file or directory`                                                             |
| `--attach empty.png`            | `empty.png is empty`                                                                                 |
| `--attach some-dir`             | `some-dir is a directory`                                                                            |
| `--attach notes.txt`            | `notes.txt is not a supported file type (supported: png, jpg, jpeg, gif, webp, svg, mp4, mov, webm)` |
| `--attach a.png --attach a.png` | `a.png and a.png are the same file; attached files must be unique`                                   |

Duplicate detection also catches a symlink or hard link to an already-attached file.
