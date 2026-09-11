---
"server": patch
"dashboard": patch
---

Stop policing the shape of an AI scan target config dir. Any path the device agent can resolve is accepted, including one written with a trailing slash such as `~/Library/Application Support/com.openai.chat/`.
