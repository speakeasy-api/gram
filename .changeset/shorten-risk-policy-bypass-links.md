---
"server": patch
---

Shorten risk policy bypass ("Request access") links. The blocked-tool-call message now embeds a short cache-backed `rpbr2.<id>` token instead of a 1000+ char encrypted blob in the URL fragment. Links already issued in the legacy `rpbr1` format keep working until they expire.
