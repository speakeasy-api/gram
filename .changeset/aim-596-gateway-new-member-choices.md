---
"server": patch
---

Where typed upstream authentication is enforced, a server added to a gateway after someone connected works for them without reconnecting when nothing is left to choose: it needs no upstream authentication, or it offers exactly one credential they may use that is ready, such as a shared credential or their already-connected account. The choice is recorded once. A new server with several ready credentials, or one needing a connection, asks them to reconnect and choose; a server present when they connected is not treated as new.
