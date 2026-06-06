---
"server": patch
---

Strip leading harness/framing envelopes from chat messages before generating thread titles. A bounded allowlist (`<message-context>` from the assistant runtime, `<notification>` from Claude Code background tasks, `<dashboard_context>` from the dashboard's "Explore with AI") is removed from the leading text; the framing is needed by the runner for replay but is noise for title generation — left in, the title model fixated on the boilerplate and produced the same generic title for every project-assistant thread.
