---
"server": patch
---

Record a served MCP request for a named caller, at most once an hour per person, server or gateway member, and app, so connection health can tell when a person who failed earlier is connected again. A recorded request failure reopens the hour, nothing is written for a request in which anything failed, and served rows have their own per-organization budget and yield the write queue to failures. Daily connection outcomes, provider health and customer log exports leave served requests out.
