---
"server": patch
---

Strip `<message-context>` source-adapter framing from chat messages before generating thread titles. The framing (EventID/UserID lines, MCP auth events) is needed by the runner for replay but is noise for title generation — left in, the title model fixated on the boilerplate and produced the same generic title for every project-assistant thread.
