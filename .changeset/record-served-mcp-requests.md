---
"server": patch
---

Record a served MCP request as a connected row at the request step, so connection health can tell when a caller who failed earlier is connected again. A connection is a named caller (a person, API key, agent or workload), a server or gateway member, and an app. Each server process writes at most one such row an hour per connection, so there can be several an hour across replicas and another after a restart; a failure for that caller on that server makes the next served request write again, at most once a minute. Nothing is written for anonymous callers, notifications, or a request or member call in which something failed. Served rows have their own per-organization budget and yield the write queue to failures. They count in the 30-minute attempt summaries table and in the `mcp.connection.steps` metric as connected request steps, and are left out of daily connection outcomes, provider health and customer log exports.
