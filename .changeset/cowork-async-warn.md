---
"server": minor
---

Risk policies are enforced in Claude Cowork again. Inside the Cowork VM, `speakeasy-hooks` now sends prompt, tool and permission events to the server as it does in Claude Code, so block, warn and quarantine policies act in real time and the session is captured per event. The transcript shipped from Stop now backfills what no hook sees: the first prompt, typed in chat before the session moved to Cowork, and assistant text. Both paths key prompts and tool calls by the ids Claude reports to both (prompt id and tool call id), so each message is stored once whichever path writes first. When a transcript arrives, the server also checks its newly shipped prompts against warn policies and the binary has Claude tell the user in the same conversation, which covers typed prompts that Claude Code can only warn about at a tool call.
