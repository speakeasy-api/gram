---
"server": patch
---

An MCP server added from a URL through the Platform MCP without a display name is now named after the name the server reports for itself, or the URL's host name when it reports none, instead of the full URL. A new server's slug and endpoint address now come from that name, for example `linear` and `<organization>-linear`, with a short random suffix only when that address is already taken. A name given when adding the server still wins, and servers that already exist keep their names and addresses.
