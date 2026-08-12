---
"server": minor
"dashboard": patch
---

chat.list accepts a `user_id` filter so callers with project-wide chat visibility can narrow results to a specific Gram user. The Project Assistant dock uses it, together with the dashboard source-kind filter, so "Continue chat" only offers sessions the viewer started from the dashboard.
