---
"server": patch
"dashboard": patch
---

Approval request DMs in Slack get Approve once and Deny buttons only when `SLACK_SIGNING_SECRET` is set on the worker as well as the server. Without it, nothing can verify a click, so each DM links to the request in Speakeasy instead. When every attempt to send the DMs fails, including a last attempt that times out, the DMs that did go out still show the outcome once the request is decided or expires. The Require approval badge on the Guardrails start screen is now blue, as in the policy list. Behind the `risk-tool-approvals` flag.
