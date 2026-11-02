---
"server": patch
---

Sign-in, discovery, token and MCP routes answer a temporary database or network failure with a retryable 503, not a 500. Broken queries and other routes are unchanged.
