---
"server": patch
---

The upstream OAuth migration status no longer reports a gateway member as needing a client when its explicit configuration already settles it: exactly one of the gateway's clients is associated with the server, and every other client with grants for its upstream was detached from it. This covers servers that share one authorization server and were each moved to a dedicated client. Members with several associated clients, or with grants for a client never associated with them, still need configuration.
