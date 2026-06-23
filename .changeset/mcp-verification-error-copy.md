---
"server": patch
---

fix(hooks): clearer message when an MCP tool call can't be verified. The deny reason now tells you to restart Claude or run /reload-plugins instead of suggesting the session is still initializing, and includes an error code so you can tell why the call couldn't be verified.
