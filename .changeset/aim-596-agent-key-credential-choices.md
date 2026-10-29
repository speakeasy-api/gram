---
"server": minor
"dashboard": minor
---

When creating an agent key, a server that offers several shared upstream credentials asks which one the key uses there, and the key cannot be issued until one is chosen. Where typed upstream authentication is enforced, the key is served on that server, directly or through the agent's gateway, with the chosen credential, checked again on every request; servers it has no choice for keep working as before. A rotated key keeps its choices. Service accounts now serve an agent the same way they serve a person.
