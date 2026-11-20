---
"dashboard": minor
"server": minor
---

Add agent feedback for MCP servers. Turning it on from a server's new Agent Feedback tab adds one tool to the server, `submit_feedback` by default, that agents call to report what blocked or confused them. Nothing else needs configuring, and the tool's name and instructions can be changed. Reports are stored with secrets and personal data masked, and the tab shows 30-day counts by severity, the tools named most often, and every report. Reports can also be sent on: a new "MCP feedback" data export delivers each one as an OTLP log, and a new `mcp_feedback.submitted_v1` webhook event carries it to subscribers. It works for hosted, remote and tunneled servers.
