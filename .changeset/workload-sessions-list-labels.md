---
"server": minor
"dashboard": minor
---

Label workload sessions on the MCP Sessions page. `userSessions.list` now returns a `workload` object for `workload:` subjects, with the workload issuer's name and URL, the external subject, the assigned agent and its state, and every admission (project or organization tier) currently letting the workload in. The dashboard marks these rows as workloads rather than people. The revoke dialog lists the controls that can stop a workload, from narrowest to widest, and shows which ones stop it from reconnecting. Revoking a workload session ends only that session: the workload can exchange a new token and reconnect.
