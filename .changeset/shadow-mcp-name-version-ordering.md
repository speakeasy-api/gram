---
"server": patch
---

Shadow MCP inventory server names now resolve reliably after renames. Name updates written in quick succession could previously tie on their stored version and intermittently revert to an older observed name; versions are now stored at full nanosecond precision and each update is guaranteed to supersede the state it was based on.
