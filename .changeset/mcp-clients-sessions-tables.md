---
"server": minor
"dashboard": minor
---

The MCP server Clients and Sessions tab now leads with active session and client counts, and renders both listings as searchable, filterable, sortable tables paginated ten rows at a time, with member avatars and creation dates on sessions. The clients table reports how many active sessions each client holds, backed by a new `active_session_count` field on the user session clients API, and clicking that count narrows both listings to that client behind a clear-filter bar.
