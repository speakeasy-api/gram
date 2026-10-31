---
"server": patch
---

The agent install script's hooks self-test no longer leaves an empty "New Chat" session and a tool call under every new agent. The hooks API still checks the key and its permission to send hooks, then answers the test without recording it.
