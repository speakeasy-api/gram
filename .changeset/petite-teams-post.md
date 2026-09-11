---
"dashboard": minor
---

Setup's Enable logging step now explains that agent sessions are private by default — admins included — and offers to create a Session Auditor role carrying chat:read and add the admin to it, so the Confirm traffic step can see the conversation an Anthropic inference hook delivers. Once traffic is confirmed, the step offers to take the role back off them again. Directory-synced organizations create the role and map it from a directory group instead.
