---
"server": minor
---

The MCP connection health API lists suspected problems for an MCP sign-in, and returns one problem's people, servers, messages and failed attempts. A problem is 2 or more people who cannot connect and failed the same way on one MCP server, one service or one app version, or one person failing for a day, counting only failures on that MCP sign-in's servers in the project. Only people who can read every log in the project see them. The demo organization and local seed now show a small set of them.
