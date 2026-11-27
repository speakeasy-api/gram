---
"function-runners": minor
---

The function runner accepts a `timeout_seconds` field on tool calls and resource reads and stops the invocation when it runs out, capped at 10 minutes. Requests without the field keep the 5-minute limit. A call that runs out of time now returns a 408 that names its limit, instead of a generic 500.
