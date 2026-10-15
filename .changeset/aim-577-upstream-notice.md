---
"dashboard": patch
---

A server's User Identity panel now shows the provider configured on that server rather than every provider its organization sign-in binds, so a server no longer displays a sibling's OAuth client as its own. Turning User Identity off affects only the server you are editing and keeps a client the other servers still sign in through. The warning about other servers now names only the ones a change leaves without a sign-in, and says that each has to be configured again.
