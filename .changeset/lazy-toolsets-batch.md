---
"server": patch
---

fix: batch toolsets.list queries to eliminate N+1. `toolsets.list` used to loop over every toolset in a project issuing 11+ DB round trips each (plus one more per external-MCP tool), making the endpoint take seconds for projects with many toolsets and slowing the dashboard home page, which prefetches it on every project route. Replaced with a single batched fetch across all toolsets, cutting round trips from `O(toolset_count)` to a fixed ~10 regardless of how many toolsets a project has.
