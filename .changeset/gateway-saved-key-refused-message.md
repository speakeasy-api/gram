---
"server": patch
---

When a gateway server refuses the credential an admin saved on it, people now get the same message agents get: an admin of the server's project has to update the credential. Before, people were told to connect the server, which could not fix it. A 403 no longer claims the credential is wrong, because the account behind it may only lack access.
