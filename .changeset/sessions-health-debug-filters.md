---
"server": minor
"dashboard": minor
---

Gateway Debug can filter sessions by identity, identity type, sign-in server, client, client registration and last use. One search bar combines free text and filter chips, with gateway-scoped facet counts in the sidebar. A new `userSessions.queryFacets` endpoint returns those counts and reports sessions on the gateway's sign-in server with no recorded origin. The session list and the assistant platform tool `platform_list_user_sessions` accept the same filters.
