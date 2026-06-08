---
"dashboard": patch
---

Redesign the Overview tab of the experimental MCP server details page (`/x/mcp` route) into status-driven cards. Server Address, Authentication, and Source/Tools each render as a consistent card with a Ready / Needs Setup signal: the Server Address card shows the connect URL plus the shareable `/install` page URL, and the Authentication card derives an explicit posture (Gram-only, Gram + remote, remote-only, or open to anyone) so a public server with no remote identity is correctly flagged as unsecured. Adds an early-access banner and an "Enhance your server" section, and wires the install-page Customize action to the Settings tab.
