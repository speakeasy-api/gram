---
"server": minor
"dashboard": minor
---

When an MCP server's identity cannot be saved because another server signs in through the client on their shared User Identity issuer, Settings > Identity now offers to create a new issuer for that server and save the change there. The dialog says what the move costs before you agree: everyone connected to the server connects again, and the gateways named in it stop offering the server. The new issuer keeps the old one's session duration, challenge mode and CIMD admission settings, and is always the server's own authorization server. API callers opt in with `dedicated_issuer` on `remoteSessions.commitServerIdentityConfiguration`; without it the save is refused as before.
