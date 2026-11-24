---
"server": patch
---

When an ID-JAG sign-in to a single MCP server is refused because the person has not connected their account for that server, the answer now names the server and links its connect page in `error_uri` and in the description. Connection logs now record that refusal as the person's connection being missing, for the person to fix, instead of as sign-in that an administrator has not set up.
