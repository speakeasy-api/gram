---
"server": minor
---

Agent API keys can now send hook events when the agent holds the `org:hooks_ingest` grant. Events are attributed to the agent itself: a self-reported `user_email` or cached session identity never re-attributes them to a person, and agent sessions are stored without requiring an email. The OTEL and LiteLLM ingestion endpoints continue to reject agent keys.
