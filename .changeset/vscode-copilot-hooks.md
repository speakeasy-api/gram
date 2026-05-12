---
"server": minor
"dashboard": minor
---

Add VSCode Copilot hooks observability and plugin distribution.

A new `/rpc/hooks.vscode` endpoint receives the eight VSCode Copilot
agent hook events (SessionStart, UserPromptSubmit, PreToolUse,
PostToolUse, PreCompact, SubagentStart, SubagentStop, Stop) with the
same `Gram-Key` + `Gram-Project` plugin-driven attribution model used
for Cursor. Risk-scan policies enforce on PreToolUse / UserPromptSubmit
via `hookSpecificOutput.permissionDecision`. Events land in ClickHouse
under `hook_source = "copilot"`.

Per-user attribution is sourced inside the hook script via a cascade —
`$GRAM_USER_EMAIL` (admin/MDM) → `gh api user/emails` (filtering
GitHub noreply addresses) → `git config user.email`. Both the email
and the source branch (`env`/`gh`/`git`/`none`) are forwarded as
`Gram-User-Email` and `Gram-User-Email-Source` headers so we can see
which fallback dominates in the wild.

Distribution is dashboard ZIP-download plus MDM rollout, not the
GitHub plugins repo: VSCode marketplaces clone via each user's local
git credentials, and Gram's published repo is private. The hooks
setup dialog gains a VSCode Copilot panel with a Local install tab
and an MDM rollout tab. The plugin is also added to the per-plugin
download dropdown for orgs that want VSCode-installable bundles of
their MCP servers.
