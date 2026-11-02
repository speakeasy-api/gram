---
"dashboard": patch
"server": patch
---

An agent key issued with "All servers" now reaches MCP servers added after the key was issued, instead of only the servers that existed at the time. The key stores the project wildcard together with any server its owner is blocked from, and that server stays hidden. Delegable grant discovery accepts `carry_exclusions` to return those exclusions beside the wildcard.
