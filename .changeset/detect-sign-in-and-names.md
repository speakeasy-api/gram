---
"server": minor
"dashboard": minor
---

Adding a credential for people's own accounts now detects the authorization server the MCP server advertises. When the project already has it, it is preselected; when it does not, it is added when the credential is saved, so there is no trip to Authorization Servers first. New MCP servers, credentials and authorization servers also get a readable name when none is typed: the name the server reports for itself when it can be reached without signing in, or one read from its address, such as "Canva" for mcp.canva.com.
