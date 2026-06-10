---
"server": patch
---

Assistants now connect to all of their MCP servers in parallel when a thread
starts, so startup time no longer grows with the number of servers and one
slow or unreachable server cannot stall the rest. Connection attempts are
bounded by connect and handshake timeouts, so a hung server fails fast instead
of blocking the assistant.
