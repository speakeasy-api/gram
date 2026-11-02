---
"dashboard": patch
"server": patch
---

New agents no longer ask for sign-in to every auth client, which most roles may not give. An agent that reaches servers can now sign in with its owner's own connections and with the shared credential each chosen server uses. When the creator may not give one of those credentials, the wizard names it before anything is created. Saving an edited agent adds only the sign-in it lacks.

A person blocked from one shared credential can now give an agent sign-in with their own connections. The block still stops them from giving that credential. The agent permissions panel names each sign-in grant instead of showing "All resources" or a credential id.
