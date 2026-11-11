---
"server": minor
---

Risk policies are enforced in Claude Cowork again. Inside the Cowork VM, `speakeasy-hooks` now sends prompt, tool and permission events to the server as it does in Claude Code, so block, warn and quarantine policies act in real time. Those events carry a new `transcript_capture` flag, and the server stores no conversation rows from them: the transcript shipped from Stop stays the only writer of the session's chat history, so no message is stored twice. When that transcript arrives, the server also checks the newly shipped prompts against warn policies and returns any matches, and the binary has Claude tell the user in the same conversation. This covers typed prompts, which Claude Code can only warn about at a tool call.
