---
"server": minor
"dashboard": minor
---

GitHub Copilot in VS Code can now report hook events to Speakeasy. The Copilot observability package ships a `.speakeasy/vscode-hooks.json` deployment manifest that the Speakeasy Device Agent turns into a `~/.copilot/hooks/agenthooks-vscode.json` registration, and the hooks relay understands VS Code's PascalCase events, including `SubagentStart` and `PreCompact`. The Copilot install instructions point VS Code users to the Device Agent.
