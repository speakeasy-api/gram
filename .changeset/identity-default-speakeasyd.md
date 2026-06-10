---
"server": patch
---

Add `speakeasyd` to the default device-agent command list in the generated
observability plugin `identity.sh`. The daemon ships as `speakeasyd`, which was
absent from the previous default (`device-agent,speakeasy-device-agent`), so
identity enrichment was skipped and hook events reached Gram anonymously
(no `user_email`) on a standard install. The new default
(`speakeasyd,device-agent,speakeasy-device-agent`) applies to the Claude Code,
Cursor, and Codex plugin templates; legacy binary names are retained for
compatibility.
