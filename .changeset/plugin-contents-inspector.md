---
"server": minor
"dashboard": minor
---

Add a plugin contents inspector to the plugin detail page so users can preview the generated Claude Code, Cursor, and Codex package files without downloading. A new `getPluginPackageContents` endpoint returns the generated files with API keys substituted by a redacted placeholder; real keys are still only minted on download or publish.
