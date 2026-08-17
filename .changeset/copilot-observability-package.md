---
"hooks": minor
---

Support GitHub Copilot as a fifth observability platform. Copilot hook events
are accepted on the unified ingest path — its native camelCase event names
resolve to the same canonical events the other platforms report, so Copilot
sessions show up in the same timelines, spend gates, and policy checks. The
dashboard offers a downloadable Copilot plugin package (root `plugin.json`,
`hooks/hooks.json` in Copilot's own dialect, and bash plus PowerShell
bootstrappers) with a hooks-scoped key already embedded, and a root
`marketplace.json` so Copilot installs that package rather than falling
through to the Claude one.

Hooks fire in Copilot CLI only. MCP servers and skills from the same plugin
load in VS Code and the Copilot app, but those surfaces never fire hooks, so
they report no telemetry.
