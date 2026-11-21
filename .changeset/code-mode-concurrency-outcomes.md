---
"server": patch
---

Record every Code Mode tool call when a program stops at the eight-helper concurrency limit. Calls already sent now appear in `tool_calls` with their own outcome instead of disappearing, and the error says the call over the limit was not dispatched.
