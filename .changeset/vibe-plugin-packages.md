---
"server": minor
"dashboard": minor
---

Mistral Vibe is now a supported agent for plugin distribution and observability. Feature plugins publish as portable Agent Plugins 1.0 packages at `<slug>-vibe/` in the marketplace repo, and every organization gets an `<org>-observability-vibe` hooks package — also downloadable as a ZIP via `plugins.downloadObservabilityPlugin?platform=vibe` — that Vibe runs automatically once copied into `~/.vibe/plugins/<org>-observability/`. The hooks relay speaks Vibe's `pre_tool`/`post_tool`/`post_agent` protocol and resolves MCP server URLs from Vibe's config and installed plugins, so Speakeasy-hosted servers are recognized instead of treated as shadow MCP. Install instructions for Vibe use `vibe mcp add <name> --url <url>` plus `/mcp login <name>` — the CLI writes the OAuth auth table that a hand-written config block lacks — in the plugins install dialog and the Platform MCP walkthrough, and publish status reports the real `vibe_observability_plugin` slug once the org's published hooks include the package. Hook coverage today is tool calls and turn end; prompt and session lifecycle hooks are a planned follow-up.
