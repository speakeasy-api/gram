---
"@gram-ai/elements": patch
---

Refactored the Elements codebase to remove hard-coded references to Gram projects and MCP servers. Some of this hard-coding affected Storybook but there were instances where the session manager was pinned to a project called `default` that was also resolved.
