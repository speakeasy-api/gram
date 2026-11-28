---
"server": patch
---

When Enterprise Managed Authorization fails for a person, a tool call's error now says what happened and who to ask, in the same words as the consent page: the company sign-in service refused, the service refused, company sign-in isn't set up correctly, the sign-in lacked permissions, the service gave fewer permissions, an answer couldn't be read, or a saved sign-in was turned down and needs reconnecting. It names the service, or says "the service" when it has no usable name, and points to the Speakeasy admin. Gateway members get the same text after the member's name. A gateway member whose saved connection needs reconnecting keeps its reconnect message.
