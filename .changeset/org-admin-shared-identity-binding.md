---
"server": patch
"dashboard": patch
---

An organization admin can now link or remove an organization-owned Remote Identity Provider client on an MCP server whose user session issuer is organization-level, from the server's own User Identity settings. Before, Save always failed there and pointed to organization settings that could not make the change either. People without organization admin still get the refusal, now as a permission error. After such a change, MCP servers in every project sharing the user session issuer pick up the new provider, not just servers in the project where it was saved.
