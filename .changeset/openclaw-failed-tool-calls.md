---
"server": patch
"dashboard": patch
---

Failed OpenClaw gateway calls no longer stay "Pending" in Tool logs. OpenClaw's Codex harness sends an after-call hook for an MCP tool only when the call succeeds, so a failed call reported its start and nothing else. The OpenClaw observability plugin now reports, at the end of each run, every call still waiting for its result from the run's transcript: a failed call as a failure with its error, a successful one with its output. A call with no result in the transcript is not reported. Reinstall the plugin to get this. In Tool logs, a hook call with no result reads "Pending" for 10 minutes and "Unknown" after that, so calls from older plugins no longer look like they are still running.
