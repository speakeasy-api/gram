---
"server": patch
---

Published plugin repositories and the public Platform MCP marketplace no longer include Agent Plugins 1.0 manifests. Codex treats any package with an Agent Plugins `plugin.json` as an Agent Plugin and silently hides every MCP tool past a shared 64,000-byte budget. That left Platform MCP and other large plugins showing only a fraction of their tools. Copilot now installs feature plugins from a native package under `copilot-plugins/<plugin-slug>/`, and its marketplace entry names are unchanged. The Agent Plugins ZIP is still available as an explicit download.
