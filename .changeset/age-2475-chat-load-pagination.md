---
"server": minor
"dashboard": patch
---

Chat loading is now paginated by generation, returning one generation per request. The chat detail panel fetches older generations in parallel until the full transcript is assembled, so long-running sessions no longer stall on the initial fetch.
