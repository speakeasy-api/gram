---
"dashboard": patch
---

The MCP connection listings now carry a status dot — green live, amber expiring, red needs re-auth, grey idle or revoked — so a roster is scanned by colour rather than read row by row; healthy rows state Live or Idle where the status column used to be blank. The OAuth client on the other side of a connection is called an agent throughout, matching the rest of the product, and the organization page moves from `/user-sessions` to `/mcp-sessions` with the nav entry and title to match.
