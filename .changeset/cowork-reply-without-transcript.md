---
"server": patch
---

Claude Cowork replies are captured again in sessions that write no transcript. Some Cowork sessions run Claude Code only as a tool server, so the Stop hook has no transcript to upload. The hooks binary now sends the reply from Stop as an event instead, and the server stores it with the session's other messages, once.
