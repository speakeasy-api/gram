---
cwd: ../..
---

# Spike: Skill sync on Cowork (DNO-432)

Point-in-time findings for the RFC open question "verify the sync design on a real
Cowork workspace". Grounds each verification question in the current code and marks
what still needs a live probe. Remove or fold into [overview.md](./overview.md) once
skill distribution ships.

RFC: _Skills — Distribution via Plugins_.

## TL;DR

- **The Cowork distribution channel is claude.ai ↔ GitHub, not Gram's device-agent
  endpoint.** Cowork syncs a Gram-published plugin repo through its own GitHub App
  (claude.ai → Organization settings → Plugins → "Sync from GitHub"). `agent.getPlugins`
  (`server/design/agent/design.go`) is the developer-machine device-agent path and is
  **not** on the Cowork path.
- **Plugins and their hooks already load and run in Cowork today** for MCP servers +
  observability. A skill-bearing plugin would ride the same channel.
- **Skills are not yet emitted into the plugin package** (`server/internal/plugins/generate.go`
  produces no `skills/` or `SKILL.md`), so the skill-specific portions of Q1 and Q3
  are currently unexercised and need a probe once a skill is added to the package.
- **No live Cowork workspace is available in this environment**, so the items below
  flagged _LIVE_ are documented with an exact probe procedure rather than executed.

## Q1 — Does the org plugin (and its hooks) load in a Cowork workspace at all?

**Plugin + hooks: yes, verified in production for the existing package.**

- Cowork loads Gram-published plugins by syncing the GitHub repo Gram writes at publish
  time (`.claude-plugin/marketplace.json` + per-plugin dirs — see
  [package-format.md](./package-format.md)). The Cowork setup flow
  (`client/dashboard/src/pages/setup/setup-data.ts`, id `claude-cowork`) is "Add plugin →
  Sync from GitHub", and the observability plugin is marked **Required** so it is
  pre-installed org-wide.
- Hooks demonstrably execute at harness level in Cowork:
  `hooks/plugin-claude/hooks/send_mcp_inventory.sh` has a dedicated cowork branch that
  detects cmux's per-run config (`local_<rid>.json` → `remoteMcpServersConfig`), and
  the changelog records `fix: get stop hook working in cowork again`
  (`server/CHANGELOG.md`). This matches the ticket's note that "hooks run at harness
  level" is already probe-verified.

**Open (skill-specific), _LIVE_:** whether Cowork surfaces *plugin-provided skills*
(a `skills/` dir inside the plugin) the same way it surfaces MCP servers and hooks.
Not answerable from this repo because no skill is emitted into the package yet.
Probe: add a trivial skill to the generated Claude plugin, publish, sync into a Cowork
org, and confirm the skill appears in the workspace's skill list.

## Q2 — Is the sync endpoint reachable through Cowork's egress policy?

Separate the two things called "sync":

1. **Plugin/marketplace sync = claude.ai ↔ GitHub (Cowork's GitHub App).** Runs on
   Anthropic infrastructure, not from inside the workspace sandbox, so the workspace
   egress policy does **not** gate it. Already working for MCP/observability plugins.
2. **Runtime hook egress = hook scripts POST to `https://app.getgram.ai/rpc/hooks.claude`
   from inside the workspace.** This *is* subject to Cowork's sandbox egress. The fact
   that MCP-inventory and stop hooks work in Cowork indicates `app.getgram.ai` is
   reachable from the workspace today. Hooks are fire-and-forget with debug tracing
   (`GRAM_HOOKS_DEBUG=1` surfaces `could not reach …` vs `server returned HTTP …`), so
   an egress block degrades to silent no-op rather than a broken session.

**Implication for skill sync:** if skills distribute as *files in the plugin* (the
GitHub-marketplace channel), they inherit path (1) and add **no new egress surface** —
nothing new to allowlist. Only if the RFC design pulls skills at runtime from a *new*
Gram endpoint would a new host need to be added to Cowork's egress allowlist and probed.
Recommendation: reuse the files-in-plugin channel.

**To confirm, _LIVE_:** whichever endpoint the final design uses, run it from a Cowork
workspace with `GRAM_HOOKS_DEBUG=1` and confirm a 2xx (not `000`).

## Q3 — Where does the workspace's skill discovery read from (config-dir resolution)?

This is Claude Code / Cowork harness behavior, **not** Gram code, so it cannot be
settled from this repo. What we know from the Cowork hook branch: in Cowork
(cmux remote runner) `CLAUDE_PROJECT_DIR` is `.../local_<rid>/outputs`, and the per-run
config lives one level up at `.../local_<rid>.json`.

**_LIVE_:** confirm whether Cowork mounts plugin-provided skills into the same skill
search path Claude Code uses (plugin dir + `.claude/skills/` under the resolved config
dir). Probe: publish a plugin carrying a marker skill, sync it into a live Cowork org,
and check whether the skill is discovered — and from which directory — inside the
workspace.

## Degraded-mode coverage

The realities observed here are already covered by existing degraded-mode handling, so
no new follow-up ticket is warranted on those grounds:

- Hook egress failure → fire-and-forget, always `exit 0`, debug-traced.
- Missing/late per-run config → falls back to the most recent sibling `local_*.json`.
- No env hook credentials → falls back to cached browser-login creds, then to
  unauthenticated OTEL session attribution.

**Follow-up tickets should be opened only for:** (a) adding a skill to the generated
plugin package so Q1/Q3 can be probed, and (b) the two _LIVE_ probes above — these
require a real Cowork org and could not be executed in this environment.
