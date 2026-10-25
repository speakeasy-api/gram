---
"dashboard": patch
---

A gateway's Debug page, its sidebar entry and its Command K action are now behind the `gateway-debug` rollout flag. They stay hidden while the flag is loading, off, missing or failing, and project read access is still required. The session APIs and Platform MCP tools are unchanged.
