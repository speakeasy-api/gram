---
"server": patch
---

When a gateway server refuses the credential configured for it in Speakeasy and the person has nothing to connect, the tool call now tells them to ask an admin to update it instead of asking them to connect. When the person can connect the server, the message asks them to connect first and to ask an admin to check the credential if the call still fails. A 403 no longer claims the credential is wrong, because its account may only lack access.
