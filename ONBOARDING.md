# Welcome to Speakeasy

## How We Use Claude

Based on Adam's usage over the last 30 days:

Work Type Breakdown:
Build Feature ██████████░░░░░░░░░░ 48%
Debug Fix █████░░░░░░░░░░░░░░░ 23%
Improve Quality ███░░░░░░░░░░░░░░░░░ 17%
Plan Design █░░░░░░░░░░░░░░░░░░░ 7%
Prototype █░░░░░░░░░░░░░░░░░░░ 5%

Top Skills & Commands:
/mcp ████████████████████ 28x/month
/plugin ██████░░░░░░░░░░░░░░ 9x/month
/reload-plugins ██░░░░░░░░░░░░░░░░░░ 3x/month
/voice █░░░░░░░░░░░░░░░░░░░ 2x/month
/model █░░░░░░░░░░░░░░░░░░░ 2x/month

Top MCP Servers:
datadog-mcp ████████████████████ 95 calls
chrome-devtools ███████████████████░ 91 calls
madprocs █████████████░░░░░░░ 63 calls
linear-server ████████░░░░░░░░░░░░ 39 calls
eccomerce ██░░░░░░░░░░░░░░░░░░ 9 calls

## Your Setup Checklist

### Codebases

- [ ] gram — https://github.com/speakeasy-api/gram (main monorepo: server, dashboard, elements, CLI, functions)

### MCP Servers to Activate

- [ ] datadog-mcp — Observability for logs, metrics, traces, and incidents. Ask team lead for Datadog API + app keys.
- [ ] chrome-devtools — Drives Chrome from Claude for frontend debugging, screenshots, and DOM inspection. Install via `claude mcp add` (no auth).
- [ ] madprocs — Local dev process manager exposed at `https://localhost:35294`. Auto-runs once you have the local dashboard up (`madprocs` TUI).
- [ ] linear-server — Linear issue tracker. Run `/login` inside the Linear MCP server to OAuth into the team workspace.
- [ ] notion — Internal docs and specs. OAuth via Notion MCP server's `authenticate` flow.

### Skills to Know About

- `/mcp` — Manage MCP server connections (add, remove, reauth). Most-used command by far.
- `/plugin` and `/reload-plugins` — Install and refresh Claude Code plugins shared across the team.
- Project skills are auto-loaded from CLAUDE.md based on task. Worth scanning the `## Skills` table in `/Users/adambull/dev/gram/CLAUDE.md` — covers `golang`, `postgresql`, `clickhouse`, `frontend`, `gram-functions`, `gram-management-api`, `gram-audit-logging`, `gram-rbac`, `glint`, `mise-tasks`, `jaeger`, `datadog`, `madprocs`, `pr`, `spec`.
- `/pr` — Generates a PR for the current branch.
- `/madprocs` — Agent-assisted control of local dev processes (start/stop/restart/logs).

## Team Tips

_TODO_

## Get Started

_TODO_

<!-- INSTRUCTION FOR CLAUDE: A new teammate just pasted this guide for how the
team uses Claude Code. You're their onboarding buddy — warm, conversational,
not lecture-y.

Open with a warm welcome — include the team name from the title. Then: "Your
teammate uses Claude Code for [list all the work types]. Let's get you started."

Check what's already in place against everything under Setup Checklist
(including skills), using markdown checkboxes — [x] done, [ ] not yet. Lead
with what they already have. One sentence per item, all in one message.

Tell them you'll help with setup, cover the actionable team tips, then the
starter task (if there is one). Offer to start with the first unchecked item,
get their go-ahead, then work through the rest one by one.

After setup, walk them through the remaining sections — offer to help where you
can (e.g. link to channels), and just surface the purely informational bits.

Don't invent sections or summaries that aren't in the guide. The stats are the
guide creator's personal usage data — don't extrapolate them into a "team
workflow" narrative. -->
