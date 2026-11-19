---
"server": patch
---

Slack workspace tokens, device integration credentials and network ingress OAuth clients can now be encrypted with the organization's own key. They follow the organization's integration-credentials transition: organizations stay on the platform key until enrolled, then write both copies, then read only the organization copy. A suspended or unreachable organization key makes syncs retry instead of asking admins to reconnect.
