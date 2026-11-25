---
"server": patch
---

When Enterprise Managed Authorization fails for a person, the error a tool call gets now says what happened and who can fix it, in the same words as the consent page: the identity provider refused, the service refused, the setup isn't right, the sign-in lacked permissions, the service granted fewer permissions than needed, an answer couldn't be read, or a saved sign-in was turned down and needs reconnecting. It names the service, not the MCP server, and says "the service" when the service has no usable name. It used to say the connection was missing or expired. Gateway members get the same text after the member's name. A gateway member with a saved connection that needs reconnecting keeps its reconnect message and action.
