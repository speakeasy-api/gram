---
"dashboard": patch
"server": patch
---

Claude Code installs now use one settings snippet instead of CLI commands. Pick Just me (`~/.claude/settings.json`) or My organization (managed settings). The snippet registers the marketplace under its exact name with `autoUpdate` on and enables the plugin, so plugins stay current with no manual step. The generated marketplace READMEs match, and the hooks setup dialog no longer uses the unsupported `plugins.required` key.
