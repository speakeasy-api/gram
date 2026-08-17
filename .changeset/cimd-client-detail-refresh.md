---
"server": minor
"dashboard": minor
---

Add a client detail sheet for user-session clients with a CIMD metadata
refresh. The per-client view now exposes the metadata document's cache state
(source URL, last successful read, cache expiry, ETag), and a new
`userSessionClients.refreshCIMD` endpoint forces a re-read: it purges the
stored validators before fetching, so a host answering 304 Not Modified cannot
re-confirm the copy being discarded, and it carries a 30s per-client
server-side cooldown because purge-then-fetch deliberately bypasses the
document cache. The dashboard's Clients listing opens the sheet from each row;
DCR clients show the base detail without the CIMD panel.
