---
"dashboard": patch
---

Add toast feedback to dashboard actions that previously saved, updated, or deleted state silently. Destructive and important actions across RBAC, security policies, org settings, MCP servers, prompts, toolsets, plugins, triggers, assistants, and environments now show a success confirmation, and the raw SDK calls and custom-`onError` mutations that bypassed the global error handler now surface failures. `handleError` accepts `unknown` so `catch` blocks can pass the caught value directly.
