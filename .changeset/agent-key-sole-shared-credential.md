---
"dashboard": patch
---

Issuing an agent key now asks which shared credential it uses on a server whose only way in is a single shared credential, instead of issuing a key that server never authenticates. A server that also offers accounts keeps using the agent's connected account unless a shared credential is picked.
