---
"dashboard": minor
"server": minor
"hooks": minor
---

Capture Pi (pi.dev) sessions, and enforce policy on them, alongside the agents
Gram already observes. Pi has no hook configuration dialect to render into and
no MCP client of its own: its only integration point is a TypeScript extension
loaded into the Pi process. The observability package for Pi is therefore a
generated extension that forwards Pi's lifecycle events to the hooks relay over
NDJSON stdio, where the existing redaction, credential, and decision paths turn
them into canonical hook events. Prompts, tool calls and their results, per-turn
tokens and cost, and session start/end all land in the dashboard under the `pi`
source, and a policy deny blocks the prompt or tool call inside Pi rather than
being reported after the fact.

MCP for Pi comes from third-party extensions that bridge servers in as ordinary
Pi tools, which leaves tool names as the only signal that a call left the
machine. The relay reads the config those extensions share (`.pi/mcp.json`,
`~/.pi/agent/mcp.json`), reports the servers a workspace can reach as an
inventory snapshot at session start — with credentials redacted — and attributes
tool calls to the matching server so Pi traffic is visible to Shadow MCP rather
than appearing as unattributed local tools.

The package is downloadable per platform from the plugins page, ships in the
published marketplace repo for extraction into `~/.pi/agent/` or a repository's
`.pi/`, and `speakeasy-hooks install --provider=pi` renders the same extension
locally. Pi also joins the setup walkthrough and the shared agent-provider
catalog, so it carries its own name and mark everywhere a captured session's
source is shown.
