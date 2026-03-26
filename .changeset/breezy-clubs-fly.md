---
"server": patch
---

Strip tools from toolset audit log snapshots

The Tools field on Toolset can be very large. Cloning the before/after snapshots and nilling out Tools avoids serializing this data into audit log entries where it is not needed.
