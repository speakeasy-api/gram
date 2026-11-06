---
"server": patch
---

Signing out everywhere now revokes chat tokens derived from earlier dashboard sessions, including access to chat attachments and creation of signed attachment URLs. New sessions remain valid, and API-key and anonymous chat tokens keep their existing behavior. Session revocation lookup failures return a server error instead of an invalid-credential response.
