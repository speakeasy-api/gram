---
"server": patch
---

When a gateway server rejects the credential configured for it in Speakeasy and the person has nothing to connect, the tool call now tells them to ask an admin to update it instead of asking them to connect. When the person can connect the server, the message asks them to connect first and to ask an admin to check the credential if the call still fails. A 403 that does not reject the credential now says the credential's account may not be allowed to do this, instead of claiming the credential was refused. The message follows what the rejected request actually carried, so a credential stripped before it reached the server keeps the earlier message.
