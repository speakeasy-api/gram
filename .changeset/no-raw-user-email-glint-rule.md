---
"server": patch
---

Add a glint rule (no-raw-user-email-filter) forbidding raw user_email
matching or bucketing in squirrel-built ClickHouse queries: the email
dimension must go through the canonical identity fold so one employee's
linked emails read as one identity. Known legacy sites carry explicit
nolint annotations that double as the GA cleanup checklist.
